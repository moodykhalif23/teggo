package merch_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"b2bcommerce/internal/auth"
	"b2bcommerce/internal/server"
	"b2bcommerce/internal/store"
	"b2bcommerce/internal/store/gen"
	"b2bcommerce/internal/testsupport"
)

const testSecret = "test-secret-please-change"

func newServer(t *testing.T) (http.Handler, *auth.Issuer, *gen.Queries) {
	t.Helper()
	pool := testsupport.NewDB(t)
	return server.New(store.New(pool), auth.NewIssuer(testSecret, time.Hour)), auth.NewIssuer(testSecret, time.Hour), gen.New(pool)
}

func token(t *testing.T, issuer *auth.Issuer, perms ...string) string {
	t.Helper()
	tok, _ := issuer.Issue("1", 1, "admin", perms)
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

func TestMerchandisingCRUD(t *testing.T) {
	h, issuer, q := newServer(t)
	tok := token(t, issuer, "merchandising.view", "merchandising.manage")

	// Synonym.
	if rr := do(t, h, http.MethodPost, "/admin/search-synonyms", tok, map[string]any{"term": "tee", "synonyms": "t-shirt shirt"}); rr.Code != http.StatusCreated {
		t.Fatalf("synonym: %d (%s)", rr.Code, rr.Body.String())
	}
	// Redirect.
	if rr := do(t, h, http.MethodPost, "/admin/search-redirects", tok, map[string]any{"query": "sale", "target": "/c/clearance"}); rr.Code != http.StatusCreated {
		t.Fatalf("redirect: %d (%s)", rr.Code, rr.Body.String())
	}
	// Rule (needs a product).
	p, _ := q.CreateProduct(context.Background(), gen.CreateProductParams{OrganizationID: 1, Sku: "M1", Type: "simple", Name: "M1", Slug: "m1", Status: "active", Attributes: []byte("{}"), Unit: "each"})
	cr := do(t, h, http.MethodPost, "/admin/merchandising-rules", tok, map[string]any{"scope_type": "query", "scope_value": "tee", "product_id": p.ID, "action": "pin", "position": 0})
	if cr.Code != http.StatusCreated {
		t.Fatalf("rule: %d (%s)", cr.Code, cr.Body.String())
	}

	// Each list shows one.
	for _, path := range []string{"/admin/search-synonyms", "/admin/search-redirects", "/admin/merchandising-rules"} {
		lr := do(t, h, http.MethodGet, path, tok, nil)
		var list struct {
			Items []json.RawMessage `json:"items"`
		}
		_ = json.Unmarshal(lr.Body.Bytes(), &list)
		if len(list.Items) != 1 {
			t.Fatalf("%s list: want 1, got %d", path, len(list.Items))
		}
	}

	// Validation: bad action.
	if rr := do(t, h, http.MethodPost, "/admin/merchandising-rules", tok, map[string]any{"scope_type": "query", "scope_value": "x", "product_id": p.ID, "action": "nope"}); rr.Code != http.StatusBadRequest {
		t.Errorf("bad action: want 400, got %d", rr.Code)
	}
	// Permission gate.
	if rr := do(t, h, http.MethodGet, "/admin/search-synonyms", token(t, issuer, "product.view"), nil); rr.Code != http.StatusForbidden {
		t.Errorf("no perm: want 403, got %d", rr.Code)
	}
}
