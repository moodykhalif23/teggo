package otc

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"b2bcommerce/internal/money"
	"b2bcommerce/internal/server/response"
	"b2bcommerce/internal/store/gen"
)

func sptr(s string) *string { return &s }

func notFoundOTC(w http.ResponseWriter, what string) {
	response.Fail(w, http.StatusNotFound, "not_found", what+" not found")
}

// Returns / RMA + credit notes (post-sale). Lifecycle: requested -> approved ->
// received (restock + credit note), or rejected. Buyers can self-serve a
// request; admins approve and receive.

type returnItemInput struct {
	OrderItemID int64   `json:"order_item_id"`
	Quantity    string  `json:"quantity"`
	Reason      *string `json:"reason"`
}

type createReturnRequest struct {
	Reason *string           `json:"reason"`
	Items  []returnItemInput `json:"items"`
}

// validateAndCreate builds a 'requested' return for an order after capping each
// line at its returnable quantity (ordered minus already-returned).
func (h *Handler) validateAndCreateReturn(w http.ResponseWriter, r *http.Request, orderID, customerID int64, req createReturnRequest) (gen.Return, bool) {
	if len(req.Items) == 0 {
		response.Fail(w, http.StatusBadRequest, "bad_request", "at least one item is required")
		return gen.Return{}, false
	}
	for _, it := range req.Items {
		oi, err := h.q.GetOrderItem(r.Context(), gen.GetOrderItemParams{ID: it.OrderItemID, OrderID: orderID})
		if err != nil {
			response.Fail(w, http.StatusBadRequest, "bad_request", "order item not on this order")
			return gen.Return{}, false
		}
		returned, err := h.q.SumReturnedForOrderItem(r.Context(), it.OrderItemID)
		if err != nil {
			response.Fail(w, http.StatusInternalServerError, "internal", "could not check returned quantity")
			return gen.Return{}, false
		}
		remaining, _ := money.Sub(oi.Quantity, returned)
		if c, err := money.Cmp(it.Quantity, remaining); err != nil || c > 0 {
			response.Fail(w, http.StatusUnprocessableEntity, "over_return", "quantity exceeds returnable amount for an item")
			return gen.Return{}, false
		}
	}

	var ret gen.Return
	err := h.tx(r.Context(), func(q *gen.Queries) error {
		var e error
		ret, e = q.CreateReturn(r.Context(), gen.CreateReturnParams{OrderID: orderID, CustomerID: customerID, Reason: req.Reason})
		if e != nil {
			return e
		}
		for _, it := range req.Items {
			oi, e := q.GetOrderItem(r.Context(), gen.GetOrderItemParams{ID: it.OrderItemID, OrderID: orderID})
			if e != nil {
				return e
			}
			if _, e := q.AddReturnItem(r.Context(), gen.AddReturnItemParams{
				ReturnID: ret.ID, OrderItemID: it.OrderItemID, ProductID: oi.ProductID, Quantity: it.Quantity, Reason: it.Reason,
			}); e != nil {
				return e
			}
		}
		return nil
	})
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not create return")
		return gen.Return{}, false
	}
	return ret, true
}

func (h *Handler) renderReturn(w http.ResponseWriter, r *http.Request, ret gen.Return, code int) {
	items, _ := h.q.ListReturnItems(r.Context(), ret.ID)
	if items == nil {
		items = []gen.ListReturnItemsRow{}
	}
	credits, _ := h.q.ListCreditNotesForReturn(r.Context(), &ret.ID)
	if credits == nil {
		credits = []gen.CreditNote{}
	}
	response.JSON(w, code, map[string]any{
		"id": ret.ID, "public_id": ret.PublicID.String(), "order_id": ret.OrderID,
		"customer_id": ret.CustomerID, "status": ret.Status, "reason": ret.Reason,
		"items": items, "credit_notes": credits,
	})
}

// ---- admin ----------------------------------------------------------------

func (h *Handler) adminCreateReturn(w http.ResponseWriter, r *http.Request) {
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
	order, err := h.q.GetOrderByID(r.Context(), gen.GetOrderByIDParams{OrganizationID: a.orgID, ID: id})
	if err != nil {
		notFoundOTC(w, "order")
		return
	}
	var req createReturnRequest
	_ = json.NewDecoder(r.Body).Decode(&req)
	ret, ok := h.validateAndCreateReturn(w, r, order.ID, order.CustomerID, req)
	if !ok {
		return
	}
	h.renderReturn(w, r, ret, http.StatusCreated)
}

func (h *Handler) adminListReturns(w http.ResponseWriter, r *http.Request) {
	a, ok := admin(r)
	if !ok {
		unauthorized(w)
		return
	}
	rows, err := h.q.ListReturnsAdmin(r.Context(), gen.ListReturnsAdminParams{OrganizationID: a.orgID, Limit: 200, Offset: 0})
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not list returns")
		return
	}
	if rows == nil {
		rows = []gen.Return{}
	}
	response.JSON(w, http.StatusOK, map[string]any{"items": rows})
}

func (h *Handler) listReturnsForOrderAdmin(w http.ResponseWriter, r *http.Request) {
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
	// Scope to the org via the order.
	if _, err := h.q.GetOrderByID(r.Context(), gen.GetOrderByIDParams{OrganizationID: a.orgID, ID: id}); err != nil {
		notFoundOTC(w, "order")
		return
	}
	rows, err := h.q.ListReturnsForOrder(r.Context(), id)
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not list returns")
		return
	}
	if rows == nil {
		rows = []gen.Return{}
	}
	response.JSON(w, http.StatusOK, map[string]any{"items": rows})
}

func (h *Handler) adminGetReturn(w http.ResponseWriter, r *http.Request) {
	ret, ok := h.loadAdminReturn(w, r)
	if !ok {
		return
	}
	h.renderReturn(w, r, ret, http.StatusOK)
}

func (h *Handler) loadAdminReturn(w http.ResponseWriter, r *http.Request) (gen.Return, bool) {
	a, ok := admin(r)
	if !ok {
		unauthorized(w)
		return gen.Return{}, false
	}
	id, err := pathID(r)
	if err != nil {
		response.Fail(w, http.StatusBadRequest, "bad_request", "invalid id")
		return gen.Return{}, false
	}
	ret, err := h.q.GetReturn(r.Context(), gen.GetReturnParams{ID: id, OrganizationID: a.orgID})
	if err != nil {
		notFoundOTC(w, "return")
		return gen.Return{}, false
	}
	return ret, true
}

func (h *Handler) approveReturn(w http.ResponseWriter, r *http.Request) {
	ret, ok := h.loadAdminReturn(w, r)
	if !ok {
		return
	}
	if ret.Status != "requested" {
		response.Fail(w, http.StatusConflict, "conflict", "only a requested return can be approved")
		return
	}
	updated, err := h.q.SetReturnStatus(r.Context(), gen.SetReturnStatusParams{ID: ret.ID, Status: "approved"})
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not approve return")
		return
	}
	h.renderReturn(w, r, updated, http.StatusOK)
}

func (h *Handler) rejectReturn(w http.ResponseWriter, r *http.Request) {
	ret, ok := h.loadAdminReturn(w, r)
	if !ok {
		return
	}
	if ret.Status != "requested" {
		response.Fail(w, http.StatusConflict, "conflict", "only a requested return can be rejected")
		return
	}
	updated, err := h.q.SetReturnStatus(r.Context(), gen.SetReturnStatusParams{ID: ret.ID, Status: "rejected"})
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not reject return")
		return
	}
	h.renderReturn(w, r, updated, http.StatusOK)
}

// receiveReturn marks an approved return received, restocks each line (default
// warehouse, tracked products only) and issues a credit note for the value.
func (h *Handler) receiveReturn(w http.ResponseWriter, r *http.Request) {
	a, ok := admin(r)
	if !ok {
		unauthorized(w)
		return
	}
	ret, ok := h.loadAdminReturn(w, r)
	if !ok {
		return
	}
	if ret.Status != "approved" {
		response.Fail(w, http.StatusConflict, "conflict", "only an approved return can be received")
		return
	}
	order, err := h.q.GetOrderByID(r.Context(), gen.GetOrderByIDParams{OrganizationID: a.orgID, ID: ret.OrderID})
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not load order")
		return
	}
	items, err := h.q.ListReturnItems(r.Context(), ret.ID)
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not load return items")
		return
	}

	// Credit value = sum(unit_price * returned qty).
	creditTotal := "0"
	for _, it := range items {
		line, _ := money.LineTotal(it.Quantity, it.UnitPrice)
		creditTotal, _ = money.Sum(creditTotal, line)
	}
	var invoiceID *int64
	if invs, err := h.q.ListInvoicesForOrder(r.Context(), ret.OrderID); err == nil && len(invs) > 0 {
		invoiceID = &invs[0].ID
	}

	by := "user:0"
	if a.userID != nil {
		by = "user:" + strconv.FormatInt(*a.userID, 10)
	}
	var creditNote gen.CreditNote
	err = h.tx(r.Context(), func(q *gen.Queries) error {
		if _, e := q.SetReturnStatus(r.Context(), gen.SetReturnStatusParams{ID: ret.ID, Status: "received"}); e != nil {
			return e
		}
		// Restock into the default warehouse for tracked products (graceful skip).
		if wh, e := q.GetDefaultWarehouse(r.Context(), a.orgID); e == nil {
			for _, it := range items {
				if _, e := q.GetInventoryLevel(r.Context(), gen.GetInventoryLevelParams{ProductID: it.ProductID, WarehouseID: wh.ID}); e != nil {
					continue // untracked product
				}
				if _, e := q.AdjustInventoryLevel(r.Context(), gen.AdjustInventoryLevelParams{
					ProductID: it.ProductID, WarehouseID: wh.ID, Column3: it.Quantity, Column4: "0",
				}); e != nil {
					return e
				}
				rid := ret.ID
				if _, e := q.AddInventoryMovement(r.Context(), gen.AddInventoryMovementParams{
					ProductID: it.ProductID, WarehouseID: wh.ID, Type: "return", Quantity: it.Quantity,
					ReferenceType: sptr("return"), ReferenceID: &rid, CreatedBy: sptr(by),
				}); e != nil {
					return e
				}
			}
		}
		var e error
		creditNote, e = q.CreateCreditNote(r.Context(), gen.CreateCreditNoteParams{
			ReturnID: &ret.ID, InvoiceID: invoiceID, CustomerID: ret.CustomerID, Amount: creditTotal, Currency: order.Currency,
		})
		return e
	})
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not receive return")
		return
	}
	_ = creditNote
	updated, _ := h.q.GetReturn(r.Context(), gen.GetReturnParams{ID: ret.ID, OrganizationID: a.orgID})
	h.renderReturn(w, r, updated, http.StatusOK)
}

func (h *Handler) adminListCreditNotes(w http.ResponseWriter, r *http.Request) {
	a, ok := admin(r)
	if !ok {
		unauthorized(w)
		return
	}
	rows, err := h.q.ListCreditNotesAdmin(r.Context(), gen.ListCreditNotesAdminParams{OrganizationID: a.orgID, Limit: 200, Offset: 0})
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not list credit notes")
		return
	}
	if rows == nil {
		rows = []gen.CreditNote{}
	}
	response.JSON(w, http.StatusOK, map[string]any{"items": rows})
}

// ---- storefront (buyer self-serve) ----------------------------------------

func (h *Handler) createMyReturn(w http.ResponseWriter, r *http.Request) {
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
		notFoundOTC(w, "order")
		return
	}
	var req createReturnRequest
	_ = json.NewDecoder(r.Body).Decode(&req)
	ret, ok := h.validateAndCreateReturn(w, r, order.ID, cid, req)
	if !ok {
		return
	}
	h.renderReturn(w, r, ret, http.StatusCreated)
}

func (h *Handler) listMyReturns(w http.ResponseWriter, r *http.Request) {
	cid, ok := customerID(r)
	if !ok {
		unauthorized(w)
		return
	}
	rows, err := h.q.ListReturnsForCustomer(r.Context(), cid)
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not list returns")
		return
	}
	if rows == nil {
		rows = []gen.Return{}
	}
	response.JSON(w, http.StatusOK, map[string]any{"items": rows})
}
