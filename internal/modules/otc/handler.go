// Package otc implements order-to-cash (Implementation Pack 1 §7): shipments,
// invoices, and payments. Fulfilment + finance are admin (operations/finance);
// buyers can view their own invoices on the storefront. Shipment quantities are
// capped at the ordered amount; issuing an invoice freezes line amounts and
// enqueues async PDF generation; a captured payment >= invoice total marks it paid.
package otc

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"b2bcommerce/internal/payments/gateway"
	mw "b2bcommerce/internal/server/middleware"
	"b2bcommerce/internal/server/response"
	"b2bcommerce/internal/store/gen"
	"b2bcommerce/internal/workflow"
)

// URLSigner mints and verifies signed, time-limited capability URLs for invoice
// PDFs. *auth.Issuer satisfies it. When nil, PDF links fall back to unsigned
// (legacy) behaviour.
type URLSigner interface {
	SignURL(path string, ttl time.Duration) string
	VerifyURL(path, exp, sig string) bool
}

// pdfURLTTL bounds how long a minted invoice-PDF link stays valid.
const pdfURLTTL = time.Hour

// PDFEnqueuer schedules async invoice-PDF generation. Satisfied by
// *queue.Enqueuer in production and a synchronous shim in tests; may be nil.
type PDFEnqueuer interface {
	EnqueueInvoicePDF(ctx context.Context, invoiceID int64) error
}

// Notifier schedules a transactional email (template key + data). Satisfied by
// *queue.Enqueuer; may be nil (emails are then skipped).
type Notifier interface {
	EnqueueEmail(ctx context.Context, to, template string, data map[string]any) error
}

type Handler struct {
	pool    *pgxpool.Pool
	q       *gen.Queries
	pdf     PDFEnqueuer
	notify  Notifier
	gateway gateway.Gateway
	signer  URLSigner
	wf      *workflow.Engine
}

func New(pool *pgxpool.Pool, pdf PDFEnqueuer, notify Notifier, gw gateway.Gateway, signer URLSigner) *Handler {
	// Shipment transitions are governed by the DB-defined `shipment_default`
	// workflow (no guards/actions configured → an empty registry suffices).
	return &Handler{pool: pool, q: gen.New(pool), pdf: pdf, notify: notify, gateway: gw, signer: signer, wf: workflow.New(pool, nil)}
}

func (h *Handler) Routes(r chi.Router, authMW func(http.Handler) http.Handler) {
	// Invoice PDFs are served at an unguessable capability URL (the invoice's
	// public_id UUID) so a plain browser download works from either frontend
	// without forwarding a bearer token. No middleware: the UUID is the secret.
	r.Get("/files/invoices/{publicID}.pdf", h.serveInvoicePDF)

	r.Group(func(ar chi.Router) {
		ar.Use(authMW)
		ar.Use(mw.RequireAudience("admin"))

		ar.With(mw.RequirePermission("shipment.view")).Get("/admin/orders/{id}/shipments", h.listShipments)
		ar.With(mw.RequirePermission("shipment.manage")).Post("/admin/orders/{id}/shipments", h.createShipment)
		ar.With(mw.RequirePermission("shipment.manage")).Patch("/admin/shipments/{id}/status", h.patchShipmentStatus)

		ar.With(mw.RequirePermission("invoice.view")).Get("/admin/invoices", h.adminListInvoices)
		ar.With(mw.RequirePermission("invoice.view")).Get("/admin/invoices/aging", h.invoiceAging)
		ar.With(mw.RequirePermission("invoice.manage")).Post("/admin/invoices/overdue-sweep", h.runOverdueSweep)
		ar.With(mw.RequirePermission("invoice.view")).Get("/admin/orders/{id}/invoices", h.listInvoicesForOrder)
		ar.With(mw.RequirePermission("invoice.manage")).Post("/admin/orders/{id}/invoices", h.issueInvoice)
		ar.With(mw.RequirePermission("invoice.view")).Get("/admin/invoices/{id}", h.getInvoice)
		ar.With(mw.RequirePermission("invoice.manage")).Post("/admin/invoices/{id}/pdf", h.regeneratePDF)

		ar.With(mw.RequirePermission("payment.view")).Get("/admin/invoices/{id}/payments", h.listPayments)
		ar.With(mw.RequirePermission("payment.manage")).Post("/admin/payments", h.recordPayment)
		ar.With(mw.RequirePermission("payment.manage")).Post("/admin/payments/{id}/refund", h.refundPayment)

		ar.With(mw.RequirePermission("return.view")).Get("/admin/returns", h.adminListReturns)
		ar.With(mw.RequirePermission("return.view")).Get("/admin/returns/{id}", h.adminGetReturn)
		ar.With(mw.RequirePermission("return.manage")).Post("/admin/orders/{id}/returns", h.adminCreateReturn)
		ar.With(mw.RequirePermission("return.view")).Get("/admin/orders/{id}/returns", h.listReturnsForOrderAdmin)
		ar.With(mw.RequirePermission("return.manage")).Post("/admin/returns/{id}/approve", h.approveReturn)
		ar.With(mw.RequirePermission("return.manage")).Post("/admin/returns/{id}/reject", h.rejectReturn)
		ar.With(mw.RequirePermission("return.manage")).Post("/admin/returns/{id}/receive", h.receiveReturn)
		ar.With(mw.RequirePermission("return.view")).Get("/admin/credit-notes", h.adminListCreditNotes)
	})

	r.Group(func(sr chi.Router) {
		sr.Use(authMW)
		sr.Use(mw.RequireAudience("storefront"))

		sr.Get("/storefront/invoices", h.listMyInvoices)
		sr.Get("/storefront/invoices/{publicID}", h.getMyInvoice)
		sr.Post("/storefront/invoices/{publicID}/pay", h.payInvoiceByCard)

		sr.Get("/storefront/returns", h.listMyReturns)
		sr.Post("/storefront/orders/{publicID}/returns", h.createMyReturn)
	})
}

type adminCtx struct {
	orgID  int64
	userID *int64
}

func admin(r *http.Request) (adminCtx, bool) {
	c, ok := mw.ClaimsFrom(r.Context())
	if !ok {
		return adminCtx{}, false
	}
	a := adminCtx{orgID: c.OrgID}
	if id, err := strconv.ParseInt(c.Subject, 10, 64); err == nil && id != 0 {
		a.userID = &id
	}
	return a, true
}

func customerID(r *http.Request) (int64, bool) {
	c, ok := mw.ClaimsFrom(r.Context())
	if !ok || c.CustomerID == 0 {
		return 0, false
	}
	return c.CustomerID, true
}

func pathID(r *http.Request) (int64, error) {
	return strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
}

func (h *Handler) tx(ctx context.Context, fn func(*gen.Queries) error) error {
	t, err := h.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer t.Rollback(ctx) //nolint:errcheck // no-op after commit
	if err := fn(gen.New(t)); err != nil {
		return err
	}
	return t.Commit(ctx)
}

// loadOrder verifies the path order belongs to the caller's org.
func (h *Handler) loadOrder(w http.ResponseWriter, r *http.Request, a adminCtx) (gen.Order, bool) {
	id, err := pathID(r)
	if err != nil {
		response.Fail(w, http.StatusBadRequest, "bad_request", "invalid id")
		return gen.Order{}, false
	}
	order, err := h.q.GetOrderByID(r.Context(), gen.GetOrderByIDParams{OrganizationID: a.orgID, ID: id})
	if err != nil {
		response.Fail(w, http.StatusNotFound, "not_found", "order not found")
		return gen.Order{}, false
	}
	return order, true
}

func unauthorized(w http.ResponseWriter) {
	response.Fail(w, http.StatusUnauthorized, "unauthorized", "no valid context")
}
