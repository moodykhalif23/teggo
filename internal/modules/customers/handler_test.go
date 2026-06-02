package customers_test

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

// newServer wires the full HTTP stack against a fresh migrated DB, exactly as
// production does, plus a token issuer so tests can mint scoped admin tokens.
func newServer(t *testing.T) (http.Handler, *auth.Issuer, *pgxpool.Pool) {
	t.Helper()
	pool := testsupport.NewDB(t)
	st := store.New(pool)
	issuer := auth.NewIssuer(testSecret, time.Hour)
	return server.New(st, issuer), issuer, pool
}

func adminToken(t *testing.T, issuer *auth.Issuer, perms ...string) string {
	t.Helper()
	tok, err := issuer.Issue("1", 1, "admin", perms)
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}
	return tok
}

func do(t *testing.T, h http.Handler, method, path, token string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("encode body: %v", err)
		}
	}
	req := httptest.NewRequest(method, path, &buf)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	return rr
}

// --- HTTP-level: auth + permission gate + org scoping end-to-end ----------

func TestListCustomers_AuthAndPermission(t *testing.T) {
	h, issuer, _ := newServer(t)

	// No token -> 401.
	if rr := do(t, h, http.MethodGet, "/admin/customers", "", nil); rr.Code != http.StatusUnauthorized {
		t.Fatalf("no token: want 401, got %d", rr.Code)
	}
	// Token without the permission -> 403.
	noPerm := adminToken(t, issuer)
	if rr := do(t, h, http.MethodGet, "/admin/customers", noPerm, nil); rr.Code != http.StatusForbidden {
		t.Fatalf("missing perm: want 403, got %d", rr.Code)
	}
	// Token with the permission -> 200.
	ok := adminToken(t, issuer, "customer.view")
	if rr := do(t, h, http.MethodGet, "/admin/customers", ok, nil); rr.Code != http.StatusOK {
		t.Fatalf("with perm: want 200, got %d (%s)", rr.Code, rr.Body.String())
	}
}

func TestCreateAndGetCustomer_HTTP(t *testing.T) {
	h, issuer, _ := newServer(t)
	tok := adminToken(t, issuer, "customer.view", "customer.manage")

	rr := do(t, h, http.MethodPost, "/admin/customers", tok, map[string]any{
		"name":               "Acme HQ",
		"credit_limit":       "50000.0000",
		"payment_terms_days": 30,
	})
	if rr.Code != http.StatusCreated {
		t.Fatalf("create: want 201, got %d (%s)", rr.Code, rr.Body.String())
	}
	var created struct {
		ID          int64  `json:"id"`
		PublicID    string `json:"public_id"`
		Name        string `json:"name"`
		CreditLimit string `json:"credit_limit"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode created: %v", err)
	}
	if created.ID == 0 || created.PublicID == "" || created.Name != "Acme HQ" {
		t.Fatalf("unexpected created customer: %+v", created)
	}
	if created.CreditLimit != "50000.0000" {
		t.Errorf("credit_limit roundtrip: want 50000.0000, got %q", created.CreditLimit)
	}

	// Round-trip GET.
	got := do(t, h, http.MethodGet, "/admin/customers/"+itoa(created.ID), tok, nil)
	if got.Code != http.StatusOK {
		t.Fatalf("get: want 200, got %d (%s)", got.Code, got.Body.String())
	}

	// Unknown id -> 404.
	missing := do(t, h, http.MethodGet, "/admin/customers/999999", tok, nil)
	if missing.Code != http.StatusNotFound {
		t.Errorf("get missing: want 404, got %d", missing.Code)
	}
}

// --- Query-level: hierarchy CTE, cycle guard, tenant isolation ------------

func seedCustomer(t *testing.T, q *gen.Queries, org int64, name string, parent *int64) gen.Customer {
	t.Helper()
	c, err := q.CreateCustomer(context.Background(), gen.CreateCustomerParams{
		OrganizationID: org,
		ParentID:       parent,
		Name:           name,
		CreditLimit:    "0",
	})
	if err != nil {
		t.Fatalf("seed customer %s: %v", name, err)
	}
	return c
}

func TestCustomerAncestors(t *testing.T) {
	pool := testsupport.NewDB(t)
	q := gen.New(pool)
	ctx := context.Background()

	hq := seedCustomer(t, q, 1, "HQ", nil)
	region := seedCustomer(t, q, 1, "Region", &hq.ID)
	branch := seedCustomer(t, q, 1, "Branch", &region.ID)

	rows, err := q.CustomerAncestors(ctx, gen.CustomerAncestorsParams{ID: branch.ID, OrganizationID: 1})
	if err != nil {
		t.Fatalf("ancestors: %v", err)
	}
	// Nearest first: Region (depth 1), then HQ (depth 2).
	if len(rows) != 2 {
		t.Fatalf("want 2 ancestors, got %d: %+v", len(rows), rows)
	}
	if rows[0].ID != region.ID || rows[0].Depth != 1 {
		t.Errorf("nearest ancestor: want region depth 1, got id=%d depth=%d", rows[0].ID, rows[0].Depth)
	}
	if rows[1].ID != hq.ID || rows[1].Depth != 2 {
		t.Errorf("furthest ancestor: want HQ depth 2, got id=%d depth=%d", rows[1].ID, rows[1].Depth)
	}
}

func TestReparentCycleRejected_HTTP(t *testing.T) {
	h, issuer, pool := newServer(t)
	q := gen.New(pool)
	tok := adminToken(t, issuer, "customer.view", "customer.manage")

	hq := seedCustomer(t, q, 1, "HQ", nil)
	child := seedCustomer(t, q, 1, "Child", &hq.ID)

	// Try to make HQ a child of its own descendant -> cycle -> 400.
	rr := do(t, h, http.MethodPut, "/admin/customers/"+itoa(hq.ID), tok, map[string]any{
		"name":         "HQ",
		"credit_limit": "0",
		"parent_id":    child.ID,
	})
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("cycle re-parent: want 400, got %d (%s)", rr.Code, rr.Body.String())
	}
}

func TestTenantIsolation_Customers(t *testing.T) {
	pool := testsupport.NewDB(t)
	q := gen.New(pool)
	ctx := context.Background()

	// Second org to isolate against.
	var org2 int64
	if err := pool.QueryRow(ctx,
		`INSERT INTO organizations (name) VALUES ('Org Two') RETURNING id`).Scan(&org2); err != nil {
		t.Fatalf("create org2: %v", err)
	}

	seedCustomer(t, q, 1, "Org1 Customer", nil)
	seedCustomer(t, q, org2, "Org2 Customer", nil)

	org1List, err := q.ListCustomers(ctx, gen.ListCustomersParams{OrganizationID: 1, Limit: 100, Offset: 0})
	if err != nil {
		t.Fatalf("list org1: %v", err)
	}
	for _, c := range org1List {
		if c.OrganizationID != 1 {
			t.Errorf("org1 list leaked org %d customer %q", c.OrganizationID, c.Name)
		}
		if c.Name == "Org2 Customer" {
			t.Error("tenant isolation breach: org1 sees org2 customer")
		}
	}
}

func itoa(n int64) string {
	return strconv.FormatInt(n, 10)
}
