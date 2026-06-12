package cart_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"b2bcommerce/internal/store/gen"
)

// TestCartDisplayCurrency verifies the indicative display-currency conversion on
// the cart and the storefront currency list.
func TestCartDisplayCurrency(t *testing.T) {
	h, _, pool := newServer(t)
	s := seedCustomer(t, pool, "acme", "buyer@acme.test")
	tok := login(t, h, "buyer@acme.test")

	// Base currency is USD (seed). Configure USD→EUR at 0.9.
	if _, err := gen.New(pool).CreateFxRate(context.Background(), gen.CreateFxRateParams{
		OrganizationID: 1, BaseCurrency: "USD", QuoteCurrency: "EUR", Rate: "0.90000000",
	}); err != nil {
		t.Fatalf("fx rate: %v", err)
	}

	// Priced product qty 2 → subtotal 20.0000 USD.
	if rr := do(t, h, http.MethodPost, "/storefront/cart/items", tok, map[string]any{"product_public_id": s.pricedPublicID, "quantity": "2"}); rr.Code != http.StatusOK {
		t.Fatalf("add item: %d (%s)", rr.Code, rr.Body.String())
	}

	// Available currencies includes EUR.
	cr := do(t, h, http.MethodGet, "/storefront/currencies", tok, nil)
	var curs struct {
		Base       string   `json:"base"`
		Currencies []string `json:"currencies"`
	}
	_ = json.Unmarshal(cr.Body.Bytes(), &curs)
	if curs.Base != "USD" || len(curs.Currencies) != 1 || curs.Currencies[0] != "EUR" {
		t.Fatalf("currencies: want base USD + [EUR], got %+v", curs)
	}

	// Cart converted to EUR: 20 × 0.9 = 18.0000.
	rr := do(t, h, http.MethodGet, "/storefront/cart?currency=EUR", tok, nil)
	var cart struct {
		Subtotal string `json:"subtotal"`
		Display  *struct {
			Currency string `json:"currency"`
			Rate     string `json:"rate"`
			Subtotal string `json:"subtotal"`
			Grand    string `json:"grand_total"`
		} `json:"display"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &cart)
	if cart.Subtotal != "20.0000" {
		t.Fatalf("base subtotal: want 20.0000, got %s", cart.Subtotal)
	}
	if cart.Display == nil || cart.Display.Currency != "EUR" || cart.Display.Subtotal != "18.0000" || cart.Display.Grand != "18.0000" {
		t.Fatalf("display block wrong: %s", rr.Body.String())
	}
}
