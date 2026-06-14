package promo_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"b2bcommerce/internal/auth"
	"b2bcommerce/internal/server"
	"b2bcommerce/internal/store"
	"b2bcommerce/internal/testsupport"
)

const testSecret = "test-secret-please-change"

func newServer(t *testing.T) (http.Handler, *auth.Issuer) {
	t.Helper()
	pool := testsupport.NewDB(t)
	st := store.New(pool)
	issuer := auth.NewIssuer(testSecret, time.Hour)
	return server.New(st, issuer), issuer
}

func token(t *testing.T, issuer *auth.Issuer, perms ...string) string {
	t.Helper()
	tok, err := issuer.Issue("1", 1, "admin", perms)
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}
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

func TestPromotionCRUD(t *testing.T) {
	h, issuer := newServer(t)
	tok := token(t, issuer, "promotion.view", "promotion.manage")

	// Create.
	cr := do(t, h, http.MethodPost, "/admin/promotions", tok, map[string]any{
		"name": "Spring Sale", "code": "SPRING10", "discount_type": "percent", "discount_value": "10",
	})
	if cr.Code != http.StatusCreated {
		t.Fatalf("create: want 201, got %d (%s)", cr.Code, cr.Body.String())
	}
	var created struct {
		ID   int64  `json:"id"`
		Code string `json:"code"`
	}
	_ = json.Unmarshal(cr.Body.Bytes(), &created)
	if created.ID == 0 || created.Code != "SPRING10" {
		t.Fatalf("unexpected create body: %s", cr.Body.String())
	}

	// List shows it.
	lr := do(t, h, http.MethodGet, "/admin/promotions", tok, nil)
	var list struct {
		Items []struct {
			ID int64 `json:"id"`
		} `json:"items"`
	}
	_ = json.Unmarshal(lr.Body.Bytes(), &list)
	if len(list.Items) != 1 {
		t.Fatalf("list: want 1, got %d", len(list.Items))
	}

	// Update (deactivate).
	idPath := "/admin/promotions/" + strconv.FormatInt(created.ID, 10)
	ur := do(t, h, http.MethodPut, idPath, tok, map[string]any{
		"name": "Spring Sale", "code": "SPRING10", "discount_type": "percent", "discount_value": "15", "is_active": false,
	})
	if ur.Code != http.StatusOK {
		t.Fatalf("update: want 200, got %d (%s)", ur.Code, ur.Body.String())
	}

	// Delete.
	dr := do(t, h, http.MethodDelete, idPath, tok, nil)
	if dr.Code != http.StatusNoContent {
		t.Fatalf("delete: want 204, got %d (%s)", dr.Code, dr.Body.String())
	}
	dr2 := do(t, h, http.MethodDelete, idPath, tok, nil)
	if dr2.Code != http.StatusNotFound {
		t.Fatalf("delete again: want 404, got %d", dr2.Code)
	}
}

func TestPromotionValidation(t *testing.T) {
	h, issuer := newServer(t)
	tok := token(t, issuer, "promotion.view", "promotion.manage")

	cases := []map[string]any{
		{"name": "", "discount_type": "percent", "discount_value": "10"},   // missing name
		{"name": "X", "discount_type": "bogus", "discount_value": "10"},    // bad type
		{"name": "X", "discount_type": "percent", "discount_value": "150"}, // >100%
		{"name": "X", "discount_type": "amount", "discount_value": "-5"},   // negative
	}
	for i, body := range cases {
		rr := do(t, h, http.MethodPost, "/admin/promotions", tok, body)
		if rr.Code != http.StatusBadRequest {
			t.Errorf("case %d: want 400, got %d (%s)", i, rr.Code, rr.Body.String())
		}
	}
}

func TestPromotionDuplicateCode(t *testing.T) {
	h, issuer := newServer(t)
	tok := token(t, issuer, "promotion.view", "promotion.manage")
	body := map[string]any{"name": "A", "code": "DUP", "discount_type": "amount", "discount_value": "5"}
	if rr := do(t, h, http.MethodPost, "/admin/promotions", tok, body); rr.Code != http.StatusCreated {
		t.Fatalf("first create: %d (%s)", rr.Code, rr.Body.String())
	}
	if rr := do(t, h, http.MethodPost, "/admin/promotions", tok, body); rr.Code != http.StatusConflict {
		t.Fatalf("duplicate code: want 409, got %d (%s)", rr.Code, rr.Body.String())
	}
}

func TestPromotionRequiresPermission(t *testing.T) {
	h, issuer := newServer(t)
	// A token without promotion perms must be forbidden.
	tok := token(t, issuer, "product.view")
	if rr := do(t, h, http.MethodGet, "/admin/promotions", tok, nil); rr.Code != http.StatusForbidden {
		t.Fatalf("no perm: want 403, got %d", rr.Code)
	}
}
