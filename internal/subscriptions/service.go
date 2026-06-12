// Package subscriptions materializes recurring orders. It owns the order-building
// for a subscription run (priced from the customer's combined prices at run time,
// taxed, marketplace-split) so the daily river job has no dependency on the
// HTTP-bound sales handler. Each run is its own transaction.
package subscriptions

import (
	"context"
	"encoding/json"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	metering "b2bcommerce/internal/billing"
	"b2bcommerce/internal/modules/marketplace"
	"b2bcommerce/internal/money"
	"b2bcommerce/internal/promotions"
	"b2bcommerce/internal/store/gen"
	"b2bcommerce/internal/tax"
)

// promoCandidates maps active promotion rows into the engine's input.
func promoCandidates(rows []gen.Promotion) []promotions.Candidate {
	out := make([]promotions.Candidate, len(rows))
	for i, p := range rows {
		var starts, ends *time.Time
		if p.StartsAt.Valid {
			t := p.StartsAt.Time
			starts = &t
		}
		if p.EndsAt.Valid {
			t := p.EndsAt.Time
			ends = &t
		}
		out[i] = promotions.Candidate{
			ID: p.ID, Name: p.Name, Code: p.Code, DiscountType: p.DiscountType,
			DiscountValue: p.DiscountValue, MinSubtotal: p.MinSubtotal,
			StartsAt: starts, EndsAt: ends,
			MaxRedemptions: p.MaxRedemptions, TimesRedeemed: p.TimesRedeemed, Priority: p.Priority,
		}
	}
	return out
}

// AdvanceDate returns the next run date for a cadence, measured from `from`.
func AdvanceDate(cadence string, from time.Time) time.Time {
	switch cadence {
	case "weekly":
		return from.AddDate(0, 0, 7)
	case "biweekly":
		return from.AddDate(0, 0, 14)
	case "quarterly":
		return from.AddDate(0, 3, 0)
	case "monthly":
		fallthrough
	default:
		return from.AddDate(0, 1, 0)
	}
}

func dateOf(t time.Time) pgtype.Date      { return pgtype.Date{Time: t, Valid: true} }
func tsOf(t time.Time) pgtype.Timestamptz { return pgtype.Timestamptz{Time: t, Valid: true} }

// Emailer enqueues a transactional email (satisfied by *queue.Enqueuer). Optional
// — a nil Emailer skips the buyer notification (e.g. admin "run now"). The org
// selects the tenant's sender identity at send time (SAAS.md #4).
type Emailer interface {
	EnqueueEmailForOrg(ctx context.Context, orgID int64, to, template string, data map[string]any) error
}

// MaterializeDue processes every active subscription due on/before `today`,
// creating one order each and advancing its schedule. Each is handled in its own
// transaction so a single failure can't roll back the rest. Returns the number of
// orders created.
func MaterializeDue(ctx context.Context, pool *pgxpool.Pool, today time.Time, mailer Emailer) (int, error) {
	q := gen.New(pool)
	due, err := q.ListDueSubscriptions(ctx, dateOf(today))
	if err != nil {
		return 0, err
	}
	created := 0
	for _, sub := range due {
		ok, err := runOne(ctx, pool, sub, today, mailer)
		if err != nil {
			// Infra error on one subscription — skip it, keep going.
			continue
		}
		if ok {
			created++
		}
	}
	return created, nil
}

// RunNow materializes a single subscription immediately (admin "run now"),
// regardless of its due date. Returns whether an order was created.
func RunNow(ctx context.Context, pool *pgxpool.Pool, sub gen.Subscription, today time.Time, mailer Emailer) (bool, error) {
	return runOne(ctx, pool, sub, today, mailer)
}

// runOne creates an order for one subscription, records a run, and advances the
// schedule — all in a single transaction. It returns (orderCreated, error). A
// subscription with no priceable lines records a "failed" run but still advances
// so it doesn't get stuck, and returns (false, nil).
func runOne(ctx context.Context, pool *pgxpool.Pool, sub gen.Subscription, today time.Time, mailer Emailer) (bool, error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	q := gen.New(tx)
	items, err := q.ListSubscriptionItems(ctx, sub.ID)
	if err != nil {
		return false, err
	}

	type line struct {
		pid                  int64
		qty, unit, price, rt string
	}
	var lines []line
	var totals []string
	var problem string
	for _, it := range items {
		price, perr := q.GetCombinedPrice(ctx, gen.GetCombinedPriceParams{
			CustomerID: sub.CustomerID, ProductID: it.ProductID, Unit: it.Unit, Column4: it.Quantity, Currency: sub.Currency,
		})
		if perr != nil {
			problem = "no current price for " + it.Sku
			break
		}
		rt, e := money.RowTotal(it.Quantity, price.Value, "0")
		if e != nil {
			problem = "bad amount for " + it.Sku
			break
		}
		lines = append(lines, line{it.ProductID, it.Quantity, it.Unit, price.Value, rt})
		totals = append(totals, rt)
	}

	runDate := dateOf(today)
	next := dateOf(AdvanceDate(sub.Cadence, today))

	// No priceable lines → record a failed run, advance, and move on.
	if problem != "" || len(lines) == 0 {
		note := problem
		if note == "" {
			note = "subscription has no items"
		}
		if _, e := q.CreateSubscriptionRun(ctx, gen.CreateSubscriptionRunParams{
			SubscriptionID: sub.ID, RunDate: runDate, Status: "failed", Note: &note,
		}); e != nil {
			return false, e
		}
		if e := q.AdvanceSubscription(ctx, gen.AdvanceSubscriptionParams{ID: sub.ID, NextRunDate: next, LastRunAt: tsOf(today)}); e != nil {
			return false, e
		}
		return false, tx.Commit(ctx)
	}

	subtotal, _ := money.Sum(totals...)

	// Apply the best automatic promotion (subscriptions carry no coupon code).
	discount := "0"
	var promoID *int64
	if cands, e := q.ListActivePromotions(ctx, sub.OrganizationID); e == nil && len(cands) > 0 {
		if res := promotions.Evaluate(subtotal, "", today, promoCandidates(cands)); res.Promotion != nil {
			discount = res.Discount
			pid := res.Promotion.ID
			promoID = &pid
		}
	}
	discountedSub, _ := money.Sub(subtotal, discount)

	billing := snapshotAddr(ctx, q, sub.CustomerID, "billing")
	shipping := snapshotAddr(ctx, q, sub.CustomerID, "shipping")

	taxLines := make([]tax.OrderLine, len(lines))
	for i, ln := range lines {
		taxLines[i] = tax.OrderLine{ProductID: ln.pid, Amount: ln.rt}
	}
	perLineTax, taxTotal, terr := tax.NewService(q).ComputeOrderTax(ctx, sub.OrganizationID, countryOf(shipping), taxLines)
	if terr != nil {
		return false, terr
	}
	grand, _ := money.Sum(discountedSub, taxTotal)

	order, e := q.CreateOrder(ctx, gen.CreateOrderParams{
		OrganizationID: sub.OrganizationID, WebsiteID: sub.WebsiteID, CustomerID: sub.CustomerID,
		CustomerUserID: sub.CustomerUserID, Currency: sub.Currency, PoNumber: sub.PoNumber,
		BillingAddress: billing, ShippingAddress: shipping,
		Subtotal: subtotal, TaxTotal: taxTotal, ShippingTotal: "0", GrandTotal: grand,
	})
	if e != nil {
		return false, e
	}
	for i, ln := range lines {
		sku, name := "", ""
		if p, gerr := q.GetProductByID(ctx, gen.GetProductByIDParams{OrganizationID: sub.OrganizationID, ID: ln.pid}); gerr == nil {
			sku, name = p.Sku, p.Name
		}
		if _, e := q.AddOrderItem(ctx, gen.AddOrderItemParams{
			OrderID: order.ID, ProductID: ln.pid, Sku: sku, Name: name,
			Quantity: ln.qty, Unit: ln.unit, UnitPrice: ln.price, TaxAmount: perLineTax[i], RowTotal: ln.rt,
		}); e != nil {
			return false, e
		}
	}
	if promoID != nil {
		if e := q.SetOrderPromotion(ctx, gen.SetOrderPromotionParams{ID: order.ID, DiscountTotal: discount, PromotionID: promoID}); e != nil {
			return false, e
		}
		cid := sub.CustomerID
		if _, e := q.CreatePromotionRedemption(ctx, gen.CreatePromotionRedemptionParams{
			PromotionID: *promoID, OrderID: &order.ID, CustomerID: &cid, Amount: discount,
		}); e != nil {
			return false, e
		}
		if e := q.IncrementPromotionRedeemed(ctx, *promoID); e != nil {
			return false, e
		}
	}
	if e := marketplace.SplitOrder(ctx, q, sub.OrganizationID, order.ID, sub.Currency); e != nil {
		return false, e
	}
	note := "created from subscription"
	if e := q.AddOrderStatusHistory(ctx, gen.AddOrderStatusHistoryParams{
		OrderID: order.ID, FromStatus: nil, ToStatus: "pending",
		ChangedBy: "subscription:" + strconv.FormatInt(sub.ID, 10), Note: &note,
	}); e != nil {
		return false, e
	}
	oid := order.ID
	if _, e := q.CreateSubscriptionRun(ctx, gen.CreateSubscriptionRunParams{
		SubscriptionID: sub.ID, OrderID: &oid, RunDate: runDate, Status: "success",
	}); e != nil {
		return false, e
	}
	if e := q.AdvanceSubscription(ctx, gen.AdvanceSubscriptionParams{ID: sub.ID, NextRunDate: next, LastRunAt: tsOf(today)}); e != nil {
		return false, e
	}

	// Resolve the buyer email before commit (still inside the tx's view).
	buyerEmail := ""
	if sub.CustomerUserID != nil {
		if em, e := q.GetCustomerUserEmailByID(ctx, *sub.CustomerUserID); e == nil {
			buyerEmail = em
		}
	}
	if e := tx.Commit(ctx); e != nil {
		return false, e
	}

	// Meter the materialized order (count-only — a recurring order is never
	// blocked by quota; the tenant sees the overage on their billing screen).
	_, _ = q.IncrementUsage(ctx, gen.IncrementUsageParams{
		OrganizationID: sub.OrganizationID, Metric: metering.MetricOrders,
		PeriodKey: metering.PeriodKeyFor(metering.MetricOrders, time.Now()), Value: 1,
	})

	// Best-effort buyer notification (post-commit, never blocks the order).
	if mailer != nil && buyerEmail != "" {
		_ = mailer.EnqueueEmailForOrg(ctx, sub.OrganizationID, buyerEmail, "subscription_order_placed", map[string]any{
			"order_public_id": order.PublicID.String(),
			"grand_total":     grand,
			"currency":        sub.Currency,
			"subscription_id": sub.ID,
		})
	}
	return true, nil
}

// snapshotAddr returns the customer's default address of a type as a JSON snapshot.
func snapshotAddr(ctx context.Context, q *gen.Queries, customerID int64, typ string) []byte {
	addr, err := q.GetCustomerDefaultAddress(ctx, gen.GetCustomerDefaultAddressParams{CustomerID: customerID, Type: typ})
	if err != nil {
		return []byte("{}")
	}
	b, _ := json.Marshal(addr)
	return b
}

// countryOf extracts the country field from an address JSON snapshot (for tax).
func countryOf(addr []byte) string {
	if len(addr) == 0 {
		return ""
	}
	var a struct {
		Country string `json:"country"`
	}
	_ = json.Unmarshal(addr, &a)
	return a.Country
}
