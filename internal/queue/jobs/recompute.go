package jobs

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"

	"b2bcommerce/internal/store/gen"
)

// RecomputeCombinedPricesArgs rebuilds the combined_prices cache for one customer
// in one currency (Pack 1 §4). Enqueued when a price, assignment, or a customer's
// group changes. Idempotent: it fully replaces the customer's rows for the currency.
type RecomputeCombinedPricesArgs struct {
	CustomerID int64  `json:"customer_id"`
	WebsiteID  *int64 `json:"website_id"` // for the website-default fallback level
	Currency   string `json:"currency"`
}

func (RecomputeCombinedPricesArgs) Kind() string { return "recompute_combined_prices" }

// RecomputeForCustomer performs the recompute in a single transaction: clear the
// customer's rows for the currency, then re-resolve and flatten the winning
// price list's tiers. Exposed directly so it can be driven synchronously from
// tests and from the worker alike.
func RecomputeForCustomer(ctx context.Context, pool *pgxpool.Pool, args RecomputeCombinedPricesArgs) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op after commit

	q := gen.New(tx)
	if err := q.DeleteCombinedPricesForCustomerCurrency(ctx, gen.DeleteCombinedPricesForCustomerCurrencyParams{
		CustomerID: args.CustomerID,
		Currency:   args.Currency,
	}); err != nil {
		return err
	}
	if err := q.RecomputeCombinedPricesForCustomer(ctx, gen.RecomputeCombinedPricesForCustomerParams{
		CustomerID: args.CustomerID,
		WebsiteID:  args.WebsiteID,
		Currency:   args.Currency,
		ComputedAt: time.Now(),
	}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// RecomputeWorker runs RecomputeCombinedPricesArgs jobs off the queue.
type RecomputeWorker struct {
	river.WorkerDefaults[RecomputeCombinedPricesArgs]
	Pool *pgxpool.Pool
}

func (w *RecomputeWorker) Work(ctx context.Context, job *river.Job[RecomputeCombinedPricesArgs]) error {
	return RecomputeForCustomer(ctx, w.Pool, job.Args)
}
