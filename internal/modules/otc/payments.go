package otc

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"b2bcommerce/internal/money"
	"b2bcommerce/internal/server/response"
	"b2bcommerce/internal/store/gen"
)

// recordPayment records a payment and, when it covers an invoice, flips the
// invoice to paid. Paying on terms (method=invoice) requires the customer to
// have payment terms and remaining credit (Pack 1 §7/§10 AC, light enforcement).
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
