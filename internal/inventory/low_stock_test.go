package inventory_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"b2bcommerce/internal/store/gen"
)

func strptr(s string) *string { return &s }

// TestLowStock verifies the org-wide low-stock endpoint: a line at/under its
// reorder threshold is reported, a healthy line is not, and a line with no
// threshold configured is excluded (no signal to compare against).
func TestLowStock(t *testing.T) {
	h, issuer, pool := newServer(t)
	tok := adminToken(t, issuer)
	q := gen.New(pool)
	ctx := context.Background()

	wh, err := q.CreateWarehouse(ctx, gen.CreateWarehouseParams{OrganizationID: 1, Name: "Main"})
	if err != nil {
		t.Fatalf("warehouse: %v", err)
	}

	// Low: threshold 20, on-hand 5 → available 5 ≤ 20.
	low := mkProduct(t, q, "LOW-1")
	if _, err := q.SetInventoryLevelConfig(ctx, gen.SetInventoryLevelConfigParams{ProductID: low, WarehouseID: wh.ID, ReorderThreshold: strptr("20")}); err != nil {
		t.Fatalf("low config: %v", err)
	}
	if _, err := q.AdjustInventoryLevel(ctx, gen.AdjustInventoryLevelParams{ProductID: low, WarehouseID: wh.ID, Column3: "5", Column4: "0"}); err != nil {
		t.Fatalf("low adjust: %v", err)
	}

	// Healthy: threshold 2, on-hand 100 → available 100 > 2.
	healthy := mkProduct(t, q, "OK-1")
	_, _ = q.SetInventoryLevelConfig(ctx, gen.SetInventoryLevelConfigParams{ProductID: healthy, WarehouseID: wh.ID, ReorderThreshold: strptr("2")})
	_, _ = q.AdjustInventoryLevel(ctx, gen.AdjustInventoryLevelParams{ProductID: healthy, WarehouseID: wh.ID, Column3: "100", Column4: "0"})

	// No threshold configured: excluded even at zero stock.
	noThresh := mkProduct(t, q, "NOTH-1")
	_ = q.EnsureInventoryLevel(ctx, gen.EnsureInventoryLevelParams{ProductID: noThresh, WarehouseID: wh.ID})

	rr := do(t, h, http.MethodGet, "/admin/inventory/low-stock", tok, nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("low-stock: want 200, got %d (%s)", rr.Code, rr.Body.String())
	}
	var resp struct {
		Items []struct {
			ProductID int64  `json:"product_id"`
			Sku       string `json:"sku"`
			Available string `json:"available"`
			Threshold string `json:"threshold"`
		} `json:"items"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Items) != 1 {
		t.Fatalf("want exactly 1 low-stock line, got %d (%s)", len(resp.Items), rr.Body.String())
	}
	if resp.Items[0].Sku != "LOW-1" {
		t.Errorf("want LOW-1 flagged, got %q", resp.Items[0].Sku)
	}
}

// TestLowStock_RequiresAuth confirms the endpoint is permission-gated.
func TestLowStock_RequiresAuth(t *testing.T) {
	h, _, _ := newServer(t)
	rr := do(t, h, http.MethodGet, "/admin/inventory/low-stock", "", nil)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated low-stock: want 401, got %d", rr.Code)
	}
}
