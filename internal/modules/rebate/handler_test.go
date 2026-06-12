package rebate_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"b2bcommerce/internal/auth"
	"b2bcommerce/internal/server"
	"b2bcommerce/internal/store"
	"b2bcommerce/internal/store/gen"
	"b2bcommerce/internal/testsupport"
)

const testSecret = "test-secret-please-change"

func newServer(t *testing.T) (http.Handler, *auth.Issuer, *pgxpool.Pool) {
	t.Helper()
	pool := testsupport.NewDB(t)
	return server.New(store.New(pool), auth.NewIssuer(testSecret, time.Hour)), auth.NewIssuer(testSecret, time.Hour), pool
}

func do(t *testing.T, h http.Handler, method, path, tok string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(method, path, &buf)
	if tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	return rr
}

func TestRebateReportSettleAndStatement(t *testing.T) {
	h, issuer, pool := newServer(t)
	q := gen.New(pool)
	ctx := context.Background()
	adminTok, _ := issuer.Issue("1", 1, "admin", []string{"rebate.view", "rebate.manage"})

	// Customer + buyer user (for the storefront statement token).
	cust, _ := q.CreateCustomer(ctx, gen.CreateCustomerParams{OrganizationID: 1, Name: "Acme", CreditLimit: "0"})
	hash, _ := auth.HashPassword("pw-123456")
	u, _ := q.CreateCustomerUser(ctx, gen.CreateCustomerUserParams{CustomerID: cust.ID, Email: "buyer@acme.test", PasswordHash: hash, FullName: "Acme", Role: "admin"})
	custTok, _ := issuer.IssueStorefront(u.ID, 1, cust.ID)

	// Two qualifying orders this period: 30,000 + 30,000 = 60,000 USD.
	for i := 0; i < 2; i++ {
		if _, err := q.CreateOrder(ctx, gen.CreateOrderParams{
			OrganizationID: 1, WebsiteID: 1, CustomerID: cust.ID, Currency: "USD",
			BillingAddress: []byte("{}"), ShippingAddress: []byte("{}"),
			Subtotal: "30000", TaxTotal: "0", ShippingTotal: "0", GrandTotal: "30000",
		}); err != nil {
			t.Fatalf("order: %v", err)
		}
	}

	// Program scoped to this customer + two tiers (10k→2%, 50k→5%).
	cr := do(t, h, http.MethodPost, "/admin/rebates", adminTok, map[string]any{
		"name": "Q-rebate", "period": "quarterly", "currency": "USD", "customer_id": cust.ID,
	})
	if cr.Code != http.StatusCreated {
		t.Fatalf("create program: %d (%s)", cr.Code, cr.Body.String())
	}
	var prog struct {
		ID int64 `json:"id"`
	}
	_ = json.Unmarshal(cr.Body.Bytes(), &prog)
	base := "/admin/rebates/" + strconv.FormatInt(prog.ID, 10)
	for _, tier := range []map[string]any{{"min_amount": "10000", "rate_percent": "2"}, {"min_amount": "50000", "rate_percent": "5"}} {
		if rr := do(t, h, http.MethodPost, base+"/tiers", adminTok, tier); rr.Code != http.StatusCreated {
			t.Fatalf("tier: %d (%s)", rr.Code, rr.Body.String())
		}
	}

	// Report: 60,000 qualifies for the 5% tier → 3,000 rebate.
	rep := do(t, h, http.MethodGet, base+"/report", adminTok, nil)
	var report struct {
		Rows []struct {
			CustomerID      int64  `json:"customer_id"`
			QualifyingTotal string `json:"qualifying_total"`
			RatePercent     string `json:"rate_percent"`
			RebateAmount    string `json:"rebate_amount"`
		} `json:"rows"`
	}
	_ = json.Unmarshal(rep.Body.Bytes(), &report)
	if len(report.Rows) != 1 || report.Rows[0].QualifyingTotal != "60000.0000" || report.Rows[0].RatePercent != "5.0000" || report.Rows[0].RebateAmount != "3000.0000" {
		t.Fatalf("report wrong: %+v (%s)", report.Rows, rep.Body.String())
	}

	// Settle: 1 settlement, 3,000 total; a credit note is issued.
	st := do(t, h, http.MethodPost, base+"/settle", adminTok, map[string]any{})
	var res struct {
		Settled     int    `json:"settled"`
		TotalRebate string `json:"total_rebate"`
	}
	_ = json.Unmarshal(st.Body.Bytes(), &res)
	if res.Settled != 1 || res.TotalRebate != "3000.0000" {
		t.Fatalf("settle: want 1 / 3000.0000, got %d / %s (%s)", res.Settled, res.TotalRebate, st.Body.String())
	}
	var cnCount, settleCount int
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM credit_notes WHERE customer_id=$1 AND amount='3000.0000'`, cust.ID).Scan(&cnCount)
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM rebate_settlements WHERE customer_id=$1`, cust.ID).Scan(&settleCount)
	if cnCount != 1 || settleCount != 1 {
		t.Fatalf("settlement effects: credit_notes=%d settlements=%d (want 1/1)", cnCount, settleCount)
	}

	// Re-settle the same period is idempotent (skips the already-settled customer).
	st2 := do(t, h, http.MethodPost, base+"/settle", adminTok, map[string]any{})
	var res2 struct {
		Settled, Skipped int
	}
	_ = json.Unmarshal(st2.Body.Bytes(), &res2)
	if res2.Settled != 0 || res2.Skipped != 1 {
		t.Fatalf("re-settle: want 0 settled / 1 skipped, got %d / %d", res2.Settled, res2.Skipped)
	}

	// Buyer statement shows the settlement.
	sr := do(t, h, http.MethodGet, "/storefront/rebates", custTok, nil)
	var stmt struct {
		Settlements []struct {
			RebateAmount string `json:"rebate_amount"`
		} `json:"settlements"`
		Current []struct {
			ProjectedRebate string `json:"projected_rebate"`
		} `json:"current"`
	}
	_ = json.Unmarshal(sr.Body.Bytes(), &stmt)
	if len(stmt.Settlements) != 1 || stmt.Settlements[0].RebateAmount != "3000.0000" {
		t.Fatalf("statement settlements: %+v (%s)", stmt.Settlements, sr.Body.String())
	}
	if len(stmt.Current) != 1 || stmt.Current[0].ProjectedRebate != "3000.0000" {
		t.Fatalf("statement current progress: %+v", stmt.Current)
	}
}

func TestRebatePermissionGate(t *testing.T) {
	h, issuer, _ := newServer(t)
	noPerm, _ := issuer.Issue("1", 1, "admin", []string{"product.view"})
	if rr := do(t, h, http.MethodGet, "/admin/rebates", noPerm, nil); rr.Code != http.StatusForbidden {
		t.Fatalf("no perm: want 403, got %d", rr.Code)
	}
}
