package marketplace_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"b2bcommerce/internal/store/gen"
)

// mkVendor creates a vendor with a given commission percent.
func mkVendor(t *testing.T, pool *pgxpool.Pool, name, slug, commission string) gen.Vendor {
	t.Helper()
	v, err := gen.New(pool).CreateVendor(context.Background(), gen.CreateVendorParams{
		OrganizationID: 1, Name: name, Slug: slug, Status: "active", CommissionRate: commission, PayoutTermsDays: 30,
	})
	if err != nil {
		t.Fatalf("create vendor %s: %v", name, err)
	}
	return v
}

// mkProduct creates an active product, optionally owned by a vendor (vendorID 0
// = operator-owned).
func mkProduct(t *testing.T, pool *pgxpool.Pool, sku, name string, vendorID int64) int64 {
	t.Helper()
	ctx := context.Background()
	q := gen.New(pool)
	p, err := q.CreateProduct(ctx, gen.CreateProductParams{
		OrganizationID: 1, Sku: sku, Type: "simple", Name: name, Slug: sku, Status: "active",
		Attributes: []byte("{}"), Unit: "each",
	})
	if err != nil {
		t.Fatalf("create product %s: %v", sku, err)
	}
	if vendorID != 0 {
		if _, err := pool.Exec(ctx, "UPDATE products SET vendor_id=$1 WHERE id=$2", vendorID, p.ID); err != nil {
			t.Fatalf("assign vendor: %v", err)
		}
	}
	return p.ID
}

// TestOrderSplitByVendor proves a multi-vendor order fans out into one
// vendor_order per vendor with a correct commission snapshot, that operator-owned
// lines are excluded, and that each order line is tagged with its owning vendor.
func TestOrderSplitByVendor(t *testing.T) {
	h, pool, issuer := newServer(t)
	ctx := context.Background()
	q := gen.New(pool)

	vendorA := mkVendor(t, pool, "Vendor A", "vendor-a", "10")   // 10%
	vendorB := mkVendor(t, pool, "Vendor B", "vendor-b", "20")   // 20%
	pA := mkProduct(t, pool, "AAA-1", "A widget", vendorA.ID)
	pB := mkProduct(t, pool, "BBB-1", "B widget", vendorB.ID)
	pHouse := mkProduct(t, pool, "HOUSE-1", "House item", 0) // operator-owned

	cust, err := q.CreateCustomer(ctx, gen.CreateCustomerParams{OrganizationID: 1, Name: "Buyer Co", CreditLimit: "0"})
	if err != nil {
		t.Fatalf("customer: %v", err)
	}

	tok := adminToken(t, issuer, "order.view", "order.manage")
	rr := req(t, h, http.MethodPost, "/admin/orders", tok, map[string]any{
		"customer_id": cust.ID,
		"currency":    "USD",
		"items": []map[string]any{
			{"product_id": pA, "quantity": "2", "unit": "each", "unit_price": "100"}, // gross 200
			{"product_id": pB, "quantity": "1", "unit": "each", "unit_price": "50"},  // gross 50
			{"product_id": pHouse, "quantity": "1", "unit": "each", "unit_price": "30"},
		},
	})
	if rr.Code != http.StatusOK && rr.Code != http.StatusCreated {
		t.Fatalf("create order: want 200/201, got %d (%s)", rr.Code, rr.Body.String())
	}
	var resp struct {
		ID int64 `json:"id"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)
	if resp.ID == 0 {
		t.Fatal("order id missing")
	}

	vos, err := q.ListVendorOrdersForOrder(ctx, resp.ID)
	if err != nil {
		t.Fatalf("list vendor orders: %v", err)
	}
	if len(vos) != 2 {
		t.Fatalf("vendor_orders: want 2 (house line excluded), got %d", len(vos))
	}
	byVendor := map[int64]gen.VendorOrder{}
	for _, vo := range vos {
		byVendor[vo.VendorID] = vo
	}
	a, b := byVendor[vendorA.ID], byVendor[vendorB.ID]
	if a.GrossTotal != "200.0000" || a.CommissionTotal != "20.0000" || a.NetTotal != "180.0000" {
		t.Errorf("vendor A: gross/commission/net = %s/%s/%s, want 200/20/180", a.GrossTotal, a.CommissionTotal, a.NetTotal)
	}
	if a.CommissionRate != "10.0000" {
		t.Errorf("vendor A rate snapshot: want 10.0000, got %s", a.CommissionRate)
	}
	if b.GrossTotal != "50.0000" || b.CommissionTotal != "10.0000" || b.NetTotal != "40.0000" {
		t.Errorf("vendor B: gross/commission/net = %s/%s/%s, want 50/10/40", b.GrossTotal, b.CommissionTotal, b.NetTotal)
	}

	// Lines are tagged: 2 vendor lines tagged, the house line stays NULL.
	var tagged, untagged int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM order_items WHERE order_id=$1 AND vendor_id IS NOT NULL", resp.ID).Scan(&tagged); err != nil {
		t.Fatalf("count tagged: %v", err)
	}
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM order_items WHERE order_id=$1 AND vendor_id IS NULL", resp.ID).Scan(&untagged); err != nil {
		t.Fatalf("count untagged: %v", err)
	}
	if tagged != 2 || untagged != 1 {
		t.Errorf("line tagging: want 2 tagged + 1 untagged, got %d + %d", tagged, untagged)
	}
}
