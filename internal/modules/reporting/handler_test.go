package reporting_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
	st := store.New(pool)
	issuer := auth.NewIssuer(testSecret, time.Hour)
	return server.New(st, issuer), issuer, pool
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

func decode(t *testing.T, rr *httptest.ResponseRecorder, v any) {
	t.Helper()
	if err := json.Unmarshal(rr.Body.Bytes(), v); err != nil {
		t.Fatalf("decode: %v (body=%s)", err, rr.Body.String())
	}
}

// seedOrder creates one non-cancelled order with a line, in the given org.
func seedOrder(t *testing.T, pool *pgxpool.Pool, org int64, grand, rowTotal string) {
	t.Helper()
	q := gen.New(pool)
	ctx := context.Background()
	cust, err := q.CreateCustomer(ctx, gen.CreateCustomerParams{OrganizationID: org, Name: "Acme", CreditLimit: "0"})
	if err != nil {
		t.Fatalf("customer: %v", err)
	}
	p, _ := q.CreateProduct(ctx, gen.CreateProductParams{OrganizationID: org, Sku: "RP1", Type: "simple", Name: "Report Product", Slug: "rp1", Status: "active", Attributes: []byte("{}"), Unit: "each"})
	o, err := q.CreateOrder(ctx, gen.CreateOrderParams{
		OrganizationID: org, WebsiteID: 1, CustomerID: cust.ID, Currency: "USD",
		BillingAddress: []byte("{}"), ShippingAddress: []byte("{}"),
		Subtotal: grand, TaxTotal: "0", ShippingTotal: "0", GrandTotal: grand,
	})
	if err != nil {
		t.Fatalf("order: %v", err)
	}
	if _, err := q.AddOrderItem(ctx, gen.AddOrderItemParams{
		OrderID: o.ID, ProductID: p.ID, Sku: p.Sku, Name: p.Name,
		Quantity: "1", Unit: "each", UnitPrice: rowTotal, TaxAmount: "0", RowTotal: rowTotal,
	}); err != nil {
		t.Fatalf("order item: %v", err)
	}
}

func reportToken(t *testing.T, issuer *auth.Issuer) string {
	t.Helper()
	tok, _ := issuer.Issue("1", 1, "admin", []string{"report.view"})
	return tok
}

func TestReportingDashboards(t *testing.T) {
	h, issuer, pool := newServer(t)
	tok := reportToken(t, issuer)
	seedOrder(t, pool, 1, "100.0000", "100.0000")

	// Materialized views are stale until refreshed (created empty in the test DB).
	if rr := do(t, h, http.MethodPost, "/admin/reports/refresh", tok, nil); rr.Code != http.StatusOK {
		t.Fatalf("refresh: %d (%s)", rr.Code, rr.Body.String())
	}

	// Summary KPIs.
	var sum struct {
		OrderCount    int64  `json:"order_count"`
		Revenue       string `json:"revenue"`
		AvgOrderValue string `json:"avg_order_value"`
	}
	decode(t, do(t, h, http.MethodGet, "/admin/reports/summary?days=30", tok, nil), &sum)
	if sum.OrderCount != 1 || sum.Revenue != "100.0000" {
		t.Fatalf("summary: want 1 order / 100.0000, got %d / %s", sum.OrderCount, sum.Revenue)
	}
	if sum.AvgOrderValue != "100.0000" {
		t.Errorf("AOV: want 100.0000, got %s", sum.AvgOrderValue)
	}

	// Daily sales series includes today.
	var series struct {
		Items []struct {
			Day        string `json:"day"`
			OrderCount int64  `json:"order_count"`
			Revenue    string `json:"revenue"`
		} `json:"items"`
	}
	decode(t, do(t, h, http.MethodGet, "/admin/reports/sales", tok, nil), &series)
	if len(series.Items) != 1 || series.Items[0].Revenue != "100.0000" {
		t.Fatalf("daily sales: want 1 day @ 100.0000, got %+v", series.Items)
	}

	// Top products (current month).
	var top struct {
		Items []struct {
			Sku     string `json:"sku"`
			Revenue string `json:"revenue"`
		} `json:"items"`
	}
	decode(t, do(t, h, http.MethodGet, "/admin/reports/top-products", tok, nil), &top)
	if len(top.Items) != 1 || top.Items[0].Sku != "RP1" || top.Items[0].Revenue != "100.0000" {
		t.Fatalf("top products: want RP1 @ 100.0000, got %+v", top.Items)
	}
}

func TestReportingTenantIsolationAndAuth(t *testing.T) {
	h, issuer, pool := newServer(t)
	tok := reportToken(t, issuer)
	ctx := context.Background()

	// org 1 order + an org-2 order that must not leak into org-1 totals.
	seedOrder(t, pool, 1, "50.0000", "50.0000")
	var org2 int64
	if err := pool.QueryRow(ctx, `INSERT INTO organizations (name) VALUES ('Org Two') RETURNING id`).Scan(&org2); err != nil {
		t.Fatalf("org2: %v", err)
	}
	// org2 needs a website id 1? website_id references websites; seed has website 1 for org1.
	// Use website 1 (FK only requires existence); revenue is grouped by org, so isolation still holds.
	seedOrder(t, pool, org2, "999.0000", "999.0000")
	do(t, h, http.MethodPost, "/admin/reports/refresh", tok, nil)

	var sum struct {
		Revenue string `json:"revenue"`
	}
	decode(t, do(t, h, http.MethodGet, "/admin/reports/summary?days=30", tok, nil), &sum)
	if sum.Revenue != "50.0000" {
		t.Errorf("tenant isolation: org-1 revenue should be 50.0000, got %s", sum.Revenue)
	}

	// Auth: storefront token + missing permission are forbidden.
	cust, _ := issuer.IssueStorefront(0, 1, 1)
	if rr := do(t, h, http.MethodGet, "/admin/reports/summary", cust, nil); rr.Code != http.StatusForbidden {
		t.Errorf("storefront token: want 403, got %d", rr.Code)
	}
	noPerm, _ := issuer.Issue("1", 1, "admin", []string{"order.view"})
	if rr := do(t, h, http.MethodGet, "/admin/reports/summary", noPerm, nil); rr.Code != http.StatusForbidden {
		t.Errorf("missing report.view: want 403, got %d", rr.Code)
	}
}
