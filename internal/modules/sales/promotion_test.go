package sales_test

import (
	"context"
	"net/http"
	"testing"

	"b2bcommerce/internal/store/gen"
)

// TestPlaceOrderAppliesPromotion verifies that an automatic promotion is applied
// at checkout: the order total is discounted, the discount is persisted, and a
// redemption is recorded (with the promotion's usage counter incremented).
func TestPlaceOrderAppliesPromotion(t *testing.T) {
	f := setup(t)
	ctx := context.Background()

	pr, err := gen.New(f.pool).CreatePromotion(ctx, gen.CreatePromotionParams{
		OrganizationID: 1, Name: "Auto 10%", DiscountType: "percent", DiscountValue: "10", IsActive: true,
	})
	if err != nil {
		t.Fatalf("create promotion: %v", err)
	}

	f.seedCart(t, [3]any{f.p1ID, "10", "10.0000"}) // subtotal 100.0000

	rr := f.do(t, http.MethodPost, "/storefront/orders", f.custTok, map[string]any{})
	if rr.Code != http.StatusOK {
		t.Fatalf("place order: want 200, got %d (%s)", rr.Code, rr.Body.String())
	}
	var order struct {
		ID         int64  `json:"id"`
		GrandTotal string `json:"grand_total"`
	}
	decode(t, rr, &order)
	if order.GrandTotal != "90.0000" {
		t.Fatalf("grand_total: want 90.0000 (100 − 10%%), got %s", order.GrandTotal)
	}

	// Discount + promotion persisted on the order.
	var disc string
	var promoID int64
	if err := f.pool.QueryRow(ctx, `SELECT discount_total, promotion_id FROM orders WHERE id = $1`, order.ID).Scan(&disc, &promoID); err != nil {
		t.Fatalf("read order: %v", err)
	}
	if disc != "10.0000" || promoID != pr.ID {
		t.Fatalf("order discount_total=%s promotion_id=%d (want 10.0000 / %d)", disc, promoID, pr.ID)
	}

	// Redemption recorded + usage counter incremented.
	var redemptions, times int
	_ = f.pool.QueryRow(ctx, `SELECT count(*) FROM promotion_redemptions WHERE promotion_id = $1`, pr.ID).Scan(&redemptions)
	_ = f.pool.QueryRow(ctx, `SELECT times_redeemed FROM promotions WHERE id = $1`, pr.ID).Scan(&times)
	if redemptions != 1 || times != 1 {
		t.Fatalf("redemptions=%d times_redeemed=%d (want 1/1)", redemptions, times)
	}
}
