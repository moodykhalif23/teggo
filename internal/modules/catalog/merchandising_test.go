package catalog_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"b2bcommerce/internal/store/gen"
)

func mkActiveProduct(t *testing.T, q *gen.Queries, sku, name, slug string) int64 {
	t.Helper()
	p, err := q.CreateProduct(context.Background(), gen.CreateProductParams{
		OrganizationID: 1, Sku: sku, Type: "simple", Name: name, Slug: slug,
		Status: "active", Attributes: []byte("{}"), Unit: "each",
	})
	if err != nil {
		t.Fatalf("product %s: %v", sku, err)
	}
	return p.ID
}

type facetedResp struct {
	Redirect string `json:"redirect"`
	Items    []struct {
		SKU  string `json:"sku"`
		Slug string `json:"slug"`
	} `json:"items"`
}

// TestMerchandisingPinReordersResults: pinning a product floats it to the top of
// the search results for its query (default order is by name).
func TestMerchandisingPinReordersResults(t *testing.T) {
	h, _, pool := newServer(t)
	q := gen.New(pool)
	ctx := context.Background()

	// Two products matching "zorptron"; by name Alpha sorts before Beta.
	mkActiveProduct(t, q, "ZA", "Alpha Zorptron", "alpha-zorptron")
	beta := mkActiveProduct(t, q, "ZB", "Beta Zorptron", "beta-zorptron")

	// Baseline: Alpha first.
	var base facetedResp
	rr := do(t, h, http.MethodGet, "/storefront/catalog?q=zorptron", "", nil)
	_ = json.Unmarshal(rr.Body.Bytes(), &base)
	if len(base.Items) != 2 || base.Items[0].SKU != "ZA" {
		t.Fatalf("baseline: want [ZA, ZB], got %+v", base.Items)
	}

	// Pin Beta for query "zorptron".
	if _, err := q.CreateMerchandisingRule(ctx, gen.CreateMerchandisingRuleParams{
		OrganizationID: 1, ScopeType: "query", ScopeValue: "zorptron", ProductID: beta, Action: "pin", Position: 0,
	}); err != nil {
		t.Fatalf("rule: %v", err)
	}

	var after facetedResp
	rr = do(t, h, http.MethodGet, "/storefront/catalog?q=zorptron", "", nil)
	_ = json.Unmarshal(rr.Body.Bytes(), &after)
	if len(after.Items) != 2 || after.Items[0].SKU != "ZB" {
		t.Fatalf("after pin: want Beta first, got %+v", after.Items)
	}
}

// TestMerchandisingRedirect: a query redirect short-circuits search.
func TestMerchandisingRedirect(t *testing.T) {
	h, _, pool := newServer(t)
	q := gen.New(pool)
	if _, err := q.UpsertSearchRedirect(context.Background(), gen.UpsertSearchRedirectParams{OrganizationID: 1, Query: "clearance", Target: "/c/sale"}); err != nil {
		t.Fatalf("redirect: %v", err)
	}
	rr := do(t, h, http.MethodGet, "/storefront/catalog?q=clearance", "", nil)
	var resp facetedResp
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)
	if resp.Redirect != "/c/sale" {
		t.Fatalf("redirect: want /c/sale, got %q (%s)", resp.Redirect, rr.Body.String())
	}
}

// TestMerchandisingSynonym: a synonym expands the query so FTS matches.
func TestMerchandisingSynonym(t *testing.T) {
	h, _, pool := newServer(t)
	q := gen.New(pool)
	ctx := context.Background()
	mkActiveProduct(t, q, "GZ", "Gizmo Zorptron", "gizmo-zorptron")

	// No product is named "thingamajig"; the synonym expands it to "zorptron".
	if _, err := q.UpsertSearchSynonym(ctx, gen.UpsertSearchSynonymParams{OrganizationID: 1, Term: "thingamajig", Synonyms: "zorptron"}); err != nil {
		t.Fatalf("synonym: %v", err)
	}

	// Without the synonym the term matches nothing; with it, it finds the product.
	rr := do(t, h, http.MethodGet, "/storefront/catalog?q=thingamajig", "", nil)
	var resp facetedResp
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)
	if len(resp.Items) != 1 || resp.Items[0].SKU != "GZ" {
		t.Fatalf("synonym expansion: want [GZ], got %+v (%s)", resp.Items, rr.Body.String())
	}
}
