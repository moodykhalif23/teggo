package otc

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"b2bcommerce/internal/money"
	"b2bcommerce/internal/payments/gateway"
	"b2bcommerce/internal/server/response"
	"b2bcommerce/internal/store/gen"
)

func (h *Handler) payInvoiceByCard(w http.ResponseWriter, r *http.Request) {
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
	if inv.Status == "paid" {
		response.Fail(w, http.StatusConflict, "already_paid", "invoice is already paid")
		return
	}
	if inv.Status == "void" {
		response.Fail(w, http.StatusConflict, "invalid_state", "invoice is void")
		return
	}

	var req struct {
		Token string `json:"token"` // gateway card token / nonce
	}
	_ = json.NewDecoder(r.Body).Decode(&req)

	res, err := h.gateway.CreateCharge(r.Context(), gateway.ChargeRequest{
		Amount: inv.GrandTotal, Currency: inv.Currency, Token: req.Token, Reference: inv.PublicID.String(),
	})
	if err != nil {
		if errors.Is(err, gateway.ErrDeclined) {
			response.Fail(w, http.StatusPaymentRequired, "declined", "the card was declined")
			return
		}
		response.Fail(w, http.StatusBadGateway, "gateway_error", "payment could not be processed")
		return
	}

	provider := h.gateway.Provider()
	ref := res.GatewayReference
	if _, err := h.q.CreatePayment(r.Context(), gen.CreatePaymentParams{
		InvoiceID: &inv.ID, OrderID: &inv.OrderID, CustomerID: cid,
		Method: "card", Gateway: &provider, GatewayReference: &ref,
		Amount: inv.GrandTotal, Currency: inv.Currency, Status: "captured",
		CapturedAt: pgtype.Timestamptz{Time: time.Now(), Valid: true},
	}); err != nil {
		// A unique-violation on (gateway, gateway_reference) means this exact
		// charge was already recorded — a retried/double-submitted payment.
		// Treat it as idempotent: fall through to (re-)settle and return state
		// rather than recording a second payment or 500-ing the client.
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != "23505" {
			response.Fail(w, http.StatusInternalServerError, "internal", "could not record payment")
			return
		}
	}
	if err := h.settleInvoiceIfCovered(r, inv.ID); err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not settle invoice")
		return
	}
	// Re-read so the response reflects the (now paid) status.
	updated, err := h.q.GetInvoiceByPublicID(r.Context(), pid)
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not load invoice")
		return
	}
	h.renderInvoice(w, r, updated)
}

// payMyOrder lets a buyer pay for their order directly. If no invoice exists yet
// it issues one (same as the admin path), then charges the card and settles it.
// This closes the storefront loop so a buyer never has to wait for an operator
// to issue an invoice before they can pay. Idempotent: an already-paid order is
// a 409, and a retried charge is absorbed by the payment unique constraint.
func (h *Handler) payMyOrder(w http.ResponseWriter, r *http.Request) {
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
	order, err := h.q.GetOrderByPublicID(r.Context(), pid)
	if err != nil || order.CustomerID != cid {
		response.Fail(w, http.StatusNotFound, "not_found", "order not found")
		return
	}
	switch order.Status {
	case "cancelled":
		response.Fail(w, http.StatusConflict, "invalid_state", "order is cancelled")
		return
	case "on_hold":
		response.Fail(w, http.StatusConflict, "awaiting_approval", "order is awaiting approval and cannot be paid yet")
		return
	}

	inv, err := h.invoiceForOrder(r.Context(), order)
	if errors.Is(err, errOrderEmpty) {
		response.Fail(w, http.StatusUnprocessableEntity, "empty_order", "order has no items to pay")
		return
	}
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not prepare invoice")
		return
	}
	if inv.Status == "paid" {
		response.Fail(w, http.StatusConflict, "already_paid", "this order has already been paid")
		return
	}

	var req struct {
		Token string `json:"token"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)

	res, err := h.gateway.CreateCharge(r.Context(), gateway.ChargeRequest{
		Amount: inv.GrandTotal, Currency: inv.Currency, Token: req.Token, Reference: inv.PublicID.String(),
	})
	if err != nil {
		if errors.Is(err, gateway.ErrDeclined) {
			response.Fail(w, http.StatusPaymentRequired, "declined", "the card was declined")
			return
		}
		response.Fail(w, http.StatusBadGateway, "gateway_error", "payment could not be processed")
		return
	}

	provider := h.gateway.Provider()
	ref := res.GatewayReference
	if _, err := h.q.CreatePayment(r.Context(), gen.CreatePaymentParams{
		InvoiceID: &inv.ID, OrderID: &inv.OrderID, CustomerID: cid,
		Method: "card", Gateway: &provider, GatewayReference: &ref,
		Amount: inv.GrandTotal, Currency: inv.Currency, Status: "captured",
		CapturedAt: pgtype.Timestamptz{Time: time.Now(), Valid: true},
	}); err != nil {
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != "23505" {
			response.Fail(w, http.StatusInternalServerError, "internal", "could not record payment")
			return
		}
	}
	if err := h.settleInvoiceIfCovered(r, inv.ID); err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not settle invoice")
		return
	}
	updated, err := h.q.GetInvoiceByPublicID(r.Context(), inv.PublicID)
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not load invoice")
		return
	}
	h.renderInvoice(w, r, updated)
}

// invoiceForOrder returns the order's payable invoice: an existing paid one wins
// (so we report already_paid), otherwise the first non-void invoice, otherwise a
// freshly issued one.
func (h *Handler) invoiceForOrder(ctx context.Context, order gen.Order) (gen.Invoice, error) {
	invs, err := h.q.ListInvoicesForOrder(ctx, order.ID)
	if err != nil {
		return gen.Invoice{}, err
	}
	var open *gen.Invoice
	for i := range invs {
		if invs[i].Status == "paid" {
			return invs[i], nil
		}
		if invs[i].Status != "void" && open == nil {
			open = &invs[i]
		}
	}
	if open != nil {
		return *open, nil
	}
	return h.createInvoiceForOrder(ctx, order)
}

func (h *Handler) recordPayment(w http.ResponseWriter, r *http.Request) {
	a, ok := admin(r)
	if !ok {
		unauthorized(w)
		return
	}
	var req struct {
		InvoiceID        *int64  `json:"invoice_id"`
		OrderID          *int64  `json:"order_id"`
		CustomerID       int64   `json:"customer_id"`
		Method           string  `json:"method"`
		Gateway          *string `json:"gateway"`
		GatewayReference *string `json:"gateway_reference"`
		Amount           string  `json:"amount"`
		Currency         string  `json:"currency"`
		Status           string  `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.CustomerID == 0 || req.Amount == "" || len(req.Currency) != 3 {
		response.Fail(w, http.StatusBadRequest, "bad_request", "customer_id, amount, 3-letter currency required")
		return
	}
	switch req.Method {
	case "card", "ach", "invoice", "po", "mpesa":
	default:
		response.Fail(w, http.StatusBadRequest, "bad_request", "invalid payment method")
		return
	}
	if req.Status == "" {
		req.Status = "captured"
	}

	// Tenant check + terms/credit gate for pay-on-terms.
	bill, err := h.q.GetCustomerBilling(r.Context(), req.CustomerID)
	if err != nil || bill.OrganizationID != a.orgID {
		response.Fail(w, http.StatusBadRequest, "bad_request", "customer not found in organization")
		return
	}
	if req.Method == "invoice" {
		if bill.PaymentTermsDays <= 0 {
			response.Fail(w, http.StatusUnprocessableEntity, "no_terms", "customer has no payment terms for invoice billing")
			return
		}
		open, _ := h.q.SumOpenInvoices(r.Context(), req.CustomerID)
		if c, _ := money.Cmp(open, bill.CreditLimit); c > 0 {
			response.Fail(w, http.StatusUnprocessableEntity, "credit_exceeded", "open invoices exceed credit limit")
			return
		}
	}

	var captured pgtype.Timestamptz
	if req.Status == "captured" {
		captured = pgtype.Timestamptz{Time: time.Now(), Valid: true}
	}

	pay, err := h.q.CreatePayment(r.Context(), gen.CreatePaymentParams{
		InvoiceID: req.InvoiceID, OrderID: req.OrderID, CustomerID: req.CustomerID,
		Method: req.Method, Gateway: req.Gateway, GatewayReference: req.GatewayReference,
		Amount: req.Amount, Currency: req.Currency, Status: req.Status, CapturedAt: captured,
	})
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not record payment")
		return
	}

	// If captured payments now cover the invoice, mark it paid.
	if req.Status == "captured" && req.InvoiceID != nil {
		if err := h.settleInvoiceIfCovered(r, *req.InvoiceID); err != nil {
			response.Fail(w, http.StatusInternalServerError, "internal", "could not settle invoice")
			return
		}
	}
	response.JSON(w, http.StatusCreated, pay)
}

func (h *Handler) settleInvoiceIfCovered(r *http.Request, invoiceID int64) error {
	inv, err := h.q.GetInvoiceByIDInternal(r.Context(), invoiceID)
	if err != nil {
		return err
	}
	if inv.Status == "paid" || inv.Status == "void" {
		return nil
	}
	captured, err := h.q.SumCapturedForInvoice(r.Context(), &invoiceID)
	if err != nil {
		return err
	}
	if c, _ := money.Cmp(captured, inv.GrandTotal); c >= 0 {
		_, err = h.q.SetInvoiceStatus(r.Context(), gen.SetInvoiceStatusParams{ID: invoiceID, Status: "paid"})
		return err
	}
	return nil
}

func (h *Handler) listPayments(w http.ResponseWriter, r *http.Request) {
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
	rows, err := h.q.ListPaymentsForInvoice(r.Context(), &id)
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not list payments")
		return
	}
	if rows == nil {
		rows = []gen.Payment{}
	}
	response.JSON(w, http.StatusOK, map[string]any{"items": rows})
}

func (h *Handler) refundPayment(w http.ResponseWriter, r *http.Request) {
	if _, ok := admin(r); !ok {
		unauthorized(w)
		return
	}
	id, err := pathID(r)
	if err != nil {
		response.Fail(w, http.StatusBadRequest, "bad_request", "invalid id")
		return
	}
	pay, err := h.q.GetPayment(r.Context(), id)
	if err != nil {
		response.Fail(w, http.StatusNotFound, "not_found", "payment not found")
		return
	}
	if pay.Status != "captured" && pay.Status != "authorized" {
		response.Fail(w, http.StatusConflict, "invalid_state", "only captured/authorized payments can be refunded")
		return
	}
	// Real refunds call the gateway adapter (Pack 2 §4); here we record the state.
	updated, err := h.q.SetPaymentStatus(r.Context(), gen.SetPaymentStatusParams{ID: id, Status: "refunded"})
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not refund payment")
		return
	}
	response.JSON(w, http.StatusOK, updated)
}
