package otc

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"b2bcommerce/internal/inventory"
	"b2bcommerce/internal/money"
	"b2bcommerce/internal/server/response"
	"b2bcommerce/internal/store/gen"
	"b2bcommerce/internal/workflow"
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
		WarehouseID    *int64  `json:"warehouse_id"`
		Items          []struct {
			OrderItemID int64  `json:"order_item_id"`
			Quantity    string `json:"quantity"`
		} `json:"items"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || len(req.Items) == 0 {
		response.Fail(w, http.StatusBadRequest, "bad_request", "at least one item is required")
		return
	}
	// Resolve the fulfilling warehouse: an explicit one (validated to the org) or
	// the org's default. Stored on the shipment so stock is drawn from it on ship.
	if req.WarehouseID != nil {
		if _, err := h.q.GetWarehouse(r.Context(), gen.GetWarehouseParams{OrganizationID: a.orgID, ID: *req.WarehouseID}); err != nil {
			response.Fail(w, http.StatusBadRequest, "bad_request", "warehouse not found in organization")
			return
		}
	} else if def, err := h.q.GetDefaultWarehouse(r.Context(), a.orgID); err == nil {
		req.WarehouseID = &def.ID
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
			OrderID: order.ID, Carrier: req.Carrier, TrackingNumber: req.TrackingNumber, WarehouseID: req.WarehouseID,
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

// Shipment lifecycle transitions live in the `shipment_default` workflow
// definition (migration 0015), applied via the configurable engine.
func (h *Handler) patchShipmentStatus(w http.ResponseWriter, r *http.Request) {
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
	by := "user:0"
	if a.userID != nil {
		by = "user:" + strconv.FormatInt(*a.userID, 10)
	}

	inst, err := h.wf.EnsureInstance(r.Context(), a.orgID, "shipment_default", "shipment", sh.ID, sh.Status)
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not load shipment workflow")
		return
	}
	hook := func(q *gen.Queries, _, to gen.WorkflowState) error {
		var shippedAt pgtype.Timestamptz
		if to.Code == "shipped" {
			shippedAt = pgtype.Timestamptz{Time: time.Now(), Valid: true}
		}
		if _, e := q.SetShipmentStatus(r.Context(), gen.SetShipmentStatusParams{ID: id, Status: to.Code, ShippedAt: shippedAt}); e != nil {
			return e
		}
		// Shipping converts reservation to fulfilment for tracked lines (§8).
		if to.Code == "shipped" {
			return inventory.FulfilShipment(r.Context(), q, a.orgID, id, by)
		}
		return nil
	}
	actor := workflow.Actor{Type: "user", ID: a.userID}
	if _, err := h.wf.ApplyTransitionTo(r.Context(), inst.ID, req.Status, actor, nil, hook); err != nil {
		switch {
		case errors.Is(err, workflow.ErrNoTransition), errors.Is(err, workflow.ErrFinalState):
			response.Fail(w, http.StatusConflict, "invalid_transition", "cannot move shipment from "+sh.Status+" to "+req.Status)
		default:
			response.Fail(w, http.StatusInternalServerError, "internal", "could not update shipment")
		}
		return
	}
	updated, err := h.q.GetShipment(r.Context(), id)
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not load shipment")
		return
	}
	response.JSON(w, http.StatusOK, updated)
}
