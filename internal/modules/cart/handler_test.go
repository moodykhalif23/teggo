package cart_test

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
	"b2bcommerce/internal/queue/jobs"
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

const custPassword = "buyer-pass-123"

type seed struct {
	customerID     int64
	email          string
	pricedPublicID string
	freePublicID   string // a product with no resolvable price
	productPriced  int64
	productFree    int64
	pricedSKU      string
	freeSKU        string
}

// seedCustomer creates a customer + login + a customer-assigned price list with
// one priced product, then warms combined_prices via the recompute job.
func seedCustomer(t *testing.T, pool *pgxpool.Pool, name, email string) seed {
	t.Helper()
	q := gen.New(pool)
	ctx := context.Background()

	cust, err := q.CreateCustomer(ctx, gen.CreateCustomerParams{OrganizationID: 1, Name: name, CreditLimit: "0"})
	if err != nil {
		t.Fatalf("customer: %v", err)
	}
	hash, _ := auth.HashPassword(custPassword)
	if _, err := q.CreateCustomerUser(ctx, gen.CreateCustomerUserParams{
		CustomerID: cust.ID, Email: email, PasswordHash: hash, FullName: name + " Buyer", Role: "buyer",
	}); err != nil {
		t.Fatalf("customer user: %v", err)
	}

	priced, err := q.CreateProduct(ctx, gen.CreateProductParams{
		OrganizationID: 1, Sku: name + "-PRICED", Type: "simple", Name: "Priced", Slug: name + "-priced",
		Status: "active", Attributes: []byte("{}"), Unit: "each",
	})
	if err != nil {
		t.Fatalf("priced product: %v", err)
	}
	free, err := q.CreateProduct(ctx, gen.CreateProductParams{
		OrganizationID: 1, Sku: name + "-FREE", Type: "simple", Name: "Unpriced", Slug: name + "-free",
		Status: "active", Attributes: []byte("{}"), Unit: "each",
	})
	if err != nil {
		t.Fatalf("free product: %v", err)
	}

	list, err := q.CreatePriceList(ctx, gen.CreatePriceListParams{OrganizationID: 1, Name: name + " List", Currency: "USD", IsActive: true})
	if err != nil {
		t.Fatalf("price list: %v", err)
	}
	if _, err := q.UpsertPrice(ctx, gen.UpsertPriceParams{PriceListID: list.ID, ProductID: priced.ID, Unit: "each", MinQuantity: "1", Value: "10.0000"}); err != nil {
		t.Fatalf("price: %v", err)
	}
	if _, err := q.CreatePriceListAssignment(ctx, gen.CreatePriceListAssignmentParams{PriceListID: list.ID, CustomerID: &cust.ID}); err != nil {
		t.Fatalf("assign: %v", err)
	}
	wid := int64(1)
	if err := jobs.RecomputeForCustomer(ctx, pool, jobs.RecomputeCombinedPricesArgs{CustomerID: cust.ID, WebsiteID: &wid, Currency: "USD"}); err != nil {
		t.Fatalf("recompute: %v", err)
	}

	return seed{
		customerID: cust.ID, email: email,
		pricedPublicID: priced.PublicID.String(), freePublicID: free.PublicID.String(),
		productPriced: priced.ID, productFree: free.ID,
		pricedSKU: priced.Sku, freeSKU: free.Sku,
	}
}

// seedOrder creates a placed order for the customer with the given line items
// (productID -> quantity), snapshotting sku/name from the product.
func seedOrder(t *testing.T, pool *pgxpool.Pool, s seed, lines map[int64]string) gen.Order {
	t.Helper()
	q := gen.New(pool)
	ctx := context.Background()
	order, err := q.CreateOrder(ctx, gen.CreateOrderParams{
		OrganizationID: 1, WebsiteID: 1, CustomerID: s.customerID, Currency: "USD",
		BillingAddress: []byte("{}"), ShippingAddress: []byte("{}"),
		Subtotal: "0", TaxTotal: "0", ShippingTotal: "0", GrandTotal: "0",
	})
	if err != nil {
		t.Fatalf("create order: %v", err)
	}
	for productID, qty := range lines {
		p, err := q.GetProductByID(ctx, gen.GetProductByIDParams{OrganizationID: 1, ID: productID})
		if err != nil {
			t.Fatalf("get product: %v", err)
		}
		if _, err := q.AddOrderItem(ctx, gen.AddOrderItemParams{
			OrderID: order.ID, ProductID: productID, Sku: p.Sku, Name: p.Name,
			Quantity: qty, Unit: "each", UnitPrice: "10.0000", TaxAmount: "0", RowTotal: "10.0000",
		}); err != nil {
			t.Fatalf("add order item: %v", err)
		}
	}
	return order
}

func login(t *testing.T, h http.Handler, email string) string {
	t.Helper()
	rr := do(t, h, http.MethodPost, "/storefront/auth/login", "", map[string]any{"email": email, "password": custPassword, "org_id": 1})
	if rr.Code != http.StatusOK {
		t.Fatalf("login %s: want 200, got %d (%s)", email, rr.Code, rr.Body.String())
	}
	var resp struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)
	if resp.Token == "" {
		t.Fatal("empty token")
	}
	return resp.Token
}

type cartResp struct {
	Subtotal string `json:"subtotal"`
	Currency string `json:"currency"`
	Items    []struct {
		ID        int64  `json:"id"`
		UnitPrice string `json:"unit_price"`
		RowTotal  string `json:"row_total"`
		Quantity  string `json:"quantity"`
	} `json:"items"`
}

// ---- Auth + audience -----------------------------------------------------

func TestStorefrontLoginAndAudienceGate(t *testing.T) {
	h, issuer, pool := newServer(t)
	s := seedCustomer(t, pool, "acme", "buyer@acme.test")

	// Bad credentials.
	if rr := do(t, h, http.MethodPost, "/storefront/auth/login", "", map[string]any{"email": s.email, "password": "wrong", "org_id": 1}); rr.Code != http.StatusUnauthorized {
		t.Fatalf("bad creds: want 401, got %d", rr.Code)
	}
	// No token on a storefront route.
	if rr := do(t, h, http.MethodGet, "/storefront/cart", "", nil); rr.Code != http.StatusUnauthorized {
		t.Fatalf("no token: want 401, got %d", rr.Code)
	}
	// An admin-audience token must be rejected on storefront routes.
	adminTok, _ := issuer.Issue("1", 1, "admin", []string{"product.view"})
	if rr := do(t, h, http.MethodGet, "/storefront/cart", adminTok, nil); rr.Code != http.StatusForbidden {
		t.Fatalf("admin token on storefront: want 403, got %d", rr.Code)
	}
	// Proper storefront token works.
	tok := login(t, h, s.email)
	if rr := do(t, h, http.MethodGet, "/storefront/cart", tok, nil); rr.Code != http.StatusOK {
		t.Fatalf("storefront cart: want 200, got %d (%s)", rr.Code, rr.Body.String())
	}
}

// ---- Cart pricing + totals -----------------------------------------------

func TestCartAddPriceAndTotals(t *testing.T) {
	h, _, pool := newServer(t)
	s := seedCustomer(t, pool, "acme", "buyer@acme.test")
	tok := login(t, h, s.email)

	// Add the priced product, qty 2 -> unit_price from combined_prices, subtotal 20.
	rr := do(t, h, http.MethodPost, "/storefront/cart/items", tok, map[string]any{"product_public_id": s.pricedPublicID, "quantity": "2"})
	if rr.Code != http.StatusOK {
		t.Fatalf("add priced: want 200, got %d (%s)", rr.Code, rr.Body.String())
	}
	var c cartResp
	_ = json.Unmarshal(rr.Body.Bytes(), &c)
	if len(c.Items) != 1 || c.Items[0].UnitPrice != "10.0000" {
		t.Fatalf("unit price snapshot: want 10.0000, got %+v", c.Items)
	}
	if c.Items[0].RowTotal != "20.0000" || c.Subtotal != "20.0000" {
		t.Errorf("totals: want row/subtotal 20.0000, got row=%s subtotal=%s", c.Items[0].RowTotal, c.Subtotal)
	}

	// Unpriced product -> 409 price-on-request.
	if rr := do(t, h, http.MethodPost, "/storefront/cart/items", tok, map[string]any{"product_public_id": s.freePublicID, "quantity": "1"}); rr.Code != http.StatusConflict {
		t.Fatalf("unpriced add: want 409, got %d (%s)", rr.Code, rr.Body.String())
	}

	// Update quantity -> totals recompute.
	itemID := c.Items[0].ID
	upd := do(t, h, http.MethodPatch, "/storefront/cart/items/"+strconv.FormatInt(itemID, 10), tok, map[string]any{"quantity": "3"})
	if upd.Code != http.StatusOK {
		t.Fatalf("update qty: %d (%s)", upd.Code, upd.Body.String())
	}
	_ = json.Unmarshal(upd.Body.Bytes(), &c)
	if c.Subtotal != "30.0000" {
		t.Errorf("after qty=3: want subtotal 30.0000, got %s", c.Subtotal)
	}

	// Remove -> empty cart.
	del := do(t, h, http.MethodDelete, "/storefront/cart/items/"+strconv.FormatInt(itemID, 10), tok, nil)
	if del.Code != http.StatusOK {
		t.Fatalf("remove: %d", del.Code)
	}
	_ = json.Unmarshal(del.Body.Bytes(), &c)
	if len(c.Items) != 0 || c.Subtotal != "0.0000" {
		t.Errorf("after remove: want empty cart, got items=%d subtotal=%s", len(c.Items), c.Subtotal)
	}
}

func TestCartRevalidateOnPriceChange(t *testing.T) {
	h, _, pool := newServer(t)
	q := gen.New(pool)
	ctx := context.Background()
	s := seedCustomer(t, pool, "acme", "buyer@acme.test")
	tok := login(t, h, s.email)

	// Add at the seeded price (10).
	do(t, h, http.MethodPost, "/storefront/cart/items", tok, map[string]any{"product_public_id": s.pricedPublicID, "quantity": "1"})

	// Change the price to 8 and rewarm the cache.
	lists, _ := q.ListPriceLists(ctx, 1)
	var listID int64
	for _, l := range lists {
		if l.Currency == "USD" {
			listID = l.ID
		}
	}
	if _, err := q.UpsertPrice(ctx, gen.UpsertPriceParams{PriceListID: listID, ProductID: s.productPriced, Unit: "each", MinQuantity: "1", Value: "8.0000"}); err != nil {
		t.Fatalf("reprice: %v", err)
	}
	wid := int64(1)
	if err := jobs.RecomputeForCustomer(ctx, pool, jobs.RecomputeCombinedPricesArgs{CustomerID: s.customerID, WebsiteID: &wid, Currency: "USD"}); err != nil {
		t.Fatalf("recompute: %v", err)
	}

	// Revalidate picks up the drift.
	rr := do(t, h, http.MethodPost, "/storefront/cart/revalidate", tok, nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("revalidate: %d (%s)", rr.Code, rr.Body.String())
	}
	var res struct {
		Changed []struct {
			OldPrice string `json:"old_price"`
			NewPrice string `json:"new_price"`
		} `json:"changed"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &res)
	if len(res.Changed) != 1 || res.Changed[0].NewPrice != "8.0000" || res.Changed[0].OldPrice != "10.0000" {
		t.Fatalf("revalidate drift: want 10->8, got %+v", res.Changed)
	}

	// Cart now reflects the new price.
	cartRR := do(t, h, http.MethodGet, "/storefront/cart", tok, nil)
	var c cartResp
	_ = json.Unmarshal(cartRR.Body.Bytes(), &c)
	if c.Subtotal != "8.0000" {
		t.Errorf("post-revalidate subtotal: want 8.0000, got %s", c.Subtotal)
	}
}

// ---- Shopping lists ------------------------------------------------------

func TestShoppingListConvertAndDefault(t *testing.T) {
	h, _, pool := newServer(t)
	s := seedCustomer(t, pool, "acme", "buyer@acme.test")
	tok := login(t, h, s.email)

	// First default list ok.
	l1 := do(t, h, http.MethodPost, "/storefront/shopping-lists", tok, map[string]any{"name": "Reorder", "is_default": true})
	if l1.Code != http.StatusCreated {
		t.Fatalf("create list: %d (%s)", l1.Code, l1.Body.String())
	}
	var list gen.ShoppingList
	_ = json.Unmarshal(l1.Body.Bytes(), &list)

	// Second default list rejected by the partial unique index.
	if dup := do(t, h, http.MethodPost, "/storefront/shopping-lists", tok, map[string]any{"name": "Other", "is_default": true}); dup.Code != http.StatusConflict {
		t.Errorf("second default list: want 409, got %d", dup.Code)
	}

	// Add a priced + an unpriced item.
	base := "/storefront/shopping-lists/" + strconv.FormatInt(list.ID, 10) + "/items"
	do(t, h, http.MethodPost, base, tok, map[string]any{"product_public_id": s.pricedPublicID, "quantity": "2"})
	do(t, h, http.MethodPost, base, tok, map[string]any{"product_public_id": s.freePublicID, "quantity": "1"})

	// Convert -> priced item lands in the cart, unpriced is skipped.
	conv := do(t, h, http.MethodPost, "/storefront/shopping-lists/"+strconv.FormatInt(list.ID, 10)+"/convert-to-cart", tok, nil)
	if conv.Code != http.StatusOK {
		t.Fatalf("convert: %d (%s)", conv.Code, conv.Body.String())
	}
	var cres struct {
		Skipped []int64 `json:"skipped_product_ids"`
	}
	_ = json.Unmarshal(conv.Body.Bytes(), &cres)
	if len(cres.Skipped) != 1 {
		t.Errorf("convert: want 1 skipped (unpriced), got %v", cres.Skipped)
	}
	cartRR := do(t, h, http.MethodGet, "/storefront/cart", tok, nil)
	var c cartResp
	_ = json.Unmarshal(cartRR.Body.Bytes(), &c)
	if len(c.Items) != 1 || c.Subtotal != "20.0000" {
		t.Errorf("converted cart: want 1 item subtotal 20.0000, got items=%d subtotal=%s", len(c.Items), c.Subtotal)
	}
}

// ---- Contract pricing visibility -----------------------------------------

func TestProductPricingTiers(t *testing.T) {
	h, _, pool := newServer(t)
	q := gen.New(pool)
	ctx := context.Background()
	s := seedCustomer(t, pool, "acme", "buyer@acme.test")

	// Add a volume tier (buy 10+ at 8) on the priced product's price list, rewarm.
	lists, _ := q.ListPriceLists(ctx, 1)
	var listID int64
	for _, l := range lists {
		if l.Currency == "USD" {
			listID = l.ID
		}
	}
	if _, err := q.UpsertPrice(ctx, gen.UpsertPriceParams{PriceListID: listID, ProductID: s.productPriced, Unit: "each", MinQuantity: "10", Value: "8.0000"}); err != nil {
		t.Fatalf("tier price: %v", err)
	}
	wid := int64(1)
	if err := jobs.RecomputeForCustomer(ctx, pool, jobs.RecomputeCombinedPricesArgs{CustomerID: s.customerID, WebsiteID: &wid, Currency: "USD"}); err != nil {
		t.Fatalf("recompute: %v", err)
	}

	tok := login(t, h, s.email)
	rr := do(t, h, http.MethodGet, "/storefront/products/acme-priced/pricing", tok, nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("pricing: want 200, got %d (%s)", rr.Code, rr.Body.String())
	}
	var res struct {
		Currency       string `json:"currency"`
		PriceOnRequest bool   `json:"price_on_request"`
		Tiers          []struct {
			MinQuantity string `json:"min_quantity"`
			Value       string `json:"value"`
		} `json:"tiers"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &res)
	if res.PriceOnRequest || len(res.Tiers) != 2 {
		t.Fatalf("tiers: want 2 priced tiers, got price_on_request=%v tiers=%+v", res.PriceOnRequest, res.Tiers)
	}
	// Tiers are ordered by min_quantity: 1 -> 10.0000, 10 -> 8.0000.
	if res.Tiers[1].Value != "8.0000" {
		t.Errorf("volume tier: want 8.0000 at qty 10, got %+v", res.Tiers)
	}
}

func TestProductPricingOnRequest(t *testing.T) {
	h, _, pool := newServer(t)
	s := seedCustomer(t, pool, "acme", "buyer@acme.test")
	tok := login(t, h, s.email)
	// The unpriced product has no resolved tiers.
	rr := do(t, h, http.MethodGet, "/storefront/products/acme-free/pricing", tok, nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("pricing: want 200, got %d", rr.Code)
	}
	var res struct {
		PriceOnRequest bool `json:"price_on_request"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &res)
	if !res.PriceOnRequest {
		t.Errorf("unpriced product: want price_on_request=true")
	}
}

// ---- Reorder -------------------------------------------------------------

func TestReorderFromOrder(t *testing.T) {
	h, _, pool := newServer(t)
	s := seedCustomer(t, pool, "acme", "buyer@acme.test")
	// An order with a priced line (qty 2) and an unpriced line (qty 1).
	order := seedOrder(t, pool, s, map[int64]string{s.productPriced: "2", s.productFree: "1"})
	tok := login(t, h, s.email)

	rr := do(t, h, http.MethodPost, "/storefront/cart/reorder", tok, map[string]any{"order_public_id": order.PublicID.String()})
	if rr.Code != http.StatusOK {
		t.Fatalf("reorder: want 200, got %d (%s)", rr.Code, rr.Body.String())
	}
	var res struct {
		Skipped []string `json:"skipped_skus"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &res)
	if len(res.Skipped) != 1 || res.Skipped[0] != s.freeSKU {
		t.Errorf("reorder: want 1 skipped (%s), got %v", s.freeSKU, res.Skipped)
	}

	// Priced line is in the cart at the CURRENT price (10), qty 2 -> subtotal 20.
	cartRR := do(t, h, http.MethodGet, "/storefront/cart", tok, nil)
	var c cartResp
	_ = json.Unmarshal(cartRR.Body.Bytes(), &c)
	if len(c.Items) != 1 || c.Subtotal != "20.0000" {
		t.Errorf("reordered cart: want 1 item subtotal 20.0000, got items=%d subtotal=%s", len(c.Items), c.Subtotal)
	}
}

func TestReorderForeignOrderIsNotFound(t *testing.T) {
	h, _, pool := newServer(t)
	a := seedCustomer(t, pool, "acme", "buyer@acme.test")
	b := seedCustomer(t, pool, "beta", "buyer@beta.test")
	order := seedOrder(t, pool, a, map[int64]string{a.productPriced: "1"})

	tokB := login(t, h, b.email)
	// Customer B must not be able to reorder customer A's order.
	if rr := do(t, h, http.MethodPost, "/storefront/cart/reorder", tokB, map[string]any{"order_public_id": order.PublicID.String()}); rr.Code != http.StatusNotFound {
		t.Errorf("foreign reorder: want 404, got %d (%s)", rr.Code, rr.Body.String())
	}
}

// ---- Quick order (bulk add) ----------------------------------------------

func TestBulkAddQuickOrder(t *testing.T) {
	h, _, pool := newServer(t)
	s := seedCustomer(t, pool, "acme", "buyer@acme.test")
	tok := login(t, h, s.email)

	rr := do(t, h, http.MethodPost, "/storefront/cart/bulk", tok, map[string]any{
		"lines": []map[string]any{
			{"sku": s.pricedSKU, "quantity": "2"},
			{"sku": s.freeSKU, "quantity": "1"}, // exists but price-on-request
			{"sku": "DOES-NOT-EXIST", "quantity": "1"},
		},
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("bulk: want 200, got %d (%s)", rr.Code, rr.Body.String())
	}
	var res struct {
		Added          int      `json:"added"`
		NotFound       []string `json:"not_found_skus"`
		PriceOnRequest []string `json:"price_on_request"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &res)
	if res.Added != 1 {
		t.Errorf("bulk added: want 1, got %d", res.Added)
	}
	if len(res.NotFound) != 1 || res.NotFound[0] != "DOES-NOT-EXIST" {
		t.Errorf("bulk not_found: want [DOES-NOT-EXIST], got %v", res.NotFound)
	}
	if len(res.PriceOnRequest) != 1 || res.PriceOnRequest[0] != s.freeSKU {
		t.Errorf("bulk price_on_request: want [%s], got %v", s.freeSKU, res.PriceOnRequest)
	}

	cartRR := do(t, h, http.MethodGet, "/storefront/cart", tok, nil)
	var c cartResp
	_ = json.Unmarshal(cartRR.Body.Bytes(), &c)
	if len(c.Items) != 1 || c.Subtotal != "20.0000" {
		t.Errorf("bulk cart: want 1 item subtotal 20.0000, got items=%d subtotal=%s", len(c.Items), c.Subtotal)
	}
}

func TestBulkAddEmptyRejected(t *testing.T) {
	h, _, pool := newServer(t)
	s := seedCustomer(t, pool, "acme", "buyer@acme.test")
	tok := login(t, h, s.email)
	if rr := do(t, h, http.MethodPost, "/storefront/cart/bulk", tok, map[string]any{"lines": []any{}}); rr.Code != http.StatusBadRequest {
		t.Errorf("empty bulk: want 400, got %d", rr.Code)
	}
}

// ---- Shopping list management --------------------------------------------

func TestShoppingListManageItemsRenameDelete(t *testing.T) {
	h, _, pool := newServer(t)
	s := seedCustomer(t, pool, "acme", "buyer@acme.test")
	tok := login(t, h, s.email)

	// Create a list and add an item.
	l := do(t, h, http.MethodPost, "/storefront/shopping-lists", tok, map[string]any{"name": "Weekly"})
	if l.Code != http.StatusCreated {
		t.Fatalf("create list: %d (%s)", l.Code, l.Body.String())
	}
	var list gen.ShoppingList
	_ = json.Unmarshal(l.Body.Bytes(), &list)
	base := "/storefront/shopping-lists/" + strconv.FormatInt(list.ID, 10)

	add := do(t, h, http.MethodPost, base+"/items", tok, map[string]any{"product_public_id": s.pricedPublicID, "quantity": "2"})
	if add.Code != http.StatusCreated {
		t.Fatalf("add item: %d (%s)", add.Code, add.Body.String())
	}
	var item struct {
		ID int64 `json:"id"`
	}
	_ = json.Unmarshal(add.Body.Bytes(), &item)

	// Update quantity.
	upd := do(t, h, http.MethodPatch, base+"/items/"+strconv.FormatInt(item.ID, 10), tok, map[string]any{"quantity": "5"})
	if upd.Code != http.StatusOK {
		t.Fatalf("update item: %d (%s)", upd.Code, upd.Body.String())
	}
	var updated struct {
		Quantity string `json:"quantity"`
	}
	_ = json.Unmarshal(upd.Body.Bytes(), &updated)
	if updated.Quantity != "5.0000" && updated.Quantity != "5" {
		t.Errorf("update qty: want 5, got %s", updated.Quantity)
	}

	// Rename the list.
	rn := do(t, h, http.MethodPatch, base, tok, map[string]any{"name": "Renamed"})
	if rn.Code != http.StatusOK {
		t.Fatalf("rename: %d (%s)", rn.Code, rn.Body.String())
	}
	var renamed gen.ShoppingList
	_ = json.Unmarshal(rn.Body.Bytes(), &renamed)
	if renamed.Name != "Renamed" {
		t.Errorf("rename: want Renamed, got %s", renamed.Name)
	}

	// Remove the item.
	if rm := do(t, h, http.MethodDelete, base+"/items/"+strconv.FormatInt(item.ID, 10), tok, nil); rm.Code != http.StatusNoContent {
		t.Fatalf("remove item: want 204, got %d (%s)", rm.Code, rm.Body.String())
	}
	items := do(t, h, http.MethodGet, base+"/items", tok, nil)
	var ir struct {
		Items []any `json:"items"`
	}
	_ = json.Unmarshal(items.Body.Bytes(), &ir)
	if len(ir.Items) != 0 {
		t.Errorf("after remove: want 0 items, got %d", len(ir.Items))
	}

	// Delete the list.
	if del := do(t, h, http.MethodDelete, base, tok, nil); del.Code != http.StatusNoContent {
		t.Fatalf("delete list: want 204, got %d (%s)", del.Code, del.Body.String())
	}
	// Subsequent item fetch is 404 (list gone).
	if again := do(t, h, http.MethodGet, base+"/items", tok, nil); again.Code != http.StatusNotFound {
		t.Errorf("after delete: want 404, got %d", again.Code)
	}
}

func TestShoppingListForeignAccessIsNotFound(t *testing.T) {
	h, _, pool := newServer(t)
	a := seedCustomer(t, pool, "acme", "buyer@acme.test")
	b := seedCustomer(t, pool, "beta", "buyer@beta.test")
	tokA := login(t, h, a.email)
	tokB := login(t, h, b.email)

	l := do(t, h, http.MethodPost, "/storefront/shopping-lists", tokA, map[string]any{"name": "Private"})
	var list gen.ShoppingList
	_ = json.Unmarshal(l.Body.Bytes(), &list)
	base := "/storefront/shopping-lists/" + strconv.FormatInt(list.ID, 10)

	// Customer B cannot rename or delete customer A's list.
	if rn := do(t, h, http.MethodPatch, base, tokB, map[string]any{"name": "Hijacked"}); rn.Code != http.StatusNotFound {
		t.Errorf("foreign rename: want 404, got %d", rn.Code)
	}
	if del := do(t, h, http.MethodDelete, base, tokB, nil); del.Code != http.StatusNotFound {
		t.Errorf("foreign delete: want 404, got %d", del.Code)
	}
}

// ---- Isolation -----------------------------------------------------------

func TestCartCustomerIsolation(t *testing.T) {
	h, _, pool := newServer(t)
	a := seedCustomer(t, pool, "acme", "buyer@acme.test")
	b := seedCustomer(t, pool, "beta", "buyer@beta.test")

	tokA := login(t, h, a.email)
	tokB := login(t, h, b.email)

	// A adds an item.
	if rr := do(t, h, http.MethodPost, "/storefront/cart/items", tokA, map[string]any{"product_public_id": a.pricedPublicID, "quantity": "1"}); rr.Code != http.StatusOK {
		t.Fatalf("A add: %d (%s)", rr.Code, rr.Body.String())
	}
	// B's cart is independent and empty.
	rr := do(t, h, http.MethodGet, "/storefront/cart", tokB, nil)
	var c cartResp
	_ = json.Unmarshal(rr.Body.Bytes(), &c)
	if len(c.Items) != 0 {
		t.Errorf("isolation: customer B should not see A's cart items, got %d", len(c.Items))
	}
}
