package marketplace

import (
	"context"

	"b2bcommerce/internal/money"
	"b2bcommerce/internal/store/gen"
)

// SplitOrder fans a freshly-created order into one vendor_order per vendor whose
// products appear on it, tagging each order line with its owning vendor and
// freezing a commission snapshot. Operator-owned lines (product.vendor_id NULL)
// are left untagged and never produce a vendor_order, so a pure single-seller
// order is a no-op. Call this inside the order-creation transaction, after the
// order items have been inserted, passing the same *gen.Queries bound to the tx.
//
// Commission is the operator's take: commission_total = gross * rate/100, and
// net_total (payable to the vendor) = gross - commission. gross is the ex-tax
// merchandise value (sum of line row_totals) for that vendor.
func SplitOrder(ctx context.Context, q *gen.Queries, orgID, orderID int64, currency string) error {
	rows, err := q.ListOrderItemsWithVendor(ctx, orderID)
	if err != nil {
		return err
	}

	// Group line ids + row totals by vendor, preserving first-seen vendor order
	// for deterministic processing.
	type bucket struct {
		itemIDs []int64
		totals  []string
	}
	buckets := map[int64]*bucket{}
	var order []int64
	for _, it := range rows {
		if it.VendorID == nil {
			continue // operator-owned line
		}
		b := buckets[*it.VendorID]
		if b == nil {
			b = &bucket{}
			buckets[*it.VendorID] = b
			order = append(order, *it.VendorID)
		}
		b.itemIDs = append(b.itemIDs, it.ID)
		b.totals = append(b.totals, it.RowTotal)
	}

	for _, vendorID := range order {
		b := buckets[vendorID]
		gross, err := money.Sum(b.totals...)
		if err != nil {
			return err
		}
		v, err := q.GetVendor(ctx, gen.GetVendorParams{ID: vendorID, OrganizationID: orgID})
		if err != nil {
			return err
		}
		commission, err := money.Percent(gross, v.CommissionRate)
		if err != nil {
			return err
		}
		net, err := money.Sub(gross, commission)
		if err != nil {
			return err
		}
		if _, err := q.CreateVendorOrder(ctx, gen.CreateVendorOrderParams{
			OrganizationID:  orgID,
			OrderID:         orderID,
			VendorID:        vendorID,
			Currency:        currency,
			GrossTotal:      gross,
			CommissionRate:  v.CommissionRate,
			CommissionTotal: commission,
			NetTotal:        net,
		}); err != nil {
			return err
		}
		for _, itemID := range b.itemIDs {
			vid := vendorID
			if err := q.SetOrderItemVendor(ctx, gen.SetOrderItemVendorParams{ID: itemID, VendorID: &vid}); err != nil {
				return err
			}
		}
	}
	return nil
}
