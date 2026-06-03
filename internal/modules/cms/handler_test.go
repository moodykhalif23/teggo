package cms_test

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
	st := store.New(pool)
	issuer := auth.NewIssuer(testSecret, time.Hour)
	return server.New(st, issuer), issuer, pool
}

func cmsToken(t *testing.T, issuer *auth.Issuer) string {
	t.Helper()
	tok, _ := issuer.Issue("1", 1, "admin", []string{"cms.view", "cms.manage"})
	return tok
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

func createPage(t *testing.T, h http.Handler, tok string, body map[string]any) int64 {
	t.Helper()
	rr := do(t, h, http.MethodPost, "/admin/pages", tok, body)
	if rr.Code != http.StatusCreated {
		t.Fatalf("create page: %d (%s)", rr.Code, rr.Body.String())
	}
	var p struct {
		ID int64 `json:"id"`
	}
	decode(t, rr, &p)
	return p.ID
}

func TestPagePublishLifecycle(t *testing.T) {
	h, issuer, _ := newServer(t)
	tok := cmsToken(t, issuer)

	id := createPage(t, h, tok, map[string]any{
		"title": "Promo", "slug": "promo",
		"blocks": []map[string]any{{"type": "rich-text", "id": "b1", "props": map[string]any{"html": "<p>Hi</p>"}}},
	})

	// Draft is not served publicly.
	if rr := do(t, h, http.MethodGet, "/storefront/pages/promo", "", nil); rr.Code != http.StatusNotFound {
		t.Fatalf("draft page should 404 publicly, got %d", rr.Code)
	}
	// Publish, then it is served.
	if rr := do(t, h, http.MethodPost, "/admin/pages/"+strconv.FormatInt(id, 10)+"/publish", tok, nil); rr.Code != http.StatusOK {
		t.Fatalf("publish: %d (%s)", rr.Code, rr.Body.String())
	}
	pub := do(t, h, http.MethodGet, "/storefront/pages/promo", "", nil)
	if pub.Code != http.StatusOK {
		t.Fatalf("published page: want 200, got %d", pub.Code)
	}
	var page struct {
		Status string `json:"status"`
		Blocks []any  `json:"blocks"`
	}
	decode(t, pub, &page)
	if page.Status != "published" || len(page.Blocks) != 1 {
		t.Errorf("page: want published with 1 block, got %s/%d", page.Status, len(page.Blocks))
	}

	// Archive → 404 again.
	do(t, h, http.MethodPost, "/admin/pages/"+strconv.FormatInt(id, 10)+"/archive", tok, nil)
	if rr := do(t, h, http.MethodGet, "/storefront/pages/promo", "", nil); rr.Code != http.StatusNotFound {
		t.Errorf("archived page should 404, got %d", rr.Code)
	}
}

func TestPageSlugUniqueAndBlockValidation(t *testing.T) {
	h, issuer, _ := newServer(t)
	tok := cmsToken(t, issuer)

	createPage(t, h, tok, map[string]any{"title": "A", "slug": "dup", "blocks": []any{}})
	// Duplicate slug on the same website → conflict.
	if rr := do(t, h, http.MethodPost, "/admin/pages", tok, map[string]any{"title": "B", "slug": "dup", "blocks": []any{}}); rr.Code != http.StatusConflict {
		t.Errorf("duplicate slug: want 409, got %d", rr.Code)
	}
	// Unknown block type → 400.
	if rr := do(t, h, http.MethodPost, "/admin/pages", tok, map[string]any{
		"title": "C", "slug": "badblocks",
		"blocks": []map[string]any{{"type": "explode", "id": "x", "props": map[string]any{}}},
	}); rr.Code != http.StatusBadRequest {
		t.Errorf("unknown block type: want 400, got %d", rr.Code)
	}
}

func TestPageTargetingByCustomerGroup(t *testing.T) {
	h, issuer, pool := newServer(t)
	tok := cmsToken(t, issuer)
	q := gen.New(pool)
	ctx := context.Background()

	grp, _ := q.CreateCustomerGroup(ctx, gen.CreateCustomerGroupParams{OrganizationID: 1, Name: "Dealers"})
	// A customer in the group + a login token.
	inCust, _ := q.CreateCustomer(ctx, gen.CreateCustomerParams{OrganizationID: 1, Name: "Dealer Co", CreditLimit: "0"})
	_, _ = pool.Exec(ctx, `UPDATE customers SET customer_group_id=$1 WHERE id=$2`, grp.ID, inCust.ID)
	inTok, _ := issuer.IssueStorefront(0, 1, inCust.ID)
	// A customer not in the group.
	outCust, _ := q.CreateCustomer(ctx, gen.CreateCustomerParams{OrganizationID: 1, Name: "Other Co", CreditLimit: "0"})
	outTok, _ := issuer.IssueStorefront(0, 1, outCust.ID)

	id := createPage(t, h, tok, map[string]any{
		"title": "Dealer Deal", "slug": "dealer-deal", "blocks": []any{},
		"target_customer_group_id": grp.ID,
	})
	do(t, h, http.MethodPost, "/admin/pages/"+strconv.FormatInt(id, 10)+"/publish", tok, nil)

	// Anonymous → 404 (targeted).
	if rr := do(t, h, http.MethodGet, "/storefront/pages/dealer-deal", "", nil); rr.Code != http.StatusNotFound {
		t.Errorf("anon on targeted page: want 404, got %d", rr.Code)
	}
	// In-group customer → 200.
	if rr := do(t, h, http.MethodGet, "/storefront/pages/dealer-deal", inTok, nil); rr.Code != http.StatusOK {
		t.Errorf("in-group customer: want 200, got %d (%s)", rr.Code, rr.Body.String())
	}
	// Out-of-group customer → 404.
	if rr := do(t, h, http.MethodGet, "/storefront/pages/dealer-deal", outTok, nil); rr.Code != http.StatusNotFound {
		t.Errorf("out-of-group customer: want 404, got %d", rr.Code)
	}
}

func TestProductGridResolution(t *testing.T) {
	h, issuer, pool := newServer(t)
	tok := cmsToken(t, issuer)
	q := gen.New(pool)
	ctx := context.Background()

	cat, _ := q.CreateCategory(ctx, gen.CreateCategoryParams{OrganizationID: 1, Name: "Valves", Slug: "valves", SortOrder: 1})
	p, _ := q.CreateProduct(ctx, gen.CreateProductParams{OrganizationID: 1, Sku: "PG1", Type: "simple", Name: "Grid Product", Slug: "grid-product", Status: "active", Attributes: []byte("{}"), Unit: "each"})
	_ = q.AssignProductToCategory(ctx, gen.AssignProductToCategoryParams{ProductID: p.ID, CategoryID: cat.ID})

	id := createPage(t, h, tok, map[string]any{
		"title": "Shop", "slug": "shop",
		"blocks": []map[string]any{{
			"type": "product-grid", "id": "g1",
			"props": map[string]any{"source": map[string]any{"kind": "category", "category_id": cat.ID, "limit": 8}},
		}},
	})
	do(t, h, http.MethodPost, "/admin/pages/"+strconv.FormatInt(id, 10)+"/publish", tok, nil)

	rr := do(t, h, http.MethodGet, "/storefront/pages/shop", "", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("get page: %d (%s)", rr.Code, rr.Body.String())
	}
	var page struct {
		Blocks []struct {
			Type  string `json:"type"`
			Props struct {
				Products []struct {
					Sku string `json:"sku"`
				} `json:"products"`
			} `json:"props"`
		} `json:"blocks"`
	}
	decode(t, rr, &page)
	if len(page.Blocks) != 1 || len(page.Blocks[0].Props.Products) != 1 || page.Blocks[0].Props.Products[0].Sku != "PG1" {
		t.Fatalf("product-grid not resolved: %+v", page.Blocks)
	}
}

func TestStorefrontMenuAndAuth(t *testing.T) {
	h, issuer, _ := newServer(t)

	// Seeded main menu is served publicly with items.
	rr := do(t, h, http.MethodGet, "/storefront/menus/main", "", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("menu: %d (%s)", rr.Code, rr.Body.String())
	}
	var menu struct {
		Items []any `json:"items"`
	}
	decode(t, rr, &menu)
	if len(menu.Items) == 0 {
		t.Error("seeded main menu should have items")
	}

	// Admin pages require cms.view; a storefront token is the wrong audience.
	cust, _ := issuer.IssueStorefront(0, 1, 1)
	if rr := do(t, h, http.MethodGet, "/admin/pages", cust, nil); rr.Code != http.StatusForbidden {
		t.Errorf("storefront token on /admin/pages: want 403, got %d", rr.Code)
	}
	noPerm, _ := issuer.Issue("1", 1, "admin", []string{"order.view"})
	if rr := do(t, h, http.MethodGet, "/admin/pages", noPerm, nil); rr.Code != http.StatusForbidden {
		t.Errorf("missing cms.view: want 403, got %d", rr.Code)
	}
}
