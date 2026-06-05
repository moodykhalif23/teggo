package automation_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"b2bcommerce/internal/automation"
	"b2bcommerce/internal/store/gen"
	"b2bcommerce/internal/testsupport"
)

// fakeEnq captures enqueued action keys instead of scheduling river jobs.
type fakeEnq struct{ keys []string }

func (f *fakeEnq) EnqueueAutomationAction(_ context.Context, key string, _, _ map[string]any) error {
	f.keys = append(f.keys, key)
	return nil
}

func TestDispatcherMatchesConditionsAndEnqueues(t *testing.T) {
	pool := testsupport.NewDB(t)
	ctx := context.Background()
	q := gen.New(pool)

	rule, err := q.CreateAutomationRule(ctx, gen.CreateAutomationRuleParams{
		OrganizationID: 1, Name: "big orders", TriggerEvent: "test.event",
		Conditions: []byte(`[{"field":"amount","op":"gt","value":100}]`),
		Actions:    []byte(`[{"key":"do_thing"}]`),
		IsActive:   true,
	})
	if err != nil {
		t.Fatalf("rule: %v", err)
	}

	enq := &fakeEnq{}
	d := automation.NewDispatcher(pool, enq)

	// Matching payload → action enqueued + one execution recorded.
	if err := d.Emit(ctx, "test.event", map[string]any{"amount": 150}); err != nil {
		t.Fatalf("emit match: %v", err)
	}
	if len(enq.keys) != 1 || enq.keys[0] != "do_thing" {
		t.Fatalf("want [do_thing] enqueued, got %v", enq.keys)
	}

	// Non-matching payload → nothing enqueued, no new execution.
	if err := d.Emit(ctx, "test.event", map[string]any{"amount": 50}); err != nil {
		t.Fatalf("emit no-match: %v", err)
	}
	if len(enq.keys) != 1 {
		t.Errorf("non-matching rule should not enqueue, got %v", enq.keys)
	}

	n, _ := q.CountAutomationExecutions(ctx, rule.ID)
	if n != 1 {
		t.Errorf("executions: want 1 (one match), got %d", n)
	}
}

// fakeEmail counts quote_expired notifications.
type fakeEmail struct{ expired int }

func (f *fakeEmail) EnqueueEmail(_ context.Context, _, template string, _ map[string]any) error {
	if template == "quote_expired" {
		f.expired++
	}
	return nil
}

func TestExpireQuotesSweep(t *testing.T) {
	pool := testsupport.NewDB(t)
	ctx := context.Background()
	q := gen.New(pool)

	cust, _ := q.CreateCustomer(ctx, gen.CreateCustomerParams{OrganizationID: 1, Name: "Acme", CreditLimit: "0"})
	_, _ = q.CreateCustomerUser(ctx, gen.CreateCustomerUserParams{
		CustomerID: cust.ID, Email: "buyer@acme.test", PasswordHash: "x", FullName: "Buyer", Role: "buyer",
	})

	past := pgtype.Timestamptz{Time: time.Now().Add(-time.Hour), Valid: true}
	future := pgtype.Timestamptz{Time: time.Now().Add(24 * time.Hour), Valid: true}
	mk := func(valid pgtype.Timestamptz) int64 {
		var id int64
		if err := pool.QueryRow(ctx,
			`INSERT INTO quotes (organization_id, website_id, customer_id, status, currency, version, valid_until, subtotal)
			 VALUES (1, 1, $1, 'sent', 'USD', 1, $2, '0') RETURNING id`, cust.ID, valid).Scan(&id); err != nil {
			t.Fatalf("seed quote: %v", err)
		}
		return id
	}
	expiredID := mk(past)
	freshID := mk(future)

	mail := &fakeEmail{}
	if err := automation.NewExpireQuotes(pool, mail).Run(ctx, nil, nil); err != nil {
		t.Fatalf("expire_quotes: %v", err)
	}

	status := func(id int64) string {
		var s string
		_ = pool.QueryRow(ctx, `SELECT status FROM quotes WHERE id=$1`, id).Scan(&s)
		return s
	}
	if status(expiredID) != "expired" {
		t.Errorf("past-validity quote: want expired, got %s", status(expiredID))
	}
	if status(freshID) != "sent" {
		t.Errorf("future-validity quote should stay sent, got %s", status(freshID))
	}
	if mail.expired != 1 {
		t.Errorf("quote_expired emails: want 1, got %d", mail.expired)
	}
}

// recEmail records (to, template) of every enqueued email.
type recEmail struct{ sent []string }

func (r *recEmail) EnqueueEmail(_ context.Context, to, template string, _ map[string]any) error {
	r.sent = append(r.sent, to+"|"+template)
	return nil
}

func TestEmailCustomerAction(t *testing.T) {
	pool := testsupport.NewDB(t)
	ctx := context.Background()
	q := gen.New(pool)
	cust, _ := q.CreateCustomer(ctx, gen.CreateCustomerParams{OrganizationID: 1, Name: "Acme", CreditLimit: "0"})
	_, _ = q.CreateCustomerUser(ctx, gen.CreateCustomerUserParams{
		CustomerID: cust.ID, Email: "buyer@acme.test", PasswordHash: "x", FullName: "Buyer", Role: "buyer",
	})

	mail := &recEmail{}
	act := automation.NewEmailCustomer(pool, mail)
	// Payload mirrors what the order.status_changed event carries (customer_id
	// arrives as a JSON number → float64).
	err := act.Run(ctx, map[string]any{"template": "order_status_update"},
		map[string]any{"customer_id": float64(cust.ID), "order_number": "ORD-abc12345", "status": "confirmed"})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(mail.sent) != 1 || mail.sent[0] != "buyer@acme.test|order_status_update" {
		t.Fatalf("want one order_status_update email to the buyer, got %v", mail.sent)
	}
}

// countEmail tallies enqueued emails by template.
type countEmail struct{ byTemplate map[string]int }

func (c *countEmail) EnqueueEmail(_ context.Context, _, template string, _ map[string]any) error {
	if c.byTemplate == nil {
		c.byTemplate = map[string]int{}
	}
	c.byTemplate[template]++
	return nil
}

func TestQuoteFollowupSweep(t *testing.T) {
	pool := testsupport.NewDB(t)
	ctx := context.Background()
	q := gen.New(pool)

	cust, _ := q.CreateCustomer(ctx, gen.CreateCustomerParams{OrganizationID: 1, Name: "Acme", CreditLimit: "0"})
	_, _ = q.CreateCustomerUser(ctx, gen.CreateCustomerUserParams{CustomerID: cust.ID, Email: "buyer@acme.test", PasswordHash: "x", FullName: "Buyer", Role: "buyer"})

	mk := func(daysOut int) int64 {
		var id int64
		_ = pool.QueryRow(ctx,
			`INSERT INTO quotes (organization_id, website_id, customer_id, status, currency, version, valid_until, subtotal)
			 VALUES (1, 1, $1, 'sent', 'USD', 1, now() + make_interval(days => $2), '0') RETURNING id`, cust.ID, daysOut).Scan(&id)
		return id
	}
	soon := mk(2)   // within the default 3-day window
	later := mk(10) // outside it

	mail := &countEmail{}
	if err := automation.NewQuoteFollowup(pool, mail).Run(ctx, nil, nil); err != nil {
		t.Fatalf("quote_followup: %v", err)
	}
	if mail.byTemplate["quote_followup"] != 1 {
		t.Fatalf("quote_followup emails: want 1, got %d", mail.byTemplate["quote_followup"])
	}
	fu := func(id int64) bool {
		var set bool
		_ = pool.QueryRow(ctx, `SELECT followup_at IS NOT NULL FROM quotes WHERE id=$1`, id).Scan(&set)
		return set
	}
	if !fu(soon) || fu(later) {
		t.Errorf("followup_at: soon should be set, later not (soon=%v later=%v)", fu(soon), fu(later))
	}
	// Re-run is a no-op (dedup).
	mail2 := &countEmail{}
	_ = automation.NewQuoteFollowup(pool, mail2).Run(ctx, nil, nil)
	if mail2.byTemplate["quote_followup"] != 0 {
		t.Errorf("re-run should not re-nudge, got %d", mail2.byTemplate["quote_followup"])
	}
}

func TestCartRecoverySweep(t *testing.T) {
	pool := testsupport.NewDB(t)
	ctx := context.Background()
	q := gen.New(pool)

	cust, _ := q.CreateCustomer(ctx, gen.CreateCustomerParams{OrganizationID: 1, Name: "Acme", CreditLimit: "0"})
	_, _ = q.CreateCustomerUser(ctx, gen.CreateCustomerUserParams{CustomerID: cust.ID, Email: "buyer@acme.test", PasswordHash: "x", FullName: "Buyer", Role: "buyer"})
	prod, _ := q.CreateProduct(ctx, gen.CreateProductParams{OrganizationID: 1, Sku: "CR-1", Type: "simple", Name: "Cart Widget", Slug: "cr-1", Status: "active", Attributes: []byte("{}"), Unit: "each"})

	mkCart := func(idleDays int) int64 {
		c, _ := q.CreateCart(ctx, gen.CreateCartParams{CustomerID: cust.ID, WebsiteID: 1, Currency: "USD"})
		_, _ = q.UpsertCartItem(ctx, gen.UpsertCartItemParams{CartID: c.ID, ProductID: prod.ID, Quantity: "1", Unit: "each", UnitPrice: "10"})
		if idleDays > 0 {
			_, _ = pool.Exec(ctx, `ALTER TABLE carts DISABLE TRIGGER trg_carts_updated`)
			_, _ = pool.Exec(ctx, `UPDATE carts SET updated_at = now() - make_interval(days => $1) WHERE id = $2`, idleDays, c.ID)
			_, _ = pool.Exec(ctx, `ALTER TABLE carts ENABLE TRIGGER trg_carts_updated`)
		}
		return c.ID
	}
	stale := mkCart(2) // idle 2 days -> recover (default idle 24h)
	fresh := mkCart(0) // just touched -> skip

	mail := &countEmail{}
	if err := automation.NewCartRecovery(pool, mail).Run(ctx, nil, nil); err != nil {
		t.Fatalf("cart_recovery: %v", err)
	}
	if mail.byTemplate["cart_recovery"] != 1 {
		t.Fatalf("cart_recovery emails: want 1, got %d", mail.byTemplate["cart_recovery"])
	}
	reminded := func(id int64) bool {
		var set bool
		_ = pool.QueryRow(ctx, `SELECT reminded_at IS NOT NULL FROM carts WHERE id=$1`, id).Scan(&set)
		return set
	}
	if !reminded(stale) || reminded(fresh) {
		t.Errorf("reminded_at: stale set, fresh not (stale=%v fresh=%v)", reminded(stale), reminded(fresh))
	}
	// Re-run is a no-op (reminded_at >= updated_at).
	mail2 := &countEmail{}
	_ = automation.NewCartRecovery(pool, mail2).Run(ctx, nil, nil)
	if mail2.byTemplate["cart_recovery"] != 0 {
		t.Errorf("re-run should not re-remind, got %d", mail2.byTemplate["cart_recovery"])
	}
}
