package queue

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"

	"b2bcommerce/internal/automation"
	"b2bcommerce/internal/blob"
	"b2bcommerce/internal/email"
	"b2bcommerce/internal/imageproc"
	"b2bcommerce/internal/pdf"
	"b2bcommerce/internal/queue/jobs"
)

// NewWorkerClient builds the worker-side river client. renderer is used by the
// invoice-PDF worker (Gotenberg in production, a stub when none is configured);
// sender by the send_email worker (SMTP in production, a log transport otherwise).
// It also wires the automation engine: registered actions (e.g. expire_quotes)
// run via run_automation_action, and an hourly periodic job emits
// schedule.hourly into the dispatcher (driving quote-expiry, overdue sweeps).
func NewWorkerClient(pool *pgxpool.Pool, renderer pdf.Renderer, sender email.Sender, store blob.Store, proc imageproc.Processor) (*river.Client[pgx.Tx], error) {
	enq, err := NewEnqueuer(pool)
	if err != nil {
		return nil, err
	}
	dispatcher := automation.NewDispatcher(pool, enq)
	reg := automation.NewRegistry()
	reg.Register(automation.NewExpireQuotes(pool, enq))
	reg.Register(automation.NewEmailCustomer(pool, enq))
	reg.Register(automation.NewMarkOverdue(pool, enq))
	reg.Register(automation.NewQuoteFollowup(pool, enq))
	reg.Register(automation.NewCartRecovery(pool, enq))

	workers := river.NewWorkers()
	river.AddWorker(workers, &jobs.SendEmailWorker{Sender: sender})
	river.AddWorker(workers, &jobs.RecomputeWorker{Pool: pool})
	river.AddWorker(workers, &jobs.InvoicePDFWorker{Pool: pool, Renderer: renderer})
	river.AddWorker(workers, &jobs.AutomationActionWorker{Registry: reg})
	river.AddWorker(workers, &jobs.ScheduledEmitWorker{Dispatcher: dispatcher})
	river.AddWorker(workers, &jobs.DispatchEventWorker{Dispatcher: dispatcher})
	river.AddWorker(workers, &jobs.RefreshReportingWorker{Pool: pool})
	river.AddWorker(workers, &jobs.GenerateRenditionWorker{Pool: pool, Store: store, Proc: proc})
	river.AddWorker(workers, &jobs.RunReportSchedulesWorker{Pool: pool, Mailer: enq})
	river.AddWorker(workers, &jobs.ERPSyncWorker{Pool: pool})
	// Register additional workers here as modules add jobs.

	periodic := []*river.PeriodicJob{
		river.NewPeriodicJob(
			river.PeriodicInterval(time.Hour),
			func() (river.JobArgs, *river.InsertOpts) {
				return jobs.EmitScheduledArgs{Event: "schedule.hourly"}, nil
			},
			&river.PeriodicJobOpts{RunOnStart: false},
		),
		// Keep the reporting dashboards' materialized views fresh.
		river.NewPeriodicJob(
			river.PeriodicInterval(time.Hour),
			func() (river.JobArgs, *river.InsertOpts) {
				return jobs.RefreshReportingArgs{}, nil
			},
			&river.PeriodicJobOpts{RunOnStart: true},
		),
		// Sweep due report schedules (custom report builder exports).
		river.NewPeriodicJob(
			river.PeriodicInterval(time.Hour),
			func() (river.JobArgs, *river.InsertOpts) {
				return jobs.RunReportSchedulesArgs{}, nil
			},
			&river.PeriodicJobOpts{RunOnStart: false},
		),
		// Sweep confirmed orders + issued invoices to active ERP connections.
		river.NewPeriodicJob(
			river.PeriodicInterval(time.Hour),
			func() (river.JobArgs, *river.InsertOpts) {
				return jobs.ERPSyncArgs{}, nil
			},
			&river.PeriodicJobOpts{RunOnStart: false},
		),
	}

	return river.NewClient(riverpgxv5.New(pool), &river.Config{
		Queues: map[string]river.QueueConfig{
			river.QueueDefault: {MaxWorkers: 50},
		},
		Workers:      workers,
		PeriodicJobs: periodic,
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

// EnqueueRendition schedules derivation of one preset rendition for an asset.
func (e *Enqueuer) EnqueueRendition(ctx context.Context, mediaAssetID int64, preset string) error {
	_, err := e.ic.Insert(ctx, jobs.GenerateRenditionArgs{MediaAssetID: mediaAssetID, Preset: preset}, nil)
	return err
}

// EnqueueEmail schedules a transactional email (rendered from a template key +
// data by the send_email worker). A nil/empty recipient is a no-op.
func (e *Enqueuer) EnqueueEmail(ctx context.Context, to, template string, data map[string]any) error {
	if to == "" {
		return nil
	}
	raw, err := json.Marshal(data)
	if err != nil {
		return err
	}
	_, err = e.ic.Insert(ctx, jobs.SendEmailArgs{To: to, Template: template, Data: raw}, nil)
	return err
}

// EmitEvent enqueues a domain event for the automation dispatcher to process
// (the per-entity half: order.status_changed, quote.created, …). Emitted by the
// API after a state change commits, so a failed rule never affects the request.
func (e *Enqueuer) EmitEvent(ctx context.Context, event string, payload map[string]any) error {
	pl, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = e.ic.Insert(ctx, jobs.DispatchEventArgs{Event: event, Payload: pl}, nil)
	return err
}

// EnqueueAutomationAction schedules one automation action (used by the
// dispatcher when a rule matches).
func (e *Enqueuer) EnqueueAutomationAction(ctx context.Context, key string, params, payload map[string]any) error {
	pp, err := json.Marshal(params)
	if err != nil {
		return err
	}
	pl, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = e.ic.Insert(ctx, jobs.RunAutomationActionArgs{Key: key, Params: pp, Payload: pl}, nil)
	return err
}
