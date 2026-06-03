package otc

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"b2bcommerce/internal/money"
	"b2bcommerce/internal/server/response"
	"b2bcommerce/internal/store/gen"
)

// issueInvoice creates an invoice from the order's lines, freezing amounts, and
// enqueues async PDF generation. Due date follows the customer's payment terms.
func (h *Handler) issueInvoice(w http.ResponseWriter, r *http.Request) {
	a, ok := admin(r)
	if !ok {
		unauthorized(w)
		return
	}
	order, ok := h.loadOrder(w, r, a)
	if !ok {
		return
	}
	items, err := h.q.ListOrderItems(r.Context(), order.ID)
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not load order items")
		return
	}
	if len(items) == 0 {
		response.Fail(w, http.StatusUnprocessableEntity, "empty_order", "order has no items to invoice")
		return
	}

	var subtotals, taxes []string
	for _, it := range items {
		subtotals = append(subtotals, it.RowTotal)
		taxes = append(taxes, it.TaxAmount)
	}
	subtotal, _ := money.Sum(subtotals...)
	taxTotal, _ := money.Sum(taxes...)
	grand, _ := money.Sum(subtotal, taxTotal)

	now := time.Now()
	due := now
	if bill, err := h.q.GetCustomerBilling(r.Context(), order.CustomerID); err == nil && bill.PaymentTermsDays > 0 {
		due = now.AddDate(0, 0, int(bill.PaymentTermsDays))
	}

	var invoice gen.Invoice
	err = h.tx(r.Context(), func(q *gen.Queries) error {
		var e error
		invoice, e = q.CreateInvoice(r.Context(), gen.CreateInvoiceParams{
			OrderID: order.ID, CustomerID: order.CustomerID, Currency: order.Currency,
			Subtotal: subtotal, TaxTotal: taxTotal, GrandTotal: grand,
			IssuedAt: tsNow(now), DueAt: tsNow(due),
		})
		if e != nil {
			return e
		}
		for _, it := range items {
			if _, e := q.AddInvoiceItem(r.Context(), gen.AddInvoiceItemParams{
				InvoiceID: invoice.ID, Description: it.Name, Quantity: it.Quantity,
				UnitPrice: it.UnitPrice, TaxAmount: it.TaxAmount, RowTotal: it.RowTotal,
			}); e != nil {
				return e
			}
		}
		return nil
	})
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not issue invoice")
		return
	}

	if h.pdf != nil {
		_ = h.pdf.EnqueueInvoicePDF(r.Context(), invoice.ID)
	}
	if h.notify != nil {
		if to, name := h.primaryContact(r.Context(), invoice.CustomerID); to != "" {
			due := ""
			if invoice.DueAt.Valid {
				due = invoice.DueAt.Time.Format("2006-01-02")
			}
			_ = h.notify.EnqueueEmail(r.Context(), to, "invoice_issued", map[string]any{
				"name":           name,
				"invoice_number": "INV-" + shortID(invoice.PublicID.String()),
				"total":          invoice.GrandTotal,
				"currency":       invoice.Currency,
				"due_at":         due,
			})
		}
	}
	h.renderInvoice(w, r, invoice)
}

// primaryContact returns the email + name of a customer's first user (the
// notification recipient), or empty strings when the customer has no users.
func (h *Handler) primaryContact(ctx context.Context, customerID int64) (email, name string) {
	users, err := h.q.ListCustomerUsers(ctx, customerID)
	if err != nil || len(users) == 0 {
		return "", ""
	}
	return users[0].Email, users[0].FullName
}

// shortID returns the first 8 chars of an id for human-facing references.
func shortID(s string) string {
	if len(s) >= 8 {
		return s[:8]
	}
	return s
}

func (h *Handler) adminListInvoices(w http.ResponseWriter, r *http.Request) {
	a, ok := admin(r)
	if !ok {
		unauthorized(w)
		return
	}
	rows, err := h.q.ListInvoicesAdmin(r.Context(), gen.ListInvoicesAdminParams{OrganizationID: a.orgID, Limit: 200, Offset: 0})
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not list invoices")
		return
	}
	if rows == nil {
		rows = []gen.Invoice{}
	}
	response.JSON(w, http.StatusOK, map[string]any{"items": rows})
}

func (h *Handler) listInvoicesForOrder(w http.ResponseWriter, r *http.Request) {
	a, ok := admin(r)
	if !ok {
		unauthorized(w)
		return
	}
	order, ok := h.loadOrder(w, r, a)
	if !ok {
		return
	}
	rows, err := h.q.ListInvoicesForOrder(r.Context(), order.ID)
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not list invoices")
		return
	}
	if rows == nil {
		rows = []gen.Invoice{}
	}
	response.JSON(w, http.StatusOK, map[string]any{"items": rows})
}

func (h *Handler) getInvoice(w http.ResponseWriter, r *http.Request) {
	a, ok := admin(r)
	if !ok {
		unauthorized(w)
		return
	}
	id, err := pathID(r)
	if err != nil {
		response.Fail(w, http.StatusBadRequest, "bad_request", "invalid id")
		return
	}
	inv, err := h.q.GetInvoice(r.Context(), gen.GetInvoiceParams{OrganizationID: a.orgID, ID: id})
	if err != nil {
		response.Fail(w, http.StatusNotFound, "not_found", "invoice not found")
		return
	}
	h.renderInvoice(w, r, inv)
}

func (h *Handler) regeneratePDF(w http.ResponseWriter, r *http.Request) {
	a, ok := admin(r)
	if !ok {
		unauthorized(w)
		return
	}
	id, err := pathID(r)
	if err != nil {
		response.Fail(w, http.StatusBadRequest, "bad_request", "invalid id")
		return
	}
	if _, err := h.q.GetInvoice(r.Context(), gen.GetInvoiceParams{OrganizationID: a.orgID, ID: id}); err != nil {
		response.Fail(w, http.StatusNotFound, "not_found", "invoice not found")
		return
	}
	if h.pdf == nil {
		response.Fail(w, http.StatusServiceUnavailable, "unavailable", "pdf queue not configured")
		return
	}
	if err := h.pdf.EnqueueInvoicePDF(r.Context(), id); err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not enqueue PDF")
		return
	}
	response.JSON(w, http.StatusAccepted, map[string]any{"enqueued": true})
}

// serveInvoicePDF streams the stored PDF for an invoice's public_id. The route
// is unauthenticated by design (capability URL); a missing document — not yet
// generated, or unknown id — is a 404.
func (h *Handler) serveInvoicePDF(w http.ResponseWriter, r *http.Request) {
	pid, err := uuid.Parse(chi.URLParam(r, "publicID"))
	if err != nil {
		response.Fail(w, http.StatusBadRequest, "bad_request", "invalid id")
		return
	}
	doc, err := h.q.GetInvoiceDocument(r.Context(), pid)
	if err != nil {
		response.Fail(w, http.StatusNotFound, "not_found", "invoice PDF not found")
		return
	}
	w.Header().Set("Content-Type", doc.ContentType)
	w.Header().Set("Content-Disposition", "inline; filename=\"invoice-"+pid.String()+".pdf\"")
	w.Header().Set("Content-Length", strconv.Itoa(len(doc.Bytes)))
	_, _ = w.Write(doc.Bytes)
}

// ---- storefront ----------------------------------------------------------

func (h *Handler) listMyInvoices(w http.ResponseWriter, r *http.Request) {
	cid, ok := customerID(r)
	if !ok {
		unauthorized(w)
		return
	}
	rows, err := h.q.ListInvoicesForCustomer(r.Context(), cid)
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not list invoices")
		return
	}
	if rows == nil {
		rows = []gen.Invoice{}
	}
	response.JSON(w, http.StatusOK, map[string]any{"items": rows})
}

func (h *Handler) getMyInvoice(w http.ResponseWriter, r *http.Request) {
	cid, ok := customerID(r)
	if !ok {
		unauthorized(w)
		return
	}
	pid, err := uuid.Parse(chi.URLParam(r, "publicID"))
	if err != nil {
		response.Fail(w, http.StatusBadRequest, "bad_request", "invalid id")
		return
	}
	inv, err := h.q.GetInvoiceByPublicID(r.Context(), pid)
	if err != nil || inv.CustomerID != cid {
		response.Fail(w, http.StatusNotFound, "not_found", "invoice not found")
		return
	}
	h.renderInvoice(w, r, inv)
}

func (h *Handler) renderInvoice(w http.ResponseWriter, r *http.Request, inv gen.Invoice) {
	items, err := h.q.ListInvoiceItems(r.Context(), inv.ID)
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not load invoice items")
		return
	}
	if items == nil {
		items = []gen.InvoiceItem{}
	}
	response.JSON(w, http.StatusOK, map[string]any{
		"id":          inv.ID,
		"public_id":   inv.PublicID.String(),
		"status":      inv.Status,
		"currency":    inv.Currency,
		"subtotal":    inv.Subtotal,
		"tax_total":   inv.TaxTotal,
		"grand_total": inv.GrandTotal,
		"due_at":      inv.DueAt,
		"pdf_url":     inv.PdfUrl,
		"items":       items,
	})
}

func tsNow(t time.Time) pgtype.Timestamptz { return pgtype.Timestamptz{Time: t, Valid: true} }
