package marketplace_test

import (
	"encoding/json"
	"net/http"
	"testing"
)

// deliverVendorOrder advances a vendor's sub-order to 'delivered' via the portal.
func deliverVendorOrder(t *testing.T, h http.Handler, tok string, voID int64) {
	t.Helper()
	for _, s := range []string{"accepted", "shipped", "delivered"} {
		if rr := req(t, h, http.MethodPatch, "/vendor/orders/"+itoa(voID)+"/status", tok, map[string]any{"status": s}); rr.Code != http.StatusOK {
			t.Fatalf("advance to %s: %d (%s)", s, rr.Code, rr.Body.String())
		}
	}
}

// TestVendorPayouts covers the payout lifecycle: nothing-to-pay before delivery,
// batching delivered orders into a pending payout (amount = sum of net), idempotent
// emptiness after settling, marking paid (once), and the vendor's own view.
func TestVendorPayouts(t *testing.T) {
	h, pool, issuer := newServer(t)
	admin := adminToken(t, issuer, "vendor.view", "vendor.manage")

	vA := mkVendor(t, pool, "Vendor A", "vendor-a", "10")
	pA := mkProduct(t, pool, "AAA-1", "A widget", vA.ID)
	placeVendorOrder(t, h, pool, issuer, pA) // net 180
	tok := vendorLogin(t, h, pool, vA.ID, "a@vendor.test")

	// Find the vendor_order id.
	rr := req(t, h, http.MethodGet, "/vendor/orders", tok, nil)
	var orders struct {
		Items []struct {
			ID int64 `json:"id"`
		} `json:"items"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &orders)
	if len(orders.Items) != 1 {
		t.Fatalf("want 1 vendor order, got %d", len(orders.Items))
	}
	voID := orders.Items[0].ID

	// No delivered orders yet -> nothing to pay.
	if rr := req(t, h, http.MethodPost, "/admin/vendors/"+itoa(vA.ID)+"/payouts", admin, nil); rr.Code != http.StatusUnprocessableEntity {
		t.Fatalf("payout before delivery: want 422, got %d (%s)", rr.Code, rr.Body.String())
	}

	deliverVendorOrder(t, h, tok, voID)

	// Generate the payout.
	rr = req(t, h, http.MethodPost, "/admin/vendors/"+itoa(vA.ID)+"/payouts", admin, nil)
	if rr.Code != http.StatusCreated {
		t.Fatalf("generate payout: want 201, got %d (%s)", rr.Code, rr.Body.String())
	}
	var payout struct {
		ID     int64  `json:"id"`
		Amount string `json:"amount"`
		Status string `json:"status"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &payout)
	if payout.Amount != "180.0000" {
		t.Errorf("payout amount: want 180.0000, got %s", payout.Amount)
	}
	if payout.Status != "pending" {
		t.Errorf("payout status: want pending, got %s", payout.Status)
	}

	// The settled order is now attached, so a second generation finds nothing.
	if rr := req(t, h, http.MethodPost, "/admin/vendors/"+itoa(vA.ID)+"/payouts", admin, nil); rr.Code != http.StatusUnprocessableEntity {
		t.Errorf("second payout: want 422, got %d", rr.Code)
	}

	// The vendor_order now carries the payout_id.
	rr = req(t, h, http.MethodGet, "/vendor/orders", tok, nil)
	var withPayout struct {
		Items []struct {
			PayoutID *int64 `json:"payout_id"`
		} `json:"items"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &withPayout)
	if withPayout.Items[0].PayoutID == nil || *withPayout.Items[0].PayoutID != payout.ID {
		t.Errorf("order payout_id: want %d, got %v", payout.ID, withPayout.Items[0].PayoutID)
	}

	// Admin lists the payout.
	rr = req(t, h, http.MethodGet, "/admin/vendors/"+itoa(vA.ID)+"/payouts", admin, nil)
	var list struct {
		Items []json.RawMessage `json:"items"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &list)
	if len(list.Items) != 1 {
		t.Errorf("admin payout list: want 1, got %d", len(list.Items))
	}

	// Mark paid (once); a second attempt finds no pending payout.
	if rr := req(t, h, http.MethodPost, "/admin/payouts/"+itoa(payout.ID)+"/pay", admin, map[string]any{"reference": "BANK-REF-1"}); rr.Code != http.StatusOK {
		t.Fatalf("mark paid: want 200, got %d (%s)", rr.Code, rr.Body.String())
	}
	if rr := req(t, h, http.MethodPost, "/admin/payouts/"+itoa(payout.ID)+"/pay", admin, nil); rr.Code != http.StatusNotFound {
		t.Errorf("re-pay: want 404, got %d", rr.Code)
	}

	// Vendor sees the paid payout.
	rr = req(t, h, http.MethodGet, "/vendor/payouts", tok, nil)
	var vp struct {
		Items []struct {
			Status string `json:"status"`
			Amount string `json:"amount"`
		} `json:"items"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &vp)
	if len(vp.Items) != 1 || vp.Items[0].Status != "paid" || vp.Items[0].Amount != "180.0000" {
		t.Errorf("vendor payouts: want 1 paid 180.0000, got %+v", vp.Items)
	}
}
