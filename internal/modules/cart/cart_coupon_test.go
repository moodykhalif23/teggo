package cart_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"b2bcommerce/internal/store/gen"
)

func cstrp(s string) *string { return &s }

type couponCartResp struct {
	Subtotal string `json:"subtotal"`
	Discount string `json:"discount_amount"`
	Grand    string `json:"grand_total"`
	Coupon   string `json:"coupon_code"`
}

// TestCartCoupon exercises the full coupon path on the cart: apply a valid code
// (10% of a 20.00 subtotal = 2.00 off → 18.00), reject an unknown code, and
// remove the coupon (discount back to zero).
func TestCartCoupon(t *testing.T) {
	h, _, pool := newServer(t)
	s := seedCustomer(t, pool, "acme", "buyer@acme.test")
	tok := login(t, h, "buyer@acme.test")

	if _, err := gen.New(pool).CreatePromotion(context.Background(), gen.CreatePromotionParams{
		OrganizationID: 1, Name: "10% off", Code: cstrp("SAVE10"),
		DiscountType: "percent", DiscountValue: "10", IsActive: true,
	}); err != nil {
		t.Fatalf("create promotion: %v", err)
	}

	// Priced product qty 2 → subtotal 20.0000.
	if rr := do(t, h, http.MethodPost, "/storefront/cart/items", tok, map[string]any{"product_public_id": s.pricedPublicID, "quantity": "2"}); rr.Code != http.StatusOK {
		t.Fatalf("add item: %d (%s)", rr.Code, rr.Body.String())
	}

	// Apply coupon (case-insensitive).
	ar := do(t, h, http.MethodPost, "/storefront/cart/coupon", tok, map[string]any{"code": "save10"})
	if ar.Code != http.StatusOK {
		t.Fatalf("apply coupon: want 200, got %d (%s)", ar.Code, ar.Body.String())
	}
	var c couponCartResp
	_ = json.Unmarshal(ar.Body.Bytes(), &c)
	if c.Subtotal != "20.0000" || c.Discount != "2.0000" || c.Grand != "18.0000" {
		t.Fatalf("after coupon: subtotal=%s discount=%s grand=%s (%s)", c.Subtotal, c.Discount, c.Grand, ar.Body.String())
	}
	if c.Coupon != "SAVE10" {
		t.Errorf("coupon_code: want SAVE10, got %q", c.Coupon)
	}

	// Unknown code is rejected.
	if bad := do(t, h, http.MethodPost, "/storefront/cart/coupon", tok, map[string]any{"code": "NOPE"}); bad.Code != http.StatusUnprocessableEntity {
		t.Fatalf("unknown coupon: want 422, got %d (%s)", bad.Code, bad.Body.String())
	}

	// Remove coupon → discount clears.
	rr := do(t, h, http.MethodDelete, "/storefront/cart/coupon", tok, nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("remove coupon: want 200, got %d", rr.Code)
	}
	var c2 couponCartResp
	_ = json.Unmarshal(rr.Body.Bytes(), &c2)
	if c2.Discount != "0" && c2.Discount != "0.0000" {
		t.Errorf("after remove: discount should be zero, got %q", c2.Discount)
	}
}
