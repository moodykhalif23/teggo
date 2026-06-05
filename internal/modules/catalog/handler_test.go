package catalog_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
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
	st := store.New(pool)
	issuer := auth.NewIssuer(testSecret, time.Hour)
	return server.New(st, issuer), issuer, pool
}

// catalogToken carries the full catalog permission set.
func catalogToken(t *testing.T, issuer *auth.Issuer) string {
	t.Helper()
	tok, err := issuer.Issue("1", 1, "admin",
		[]string{"product.view", "product.manage", "category.view", "category.manage", "attribute.view", "attribute.manage"})
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}
	return tok
}

func do(t *testing.T, h http.Handler, method, path, token string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(method, path, &buf)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	return rr
}

type listResp struct {
	Items []struct {
		ID         int64          `json:"id"`
		PublicID   string         `json:"public_id"`
		Slug       string         `json:"slug"`
		Status     string         `json:"status"`
		Attributes map[string]any `json:"attributes"`
	} `json:"items"`
	Total int64 `json:"total"`
}

// --- Storefront -----------------------------------------------------------

func TestStorefrontList_SeededProducts(t *testing.T) {
	h, _, _ := newServer(t)
	rr := do(t, h, http.MethodGet, "/storefront/products", "", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("want 200, got %d (%s)", rr.Code, rr.Body.String())
	}
	var resp listResp
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// Seed has 2 active products (the third is draft).
	if len(resp.Items) != 2 {
		t.Fatalf("want 2 active products, got %d", len(resp.Items))
	}
	// Attributes must serialize as a JSON object, not a base64 string.
	for _, it := range resp.Items {
		if it.Attributes == nil {
			t.Errorf("product %s: attributes should be a JSON object", it.Slug)
		}
	}
}

func TestStorefrontGetBySlug(t *testing.T) {
	h, _, _ := newServer(t)

	ok := do(t, h, http.MethodGet, "/storefront/products/brass-ball-valve-1in", "", nil)
	if ok.Code != http.StatusOK {
		t.Fatalf("known slug: want 200, got %d (%s)", ok.Code, ok.Body.String())
	}
	missing := do(t, h, http.MethodGet, "/storefront/products/does-not-exist", "", nil)
	if missing.Code != http.StatusNotFound {
		t.Errorf("unknown slug: want 404, got %d", missing.Code)
	}
}

// --- Admin products: auth gate + CRUD -------------------------------------

func TestAdminProductCRUD(t *testing.T) {
	h, issuer, _ := newServer(t)

	// 401 without token, 403 with a token lacking product.manage.
	if rr := do(t, h, http.MethodPost, "/admin/products", "", nil); rr.Code != http.StatusUnauthorized {
		t.Fatalf("no token: want 401, got %d", rr.Code)
	}
	viewOnly, _ := issuer.Issue("1", 1, "admin", []string{"product.view"})
	if rr := do(t, h, http.MethodPost, "/admin/products", viewOnly, map[string]any{"sku": "X", "name": "X", "slug": "x"}); rr.Code != http.StatusForbidden {
		t.Fatalf("view-only create: want 403, got %d", rr.Code)
	}

	tok := catalogToken(t, issuer)
	create := do(t, h, http.MethodPost, "/admin/products", tok, map[string]any{
		"sku": "WIDGET-1", "name": "Widget", "slug": "widget", "status": "active",
		"attributes": map[string]any{"color": "red"},
	})
	if create.Code != http.StatusCreated {
		t.Fatalf("create: want 201, got %d (%s)", create.Code, create.Body.String())
	}
	var created struct {
		ID         int64          `json:"id"`
		Attributes map[string]any `json:"attributes"`
	}
	_ = json.Unmarshal(create.Body.Bytes(), &created)
	if created.ID == 0 {
		t.Fatal("expected created id")
	}
	if created.Attributes["color"] != "red" {
		t.Errorf("attributes round-trip: want color=red, got %v", created.Attributes)
	}

	id := strconv.FormatInt(created.ID, 10)
	if rr := do(t, h, http.MethodGet, "/admin/products/"+id, tok, nil); rr.Code != http.StatusOK {
		t.Fatalf("get: want 200, got %d", rr.Code)
	}
	if rr := do(t, h, http.MethodPut, "/admin/products/"+id, tok, map[string]any{
		"sku": "WIDGET-1", "name": "Widget v2", "slug": "widget", "status": "active",
	}); rr.Code != http.StatusOK {
		t.Fatalf("update: want 200, got %d (%s)", rr.Code, rr.Body.String())
	}
	if rr := do(t, h, http.MethodDelete, "/admin/products/"+id, tok, nil); rr.Code != http.StatusNoContent {
		t.Fatalf("delete: want 204, got %d", rr.Code)
	}
	// Soft-deleted -> 404.
	if rr := do(t, h, http.MethodGet, "/admin/products/"+id, tok, nil); rr.Code != http.StatusNotFound {
		t.Errorf("get after delete: want 404, got %d", rr.Code)
	}
}

// --- Category subtree (§12.3) ---------------------------------------------

func TestCategorySubtreeListing(t *testing.T) {
	h, issuer, pool := newServer(t)
	tok := catalogToken(t, issuer)
	ctx := context.Background()
	q := gen.New(pool)

	// root -> child category tree.
	rootResp := do(t, h, http.MethodPost, "/admin/categories", tok, map[string]any{"name": "Valves", "slug": "valves"})
	if rootResp.Code != http.StatusCreated {
		t.Fatalf("create root cat: %d (%s)", rootResp.Code, rootResp.Body.String())
	}
	var root gen.Category
	_ = json.Unmarshal(rootResp.Body.Bytes(), &root)
	childResp := do(t, h, http.MethodPost, "/admin/categories", tok, map[string]any{"name": "Ball Valves", "slug": "ball-valves", "parent_id": root.ID})
	if childResp.Code != http.StatusCreated {
		t.Fatalf("create child cat: %d", childResp.Code)
	}
	var child gen.Category
	_ = json.Unmarshal(childResp.Body.Bytes(), &child)

	// Descendant CTE returns root + child.
	desc, err := q.CategoryDescendantIDs(ctx, gen.CategoryDescendantIDsParams{ID: root.ID, OrganizationID: 1})
	if err != nil {
		t.Fatalf("descendants: %v", err)
	}
	if len(desc) != 2 {
		t.Fatalf("subtree: want 2 categories, got %d", len(desc))
	}

	// New active product assigned only to the CHILD category.
	prodResp := do(t, h, http.MethodPost, "/admin/products", tok, map[string]any{
		"sku": "BV-1", "name": "Ball Valve 1in", "slug": "bv-1in", "status": "active",
	})
	var prod struct {
		ID       int64  `json:"id"`
		PublicID string `json:"public_id"`
	}
	_ = json.Unmarshal(prodResp.Body.Bytes(), &prod)
	if rr := do(t, h, http.MethodPost, "/admin/products/"+strconv.FormatInt(prod.ID, 10)+"/categories", tok, map[string]any{"category_id": child.ID}); rr.Code != http.StatusNoContent {
		t.Fatalf("assign category: want 204, got %d (%s)", rr.Code, rr.Body.String())
	}

	// Querying the ROOT slug must surface the product via the subtree.
	rr := do(t, h, http.MethodGet, "/storefront/products?category=valves", "", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("storefront by category: %d", rr.Code)
	}
	var resp listResp
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)
	found := false
	for _, it := range resp.Items {
		if it.PublicID == prod.PublicID {
			found = true
		}
	}
	if !found {
		t.Errorf("subtree listing: product assigned to child not returned for root category (%d items)", len(resp.Items))
	}
}

// --- JSONB facet filter (§12.5) -------------------------------------------

func TestFacetFilter(t *testing.T) {
	h, issuer, _ := newServer(t)
	tok := catalogToken(t, issuer)

	mk := func(sku, slug, color string) {
		rr := do(t, h, http.MethodPost, "/admin/products", tok, map[string]any{
			"sku": sku, "name": sku, "slug": slug, "status": "active",
			"attributes": map[string]any{"color": color},
		})
		if rr.Code != http.StatusCreated {
			t.Fatalf("create %s: %d (%s)", sku, rr.Code, rr.Body.String())
		}
	}
	mk("RED-1", "red-1", "red")
	mk("BLUE-1", "blue-1", "blue")

	rr := do(t, h, http.MethodGet, `/storefront/products?filter={"color":"red"}`, "", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("facet: want 200, got %d (%s)", rr.Code, rr.Body.String())
	}
	var resp listResp
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)
	for _, it := range resp.Items {
		if c, _ := it.Attributes["color"].(string); c != "red" {
			t.Errorf("facet leak: got product with color=%v", it.Attributes["color"])
		}
	}
	if len(resp.Items) == 0 {
		t.Error("facet: expected at least the red product")
	}

	// Invalid filter JSON -> 400.
	if bad := do(t, h, http.MethodGet, "/storefront/products?filter=not-json", "", nil); bad.Code != http.StatusBadRequest {
		t.Errorf("invalid filter: want 400, got %d", bad.Code)
	}
}

// --- Tenant isolation -----------------------------------------------------

func TestTenantIsolation_Products(t *testing.T) {
	h, issuer, pool := newServer(t)
	tok := catalogToken(t, issuer) // org 1
	ctx := context.Background()

	// A product in a different org via the query layer.
	var org2 int64
	if err := pool.QueryRow(ctx, `INSERT INTO organizations (name) VALUES ('Org Two') RETURNING id`).Scan(&org2); err != nil {
		t.Fatalf("create org2: %v", err)
	}
	if _, err := gen.New(pool).CreateProduct(ctx, gen.CreateProductParams{
		OrganizationID: org2, Sku: "OTHER-1", Type: "simple", Name: "Other", Slug: "other",
		Status: "active", Attributes: []byte("{}"), Unit: "each",
	}); err != nil {
		t.Fatalf("create org2 product: %v", err)
	}

	rr := do(t, h, http.MethodGet, "/admin/products?page_size=100", tok, nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("admin list: %d", rr.Code)
	}
	var resp listResp
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)
	for _, it := range resp.Items {
		if it.Slug == "other" {
			t.Error("tenant isolation breach: org1 admin list returned org2 product")
		}
	}
}

// --- Full-text search (PRD §14) -------------------------------------------

// searchResp captures the fields needed to assert search relevance + scoping.
type searchResp struct {
	Items []struct {
		Sku    string `json:"sku"`
		Name   string `json:"name"`
		Slug   string `json:"slug"`
		Status string `json:"status"`
	} `json:"items"`
	Total int64 `json:"total"`
}

func seedSearchProducts(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	q := gen.New(pool)
	ctx := context.Background()
	desc := "compatible with zorptron systems"
	mk := func(org int64, sku, name, slug, status string, description *string) {
		if _, err := q.CreateProduct(ctx, gen.CreateProductParams{
			OrganizationID: org, Sku: sku, Type: "simple", Name: name, Slug: slug,
			Description: description, Status: status, Attributes: []byte("{}"), Unit: "each",
		}); err != nil {
			t.Fatalf("seed %s: %v", sku, err)
		}
	}
	// org 1: two active matches (name-weighted beats description-weighted),
	// one draft (excluded from storefront), one non-match.
	mk(1, "ZW-1", "Zorptron Widget", "zorptron-widget", "active", nil)
	mk(1, "SB-2", "Standard Bolt", "standard-bolt", "active", &desc)
	mk(1, "ZG-3", "Zorptron Gadget", "zorptron-gadget", "draft", nil)
	mk(1, "PB-9", "Plastic Bucket", "plastic-bucket", "active", nil)
	// A second org with a match that must never leak into org-1 results.
	var org2 int64
	if err := pool.QueryRow(ctx, `INSERT INTO organizations (name) VALUES ('Org Two') RETURNING id`).Scan(&org2); err != nil {
		t.Fatalf("create org2: %v", err)
	}
	mk(org2, "ZT-4", "Zorptron Thing", "zorptron-thing", "active", nil)
}

func TestStorefrontSearch_RelevanceAndScope(t *testing.T) {
	h, _, pool := newServer(t)
	seedSearchProducts(t, pool)

	rr := do(t, h, http.MethodGet, "/storefront/products?q=zorptron", "", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("want 200, got %d (%s)", rr.Code, rr.Body.String())
	}
	var resp searchResp
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// Active org-1 matches only: the name match + the description match.
	// The draft (ZG-3) and the org-2 product (ZT-4) must be absent.
	if len(resp.Items) != 2 {
		t.Fatalf("want 2 results, got %d: %+v", len(resp.Items), resp.Items)
	}
	for _, it := range resp.Items {
		if it.Sku == "ZG-3" {
			t.Error("draft product leaked into storefront search")
		}
		if it.Sku == "ZT-4" {
			t.Error("tenant isolation breach: org-2 product in org-1 search")
		}
	}
	// Relevance: a name hit (weight A) ranks above a description-only hit (weight B).
	if resp.Items[0].Sku != "ZW-1" {
		t.Errorf("relevance: want name-match ZW-1 first, got %s", resp.Items[0].Sku)
	}

	// A term that matches nothing returns an empty list (not an error).
	empty := do(t, h, http.MethodGet, "/storefront/products?q=nonexistentxyz", "", nil)
	var er searchResp
	_ = json.Unmarshal(empty.Body.Bytes(), &er)
	if empty.Code != http.StatusOK || len(er.Items) != 0 {
		t.Errorf("no-match search: want 200 + 0 items, got %d / %d", empty.Code, len(er.Items))
	}
}

func TestAdminSearch_IncludesDraftsScoped(t *testing.T) {
	h, issuer, pool := newServer(t)
	seedSearchProducts(t, pool)
	tok := catalogToken(t, issuer)

	rr := do(t, h, http.MethodGet, "/admin/products?q=zorptron", tok, nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("want 200, got %d (%s)", rr.Code, rr.Body.String())
	}
	var resp searchResp
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// Admin sees all org-1 statuses (incl. the draft ZG-3) but never org-2's.
	if resp.Total != 3 || len(resp.Items) != 3 {
		t.Fatalf("want 3 org-1 matches (incl. draft), got total=%d items=%d", resp.Total, len(resp.Items))
	}
	for _, it := range resp.Items {
		if it.Sku == "ZT-4" {
			t.Error("tenant isolation breach: org-2 product in org-1 admin search")
		}
	}
}

// --- Faceted search (PRD §14 V1) ------------------------------------------

func TestFacetedSearch(t *testing.T) {
	h, _, pool := newServer(t)
	q := gen.New(pool)
	ctx := context.Background()

	// A dedicated category so facet counts are deterministic (seed products
	// aren't assigned to it).
	cat, _ := q.CreateCategory(ctx, gen.CreateCategoryParams{OrganizationID: 1, Name: "Fittings", Slug: "fittings", SortOrder: 1})
	mk := func(sku, name, attrs string) {
		p, err := q.CreateProduct(ctx, gen.CreateProductParams{
			OrganizationID: 1, Sku: sku, Type: "simple", Name: name, Slug: sku, Status: "active",
			Attributes: []byte(attrs), Unit: "each",
		})
		if err != nil {
			t.Fatalf("product %s: %v", sku, err)
		}
		_ = q.AssignProductToCategory(ctx, gen.AssignProductToCategoryParams{ProductID: p.ID, CategoryID: cat.ID})
	}
	mk("F1", "Brass Elbow", `{"material":"brass","size":"1in"}`)
	mk("F2", "Brass Tee", `{"material":"brass","size":"2in"}`)
	mk("F3", "Zorptron Coupler", `{"material":"steel","size":"1in"}`)

	type facetValue struct {
		Value string `json:"value"`
		Count int64  `json:"count"`
	}
	type resp struct {
		Items  []struct{ Sku, Name string } `json:"items"`
		Total  int64                        `json:"total"`
		Facets []struct {
			Attr   string       `json:"attr"`
			Values []facetValue `json:"values"`
		} `json:"facets"`
	}
	get := func(qs string) resp {
		rr := do(t, h, http.MethodGet, "/storefront/catalog?"+qs, "", nil)
		if rr.Code != http.StatusOK {
			t.Fatalf("catalog %q: %d (%s)", qs, rr.Code, rr.Body.String())
		}
		var out resp
		if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
			t.Fatalf("decode: %v", err)
		}
		return out
	}
	facetCount := func(r resp, attr, value string) int64 {
		for _, f := range r.Facets {
			if f.Attr == attr {
				for _, v := range f.Values {
					if v.Value == value {
						return v.Count
					}
				}
			}
		}
		return -1
	}

	// All three in the category → facet counts material brass(2)/steel(1), size 1in(2)/2in(1).
	all := get("category=fittings")
	if all.Total != 3 {
		t.Fatalf("category total: want 3, got %d", all.Total)
	}
	if facetCount(all, "material", "brass") != 2 || facetCount(all, "material", "steel") != 1 {
		t.Errorf("material facet wrong: %+v", all.Facets)
	}
	if facetCount(all, "size", "1in") != 2 {
		t.Errorf("size facet wrong: %+v", all.Facets)
	}

	// Selecting a facet narrows the result set.
	brass := get(`category=fittings&filter=` + url.QueryEscape(`{"material":"brass"}`))
	if brass.Total != 2 {
		t.Errorf("material=brass total: want 2, got %d", brass.Total)
	}

	// Keyword narrows to one.
	kw := get("category=fittings&q=zorptron")
	if kw.Total != 1 || len(kw.Items) != 1 || kw.Items[0].Sku != "F3" {
		t.Errorf("keyword search: want only F3, got total=%d %+v", kw.Total, kw.Items)
	}

	// Sort=newest puts the last-created (F3) first.
	newest := get("category=fittings&sort=newest")
	if len(newest.Items) == 0 || newest.Items[0].Sku != "F3" {
		t.Errorf("sort=newest: want F3 first, got %+v", newest.Items)
	}
}

// ---- per-customer catalog visibility -------------------------------------

func TestCatalogVisibilityHidesProductsPerCustomer(t *testing.T) {
	h, issuer, pool := newServer(t)
	q := gen.New(pool)
	ctx := context.Background()
	admin := catalogToken(t, issuer)

	// Two active products.
	mk := func(sku, slug string) (int64, string) {
		rr := do(t, h, http.MethodPost, "/admin/products", admin, map[string]any{
			"sku": sku, "name": sku, "slug": slug, "status": "active",
		})
		if rr.Code != http.StatusCreated {
			t.Fatalf("create %s: %d (%s)", sku, rr.Code, rr.Body.String())
		}
		var p struct {
			ID   int64  `json:"id"`
			Slug string `json:"slug"`
		}
		_ = json.Unmarshal(rr.Body.Bytes(), &p)
		return p.ID, p.Slug
	}
	_, slugA := mk("VIS-A", "vis-a")
	idB, slugB := mk("VIS-B", "vis-b")

	cust, err := q.CreateCustomer(ctx, gen.CreateCustomerParams{OrganizationID: 1, Name: "Acme", CreditLimit: "0"})
	if err != nil {
		t.Fatalf("customer: %v", err)
	}
	custTok, _ := issuer.IssueStorefront(1, 1, cust.ID)

	_ = slugA
	// has reports whether the storefront list for a token includes a slug. The
	// seed DB has other demo products, so we assert membership, not counts.
	has := func(tok, slug string) bool {
		rr := do(t, h, http.MethodGet, "/storefront/products?page_size=200", tok, nil)
		var resp struct {
			Items []struct {
				Slug string `json:"slug"`
			} `json:"items"`
		}
		_ = json.Unmarshal(rr.Body.Bytes(), &resp)
		for _, it := range resp.Items {
			if it.Slug == slug {
				return true
			}
		}
		return false
	}

	// Before any rule the customer sees product B.
	if !has(custTok, slugB) {
		t.Fatalf("cust pre-rule: should see %s", slugB)
	}

	// Hide product B from this customer.
	cr := do(t, h, http.MethodPost, "/admin/products/"+strconv.FormatInt(idB, 10)+"/visibility", admin, map[string]any{
		"customer_id": cust.ID, "visible": false,
	})
	if cr.Code != http.StatusCreated {
		t.Fatalf("create visibility: %d (%s)", cr.Code, cr.Body.String())
	}
	var rule struct {
		ID int64 `json:"id"`
	}
	_ = json.Unmarshal(cr.Body.Bytes(), &rule)

	// The customer no longer sees B; anonymous still does.
	if has(custTok, slugB) {
		t.Errorf("cust post-rule: should NOT see %s", slugB)
	}
	if !has("", slugB) {
		t.Errorf("anon post-rule: should still see %s (rules are per-customer)", slugB)
	}

	// Detail for B: 404 for the customer, 200 for anonymous.
	if rr := do(t, h, http.MethodGet, "/storefront/products/"+slugB, custTok, nil); rr.Code != http.StatusNotFound {
		t.Errorf("cust detail hidden product: want 404, got %d", rr.Code)
	}
	if rr := do(t, h, http.MethodGet, "/storefront/products/"+slugB, "", nil); rr.Code != http.StatusOK {
		t.Errorf("anon detail: want 200, got %d", rr.Code)
	}

	// Admin can list + delete the rule.
	lr := do(t, h, http.MethodGet, "/admin/products/"+strconv.FormatInt(idB, 10)+"/visibility", admin, nil)
	var lresp struct {
		Items []any `json:"items"`
	}
	_ = json.Unmarshal(lr.Body.Bytes(), &lresp)
	if len(lresp.Items) != 1 {
		t.Errorf("list visibility: want 1, got %d", len(lresp.Items))
	}
	if del := do(t, h, http.MethodDelete, "/admin/catalog-visibility/"+strconv.FormatInt(rule.ID, 10), admin, nil); del.Code != http.StatusNoContent {
		t.Errorf("delete visibility: want 204, got %d", del.Code)
	}
	// After delete, the customer sees B again.
	if !has(custTok, slugB) {
		t.Errorf("cust after delete: should see %s again", slugB)
	}
}

func TestCatalogVisibilityByGroup(t *testing.T) {
	h, issuer, pool := newServer(t)
	q := gen.New(pool)
	ctx := context.Background()
	admin := catalogToken(t, issuer)

	rr := do(t, h, http.MethodPost, "/admin/products", admin, map[string]any{"sku": "GV-1", "name": "GV1", "slug": "gv-1", "status": "active"})
	var p struct {
		ID int64 `json:"id"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &p)

	grp, err := q.CreateCustomerGroup(ctx, gen.CreateCustomerGroupParams{OrganizationID: 1, Name: "Wholesale"})
	if err != nil {
		t.Fatalf("group: %v", err)
	}
	cust, err := q.CreateCustomer(ctx, gen.CreateCustomerParams{OrganizationID: 1, Name: "Acme", CreditLimit: "0", CustomerGroupID: &grp.ID})
	if err != nil {
		t.Fatalf("customer: %v", err)
	}
	custTok, _ := issuer.IssueStorefront(1, 1, cust.ID)

	// Hide from the GROUP, not the customer directly.
	if cr := do(t, h, http.MethodPost, "/admin/products/"+strconv.FormatInt(p.ID, 10)+"/visibility", admin, map[string]any{
		"customer_group_id": grp.ID, "visible": false,
	}); cr.Code != http.StatusCreated {
		t.Fatalf("group rule: %d (%s)", cr.Code, cr.Body.String())
	}

	lr := do(t, h, http.MethodGet, "/storefront/products?page_size=200", custTok, nil)
	var resp struct {
		Items []struct {
			Slug string `json:"slug"`
		} `json:"items"`
	}
	_ = json.Unmarshal(lr.Body.Bytes(), &resp)
	for _, it := range resp.Items {
		if it.Slug == "gv-1" {
			t.Errorf("group-hidden product gv-1 still visible to a group member")
		}
	}
}

// ---- per-warehouse availability ------------------------------------------

func TestStorefrontPerWarehouseAvailability(t *testing.T) {
	h, issuer, pool := newServer(t)
	q := gen.New(pool)
	ctx := context.Background()
	admin := catalogToken(t, issuer)

	rr := do(t, h, http.MethodPost, "/admin/products", admin, map[string]any{"sku": "WH-1", "name": "WH1", "slug": "wh-1", "status": "active"})
	var p struct {
		ID int64 `json:"id"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &p)

	wA, _ := q.CreateWarehouse(ctx, gen.CreateWarehouseParams{OrganizationID: 1, Name: "Main"})
	wB, _ := q.CreateWarehouse(ctx, gen.CreateWarehouseParams{OrganizationID: 1, Name: "East"})
	for _, w := range []int64{wA.ID, wB.ID} {
		_ = q.EnsureInventoryLevel(ctx, gen.EnsureInventoryLevelParams{ProductID: p.ID, WarehouseID: w})
	}
	_, _ = q.AdjustInventoryLevel(ctx, gen.AdjustInventoryLevelParams{ProductID: p.ID, WarehouseID: wA.ID, Column3: "40", Column4: "0"})
	_, _ = q.AdjustInventoryLevel(ctx, gen.AdjustInventoryLevelParams{ProductID: p.ID, WarehouseID: wB.ID, Column3: "10", Column4: "3"})

	av := do(t, h, http.MethodGet, "/storefront/products/wh-1/availability", "", nil)
	if av.Code != http.StatusOK {
		t.Fatalf("availability: %d (%s)", av.Code, av.Body.String())
	}
	var resp struct {
		Warehouses []struct {
			WarehouseName string `json:"warehouse_name"`
			Available     string `json:"available"`
		} `json:"warehouses"`
	}
	_ = json.Unmarshal(av.Body.Bytes(), &resp)
	if len(resp.Warehouses) != 2 {
		t.Fatalf("want 2 warehouses, got %d (%+v)", len(resp.Warehouses), resp.Warehouses)
	}
	got := map[string]string{}
	for _, w := range resp.Warehouses {
		got[w.WarehouseName] = w.Available
	}
	if got["Main"] != "40.0000" || got["East"] != "7.0000" {
		t.Errorf("availability: want Main 40, East 7 (10-3), got %+v", got)
	}
}
