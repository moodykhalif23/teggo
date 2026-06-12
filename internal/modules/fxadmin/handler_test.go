package fxadmin_test

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
	return server.New(store.New(pool), auth.NewIssuer(testSecret, time.Hour)), auth.NewIssuer(testSecret, time.Hour)
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

func TestFxRateCRUD(t *testing.T) {
	h, issuer := newServer(t)
	tok := token(t, issuer, "fx.view", "fx.manage")

	cr := do(t, h, http.MethodPost, "/admin/fx-rates", tok, map[string]any{"base_currency": "kes", "quote_currency": "usd", "rate": "0.00750000"})
	if cr.Code != http.StatusCreated {
		t.Fatalf("create: want 201, got %d (%s)", cr.Code, cr.Body.String())
	}
	var created struct {
		ID            int64  `json:"id"`
		BaseCurrency  string `json:"base_currency"`
		QuoteCurrency string `json:"quote_currency"`
	}
	_ = json.Unmarshal(cr.Body.Bytes(), &created)
	if created.BaseCurrency != "KES" || created.QuoteCurrency != "USD" {
		t.Fatalf("codes should upper-case: %s", cr.Body.String())
	}

	// A newer rate for the same pair → list still shows one (latest) row.
	_ = do(t, h, http.MethodPost, "/admin/fx-rates", tok, map[string]any{"base_currency": "KES", "quote_currency": "USD", "rate": "0.00800000"})
	lr := do(t, h, http.MethodGet, "/admin/fx-rates", tok, nil)
	var list struct {
		Items []struct {
			ID   int64  `json:"id"`
			Rate string `json:"rate"`
		} `json:"items"`
	}
	_ = json.Unmarshal(lr.Body.Bytes(), &list)
	if len(list.Items) != 1 {
		t.Fatalf("list latest: want 1 pair, got %d (%s)", len(list.Items), lr.Body.String())
	}

	// Delete.
	dr := do(t, h, http.MethodDelete, "/admin/fx-rates/"+strconv.FormatInt(list.Items[0].ID, 10), tok, nil)
	if dr.Code != http.StatusNoContent {
		t.Fatalf("delete: want 204, got %d", dr.Code)
	}
}

func TestFxRateValidation(t *testing.T) {
	h, issuer := newServer(t)
	tok := token(t, issuer, "fx.view", "fx.manage")
	bad := []map[string]any{
		{"base_currency": "USD", "quote_currency": "USD", "rate": "1"}, // same
		{"base_currency": "US", "quote_currency": "EUR", "rate": "1"},  // bad code
		{"base_currency": "USD", "quote_currency": "EUR", "rate": "0"}, // non-positive
		{"base_currency": "USD", "quote_currency": "EUR", "rate": "x"}, // non-numeric
	}
	for i, b := range bad {
		if rr := do(t, h, http.MethodPost, "/admin/fx-rates", tok, b); rr.Code != http.StatusBadRequest {
			t.Errorf("case %d: want 400, got %d", i, rr.Code)
		}
	}
	// Permission gate.
	noPerm := token(t, issuer, "product.view")
	if rr := do(t, h, http.MethodGet, "/admin/fx-rates", noPerm, nil); rr.Code != http.StatusForbidden {
		t.Errorf("no perm: want 403, got %d", rr.Code)
	}
}
