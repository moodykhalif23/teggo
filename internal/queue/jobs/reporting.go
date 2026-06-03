package jobs

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"

	"b2bcommerce/internal/modules/reporting"
)

// RefreshReportingArgs refreshes the reporting materialized views (Pack 3 §1).
// Inserted by a daily river periodic job; also re-runnable on demand.
type RefreshReportingArgs struct{}

func (RefreshReportingArgs) Kind() string { return "refresh_reporting_views" }

type RefreshReportingWorker struct {
	river.WorkerDefaults[RefreshReportingArgs]
	Pool *pgxpool.Pool
}

func (w *RefreshReportingWorker) Work(ctx context.Context, _ *river.Job[RefreshReportingArgs]) error {
	return reporting.RefreshViews(ctx, w.Pool)
}
