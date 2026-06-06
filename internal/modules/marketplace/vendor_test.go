package marketplace_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"b2bcommerce/internal/auth"
	"b2bcommerce/internal/store/gen"
)

// vendorLogin creates a portal user for a vendor and returns a vendor token.
func vendorLogin(t *testing.T, h http.Handler, pool *pgxpool.Pool, vendorID int64, email string) string {
	t.Helper()
	const pw = "vendor-pass-123"
	hash, _ := auth.HashPassword(pw)
	if _, err := gen.New(pool).CreateVendorUser(context.Background(), gen.CreateVendorUserParams{
		VendorID: vendorID, Email: email, PasswordHash: hash, FullName: "Portal User", Role: "admin",
	}); err != nil {
		t.Fatalf("create vendor user: %v", err)
	}
	rr := req(t, h, http.MethodPost, "/vendor/auth/login", "", map[string]any{"email": email, "password": pw, "org_id": 1})
	if rr.Code != http.StatusOK {
		t.Fatalf("vendor login: want 200, got %d (%s)", rr.Code, rr.Body.String())
	}
	var resp struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)
	return resp.Token
}

// placeVendorOrder creates a customer + order on behalf with one line for the
// given vendor product, returning nothing (the vendor_order is created by split).
func placeVendorOrder(t *testing.T, h http.Handler, pool *pgxpool.Pool, issuer *auth.Issuer, productID int64) {
	t.Helper()
	cust, err := gen.New(pool).CreateCustomer(context.Background(), gen.CreateCustomerParams{OrganizationID: 1, Name: "Buyer", CreditLimit: "0"})
	if err != nil {
		t.Fatalf("customer: %v", err)
	}
	tok := adminToken(t, issuer, "order.view", "order.manage")
	rr := req(t, h, http.MethodPost, "/admin/orders", tok, map[string]any{
		"customer_id": cust.ID, "currency": "USD",
		"items": []map[string]any{{"product_id": productID, "quantity": "2", "unit": "each", "unit_price": "100"}},
	})
	if rr.Code != http.StatusOK && rr.Code != http.StatusCreated {
		t.Fatalf("place order: %d (%s)", rr.Code, rr.Body.String())
	}
}

// TestVendorPortal exercises the vendor self-service surface: profile, dashboard
// totals, my-products, my-orders, order detail (only this vendor's lines), the
// fulfilment transition rules, and cross-vendor isolation.
func TestVendorPortal(t *testing.T) {
	h, pool, issuer := newServer(t)

	vA := mkVendor(t, pool, "Vendor A", "vendor-a", "10")
	pA := mkProduct(t, pool, "AAA-1", "A widget", vA.ID)
	placeVendorOrder(t, h, pool, issuer, pA) // gross 200, commission 20, net 180

	tok := vendorLogin(t, h, pool, vA.ID, "a@vendor.test")

	// Profile.
	rr := req(t, h, http.MethodGet, "/vendor/me", tok, nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("me: %d (%s)", rr.Code, rr.Body.String())
	}

	// Dashboard totals.
	rr = req(t, h, http.MethodGet, "/vendor/dashboard", tok, nil)
	var dash struct {
		OrderCount int64  `json:"order_count"`
		NetTotal   string `json:"net_total"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &dash)
	if dash.OrderCount != 1 || dash.NetTotal != "180.0000" {
		t.Errorf("dashboard: want 1 order / net 180, got %d / %s", dash.OrderCount, dash.NetTotal)
	}

	// Products.
	rr = req(t, h, http.MethodGet, "/vendor/products", tok, nil)
	var prods struct {
		Items []struct {
			ID int64 `json:"id"`
		} `json:"items"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &prods)
	if len(prods.Items) != 1 || prods.Items[0].ID != pA {
		t.Errorf("products: want [%d], got %+v", pA, prods.Items)
	}

	// Orders list.
	rr = req(t, h, http.MethodGet, "/vendor/orders", tok, nil)
	var orders struct {
		Items []struct {
			ID       int64  `json:"id"`
			NetTotal string `json:"net_total"`
			Status   string `json:"status"`
		} `json:"items"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &orders)
	if len(orders.Items) != 1 || orders.Items[0].NetTotal != "180.0000" {
		t.Fatalf("orders: want 1 with net 180, got %+v", orders.Items)
	}
	voID := orders.Items[0].ID

	// Detail returns this vendor's line(s).
	rr = req(t, h, http.MethodGet, "/vendor/orders/"+itoa(voID), tok, nil)
	var detail struct {
		Items []struct {
			Sku string `json:"sku"`
		} `json:"items"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &detail)
	if len(detail.Items) != 1 || detail.Items[0].Sku != "AAA-1" {
		t.Errorf("detail items: want [AAA-1], got %+v", detail.Items)
	}

	// Fulfilment lifecycle: pending -> accepted -> shipped -> delivered.
	for _, s := range []string{"accepted", "shipped", "delivered"} {
		if rr := req(t, h, http.MethodPatch, "/vendor/orders/"+itoa(voID)+"/status", tok, map[string]any{"status": s}); rr.Code != http.StatusOK {
			t.Fatalf("transition to %s: want 200, got %d (%s)", s, rr.Code, rr.Body.String())
		}
	}
	// Invalid transition from a final state.
	if rr := req(t, h, http.MethodPatch, "/vendor/orders/"+itoa(voID)+"/status", tok, map[string]any{"status": "shipped"}); rr.Code != http.StatusConflict {
		t.Errorf("delivered->shipped: want 409, got %d", rr.Code)
	}

	// Isolation: a different vendor cannot see vendor A's order.
	vB := mkVendor(t, pool, "Vendor B", "vendor-b", "20")
	tokB := vendorLogin(t, h, pool, vB.ID, "b@vendor.test")
	if rr := req(t, h, http.MethodGet, "/vendor/orders/"+itoa(voID), tokB, nil); rr.Code != http.StatusNotFound {
		t.Errorf("cross-vendor read: want 404, got %d", rr.Code)
	}
	if rr := req(t, h, http.MethodGet, "/vendor/orders", tokB, nil); rr.Code == http.StatusOK {
		var ob struct {
			Items []json.RawMessage `json:"items"`
		}
		_ = json.Unmarshal(rr.Body.Bytes(), &ob)
		if len(ob.Items) != 0 {
			t.Errorf("vendor B orders: want empty, got %d", len(ob.Items))
		}
	}
}
