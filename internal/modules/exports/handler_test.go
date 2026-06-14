package exports_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
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

func do(t *testing.T, h http.Handler, method, path, tok string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, &bytes.Buffer{})
	if tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	return rr
}

func adminToken(t *testing.T, issuer *auth.Issuer, perms ...string) string {
	t.Helper()
	tok, _ := issuer.Issue("1", 1, "admin", perms)
	return tok
}

func seedOrder(t *testing.T, pool *pgxpool.Pool, org int64) {
	t.Helper()
	q := gen.New(pool)
	ctx := context.Background()
	cust, err := q.CreateCustomer(ctx, gen.CreateCustomerParams{OrganizationID: org, Name: "Acme Industrial", CreditLimit: "0"})
	if err != nil {
		t.Fatalf("customer: %v", err)
	}
	p, _ := q.CreateProduct(ctx, gen.CreateProductParams{
		OrganizationID: org, Sku: "XP1", Type: "simple", Name: "Export Product",
		Slug: "xp1", Status: "active", Attributes: []byte("{}"), Unit: "each",
	})
	o, err := q.CreateOrder(ctx, gen.CreateOrderParams{
		OrganizationID: org, WebsiteID: 1, CustomerID: cust.ID, Currency: "USD",
		BillingAddress: []byte("{}"), ShippingAddress: []byte("{}"),
		Subtotal: "250.0000", TaxTotal: "0", ShippingTotal: "0", GrandTotal: "250.0000",
	})
	if err != nil {
		t.Fatalf("order: %v", err)
	}
	if _, err := q.AddOrderItem(ctx, gen.AddOrderItemParams{
		OrderID: o.ID, ProductID: p.ID, Sku: p.Sku, Name: p.Name,
		Quantity: "2", Unit: "each", UnitPrice: "125.0000", TaxAmount: "0", RowTotal: "250.0000",
	}); err != nil {
		t.Fatalf("order item: %v", err)
	}
}

func TestExportManifestFiltersByPermission(t *testing.T) {
	h, issuer, _ := newServer(t)

	// Full data permissions → every dataset is offered.
	full := adminToken(t, issuer, "report.view", "order.view", "customer.view", "invoice.view")
	var man struct {
		Datasets []struct {
			Key     string   `json:"key"`
			Formats []string `json:"formats"`
		} `json:"datasets"`
	}
	decodeBody(t, do(t, h, http.MethodGet, "/admin/exports", full), &man)
	if len(man.Datasets) != 4 {
		t.Fatalf("want 4 datasets, got %d (%+v)", len(man.Datasets), man.Datasets)
	}

	// report.view alone (no entity permissions) → the center is reachable but
	// lists nothing the caller may export.
	viewOnly := adminToken(t, issuer, "report.view")
	var empty struct {
		Datasets []any `json:"datasets"`
	}
	decodeBody(t, do(t, h, http.MethodGet, "/admin/exports", viewOnly), &empty)
	if len(empty.Datasets) != 0 {
		t.Errorf("report.view alone should expose no datasets, got %d", len(empty.Datasets))
	}
}

func TestExportOrdersCSVAndXLSX(t *testing.T) {
	h, issuer, pool := newServer(t)
	seedOrder(t, pool, 1)
	tok := adminToken(t, issuer, "report.view", "order.view", "customer.view")

	// CSV
	rr := do(t, h, http.MethodGet, "/admin/exports/orders?format=csv", tok)
	if rr.Code != http.StatusOK {
		t.Fatalf("orders csv: status %d", rr.Code)
	}
	if ct := rr.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/csv") {
		t.Errorf("csv content-type = %q", ct)
	}
	body := rr.Body.String()
	if !strings.Contains(body, "order_id,customer,status") || !strings.Contains(body, "Acme Industrial") {
		t.Errorf("orders CSV missing header/data:\n%s", body)
	}

	// XLSX — body must be a zip (PK magic) with the spreadsheet content-type.
	rr = do(t, h, http.MethodGet, "/admin/exports/orders?format=xlsx", tok)
	if rr.Code != http.StatusOK {
		t.Fatalf("orders xlsx: status %d", rr.Code)
	}
	if ct := rr.Header().Get("Content-Type"); !strings.Contains(ct, "spreadsheetml") {
		t.Errorf("xlsx content-type = %q", ct)
	}
	if b := rr.Body.Bytes(); len(b) < 2 || b[0] != 'P' || b[1] != 'K' {
		t.Errorf("xlsx body is not a zip (no PK magic)")
	}
}

func TestExportPerDatasetPermission(t *testing.T) {
	h, issuer, _ := newServer(t)

	// report.view reaches the route, but customers needs customer.view.
	noCust := adminToken(t, issuer, "report.view", "order.view")
	if rr := do(t, h, http.MethodGet, "/admin/exports/customers", noCust); rr.Code != http.StatusForbidden {
		t.Errorf("customers without customer.view: want 403, got %d", rr.Code)
	}

	// Unknown dataset → 404.
	full := adminToken(t, issuer, "report.view", "order.view", "customer.view", "invoice.view")
	if rr := do(t, h, http.MethodGet, "/admin/exports/widgets", full); rr.Code != http.StatusNotFound {
		t.Errorf("unknown dataset: want 404, got %d", rr.Code)
	}

	// Storefront token is the wrong audience.
	cust, _ := issuer.IssueStorefront(0, 1, 1)
	if rr := do(t, h, http.MethodGet, "/admin/exports", cust); rr.Code != http.StatusForbidden {
		t.Errorf("storefront token: want 403, got %d", rr.Code)
	}
}

func TestExportIsAudited(t *testing.T) {
	h, issuer, pool := newServer(t)
	seedOrder(t, pool, 1)
	tok := adminToken(t, issuer, "report.view", "order.view", "audit.view")

	if rr := do(t, h, http.MethodGet, "/admin/exports/orders?format=csv", tok); rr.Code != http.StatusOK {
		t.Fatalf("export: status %d", rr.Code)
	}
	// The export (a read) is recorded in the audit trail via the explicit path.
	var list struct {
		Items []struct {
			Action     string `json:"action"`
			EntityType string `json:"entity_type"`
		} `json:"items"`
	}
	decodeBody(t, do(t, h, http.MethodGet, "/admin/audit", tok), &list)
	found := false
	for _, it := range list.Items {
		if it.Action == "exports.download" && it.EntityType == "orders" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected an exports.download audit entry for orders, got %+v", list.Items)
	}
}

func decodeBody(t *testing.T, rr *httptest.ResponseRecorder, v any) {
	t.Helper()
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d (%s)", rr.Code, rr.Body.String())
	}
	if err := json.Unmarshal(rr.Body.Bytes(), v); err != nil {
		t.Fatalf("decode: %v (body=%s)", err, rr.Body.String())
	}
}
