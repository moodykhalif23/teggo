package catalog_test

import (
	"encoding/json"
	"net/http"
	"strconv"
	"testing"
)

// TestProductTranslations exercises the i18n path: admin sets a French
// translation, storefront reads return it for ?locale=fr and fall back to the
// base content otherwise — on both the detail and faceted-search endpoints.
func TestProductTranslations(t *testing.T) {
	h, issuer, _ := newServer(t)
	tok := catalogToken(t, issuer)

	// Create a product with a distinctive base name.
	cr := do(t, h, http.MethodPost, "/admin/products", tok, map[string]any{
		"sku": "I18N-1", "name": "Flibberty", "slug": "i18n-flibberty", "status": "active",
	})
	if cr.Code != http.StatusCreated {
		t.Fatalf("create: %d (%s)", cr.Code, cr.Body.String())
	}
	var created struct {
		ID int64 `json:"id"`
	}
	_ = json.Unmarshal(cr.Body.Bytes(), &created)

	// Add a French translation.
	tr := do(t, h, http.MethodPut, "/admin/products/"+strconv.FormatInt(created.ID, 10)+"/translations", tok, map[string]any{
		"locale": "fr", "name": "Flibustier", "description": "Description française",
	})
	if tr.Code != http.StatusOK {
		t.Fatalf("upsert translation: %d (%s)", tr.Code, tr.Body.String())
	}

	type prod struct {
		Name        string  `json:"name"`
		Description *string `json:"description"`
	}
	get := func(url string) prod {
		rr := do(t, h, http.MethodGet, url, "", nil)
		var p prod
		_ = json.Unmarshal(rr.Body.Bytes(), &p)
		return p
	}

	// Detail: base, fr, and unknown-locale fallback.
	if p := get("/storefront/products/i18n-flibberty"); p.Name != "Flibberty" {
		t.Errorf("base detail: want Flibberty, got %q", p.Name)
	}
	if p := get("/storefront/products/i18n-flibberty?locale=fr"); p.Name != "Flibustier" {
		t.Errorf("fr detail: want Flibustier, got %q", p.Name)
	}
	if p := get("/storefront/products/i18n-flibberty?locale=zz"); p.Name != "Flibberty" {
		t.Errorf("unknown locale should fall back to base, got %q", p.Name)
	}

	// Faceted search: the FTS matches the base name; the returned name is localized.
	var resp struct {
		Items []struct {
			SKU  string `json:"sku"`
			Name string `json:"name"`
		} `json:"items"`
	}
	rr := do(t, h, http.MethodGet, "/storefront/catalog?q=Flibberty&locale=fr", "", nil)
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)
	if len(resp.Items) != 1 || resp.Items[0].Name != "Flibustier" {
		t.Fatalf("faceted fr: want [Flibustier], got %+v (%s)", resp.Items, rr.Body.String())
	}

	// Admin list shows the translation; delete removes it.
	lr := do(t, h, http.MethodGet, "/admin/products/"+strconv.FormatInt(created.ID, 10)+"/translations", tok, nil)
	var list struct {
		Items []struct {
			Locale string `json:"locale"`
		} `json:"items"`
	}
	_ = json.Unmarshal(lr.Body.Bytes(), &list)
	if len(list.Items) != 1 || list.Items[0].Locale != "fr" {
		t.Fatalf("admin list: want [fr], got %+v", list.Items)
	}
	if dr := do(t, h, http.MethodDelete, "/admin/products/"+strconv.FormatInt(created.ID, 10)+"/translations/fr", tok, nil); dr.Code != http.StatusNoContent {
		t.Fatalf("delete: want 204, got %d", dr.Code)
	}
}
