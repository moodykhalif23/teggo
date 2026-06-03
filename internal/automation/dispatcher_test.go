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
