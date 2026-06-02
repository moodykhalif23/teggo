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

func (h *Handler) listShipments(w http.ResponseWriter, r *http.Request) {
	a, ok := admin(r)
	if !ok {
		unauthorized(w)
		return
	}
	order, ok := h.loadOrder(w, r, a)
	if !ok {
		return
	}
	rows, err := h.q.ListShipmentsForOrder(r.Context(), order.ID)
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not list shipments")
		return
	}
	if rows == nil {
		rows = []gen.Shipment{}
	}
	response.JSON(w, http.StatusOK, map[string]any{"items": rows})
}

// createShipment records a (full or partial) shipment. Each line is capped at
// the ordered quantity minus what has already shipped (§7 AC).
func (h *Handler) createShipment(w http.ResponseWriter, r *http.Request) {
	a, ok := admin(r)
	if !ok {
		unauthorized(w)
		return
	}
	order, ok := h.loadOrder(w, r, a)
	if !ok {
		return
	}
	var req struct {
		Carrier        *string `json:"carrier"`
		TrackingNumber *string `json:"tracking_number"`
		Items          []struct {
			OrderItemID int64  `json:"order_item_id"`
			Quantity    string `json:"quantity"`
		} `json:"items"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || len(req.Items) == 0 {
		response.Fail(w, http.StatusBadRequest, "bad_request", "at least one item is required")
		return
	}

	// Validate quantities before writing anything.
	for _, it := range req.Items {
		oi, err := h.q.GetOrderItem(r.Context(), gen.GetOrderItemParams{ID: it.OrderItemID, OrderID: order.ID})
		if err != nil {
			response.Fail(w, http.StatusBadRequest, "bad_request", "order item not on this order")
			return
		}
		already, err := h.q.ShippedQtyForOrderItem(r.Context(), it.OrderItemID)
		if err != nil {
			response.Fail(w, http.StatusInternalServerError, "internal", "could not check shipped quantity")
			return
		}
		remaining, _ := money.Sub(oi.Quantity, already)
		if c, _ := money.Cmp(it.Quantity, remaining); c > 0 {
			response.Fail(w, http.StatusUnprocessableEntity, "over_ship", "quantity exceeds un-shipped amount for an item")
			return
		}
	}

	var shipment gen.Shipment
	err := h.tx(r.Context(), func(q *gen.Queries) error {
		var e error
		shipment, e = q.CreateShipment(r.Context(), gen.CreateShipmentParams{
			OrderID: order.ID, Carrier: req.Carrier, TrackingNumber: req.TrackingNumber,
		})
		if e != nil {
			return e
		}
		for _, it := range req.Items {
			if _, e := q.AddShipmentItem(r.Context(), gen.AddShipmentItemParams{
				ShipmentID: shipment.ID, OrderItemID: it.OrderItemID, Quantity: it.Quantity,
			}); e != nil {
				return e
			}
		}
		return nil
	})
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not create shipment")
		return
	}
	response.JSON(w, http.StatusCreated, shipment)
}

var shipmentTransitions = map[string][]string{
	"pending":   {"shipped", "returned"},
	"shipped":   {"delivered", "returned"},
	"delivered": {"returned"},
}

func (h *Handler) patchShipmentStatus(w http.ResponseWriter, r *http.Request) {
	if _, ok := admin(r); !ok {
		unauthorized(w)
		return
	}
	id, err := pathID(r)
	if err != nil {
		response.Fail(w, http.StatusBadRequest, "bad_request", "invalid id")
		return
	}
	sh, err := h.q.GetShipment(r.Context(), id)
	if err != nil {
		response.Fail(w, http.StatusNotFound, "not_found", "shipment not found")
		return
	}
	var req struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Status == "" {
		response.Fail(w, http.StatusBadRequest, "bad_request", "status is required")
		return
	}
	allowed := false
	for _, t := range shipmentTransitions[sh.Status] {
		if t == req.Status {
			allowed = true
		}
	}
	if !allowed {
		response.Fail(w, http.StatusConflict, "invalid_transition", "cannot move shipment from "+sh.Status+" to "+req.Status)
		return
	}
	var shippedAt pgtype.Timestamptz
	if req.Status == "shipped" {
		shippedAt = pgtype.Timestamptz{Time: time.Now(), Valid: true}
	}
	updated, err := h.q.SetShipmentStatus(r.Context(), gen.SetShipmentStatusParams{ID: id, Status: req.Status, ShippedAt: shippedAt})
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not update shipment")
		return
	}
	response.JSON(w, http.StatusOK, updated)
}
