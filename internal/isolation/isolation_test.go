// Package isolation_test is the tenant-isolation convention gate (SAAS.md #3):
// it asserts the RLS net covers every org table (so a NEW table without a
// policy fails CI), proves the net actually scopes unfiltered SQL when armed,
// and sweeps the API end-to-end on an ARMED pool with a foreign org's token.
package isolation_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"b2bcommerce/internal/auth"
	appdb "b2bcommerce/internal/db"
	"b2bcommerce/internal/server"
	"b2bcommerce/internal/store"
	"b2bcommerce/internal/tenantctx"
	"b2bcommerce/internal/testsupport"
)

const testSecret = "test-secret-please-change"

// armedDB returns two pools onto the SAME database: the plain one for seeding
// and an RLS-armed one (exactly as cmd/api builds it).
func armedDB(t *testing.T) (*pgxpool.Pool, *pgxpool.Pool) {
	t.Helper()
	plain, dsn := testsupport.NewDBWithDSN(t)
	armed, err := appdb.NewPoolWithConfig(context.Background(), dsn, appdb.PoolConfig{ArmTenantRLS: true})
	if err != nil {
		t.Fatalf("armed pool: %v", err)
	}
	t.Cleanup(armed.Close)
	return plain, armed
}

// TestRLSLintEveryOrgTableCovered is the migration-lint gate: every table with
// an organization_id column must carry FORCEd row-level security and the
// org_isolation policy. Adding a tenant table without them fails this test —
// extend migration-style (ALTER TABLE … / CREATE POLICY org_isolation …) in the
// migration that creates the table.
func TestRLSLintEveryOrgTableCovered(t *testing.T) {
	pool := testsupport.NewDB(t)
	rows, err := pool.Query(context.Background(), `
		SELECT c.relname,
		       c.relrowsecurity AND c.relforcerowsecurity AS forced,
		       EXISTS (SELECT 1 FROM pg_policies p
		               WHERE p.schemaname = 'public' AND p.tablename = c.relname
		                 AND p.policyname = 'org_isolation') AS has_policy
		FROM pg_class c
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = 'public' AND c.relkind = 'r'
		  AND EXISTS (SELECT 1 FROM information_schema.columns col
		              WHERE col.table_schema = 'public'
		                AND col.table_name = c.relname
		                AND col.column_name = 'organization_id')
		ORDER BY c.relname`)
	if err != nil {
		t.Fatalf("introspect: %v", err)
	}
	defer rows.Close()
	checked := 0
	for rows.Next() {
		var name string
		var forced, hasPolicy bool
		if err := rows.Scan(&name, &forced, &hasPolicy); err != nil {
			t.Fatal(err)
		}
		checked++
		if !forced || !hasPolicy {
			t.Errorf("table %q has organization_id but no enforced RLS net "+
				"(forced=%v policy=%v) — add ENABLE/FORCE ROW LEVEL SECURITY + the org_isolation policy in its migration",
				name, forced, hasPolicy)
		}
	}
	if checked < 30 {
		t.Fatalf("lint swept only %d org tables — introspection looks broken", checked)
	}
	t.Logf("RLS lint: %d org-scoped tables covered", checked)
}

// TestRLSNetScopesUnfilteredSQL proves the net: with app.org_id armed, even a
// query MISSING its WHERE organization_id clause only sees the armed org.
func TestRLSNetScopesUnfilteredSQL(t *testing.T) {
	plain, armed := armedDB(t)
	ctx := context.Background()

	// A second org with one product, seeded through the unarmed pool.
	var org2 int64
	if err := plain.QueryRow(ctx,
		`INSERT INTO organizations (name, status) VALUES ('Other Co', 'active') RETURNING id`,
	).Scan(&org2); err != nil {
		t.Fatal(err)
	}
	if _, err := plain.Exec(ctx,
		`INSERT INTO products (organization_id, sku, type, name, slug, status, attributes)
		 VALUES ($1, 'OTHER-1', 'simple', 'Other Widget', 'other-widget', 'active', '{}')`, org2); err != nil {
		t.Fatal(err)
	}

	count := func(ctx context.Context) int {
		var n int
		// Deliberately UNFILTERED — exactly the bug class the net exists for.
		if err := armed.QueryRow(ctx, `SELECT count(*) FROM products`).Scan(&n); err != nil {
			t.Fatalf("count: %v", err)
		}
		return n
	}

	all := count(ctx) // unarmed ctx → fail-open → everything
	org1Seen := count(tenantctx.WithOrg(ctx, 1))
	org2Seen := count(tenantctx.WithOrg(ctx, org2))

	if org2Seen != 1 {
		t.Errorf("armed org2 sees %d products, want exactly its 1", org2Seen)
	}
	if org1Seen != all-1 {
		t.Errorf("armed org1 sees %d products, want %d (all minus org2's)", org1Seen, all-1)
	}
	if all <= org1Seen {
		t.Errorf("fail-open sanity: all=%d should exceed org1=%d", all, org1Seen)
	}

	// WITH CHECK: writing another org's row under an armed session must fail.
	_, err := armed.Exec(tenantctx.WithOrg(ctx, 1),
		`INSERT INTO products (organization_id, sku, type, name, slug, status, attributes)
		 VALUES ($1, 'SMUGGLED', 'simple', 'x', 'smuggled', 'active', '{}')`, org2)
	if err == nil || !strings.Contains(err.Error(), "row-level security") {
		t.Errorf("cross-org INSERT under armed session: want RLS violation, got %v", err)
	}

	// Bypass is the deliberate, grep-able escape hatch for operator paths.
	if n := count(tenantctx.Bypass(tenantctx.WithOrg(ctx, org2))); n != all {
		t.Errorf("bypass sees %d, want all %d", n, all)
	}
}

// TestArmedAPICrossTenantProbe runs the real HTTP stack on the ARMED pool: org
// 1 traffic behaves normally, a foreign org's fully-permissioned token leaks
// nothing of org 1 across the admin read surface, and the platform-operator
// endpoints (deliberate cross-tenant) still work via their explicit bypass.
func TestArmedAPICrossTenantProbe(t *testing.T) {
	plain, armed := armedDB(t)
	ctx := context.Background()
	issuer := auth.NewIssuer(testSecret, time.Hour)
	h := server.New(store.New(armed), issuer)

	var org2 int64
	if err := plain.QueryRow(ctx,
		`INSERT INTO organizations (name, status) VALUES ('Probe Co', 'active') RETURNING id`,
	).Scan(&org2); err != nil {
		t.Fatal(err)
	}

	do := func(method, path, tok string, body any) *httptest.ResponseRecorder {
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

	// Org 1 behaves exactly as before on the armed pool.
	org1Tok, _ := issuer.Issue("1", 1, "admin", []string{"product.view"})
	or := do(http.MethodGet, "/admin/products", org1Tok, nil)
	if or.Code != http.StatusOK || !strings.Contains(or.Body.String(), "VALVE-100") {
		t.Fatalf("org1 on armed pool: %d (%s)", or.Code, or.Body.String())
	}

	// The probe: a foreign org with every relevant view permission sweeps the
	// admin read surface; org 1's seeded markers must never appear.
	perms := []string{
		"product.view", "category.view", "attribute.view", "customer.view",
		"order.view", "quote.view", "rfq.view", "invoice.view", "return.view",
		"price_list.view", "promotion.view", "settings.view", "inventory.view",
		"crm.view", "workflow.view", "cms.view", "report.view", "tenant.view",
		"subscription.view", "rebate.view", "fx.view", "merchandising.view",
	}
	probeTok, _ := issuer.Issue("9", org2, "admin", perms)
	paths := []string{
		"/admin/products", "/admin/categories", "/admin/attributes",
		"/admin/customers", "/admin/orders", "/admin/quotes", "/admin/rfqs",
		"/admin/invoices", "/admin/returns", "/admin/promotions",
		"/admin/settings", "/admin/websites", "/admin/subscriptions",
		"/admin/rebates", "/admin/fx-rates", "/admin/search-synonyms",
	}
	markers := []string{"VALVE-100", "PIPE-200", "Demo Org", "Demo Store", "admin@demo.test"}
	for _, p := range paths {
		rr := do(http.MethodGet, p, probeTok, nil)
		if rr.Code != http.StatusOK {
			t.Errorf("probe %s: %d (%s)", p, rr.Code, rr.Body.String())
			continue
		}
		for _, m := range markers {
			if strings.Contains(rr.Body.String(), m) {
				t.Errorf("probe %s leaked org-1 marker %q", p, m)
			}
		}
	}

	// Targeted object probes: org 1's first product is invisible, not just unlisted.
	if rr := do(http.MethodGet, "/admin/products/1", probeTok, nil); rr.Code != http.StatusNotFound {
		t.Errorf("foreign product fetch: want 404, got %d", rr.Code)
	}
	if rr := do(http.MethodPut, "/admin/products/1", probeTok, map[string]any{
		"sku": "HIJACK", "name": "x", "slug": "x", "type": "simple", "status": "active",
	}); rr.Code == http.StatusOK {
		t.Errorf("foreign product update must not succeed, got %d", rr.Code)
	}

	// Operator endpoints stay functional on the armed pool (explicit bypass):
	// real cross-org counts and a cross-org plan assignment.
	opTok, _ := issuer.Issue("1", 1, "admin", []string{"platform.view", "platform.manage"})
	lr := do(http.MethodGet, "/admin/platform/organizations", opTok, nil)
	var orgs struct {
		Items []struct {
			ID        int64 `json:"id"`
			UserCount int64 `json:"user_count"`
		} `json:"items"`
	}
	_ = json.Unmarshal(lr.Body.Bytes(), &orgs)
	var sawOrg1Users bool
	for _, o := range orgs.Items {
		if o.ID == 1 && o.UserCount >= 1 {
			sawOrg1Users = true
		}
	}
	if lr.Code != http.StatusOK || len(orgs.Items) < 2 || !sawOrg1Users {
		t.Errorf("operator org list on armed pool: %d (%s)", lr.Code, lr.Body.String())
	}
	if rr := do(http.MethodPost, "/admin/platform/organizations/"+strconv.FormatInt(org2, 10)+"/plan", opTok,
		map[string]any{"plan_code": "growth"}); rr.Code != http.StatusOK {
		t.Errorf("operator plan assign on armed pool: %d (%s)", rr.Code, rr.Body.String())
	}
}
