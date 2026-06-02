package sales

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"b2bcommerce/internal/server/response"
	"b2bcommerce/internal/store/gen"
)

// ---- storefront ----------------------------------------------------------

func (h *Handler) createRFQ(w http.ResponseWriter, r *http.Request) {
	cc, ok := customer(r)
	if !ok {
		unauthorized(w)
		return
	}
	var req struct {
		Notes string `json:"notes"`
		Items []struct {
			ProductPublicID string  `json:"product_public_id"`
			Quantity        string  `json:"quantity"`
			TargetPrice     *string `json:"target_price"`
			Notes           *string `json:"notes"`
		} `json:"items"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || len(req.Items) == 0 {
		response.Fail(w, http.StatusBadRequest, "bad_request", "at least one item is required")
		return
	}
	ws, err := h.q.GetDefaultWebsite(r.Context(), cc.orgID)
	if err != nil {
		response.Fail(w, http.StatusBadRequest, "bad_request", "no website configured")
		return
	}

	var rfq gen.Rfq
	err = h.tx(r.Context(), func(q *gen.Queries) error {
		var notes *string
		if req.Notes != "" {
			notes = &req.Notes
		}
		var e error
		rfq, e = q.CreateRFQ(r.Context(), gen.CreateRFQParams{
			OrganizationID: cc.orgID, WebsiteID: ws.ID, CustomerID: cc.customerID,
			CustomerUserID: cc.customerUserID, Notes: notes,
		})
		if e != nil {
			return e
		}
		for _, it := range req.Items {
			pid, perr := uuid.Parse(it.ProductPublicID)
			if perr != nil {
				return perr
			}
			productID, perr := q.GetProductIDByPublicID(r.Context(), gen.GetProductIDByPublicIDParams{OrganizationID: cc.orgID, PublicID: pid})
			if perr != nil {
				return perr
			}
			qty := it.Quantity
			if qty == "" {
				qty = "1"
			}
			if _, perr := q.AddRFQItem(r.Context(), gen.AddRFQItemParams{
				RfqID: rfq.ID, ProductID: productID, Quantity: qty, Unit: "each",
				TargetPrice: it.TargetPrice, Notes: it.Notes,
			}); perr != nil {
				return perr
			}
		}
		return nil
	})
	if err != nil {
		response.Fail(w, http.StatusBadRequest, "bad_request", "could not create RFQ (check product ids)")
		return
	}
	h.renderRFQ(w, r, rfq)
}

func (h *Handler) submitRFQ(w http.ResponseWriter, r *http.Request) {
	cc, ok := customer(r)
	if !ok {
		unauthorized(w)
		return
	}
	rfq, ok := h.loadMyRFQ(w, r, cc)
	if !ok {
		return
	}
	if rfq.Status != "draft" {
		response.Fail(w, http.StatusConflict, "invalid_state", "only a draft RFQ can be submitted")
		return
	}
	updated, err := h.q.SetRFQStatus(r.Context(), gen.SetRFQStatusParams{ID: rfq.ID, Status: "submitted"})
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not submit RFQ")
		return
	}
	h.renderRFQ(w, r, updated)
}

func (h *Handler) listMyRFQs(w http.ResponseWriter, r *http.Request) {
	cc, ok := customer(r)
	if !ok {
		unauthorized(w)
		return
	}
	rows, err := h.q.ListRFQsForCustomer(r.Context(), cc.customerID)
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not list RFQs")
		return
	}
	if rows == nil {
		rows = []gen.Rfq{}
	}
	response.JSON(w, http.StatusOK, map[string]any{"items": rows})
}

func (h *Handler) getMyRFQ(w http.ResponseWriter, r *http.Request) {
	cc, ok := customer(r)
	if !ok {
		unauthorized(w)
		return
	}
	rfq, ok := h.loadMyRFQ(w, r, cc)
	if !ok {
		return
	}
	h.renderRFQ(w, r, rfq)
}

// ---- admin ---------------------------------------------------------------

func (h *Handler) adminListRFQs(w http.ResponseWriter, r *http.Request) {
	a, ok := admin(r)
	if !ok {
		unauthorized(w)
		return
	}
	rows, err := h.q.ListRFQsAdmin(r.Context(), gen.ListRFQsAdminParams{OrganizationID: a.orgID, Limit: 100, Offset: 0})
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not list RFQs")
		return
	}
	if rows == nil {
		rows = []gen.Rfq{}
	}
	response.JSON(w, http.StatusOK, map[string]any{"items": rows})
}

func (h *Handler) adminGetRFQ(w http.ResponseWriter, r *http.Request) {
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
	rfq, err := h.q.GetRFQByID(r.Context(), gen.GetRFQByIDParams{OrganizationID: a.orgID, ID: id})
	if err != nil {
		notFound(w, "RFQ")
		return
	}
	h.renderRFQ(w, r, rfq)
}

// ---- shared --------------------------------------------------------------

func (h *Handler) loadMyRFQ(w http.ResponseWriter, r *http.Request, cc custCtx) (gen.Rfq, bool) {
	pid, err := uuid.Parse(chi.URLParam(r, "publicID"))
	if err != nil {
		response.Fail(w, http.StatusBadRequest, "bad_request", "invalid id")
		return gen.Rfq{}, false
	}
	rfq, err := h.q.GetRFQByPublicID(r.Context(), gen.GetRFQByPublicIDParams{OrganizationID: cc.orgID, PublicID: pid})
	if err != nil || rfq.CustomerID != cc.customerID {
		notFound(w, "RFQ")
		return gen.Rfq{}, false
	}
	return rfq, true
}

func (h *Handler) renderRFQ(w http.ResponseWriter, r *http.Request, rfq gen.Rfq) {
	items, err := h.q.ListRFQItems(r.Context(), rfq.ID)
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not load RFQ items")
		return
	}
	if items == nil {
		items = []gen.ListRFQItemsRow{}
	}
	response.JSON(w, http.StatusOK, map[string]any{
		"public_id": rfq.PublicID.String(),
		"status":    rfq.Status,
		"notes":     rfq.Notes,
		"items":     items,
	})
}
