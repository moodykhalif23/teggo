package jobs

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"

	"b2bcommerce/internal/subscriptions"
)

// MaterializeDueSubscriptionsArgs triggers the daily sweep that turns due
// subscriptions into orders. No fields — the worker queries for what's due.
type MaterializeDueSubscriptionsArgs struct{}

func (MaterializeDueSubscriptionsArgs) Kind() string { return "materialize_due_subscriptions" }

type MaterializeSubscriptionsWorker struct {
	river.WorkerDefaults[MaterializeDueSubscriptionsArgs]
	Pool   *pgxpool.Pool
	Mailer subscriptions.Emailer // optional; sends buyer order-placed emails
}

func (w *MaterializeSubscriptionsWorker) Work(ctx context.Context, _ *river.Job[MaterializeDueSubscriptionsArgs]) error {
	_, err := subscriptions.MaterializeDue(ctx, w.Pool, time.Now().UTC(), w.Mailer)
	return err
}
