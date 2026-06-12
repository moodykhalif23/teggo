package subscription_test

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

// seedCustomer makes a customer + buyer user, returns (customerID, storefront token).
func seedCustomer(t *testing.T, pool *pgxpool.Pool, issuer *auth.Issuer, name, email string) (int64, string) {
	t.Helper()
	q := gen.New(pool)
	ctx := context.Background()
	cust, err := q.CreateCustomer(ctx, gen.CreateCustomerParams{OrganizationID: 1, Name: name, CreditLimit: "0"})
	if err != nil {
		t.Fatalf("customer: %v", err)
	}
	hash, _ := auth.HashPassword("pw-123456")
	u, err := q.CreateCustomerUser(ctx, gen.CreateCustomerUserParams{CustomerID: cust.ID, Email: email, PasswordHash: hash, FullName: name, Role: "admin"})
	if err != nil {
		t.Fatalf("customer user: %v", err)
	}
	tok, err := issuer.IssueStorefront(u.ID, 1, cust.ID)
	if err != nil {
		t.Fatalf("token: %v", err)
	}
	return cust.ID, tok
}

func seedProduct(t *testing.T, pool *pgxpool.Pool, sku string) int64 {
	t.Helper()
	p, err := gen.New(pool).CreateProduct(context.Background(), gen.CreateProductParams{
		OrganizationID: 1, Sku: sku, Type: "simple", Name: sku, Slug: sku, Status: "active", Attributes: []byte("{}"), Unit: "each",
	})
	if err != nil {
		t.Fatalf("product: %v", err)
	}
	return p.ID
}

func TestSubscriptionStorefrontLifecycle(t *testing.T) {
	h, issuer, pool := newServer(t)
	_, tok := seedCustomer(t, pool, issuer, "Acme", "buyer@acme.test")
	pid := seedProduct(t, pool, "SUBP-1")

	// Subscribe.
	cr := do(t, h, http.MethodPost, "/storefront/subscriptions", tok, map[string]any{
		"name": "Monthly resupply", "cadence": "monthly",
		"items": []map[string]any{{"product_id": pid, "quantity": "5"}},
	})
	if cr.Code != http.StatusCreated {
		t.Fatalf("subscribe: want 201, got %d (%s)", cr.Code, cr.Body.String())
	}
	var sub struct {
		ID          int64  `json:"id"`
		NextRunDate string `json:"next_run_date"`
		Status      string `json:"status"`
		Items       []struct {
			ProductID int64 `json:"product_id"`
		} `json:"items"`
	}
	_ = json.Unmarshal(cr.Body.Bytes(), &sub)
	if sub.ID == 0 || sub.Status != "active" || len(sub.Items) != 1 {
		t.Fatalf("unexpected subscription: %s", cr.Body.String())
	}
	firstNext := sub.NextRunDate

	// List shows it.
	lr := do(t, h, http.MethodGet, "/storefront/subscriptions", tok, nil)
	var list struct {
		Items []struct {
			ID int64 `json:"id"`
		} `json:"items"`
	}
	_ = json.Unmarshal(lr.Body.Bytes(), &list)
	if len(list.Items) != 1 {
		t.Fatalf("list: want 1, got %d", len(list.Items))
	}

	idPath := "/storefront/subscriptions/" + strconv.FormatInt(sub.ID, 10)

	// Skip → next run date moves forward.
	sk := do(t, h, http.MethodPost, idPath+"/skip", tok, nil)
	if sk.Code != http.StatusOK {
		t.Fatalf("skip: want 200, got %d (%s)", sk.Code, sk.Body.String())
	}
	var afterSkip struct {
		NextRunDate string `json:"next_run_date"`
	}
	_ = json.Unmarshal(sk.Body.Bytes(), &afterSkip)
	if afterSkip.NextRunDate == firstNext || afterSkip.NextRunDate < firstNext {
		t.Errorf("skip should advance next_run_date: %s -> %s", firstNext, afterSkip.NextRunDate)
	}

	// Pause then cancel.
	if rr := do(t, h, http.MethodPost, idPath+"/status", tok, map[string]any{"status": "paused"}); rr.Code != http.StatusOK {
		t.Fatalf("pause: %d (%s)", rr.Code, rr.Body.String())
	}
	if rr := do(t, h, http.MethodPost, idPath+"/status", tok, map[string]any{"status": "cancelled"}); rr.Code != http.StatusOK {
		t.Fatalf("cancel: %d", rr.Code)
	}

	// Ownership isolation: another company can't see it.
	_, tokB := seedCustomer(t, pool, issuer, "Beta", "buyer@beta.test")
	if rr := do(t, h, http.MethodGet, idPath, tokB, nil); rr.Code != http.StatusNotFound {
		t.Fatalf("cross-company read: want 404, got %d", rr.Code)
	}
}

func TestSubscriptionEdit(t *testing.T) {
	h, issuer, pool := newServer(t)
	_, tok := seedCustomer(t, pool, issuer, "Acme", "buyer@acme.test")
	p1 := seedProduct(t, pool, "EDIT-1")
	p2 := seedProduct(t, pool, "EDIT-2")

	cr := do(t, h, http.MethodPost, "/storefront/subscriptions", tok, map[string]any{
		"cadence": "monthly", "items": []map[string]any{{"product_id": p1, "quantity": "1"}},
	})
	if cr.Code != http.StatusCreated {
		t.Fatalf("create: %d (%s)", cr.Code, cr.Body.String())
	}
	var sub struct {
		ID int64 `json:"id"`
	}
	_ = json.Unmarshal(cr.Body.Bytes(), &sub)
	idPath := "/storefront/subscriptions/" + strconv.FormatInt(sub.ID, 10)

	// Edit: change cadence + replace items (qty change + add a product).
	er := do(t, h, http.MethodPut, idPath, tok, map[string]any{
		"cadence": "weekly",
		"items": []map[string]any{
			{"product_id": p1, "quantity": "5"},
			{"product_id": p2, "quantity": "2"},
		},
	})
	if er.Code != http.StatusOK {
		t.Fatalf("edit: want 200, got %d (%s)", er.Code, er.Body.String())
	}

	gr := do(t, h, http.MethodGet, idPath, tok, nil)
	var got struct {
		Cadence string `json:"cadence"`
		Items   []struct {
			ProductID int64  `json:"product_id"`
			Quantity  string `json:"quantity"`
		} `json:"items"`
	}
	_ = json.Unmarshal(gr.Body.Bytes(), &got)
	if got.Cadence != "weekly" || len(got.Items) != 2 {
		t.Fatalf("after edit: want weekly + 2 items, got %s + %d (%s)", got.Cadence, len(got.Items), gr.Body.String())
	}
}

func TestSubscriptionAdminAccess(t *testing.T) {
	h, issuer, pool := newServer(t)
	custID, custTok := seedCustomer(t, pool, issuer, "Acme", "buyer@acme.test")
	pid := seedProduct(t, pool, "SUBP-2")
	// Buyer creates a subscription.
	if rr := do(t, h, http.MethodPost, "/storefront/subscriptions", custTok, map[string]any{
		"cadence": "weekly", "items": []map[string]any{{"product_id": pid, "quantity": "1"}},
	}); rr.Code != http.StatusCreated {
		t.Fatalf("seed subscription: %d (%s)", rr.Code, rr.Body.String())
	}
	_ = custID

	// Admin with permission sees it; admin run-now returns 200 (no price → no order).
	adminTok, _ := issuer.Issue("1", 1, "admin", []string{"subscription.view", "subscription.manage"})
	lr := do(t, h, http.MethodGet, "/admin/subscriptions", adminTok, nil)
	var list struct {
		Items []struct {
			ID int64 `json:"id"`
		} `json:"items"`
	}
	_ = json.Unmarshal(lr.Body.Bytes(), &list)
	if len(list.Items) != 1 {
		t.Fatalf("admin list: want 1, got %d (%s)", len(list.Items), lr.Body.String())
	}
	runPath := "/admin/subscriptions/" + strconv.FormatInt(list.Items[0].ID, 10) + "/run"
	if rr := do(t, h, http.MethodPost, runPath, adminTok, nil); rr.Code != http.StatusOK {
		t.Fatalf("run-now: want 200, got %d (%s)", rr.Code, rr.Body.String())
	}

	// Admin without permission is forbidden.
	noPerm, _ := issuer.Issue("1", 1, "admin", []string{"product.view"})
	if rr := do(t, h, http.MethodGet, "/admin/subscriptions", noPerm, nil); rr.Code != http.StatusForbidden {
		t.Fatalf("no perm: want 403, got %d", rr.Code)
	}
}
