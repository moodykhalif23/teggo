package jobs

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"

	"b2bcommerce/internal/pdf"
	"b2bcommerce/internal/store/gen"
)

// GenerateInvoicePDFArgs renders an invoice to a PDF (Pack 1 §7: PDF generation
// is async). The renderer is Gotenberg (headless Chromium) in production; the
// resulting bytes are stored in invoice_documents and the invoice's pdf_url is
// set to the capability URL the API serves them at.
type GenerateInvoicePDFArgs struct {
	InvoiceID int64 `json:"invoice_id"`
}

func (GenerateInvoicePDFArgs) Kind() string { return "generate_invoice_pdf" }

// GenerateInvoicePDF performs the work; exposed directly so tests can drive it
// synchronously and the worker can call it off the queue.
func GenerateInvoicePDF(ctx context.Context, pool *pgxpool.Pool, renderer pdf.Renderer, invoiceID int64) error {
	q := gen.New(pool)
	row, err := q.GetInvoiceForRender(ctx, invoiceID)
	if err != nil {
		return err
	}
	items, err := q.ListInvoiceItems(ctx, invoiceID)
	if err != nil {
		return err
	}

	lines := make([]pdf.InvoiceLine, 0, len(items))
	for _, it := range items {
		lines = append(lines, pdf.InvoiceLine{
			Description: it.Description, Quantity: it.Quantity,
			UnitPrice: it.UnitPrice, RowTotal: it.RowTotal,
		})
	}

	data := pdf.InvoiceData{
		OrganizationName: row.OrganizationName,
		CustomerName:     row.CustomerName,
		Number:           "INV-" + shortID(row.PublicID.String()),
		Status:           row.Status,
		Currency:         row.Currency,
		IssuedAt:         fmtDate(row.IssuedAt),
		DueAt:            fmtDate(row.DueAt),
		OrderNumber:      "ORD-" + shortID(row.OrderPublicID.String()),
		PONumber:         deref(row.PoNumber),
		Billing:          parseAddr(row.BillingAddress),
		Shipping:         parseAddr(row.ShippingAddress),
		Lines:            lines,
		Subtotal:         row.Subtotal,
		TaxTotal:         row.TaxTotal,
		GrandTotal:       row.GrandTotal,
	}

	html, err := pdf.RenderInvoiceHTML(data)
	if err != nil {
		return err
	}
	bytesPDF, err := renderer.Render(ctx, html)
	if err != nil {
		return err
	}

	if err := q.UpsertInvoiceDocument(ctx, gen.UpsertInvoiceDocumentParams{
		InvoiceID: invoiceID, ContentType: "application/pdf", Bytes: bytesPDF,
	}); err != nil {
		return err
	}
	url := fmt.Sprintf("/files/invoices/%s.pdf", row.PublicID.String())
	return q.SetInvoicePDFURL(ctx, gen.SetInvoicePDFURLParams{ID: invoiceID, PdfUrl: &url})
}

func shortID(s string) string {
	if len(s) >= 8 {
		return s[:8]
	}
	return s
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func fmtDate(t pgtype.Timestamptz) string {
	if !t.Valid {
		return "—"
	}
	return t.Time.Format("2006-01-02")
}

func parseAddr(raw []byte) pdf.Address {
	var a pdf.Address
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &a)
	}
	return a
}

// InvoicePDFWorker runs GenerateInvoicePDFArgs jobs off the queue.
type InvoicePDFWorker struct {
	river.WorkerDefaults[GenerateInvoicePDFArgs]
	Pool     *pgxpool.Pool
	Renderer pdf.Renderer
}

func (w *InvoicePDFWorker) Work(ctx context.Context, job *river.Job[GenerateInvoicePDFArgs]) error {
	return GenerateInvoicePDF(ctx, w.Pool, w.Renderer, job.Args.InvoiceID)
}
