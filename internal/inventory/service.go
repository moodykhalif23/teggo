// Package inventory holds the stock domain: the movement ledger is the source
// of truth, inventory_levels is its cache. The Reserve/Fulfil functions take a
// *gen.Queries so they compose inside the order and shipment transactions
// (reserve on order confirm; convert reservation to fulfilment on ship — §8).
// They are graceful: a product/warehouse with no level row is treated as
// untracked (skipped), so stock control is opt-in per product.
package inventory

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"b2bcommerce/internal/money"
	"b2bcommerce/internal/store/gen"
)

// ErrInsufficientStock is returned when a reservation would drive available
// below zero on a product that does not allow backorder.
var ErrInsufficientStock = errors.New("insufficient stock")

// ReserveForOrder reserves stock for every tracked order line at the org's
// default warehouse. Untracked products are skipped.
func ReserveForOrder(ctx context.Context, q *gen.Queries, orgID, orderID int64, by string) error {
	wh, err := q.GetDefaultWarehouse(ctx, orgID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil // no warehouse configured -> stock not tracked
	}
	if err != nil {
		return err
	}
	items, err := q.ListOrderItems(ctx, orderID)
	if err != nil {
		return err
	}
	ref := orderID
	for _, it := range items {
		lvl, err := q.GetInventoryLevel(ctx, gen.GetInventoryLevelParams{ProductID: it.ProductID, WarehouseID: wh.ID})
		if errors.Is(err, pgx.ErrNoRows) {
			continue // untracked product
		}
		if err != nil {
			return err
		}
		available, _ := money.Sub(lvl.QuantityOnHand, lvl.QuantityReserved)
		if c, _ := money.Cmp(it.Quantity, available); c > 0 && !lvl.AllowBackorder {
			return ErrInsufficientStock
		}
		if _, err := q.AdjustInventoryLevel(ctx, gen.AdjustInventoryLevelParams{
			ProductID: it.ProductID, WarehouseID: wh.ID, Column3: "0", Column4: it.Quantity,
		}); err != nil {
			return err
		}
		if _, err := q.AddInventoryMovement(ctx, gen.AddInventoryMovementParams{
			ProductID: it.ProductID, WarehouseID: wh.ID, Type: "reservation", Quantity: it.Quantity,
			ReferenceType: strptr("order"), ReferenceID: &ref, CreatedBy: strptr(by),
		}); err != nil {
			return err
		}
	}
	return nil
}

// FulfilShipment converts reservations to fulfilment for a shipment's lines:
// on-hand and reserved both drop by the shipped quantity. Untracked products
// are skipped.
func FulfilShipment(ctx context.Context, q *gen.Queries, orgID, shipmentID int64, by string) error {
	// Decrement from the shipment's assigned warehouse (multi-warehouse), falling
	// back to the org's default warehouse for shipments with none set.
	var whID int64
	if sh, err := q.GetShipment(ctx, shipmentID); err == nil && sh.WarehouseID != nil {
		whID = *sh.WarehouseID
	} else {
		def, derr := q.GetDefaultWarehouse(ctx, orgID)
		if errors.Is(derr, pgx.ErrNoRows) {
			return nil
		}
		if derr != nil {
			return derr
		}
		whID = def.ID
	}
	wh := struct{ ID int64 }{ID: whID}
	lines, err := q.ListShipmentItemProducts(ctx, shipmentID)
	if err != nil {
		return err
	}
	ref := shipmentID
	for _, ln := range lines {
		if _, err := q.GetInventoryLevel(ctx, gen.GetInventoryLevelParams{ProductID: ln.ProductID, WarehouseID: wh.ID}); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				continue
			}
			return err
		}
		neg, _ := money.Sub("0", ln.Quantity) // -quantity
		if _, err := q.AdjustInventoryLevel(ctx, gen.AdjustInventoryLevelParams{
			ProductID: ln.ProductID, WarehouseID: wh.ID, Column3: neg, Column4: neg,
		}); err != nil {
			return err
		}
		if _, err := q.AddInventoryMovement(ctx, gen.AddInventoryMovementParams{
			ProductID: ln.ProductID, WarehouseID: wh.ID, Type: "fulfillment", Quantity: neg,
			ReferenceType: strptr("shipment"), ReferenceID: &ref, CreatedBy: strptr(by),
		}); err != nil {
			return err
		}
	}
	return nil
}

func strptr(s string) *string { return &s }
