package queue

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"

	"b2bcommerce/internal/queue/jobs"
)

func NewWorkerClient(pool *pgxpool.Pool) (*river.Client[pgx.Tx], error) {
	workers := river.NewWorkers()
	river.AddWorker(workers, &jobs.SendEmailWorker{})
	river.AddWorker(workers, &jobs.RecomputeWorker{Pool: pool})
	river.AddWorker(workers, &jobs.InvoicePDFWorker{Pool: pool})
	// Register additional workers here as modules add jobs.

	return river.NewClient(riverpgxv5.New(pool), &river.Config{
		Queues: map[string]river.QueueConfig{
			river.QueueDefault: {MaxWorkers: 50},
		},
		Workers: workers,
	})
}

// NewInsertClient builds an insert-only river client (used by the API to enqueue).
func NewInsertClient(pool *pgxpool.Pool) (*river.Client[pgx.Tx], error) {
	return river.NewClient(riverpgxv5.New(pool), &river.Config{})
}

// Enqueuer wraps an insert-only river client to provide the typed enqueue calls
// the HTTP handlers need (so handlers depend on a small interface, not river).
type Enqueuer struct {
	ic *river.Client[pgx.Tx]
}

// NewEnqueuer builds an Enqueuer backed by an insert-only client.
func NewEnqueuer(pool *pgxpool.Pool) (*Enqueuer, error) {
	ic, err := NewInsertClient(pool)
	if err != nil {
		return nil, err
	}
	return &Enqueuer{ic: ic}, nil
}

// EnqueueRecompute schedules a combined_prices rebuild for one customer/currency.
func (e *Enqueuer) EnqueueRecompute(ctx context.Context, customerID int64, websiteID *int64, currency string) error {
	_, err := e.ic.Insert(ctx, jobs.RecomputeCombinedPricesArgs{
		CustomerID: customerID,
		WebsiteID:  websiteID,
		Currency:   currency,
	}, nil)
	return err
}

// EnqueueInvoicePDF schedules PDF generation for an issued invoice.
func (e *Enqueuer) EnqueueInvoicePDF(ctx context.Context, invoiceID int64) error {
	_, err := e.ic.Insert(ctx, jobs.GenerateInvoicePDFArgs{InvoiceID: invoiceID}, nil)
	return err
}
