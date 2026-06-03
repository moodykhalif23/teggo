package automation

import (
	"context"
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
