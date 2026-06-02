package jobs

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"

	"b2bcommerce/internal/store/gen"
)

// GenerateInvoicePDFArgs renders an invoice to a PDF and stores its URL
// (Pack 1 §7: PDF generation is async). The real renderer is a headless-Chromium
// service (Gotenberg, PRD §16) wired via the integration framework; until that
// lands this job records a deterministic placeholder URL so the flow is complete
// and testable end-to-end.
type GenerateInvoicePDFArgs struct {
	InvoiceID int64 `json:"invoice_id"`
}

func (GenerateInvoicePDFArgs) Kind() string { return "generate_invoice_pdf" }

// GenerateInvoicePDF performs the work; exposed directly so tests can drive it
// synchronously and the worker can call it off the queue.
func GenerateInvoicePDF(ctx context.Context, pool *pgxpool.Pool, invoiceID int64) error {
	q := gen.New(pool)
	inv, err := q.GetInvoiceByIDInternal(ctx, invoiceID)
	if err != nil {
		return err
	}
	// TODO: POST the rendered invoice HTML to Gotenberg and upload the returned
	// PDF to blob storage; use that URL here.
	url := fmt.Sprintf("/invoices/%s.pdf", inv.PublicID.String())
	return q.SetInvoicePDFURL(ctx, gen.SetInvoicePDFURLParams{ID: invoiceID, PdfUrl: &url})
}

// InvoicePDFWorker runs GenerateInvoicePDFArgs jobs off the queue.
type InvoicePDFWorker struct {
	river.WorkerDefaults[GenerateInvoicePDFArgs]
	Pool *pgxpool.Pool
}

func (w *InvoicePDFWorker) Work(ctx context.Context, job *river.Job[GenerateInvoicePDFArgs]) error {
	return GenerateInvoicePDF(ctx, w.Pool, job.Args.InvoiceID)
}
