package marketplace

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"b2bcommerce/internal/money"
	"b2bcommerce/internal/server/response"
	"b2bcommerce/internal/store/gen"
)

// errNothingToPay is returned when a payout is requested but the vendor has no
// settled, unpaid orders.
var errNothingToPay = errors.New("nothing to pay out")

// generatePayout batches a vendor's delivered, not-yet-paid sub-orders into a
// single pending payout (amount = sum of their net_total) and attaches them, all
// in one transaction so the totalled set and the settled set are identical.
func (h *Handler) generatePayout(w http.ResponseWriter, r *http.Request) {
	org, ok := orgID(r)
	if !ok {
		response.Fail(w, http.StatusUnauthorized, "unauthorized", "no claims")
		return
	}
	vendorID, err := pathID(r)
	if err != nil {
		response.Fail(w, http.StatusBadRequest, "bad_request", "invalid id")
		return
	}
	if _, err := h.q.GetVendor(r.Context(), gen.GetVendorParams{ID: vendorID, OrganizationID: org}); err != nil {
		response.Fail(w, http.StatusNotFound, "not_found", "vendor not found")
		return
	}

	var payout gen.VendorPayout
	err = h.tx(r.Context(), func(q *gen.Queries) error {
		rows, e := q.ListSettledUnpaidVendorOrders(r.Context(), vendorID)
		if e != nil {
			return e
		}
		if len(rows) == 0 {
			return errNothingToPay
		}
		nets := make([]string, len(rows))
		for i, vo := range rows {
			nets[i] = vo.NetTotal
		}
		amount, e := money.Sum(nets...)
		if e != nil {
			return e
		}
		payout, e = q.CreateVendorPayout(r.Context(), gen.CreateVendorPayoutParams{
			OrganizationID: org, VendorID: vendorID, Currency: rows[0].Currency, Amount: amount,
		})
		if e != nil {
			return e
		}
		if _, e := q.AttachOrdersToPayout(r.Context(), gen.AttachOrdersToPayoutParams{PayoutID: &payout.ID, VendorID: vendorID}); e != nil {
			return e
		}
		return nil
	})
	if errors.Is(err, errNothingToPay) {
		response.Fail(w, http.StatusUnprocessableEntity, "nothing_to_pay", "no settled, unpaid orders for this vendor")
		return
	}
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not generate payout")
		return
	}
	response.JSON(w, http.StatusCreated, toPayoutDTO(payout))
}

func (h *Handler) listPayouts(w http.ResponseWriter, r *http.Request) {
	org, ok := orgID(r)
	if !ok {
		response.Fail(w, http.StatusUnauthorized, "unauthorized", "no claims")
		return
	}
	vendorID, err := pathID(r)
	if err != nil {
		response.Fail(w, http.StatusBadRequest, "bad_request", "invalid id")
		return
	}
	rows, err := h.q.ListVendorPayouts(r.Context(), gen.ListVendorPayoutsParams{VendorID: vendorID, OrganizationID: org})
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not list payouts")
		return
	}
	response.JSON(w, http.StatusOK, map[string]any{"items": payoutDTOs(rows)})
}

func (h *Handler) markPayoutPaid(w http.ResponseWriter, r *http.Request) {
	org, ok := orgID(r)
	if !ok {
		response.Fail(w, http.StatusUnauthorized, "unauthorized", "no claims")
		return
	}
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		response.Fail(w, http.StatusBadRequest, "bad_request", "invalid id")
		return
	}
	var req struct {
		Reference *string `json:"reference"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	p, err := h.q.MarkVendorPayoutPaid(r.Context(), gen.MarkVendorPayoutPaidParams{ID: id, OrganizationID: org, Reference: req.Reference})
	if err != nil {
		// No pending payout with that id (already paid, cancelled, or wrong org).
		response.Fail(w, http.StatusNotFound, "not_found", "pending payout not found")
		return
	}
	response.JSON(w, http.StatusOK, toPayoutDTO(p))
}

// vendorPayouts is the vendor-portal view of their own payouts.
func (h *Handler) vendorPayouts(w http.ResponseWriter, r *http.Request) {
	vc, ok := vendorOf(r)
	if !ok {
		response.Fail(w, http.StatusUnauthorized, "unauthorized", "no vendor claims")
		return
	}
	rows, err := h.q.ListVendorPayoutsForVendor(r.Context(), vc.vendorID)
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not list payouts")
		return
	}
	response.JSON(w, http.StatusOK, map[string]any{"items": payoutDTOs(rows)})
}

type payoutDTO struct {
	ID        int64   `json:"id"`
	PublicID  string  `json:"public_id"`
	VendorID  int64   `json:"vendor_id"`
	Status    string  `json:"status"`
	Currency  string  `json:"currency"`
	Amount    string  `json:"amount"`
	Reference *string `json:"reference"`
}

func toPayoutDTO(p gen.VendorPayout) payoutDTO {
	return payoutDTO{
		ID: p.ID, PublicID: p.PublicID.String(), VendorID: p.VendorID,
		Status: p.Status, Currency: p.Currency, Amount: p.Amount, Reference: p.Reference,
	}
}

func payoutDTOs(rows []gen.VendorPayout) []payoutDTO {
	out := make([]payoutDTO, 0, len(rows))
	for _, p := range rows {
		out = append(out, toPayoutDTO(p))
	}
	return out
}
