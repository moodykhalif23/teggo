package inventory

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	mw "b2bcommerce/internal/server/middleware"
	"b2bcommerce/internal/server/response"
	"b2bcommerce/internal/store/gen"
)

type Handler struct {
	pool *pgxpool.Pool
	q    *gen.Queries
}

func New(pool *pgxpool.Pool) *Handler { return &Handler{pool: pool, q: gen.New(pool)} }

func (h *Handler) Routes(r chi.Router, authMW func(http.Handler) http.Handler) {
	r.Group(func(ar chi.Router) {
		ar.Use(authMW)
		ar.Use(mw.RequireAudience("admin"))

		ar.With(mw.RequirePermission("inventory.view")).Get("/admin/warehouses", h.listWarehouses)
		ar.With(mw.RequirePermission("inventory.manage")).Post("/admin/warehouses", h.createWarehouse)

		ar.With(mw.RequirePermission("inventory.view")).Get("/admin/inventory/movements", h.listMovements)
		ar.With(mw.RequirePermission("inventory.view")).Get("/admin/inventory/atp", h.atp)
		ar.With(mw.RequirePermission("inventory.view")).Get("/admin/inventory/{productId}", h.levelsForProduct)
		ar.With(mw.RequirePermission("inventory.manage")).Put("/admin/inventory/{productId}", h.setLevelConfig)
		ar.With(mw.RequirePermission("inventory.manage")).Post("/admin/inventory/adjustments", h.adjust)
	})
}

func orgID(r *http.Request) (int64, bool) {
	c, ok := mw.ClaimsFrom(r.Context())
	if !ok {
		return 0, false
	}
	return c.OrgID, true
}

func actorOrg(r *http.Request) (int64, string, bool) {
	c, ok := mw.ClaimsFrom(r.Context())
	if !ok {
		return 0, "", false
	}
	by := "user:" + c.Subject
	return c.OrgID, by, true
}

func (h *Handler) listWarehouses(w http.ResponseWriter, r *http.Request) {
	org, ok := orgID(r)
	if !ok {
		response.Fail(w, http.StatusUnauthorized, "unauthorized", "no claims")
		return
	}
	rows, err := h.q.ListWarehouses(r.Context(), org)
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not list warehouses")
		return
	}
	if rows == nil {
		rows = []gen.Warehouse{}
	}
	response.JSON(w, http.StatusOK, map[string]any{"items": rows})
}

func (h *Handler) createWarehouse(w http.ResponseWriter, r *http.Request) {
	org, ok := orgID(r)
	if !ok {
		response.Fail(w, http.StatusUnauthorized, "unauthorized", "no claims")
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" {
		response.Fail(w, http.StatusBadRequest, "bad_request", "name is required")
		return
	}
	wh, err := h.q.CreateWarehouse(r.Context(), gen.CreateWarehouseParams{OrganizationID: org, Name: req.Name})
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not create warehouse")
		return
	}
	response.JSON(w, http.StatusCreated, wh)
}

func (h *Handler) levelsForProduct(w http.ResponseWriter, r *http.Request) {
	org, ok := orgID(r)
	if !ok {
		response.Fail(w, http.StatusUnauthorized, "unauthorized", "no claims")
		return
	}
	productID, err := strconv.ParseInt(chi.URLParam(r, "productId"), 10, 64)
	if err != nil {
		response.Fail(w, http.StatusBadRequest, "bad_request", "invalid product id")
		return
	}
	if _, err := h.q.GetProductByID(r.Context(), gen.GetProductByIDParams{OrganizationID: org, ID: productID}); err != nil {
		response.Fail(w, http.StatusNotFound, "not_found", "product not found")
		return
	}
	rows, err := h.q.ListInventoryLevelsForProduct(r.Context(), productID)
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not load levels")
		return
	}
	if rows == nil {
		rows = []gen.ListInventoryLevelsForProductRow{}
	}
	response.JSON(w, http.StatusOK, map[string]any{"product_id": productID, "levels": rows})
}

func (h *Handler) setLevelConfig(w http.ResponseWriter, r *http.Request) {
	org, by, ok := actorOrg(r)
	if !ok {
		response.Fail(w, http.StatusUnauthorized, "unauthorized", "no claims")
		return
	}
	_ = by
	productID, err := strconv.ParseInt(chi.URLParam(r, "productId"), 10, 64)
	if err != nil {
		response.Fail(w, http.StatusBadRequest, "bad_request", "invalid product id")
		return
	}
	var req struct {
		WarehouseID      int64   `json:"warehouse_id"`
		ReorderThreshold *string `json:"reorder_threshold"`
		AllowBackorder   bool    `json:"allow_backorder"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.WarehouseID == 0 {
		response.Fail(w, http.StatusBadRequest, "bad_request", "warehouse_id is required")
		return
	}
	if _, err := h.q.GetProductByID(r.Context(), gen.GetProductByIDParams{OrganizationID: org, ID: productID}); err != nil {
		response.Fail(w, http.StatusNotFound, "not_found", "product not found")
		return
	}
	if _, err := h.q.GetWarehouse(r.Context(), gen.GetWarehouseParams{OrganizationID: org, ID: req.WarehouseID}); err != nil {
		response.Fail(w, http.StatusBadRequest, "bad_request", "warehouse not found in organization")
		return
	}
	lvl, err := h.q.SetInventoryLevelConfig(r.Context(), gen.SetInventoryLevelConfigParams{
		ProductID: productID, WarehouseID: req.WarehouseID, ReorderThreshold: req.ReorderThreshold, AllowBackorder: req.AllowBackorder,
	})
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not set level config")
		return
	}
	response.JSON(w, http.StatusOK, lvl)
}

// adjust records a manual stock movement (receipt, return, or adjustment) and
// updates on-hand. Reservations/fulfilment are driven by the order flow, not here.
func (h *Handler) adjust(w http.ResponseWriter, r *http.Request) {
	org, by, ok := actorOrg(r)
	if !ok {
		response.Fail(w, http.StatusUnauthorized, "unauthorized", "no claims")
		return
	}
	var req struct {
		ProductID   int64  `json:"product_id"`
		WarehouseID int64  `json:"warehouse_id"`
		Type        string `json:"type"`
		Quantity    string `json:"quantity"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ProductID == 0 || req.WarehouseID == 0 || req.Quantity == "" {
		response.Fail(w, http.StatusBadRequest, "bad_request", "product_id, warehouse_id, quantity required")
		return
	}
	switch req.Type {
	case "receipt", "return", "adjustment":
	default:
		response.Fail(w, http.StatusBadRequest, "bad_request", "type must be receipt, return, or adjustment")
		return
	}
	if _, err := h.q.GetProductByID(r.Context(), gen.GetProductByIDParams{OrganizationID: org, ID: req.ProductID}); err != nil {
		response.Fail(w, http.StatusNotFound, "not_found", "product not found")
		return
	}
	if _, err := h.q.GetWarehouse(r.Context(), gen.GetWarehouseParams{OrganizationID: org, ID: req.WarehouseID}); err != nil {
		response.Fail(w, http.StatusBadRequest, "bad_request", "warehouse not found in organization")
		return
	}

	var level gen.InventoryLevel
	err := h.txn(r.Context(), func(q *gen.Queries) error {
		if err := q.EnsureInventoryLevel(r.Context(), gen.EnsureInventoryLevelParams{ProductID: req.ProductID, WarehouseID: req.WarehouseID}); err != nil {
			return err
		}
		var e error
		level, e = q.AdjustInventoryLevel(r.Context(), gen.AdjustInventoryLevelParams{
			ProductID: req.ProductID, WarehouseID: req.WarehouseID, Column3: req.Quantity, Column4: "0",
		})
		if e != nil {
			return e
		}
		_, e = q.AddInventoryMovement(r.Context(), gen.AddInventoryMovementParams{
			ProductID: req.ProductID, WarehouseID: req.WarehouseID, Type: req.Type, Quantity: req.Quantity,
			ReferenceType: refptr("manual"), CreatedBy: refptr(by),
		})
		return e
	})
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not record adjustment")
		return
	}
	response.JSON(w, http.StatusCreated, level)
}

func (h *Handler) listMovements(w http.ResponseWriter, r *http.Request) {
	if _, ok := orgID(r); !ok {
		response.Fail(w, http.StatusUnauthorized, "unauthorized", "no claims")
		return
	}
	productID, _ := strconv.ParseInt(r.URL.Query().Get("product_id"), 10, 64)
	warehouseID, _ := strconv.ParseInt(r.URL.Query().Get("warehouse_id"), 10, 64)
	if productID == 0 || warehouseID == 0 {
		response.Fail(w, http.StatusBadRequest, "bad_request", "product_id and warehouse_id are required")
		return
	}
	rows, err := h.q.ListInventoryMovements(r.Context(), gen.ListInventoryMovementsParams{ProductID: productID, WarehouseID: warehouseID})
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not list movements")
		return
	}
	if rows == nil {
		rows = []gen.InventoryMovement{}
	}
	response.JSON(w, http.StatusOK, map[string]any{"items": rows})
}

func (h *Handler) atp(w http.ResponseWriter, r *http.Request) {
	if _, ok := orgID(r); !ok {
		response.Fail(w, http.StatusUnauthorized, "unauthorized", "no claims")
		return
	}
	var ids []int64
	for _, s := range strings.Split(r.URL.Query().Get("product_ids"), ",") {
		if s == "" {
			continue
		}
		if id, err := strconv.ParseInt(s, 10, 64); err == nil {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		response.Fail(w, http.StatusBadRequest, "bad_request", "product_ids is required")
		return
	}
	rows, err := h.q.AvailableToPromise(r.Context(), ids)
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not compute availability")
		return
	}
	out := make(map[string]string, len(rows))
	for _, row := range rows {
		out[strconv.FormatInt(row.ProductID, 10)] = row.Available
	}
	response.JSON(w, http.StatusOK, map[string]any{"available": out})
}

func (h *Handler) txn(ctx context.Context, fn func(*gen.Queries) error) error {
	t, err := h.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer t.Rollback(ctx) //nolint:errcheck
	if err := fn(gen.New(t)); err != nil {
		return err
	}
	return t.Commit(ctx)
}

func refptr(s string) *string { return &s }
