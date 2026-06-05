package automation

import (
	"context"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"b2bcommerce/internal/store/gen"
)

// Action is a unit of automation work, referenced by Key() in rule config and
// executed (as a river job) by the worker.
type Action interface {
	Key() string
	Run(ctx context.Context, params, payload map[string]any) error
}

// Registry maps action keys to implementations (populated at worker boot).
type Registry struct{ actions map[string]Action }

func NewRegistry() *Registry { return &Registry{actions: map[string]Action{}} }

func (r *Registry) Register(a Action) { r.actions[a.Key()] = a }

// Run executes a registered action; an unknown key is a no-op (config may
// reference actions not present in this build).
func (r *Registry) Run(ctx context.Context, key string, params, payload map[string]any) error {
	a, ok := r.actions[key]
	if !ok {
		return nil
	}
	return a.Run(ctx, params, payload)
}

// EmailEnqueuer schedules transactional email (satisfied by *queue.Enqueuer).
type EmailEnqueuer interface {
	EnqueueEmail(ctx context.Context, to, template string, data map[string]any) error
}

// EmailCustomer is the `email_customer` action: it sends a template (param
// "template") to the primary contact of the customer named in the event
// payload ("customer_id"), passing the whole payload as template data. Used by
// the order.status_changed automation rule to notify buyers of status changes.
type EmailCustomer struct {
	pool  *pgxpool.Pool
	email EmailEnqueuer
}

func NewEmailCustomer(pool *pgxpool.Pool, email EmailEnqueuer) EmailCustomer {
	return EmailCustomer{pool: pool, email: email}
}

func (EmailCustomer) Key() string { return "email_customer" }

func (a EmailCustomer) Run(ctx context.Context, params, payload map[string]any) error {
	if a.email == nil {
		return nil
	}
	template, _ := params["template"].(string)
	if template == "" {
		return nil
	}
	cid, ok := payloadInt(payload, "customer_id")
	if !ok {
		return nil
	}
	users, err := gen.New(a.pool).ListCustomerUsers(ctx, cid)
	if err != nil || len(users) == 0 {
		return nil
	}
	data := map[string]any{"name": users[0].FullName}
	for k, v := range payload {
		data[k] = v
	}
	return a.email.EnqueueEmail(ctx, users[0].Email, template, data)
}

// payloadInt coerces a JSON payload value (float64/int/string) to int64.
func payloadInt(payload map[string]any, key string) (int64, bool) {
	switch n := payload[key].(type) {
	case float64:
		return int64(n), true
	case int64:
		return n, true
	case int:
		return int64(n), true
	case string:
		v, err := strconv.ParseInt(n, 10, 64)
		return v, err == nil
	default:
		return 0, false
	}
}

// ExpireQuotes is the `expire_quotes` action: it flips open quotes whose
// validity has passed to `expired` and notifies the customer. Wired to the
// schedule.hourly automation rule (Pack 2 §3.6 quote-expiry example).
type ExpireQuotes struct {
	pool  *pgxpool.Pool
	email EmailEnqueuer
}

func NewExpireQuotes(pool *pgxpool.Pool, email EmailEnqueuer) ExpireQuotes {
	return ExpireQuotes{pool: pool, email: email}
}

func (ExpireQuotes) Key() string { return "expire_quotes" }

func (a ExpireQuotes) Run(ctx context.Context, _, _ map[string]any) error {
	q := gen.New(a.pool)
	quotes, err := q.ListExpirableQuotes(ctx, pgtype.Timestamptz{Time: time.Now(), Valid: true})
	if err != nil {
		return err
	}
	for _, qt := range quotes {
		if _, err := q.SetQuoteStatus(ctx, gen.SetQuoteStatusParams{ID: qt.ID, Status: "expired"}); err != nil {
			return err
		}
		if a.email == nil {
			continue
		}
		users, err := q.ListCustomerUsers(ctx, qt.CustomerID)
		if err != nil || len(users) == 0 {
			continue
		}
		_ = a.email.EnqueueEmail(ctx, users[0].Email, "quote_expired", map[string]any{
			"name":         users[0].FullName,
			"quote_number": "Q-" + qt.PublicID.String()[:8],
		})
	}
	return nil
}

// MarkOverdue is the `mark_overdue` action (revenue ops / dunning): it flips
// every past-due issued invoice to 'overdue' and dunns the customer's primary
// contact. Wired to a schedule.hourly/daily automation rule, like expire_quotes.
type MarkOverdue struct {
	pool  *pgxpool.Pool
	email EmailEnqueuer
}

func NewMarkOverdue(pool *pgxpool.Pool, email EmailEnqueuer) MarkOverdue {
	return MarkOverdue{pool: pool, email: email}
}

func (MarkOverdue) Key() string { return "mark_overdue" }

func (a MarkOverdue) Run(ctx context.Context, _, _ map[string]any) error {
	q := gen.New(a.pool)
	invoices, err := q.MarkOverdueInvoicesGlobal(ctx)
	if err != nil {
		return err
	}
	for _, inv := range invoices {
		if a.email == nil {
			continue
		}
		users, err := q.ListCustomerUsers(ctx, inv.CustomerID)
		if err != nil || len(users) == 0 {
			continue
		}
		_ = a.email.EnqueueEmail(ctx, users[0].Email, "invoice_overdue", map[string]any{
			"name":           users[0].FullName,
			"invoice_number": "INV-" + inv.PublicID.String()[:8],
			"amount":         inv.GrandTotal,
			"currency":       inv.Currency,
		})
	}
	return nil
}

// paramInt reads an integer action param with a default (JSON numbers decode as
// float64).
func paramInt(params map[string]any, key string, def int) int {
	if params != nil {
		if v, ok := params[key]; ok {
			switch t := v.(type) {
			case float64: // JSON number
				return int(t)
			case string: // the admin rule builder stores params as strings
				if n, err := strconv.Atoi(t); err == nil {
					return n
				}
			}
		}
	}
	return def
}

// QuoteFollowup is the `quote_followup` action: nudge buyers about 'sent' quotes
// expiring within `within_days` (default 3), once per quote.
type QuoteFollowup struct {
	pool  *pgxpool.Pool
	email EmailEnqueuer
}

func NewQuoteFollowup(pool *pgxpool.Pool, email EmailEnqueuer) QuoteFollowup {
	return QuoteFollowup{pool: pool, email: email}
}

func (QuoteFollowup) Key() string { return "quote_followup" }

func (a QuoteFollowup) Run(ctx context.Context, params, _ map[string]any) error {
	q := gen.New(a.pool)
	withinDays := paramInt(params, "within_days", 3)
	cutoff := pgtype.Timestamptz{Time: time.Now().Add(time.Duration(withinDays) * 24 * time.Hour), Valid: true}
	quotes, err := q.ListQuotesForFollowup(ctx, cutoff)
	if err != nil {
		return err
	}
	for _, qt := range quotes {
		if a.email != nil {
			if users, err := q.ListCustomerUsers(ctx, qt.CustomerID); err == nil && len(users) > 0 {
				_ = a.email.EnqueueEmail(ctx, users[0].Email, "quote_followup", map[string]any{
					"name":         users[0].FullName,
					"quote_number": "Q-" + qt.PublicID.String()[:8],
				})
			}
		}
		if err := q.MarkQuoteFollowedUp(ctx, qt.ID); err != nil {
			return err
		}
	}
	return nil
}

// CartRecovery is the `cart_recovery` action: nudge buyers about active carts
// idle longer than `idle_hours` (default 24), once per idle episode.
type CartRecovery struct {
	pool  *pgxpool.Pool
	email EmailEnqueuer
}

func NewCartRecovery(pool *pgxpool.Pool, email EmailEnqueuer) CartRecovery {
	return CartRecovery{pool: pool, email: email}
}

func (CartRecovery) Key() string { return "cart_recovery" }

func (a CartRecovery) Run(ctx context.Context, params, _ map[string]any) error {
	q := gen.New(a.pool)
	idleHours := paramInt(params, "idle_hours", 24)
	cutoff := time.Now().Add(-time.Duration(idleHours) * time.Hour)
	carts, err := q.ListAbandonedCarts(ctx, cutoff)
	if err != nil {
		return err
	}
	for _, c := range carts {
		if a.email != nil {
			if users, err := q.ListCustomerUsers(ctx, c.CustomerID); err == nil && len(users) > 0 {
				_ = a.email.EnqueueEmail(ctx, users[0].Email, "cart_recovery", map[string]any{
					"name": users[0].FullName,
				})
			}
		}
		if err := q.MarkCartReminded(ctx, c.ID); err != nil {
			return err
		}
	}
	return nil
}
