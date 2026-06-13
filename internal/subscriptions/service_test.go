package subscriptions_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"b2bcommerce/internal/store/gen"
	"b2bcommerce/internal/subscriptions"
	"b2bcommerce/internal/testsupport"
)

func TestAdvanceDate(t *testing.T) {
	base := time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)
	cases := map[string]time.Time{
		"weekly":    base.AddDate(0, 0, 7),
		"biweekly":  base.AddDate(0, 0, 14),
		"monthly":   base.AddDate(0, 1, 0),
		"quarterly": base.AddDate(0, 3, 0),
		"unknown":   base.AddDate(0, 1, 0), // defaults to monthly
	}
	for cadence, want := range cases {
		if got := subscriptions.AdvanceDate(cadence, base); !got.Equal(want) {
			t.Errorf("%s: want %s, got %s", cadence, want.Format("2006-01-02"), got.Format("2006-01-02"))
		}
	}
}

func dateOf(t time.Time) pgtype.Date { return pgtype.Date{Time: t, Valid: true} }

// TestMaterializeDue seeds a priced product + a due subscription and verifies the
// daily sweep creates one order (correctly priced), records a success run, and
// advances the schedule so it isn't due again the same day.
func TestMaterializeDue(t *testing.T) {
	pool := testsupport.NewDB(t)
	q := gen.New(pool)
	ctx := context.Background()

	cust, err := q.CreateCustomer(ctx, gen.CreateCustomerParams{OrganizationID: 1, Name: "Acme", CreditLimit: "0"})
	if err != nil {
		t.Fatalf("customer: %v", err)
	}
	prod, err := q.CreateProduct(ctx, gen.CreateProductParams{
		OrganizationID: 1, Sku: "SUB-1", Type: "simple", Name: "Sub Widget", Slug: "sub-1",
		Status: "active", Attributes: []byte("{}"), Unit: "each",
	})
	if err != nil {
		t.Fatalf("product: %v", err)
	}
	list, err := q.CreatePriceList(ctx, gen.CreatePriceListParams{OrganizationID: 1, Name: "L", Currency: "USD", IsActive: true})
	if err != nil {
		t.Fatalf("price list: %v", err)
	}
	if _, err := q.UpsertPrice(ctx, gen.UpsertPriceParams{PriceListID: list.ID, ProductID: prod.ID, Unit: "each", MinQuantity: "1", Value: "10.0000"}); err != nil {
		t.Fatalf("price: %v", err)
	}
	if _, err := q.CreatePriceListAssignment(ctx, gen.CreatePriceListAssignmentParams{PriceListID: list.ID, CustomerID: &cust.ID}); err != nil {
		t.Fatalf("assign: %v", err)
	}
	// Price resolves live at run time — no cache to prime.

	today := time.Now().UTC()
	sub, err := q.CreateSubscription(ctx, gen.CreateSubscriptionParams{
		OrganizationID: 1, WebsiteID: 1, CustomerID: cust.ID, Currency: "USD", Cadence: "monthly",
		NextRunDate: dateOf(today.AddDate(0, 0, -1)), // due yesterday
	})
	if err != nil {
		t.Fatalf("subscription: %v", err)
	}
	if _, err := q.CreateSubscriptionItem(ctx, gen.CreateSubscriptionItemParams{
		SubscriptionID: sub.ID, ProductID: prod.ID, Quantity: "3", Unit: "each",
	}); err != nil {
		t.Fatalf("item: %v", err)
	}

	// Materialize.
	n, err := subscriptions.MaterializeDue(ctx, pool, today, nil)
	if err != nil {
		t.Fatalf("materialize: %v", err)
	}
	if n != 1 {
		t.Fatalf("orders created: want 1, got %d", n)
	}

	// One order, 3 × 10.00 = 30.00 (no tax configured).
	var ordCount int
	var grand string
	if err := pool.QueryRow(ctx, `SELECT count(*), coalesce(max(grand_total)::text, '0') FROM orders WHERE customer_id = $1`, cust.ID).Scan(&ordCount, &grand); err != nil {
		t.Fatalf("order query: %v", err)
	}
	if ordCount != 1 || grand != "30.0000" {
		t.Fatalf("order: want 1 order @ 30.0000, got %d @ %s", ordCount, grand)
	}

	// A success run referencing the order.
	runs, _ := q.ListSubscriptionRuns(ctx, sub.ID)
	if len(runs) != 1 || runs[0].Status != "success" || runs[0].OrderID == nil {
		t.Fatalf("runs: want 1 success run with order, got %+v", runs)
	}

	// Next run advanced one month → not due again today.
	got, _ := q.GetSubscription(ctx, gen.GetSubscriptionParams{OrganizationID: 1, ID: sub.ID})
	wantNext := subscriptions.AdvanceDate("monthly", today)
	if got.NextRunDate.Time.Format("2006-01-02") != wantNext.Format("2006-01-02") {
		t.Errorf("next_run_date: want %s, got %s", wantNext.Format("2006-01-02"), got.NextRunDate.Time.Format("2006-01-02"))
	}
	n2, _ := subscriptions.MaterializeDue(ctx, pool, today, nil)
	if n2 != 0 {
		t.Errorf("second sweep same day: want 0 orders, got %d", n2)
	}
}

// TestMaterializeAppliesPromotion verifies an active automatic promotion
// discounts the order a subscription run creates.
func TestMaterializeAppliesPromotion(t *testing.T) {
	pool := testsupport.NewDB(t)
	q := gen.New(pool)
	ctx := context.Background()

	cust, _ := q.CreateCustomer(ctx, gen.CreateCustomerParams{OrganizationID: 1, Name: "Acme", CreditLimit: "0"})
	prod, _ := q.CreateProduct(ctx, gen.CreateProductParams{OrganizationID: 1, Sku: "PROMO-SUB", Type: "simple", Name: "P", Slug: "promo-sub", Status: "active", Attributes: []byte("{}"), Unit: "each"})
	list, _ := q.CreatePriceList(ctx, gen.CreatePriceListParams{OrganizationID: 1, Name: "L", Currency: "USD", IsActive: true})
	_, _ = q.UpsertPrice(ctx, gen.UpsertPriceParams{PriceListID: list.ID, ProductID: prod.ID, Unit: "each", MinQuantity: "1", Value: "10.0000"})
	_, _ = q.CreatePriceListAssignment(ctx, gen.CreatePriceListAssignmentParams{PriceListID: list.ID, CustomerID: &cust.ID})

	// Automatic 10%-off promotion.
	if _, err := q.CreatePromotion(ctx, gen.CreatePromotionParams{OrganizationID: 1, Name: "10% off", DiscountType: "percent", DiscountValue: "10", IsActive: true}); err != nil {
		t.Fatalf("promotion: %v", err)
	}

	today := time.Now().UTC()
	sub, _ := q.CreateSubscription(ctx, gen.CreateSubscriptionParams{OrganizationID: 1, WebsiteID: 1, CustomerID: cust.ID, Currency: "USD", Cadence: "monthly", NextRunDate: dateOf(today.AddDate(0, 0, -1))})
	_, _ = q.CreateSubscriptionItem(ctx, gen.CreateSubscriptionItemParams{SubscriptionID: sub.ID, ProductID: prod.ID, Quantity: "3", Unit: "each"})

	if n, err := subscriptions.MaterializeDue(ctx, pool, today, nil); err != nil || n != 1 {
		t.Fatalf("materialize: n=%d err=%v", n, err)
	}

	// 3 × 10 = 30, less 10% = 27.0000; discount_total 3.0000.
	var grand, disc string
	if err := pool.QueryRow(ctx, `SELECT grand_total::text, discount_total::text FROM orders WHERE customer_id=$1`, cust.ID).Scan(&grand, &disc); err != nil {
		t.Fatalf("order query: %v", err)
	}
	if grand != "27.0000" || disc != "3.0000" {
		t.Fatalf("discounted order: want grand 27.0000 / discount 3.0000, got %s / %s", grand, disc)
	}
}
