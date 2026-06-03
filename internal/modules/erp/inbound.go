package erp

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgconn"

	erpconn "b2bcommerce/internal/erp"
	"b2bcommerce/internal/money"
	"b2bcommerce/internal/server/response"
	"b2bcommerce/internal/store/gen"
)

func (h *Handler) inbound(w http.ResponseWriter, r *http.Request) {
	connID, err := strconv.ParseInt(chi.URLParam(r, "connectionID"), 10, 64)
	if err != nil {
		response.Fail(w, http.StatusBadRequest, "bad_request", "invalid connection id")
		return
	}
	conn, err := h.q.GetIntegrationConnectionByID(r.Context(), connID)
	if err != nil || !conn.IsActive {
		response.Fail(w, http.StatusNotFound, "not_found", "connection not found")
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		response.Fail(w, http.StatusBadRequest, "bad_request", "could not read body")
		return
	}
	// Verify signature when the connection has a secret.
	if conn.Secret != nil && *conn.Secret != "" {
		if !erpconn.Verify(*conn.Secret, body, r.Header.Get(erpconn.SignatureHeader)) {
			response.Fail(w, http.StatusUnauthorized, "bad_signature", "invalid webhook signature")
			return
		}
	}

	var ev struct {
		EventID        string `json:"event_id"`
		EntityType     string `json:"entity_type"`
		SKU            string `json:"sku"`
		QuantityOnHand string `json:"quantity_on_hand"`
	}
	if err := json.Unmarshal(body, &ev); err != nil || ev.EventID == "" || ev.EntityType == "" {
		response.Fail(w, http.StatusBadRequest, "bad_request", "event_id and entity_type are required")
		return
	}

	status := "processed"
	var applyErr string
	switch ev.EntityType {
	case "inventory":
		if err := h.applyInventory(r, conn.OrganizationID, ev.SKU, ev.QuantityOnHand); err != nil {
			status, applyErr = "error", err.Error()
		}
	default:
		status = "skipped" // unsupported inbound entity
	}

	var ep *string
	if applyErr != "" {
		ep = &applyErr
	}
	var key *string
	if status != "error" {
		key = &ev.EventID
	}
	if _, err := h.q.CreateSyncLog(r.Context(), gen.CreateSyncLogParams{
		OrganizationID: conn.OrganizationID, ConnectionID: conn.ID, Direction: "inbound",
		EntityType: ev.EntityType, Operation: "upsert", Status: status,
		IdempotencyKey: key, Error: ep,
	}); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			response.JSON(w, http.StatusOK, map[string]any{"duplicate": true})
			return
		}
		response.Fail(w, http.StatusInternalServerError, "internal", "could not record event")
		return
	}
	if status == "error" {
		response.Fail(w, http.StatusUnprocessableEntity, "apply_failed", applyErr)
		return
	}
	response.JSON(w, http.StatusOK, map[string]any{"status": status})
}

func (h *Handler) applyInventory(r *http.Request, org int64, sku, qty string) error {
	if sku == "" || qty == "" {
		return errInvalid("sku and quantity_on_hand are required")
	}
	if _, err := money.Parse(qty); err != nil {
		return errInvalid("quantity_on_hand must be a decimal")
	}
	prod, err := h.q.GetProductBySKU(r.Context(), gen.GetProductBySKUParams{OrganizationID: org, Sku: sku})
	if err != nil {
		return errInvalid("unknown SKU: " + sku)
	}
	wh, err := h.q.GetDefaultWarehouse(r.Context(), org)
	if err != nil {
		return errInvalid("no default warehouse")
	}
	return h.q.SetInventoryOnHand(r.Context(), gen.SetInventoryOnHandParams{
		ProductID: prod.ID, WarehouseID: wh.ID, QuantityOnHand: qty,
	})
}

type strErr string

func (e strErr) Error() string  { return string(e) }
func errInvalid(s string) error { return strErr(s) }
