// Package pricing implements the pricing engine (Implementation Pack 1 §4 + §12.1):
// price lists, tiered prices, assignments, deterministic resolution, and the
// precomputed combined_prices cache rebuilt by a river job. Admin-only, org-scoped.
package pricing

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	mw "b2bcommerce/internal/server/middleware"
	"b2bcommerce/internal/server/response"
	"b2bcommerce/internal/store/gen"
)

// Enqueuer schedules background recompute of the combined_prices cache. It is
// satisfied by *queue.Enqueuer in production and by a synchronous shim in tests.
// May be nil (auto-enqueue and the recompute endpoint then become no-ops/503).
type Enqueuer interface {
	EnqueueRecompute(ctx context.Context, customerID int64, websiteID *int64, currency string) error
}

type Handler struct {
	q   *gen.Queries
	enq Enqueuer
}

func New(q *gen.Queries, enq Enqueuer) *Handler { return &Handler{q: q, enq: enq} }

func (h *Handler) Routes(r chi.Router, authMW func(http.Handler) http.Handler) {
	r.Group(func(ar chi.Router) {
		ar.Use(authMW)

		ar.With(mw.RequirePermission("price_list.view")).Get("/admin/price-lists", h.listLists)
		ar.With(mw.RequirePermission("price_list.manage")).Post("/admin/price-lists", h.createList)
		ar.With(mw.RequirePermission("price_list.view")).Get("/admin/price-lists/{id}", h.getList)
		ar.With(mw.RequirePermission("price_list.manage")).Put("/admin/price-lists/{id}", h.updateList)

		ar.With(mw.RequirePermission("price_list.view")).Get("/admin/price-lists/{id}/prices", h.listPrices)
		ar.With(mw.RequirePermission("price_list.manage")).Post("/admin/price-lists/{id}/prices", h.upsertPrice)

		ar.With(mw.RequirePermission("price_list.view")).Get("/admin/price-lists/{id}/assignments", h.listAssignments)
		ar.With(mw.RequirePermission("price_list.manage")).Post("/admin/price-list-assignments", h.createAssignment)
		ar.With(mw.RequirePermission("price_list.manage")).Delete("/admin/price-list-assignments/{id}", h.deleteAssignment)

		ar.With(mw.RequirePermission("price_list.view")).Get("/admin/pricing/resolve", h.resolve)
		ar.With(mw.RequirePermission("price_list.view")).Get("/admin/customers/{id}/combined-prices", h.combinedForCustomer)
		ar.With(mw.RequirePermission("price_list.manage")).Post("/admin/pricing/recompute", h.recompute)
	})
}

func orgID(r *http.Request) (int64, bool) {
	c, ok := mw.ClaimsFrom(r.Context())
	if !ok {
		return 0, false
	}
	return c.OrgID, true
}

func pathID(r *http.Request) (int64, error) {
	return strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
}

// ---- Price lists ---------------------------------------------------------

func (h *Handler) listLists(w http.ResponseWriter, r *http.Request) {
	org, ok := orgID(r)
	if !ok {
		response.Fail(w, http.StatusUnauthorized, "unauthorized", "no claims")
		return
	}
	rows, err := h.q.ListPriceLists(r.Context(), org)
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not list price lists")
		return
	}
	if rows == nil {
		rows = []gen.PriceList{}
	}
	response.JSON(w, http.StatusOK, map[string]any{"items": rows})
}

func (h *Handler) createList(w http.ResponseWriter, r *http.Request) {
	org, ok := orgID(r)
	if !ok {
		response.Fail(w, http.StatusUnauthorized, "unauthorized", "no claims")
		return
	}
	var req struct {
		Name      string `json:"name"`
		Currency  string `json:"currency"`
		IsDefault bool   `json:"is_default"`
		IsActive  *bool  `json:"is_active"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" || len(req.Currency) != 3 {
		response.Fail(w, http.StatusBadRequest, "bad_request", "name and 3-letter currency required")
		return
	}
	active := true
	if req.IsActive != nil {
		active = *req.IsActive
	}
	pl, err := h.q.CreatePriceList(r.Context(), gen.CreatePriceListParams{
		OrganizationID: org, Name: req.Name, Currency: req.Currency, IsDefault: req.IsDefault, IsActive: active,
	})
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not create price list")
		return
	}
	response.JSON(w, http.StatusCreated, pl)
}

func (h *Handler) getList(w http.ResponseWriter, r *http.Request) {
	pl, ok := h.loadList(w, r)
	if !ok {
		return
	}
	response.JSON(w, http.StatusOK, pl)
}

func (h *Handler) updateList(w http.ResponseWriter, r *http.Request) {
	org, ok := orgID(r)
	if !ok {
		response.Fail(w, http.StatusUnauthorized, "unauthorized", "no claims")
		return
	}
	id, err := pathID(r)
	if err != nil {
		response.Fail(w, http.StatusBadRequest, "bad_request", "invalid id")
		return
	}
	var req struct {
		Name      string `json:"name"`
		Currency  string `json:"currency"`
		IsDefault bool   `json:"is_default"`
		IsActive  bool   `json:"is_active"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" || len(req.Currency) != 3 {
		response.Fail(w, http.StatusBadRequest, "bad_request", "name and 3-letter currency required")
		return
	}
	pl, err := h.q.UpdatePriceList(r.Context(), gen.UpdatePriceListParams{
		OrganizationID: org, ID: id, Name: req.Name, Currency: req.Currency, IsDefault: req.IsDefault, IsActive: req.IsActive,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			response.Fail(w, http.StatusNotFound, "not_found", "price list not found")
			return
		}
		response.Fail(w, http.StatusInternalServerError, "internal", "could not update price list")
		return
	}
	h.enqueueForList(r.Context(), pl)
	response.JSON(w, http.StatusOK, pl)
}

// ---- Prices (tiers) ------------------------------------------------------

func (h *Handler) listPrices(w http.ResponseWriter, r *http.Request) {
	pl, ok := h.loadList(w, r)
	if !ok {
		return
	}
	rows, err := h.q.ListPricesForList(r.Context(), pl.ID)
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not list prices")
		return
	}
	if rows == nil {
		rows = []gen.Price{}
	}
	response.JSON(w, http.StatusOK, map[string]any{"items": rows})
}

func (h *Handler) upsertPrice(w http.ResponseWriter, r *http.Request) {
	pl, ok := h.loadList(w, r)
	if !ok {
		return
	}
	var req struct {
		ProductID   int64      `json:"product_id"`
		Unit        string     `json:"unit"`
		MinQuantity string     `json:"min_quantity"`
		Value       string     `json:"value"`
		ValidFrom   *time.Time `json:"valid_from"`
		ValidTo     *time.Time `json:"valid_to"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ProductID == 0 || req.Value == "" {
		response.Fail(w, http.StatusBadRequest, "bad_request", "product_id and value are required")
		return
	}
	if req.Unit == "" {
		req.Unit = "each"
	}
	if req.MinQuantity == "" {
		req.MinQuantity = "1"
	}
	p, err := h.q.UpsertPrice(r.Context(), gen.UpsertPriceParams{
		PriceListID: pl.ID, ProductID: req.ProductID, Unit: req.Unit,
		MinQuantity: req.MinQuantity, Value: req.Value,
		ValidFrom: tsPtr(req.ValidFrom), ValidTo: tsPtr(req.ValidTo),
	})
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not upsert price")
		return
	}
	h.enqueueForList(r.Context(), pl)
	response.JSON(w, http.StatusCreated, p)
}

// ---- Assignments ---------------------------------------------------------

func (h *Handler) listAssignments(w http.ResponseWriter, r *http.Request) {
	pl, ok := h.loadList(w, r)
	if !ok {
		return
	}
	rows, err := h.q.ListAssignmentsForList(r.Context(), pl.ID)
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not list assignments")
		return
	}
	if rows == nil {
		rows = []gen.PriceListAssignment{}
	}
	response.JSON(w, http.StatusOK, map[string]any{"items": rows})
}

func (h *Handler) createAssignment(w http.ResponseWriter, r *http.Request) {
	org, ok := orgID(r)
	if !ok {
		response.Fail(w, http.StatusUnauthorized, "unauthorized", "no claims")
		return
	}
	var req struct {
		PriceListID     int64  `json:"price_list_id"`
		CustomerID      *int64 `json:"customer_id"`
		CustomerGroupID *int64 `json:"customer_group_id"`
		WebsiteID       *int64 `json:"website_id"`
		Priority        int32  `json:"priority"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.PriceListID == 0 {
		response.Fail(w, http.StatusBadRequest, "bad_request", "price_list_id is required")
		return
	}
	// Exactly one target (mirrors the DB CHECK; fail fast with a clear message).
	if boolToInt(req.CustomerID != nil)+boolToInt(req.CustomerGroupID != nil)+boolToInt(req.WebsiteID != nil) != 1 {
		response.Fail(w, http.StatusBadRequest, "bad_request", "exactly one of customer_id, customer_group_id, website_id")
		return
	}
	// Price list must belong to the caller's org.
	pl, err := h.q.GetPriceList(r.Context(), gen.GetPriceListParams{OrganizationID: org, ID: req.PriceListID})
	if err != nil {
		response.Fail(w, http.StatusBadRequest, "bad_request", "price_list_id not found in organization")
		return
	}
	a, err := h.q.CreatePriceListAssignment(r.Context(), gen.CreatePriceListAssignmentParams{
		PriceListID: req.PriceListID, CustomerID: req.CustomerID,
		CustomerGroupID: req.CustomerGroupID, WebsiteID: req.WebsiteID, Priority: req.Priority,
	})
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not create assignment")
		return
	}
	h.enqueueForList(r.Context(), pl)
	response.JSON(w, http.StatusCreated, a)
}

func (h *Handler) deleteAssignment(w http.ResponseWriter, r *http.Request) {
	if _, ok := orgID(r); !ok {
		response.Fail(w, http.StatusUnauthorized, "unauthorized", "no claims")
		return
	}
	id, err := pathID(r)
	if err != nil {
		response.Fail(w, http.StatusBadRequest, "bad_request", "invalid id")
		return
	}
	n, err := h.q.DeleteAssignment(r.Context(), id)
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not delete assignment")
		return
	}
	if n == 0 {
		response.Fail(w, http.StatusNotFound, "not_found", "assignment not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---- Resolution + recompute ----------------------------------------------

func (h *Handler) resolve(w http.ResponseWriter, r *http.Request) {
	if _, ok := orgID(r); !ok {
		response.Fail(w, http.StatusUnauthorized, "unauthorized", "no claims")
		return
	}
	q := r.URL.Query()
	customerID, _ := strconv.ParseInt(q.Get("customer_id"), 10, 64)
	productID, _ := strconv.ParseInt(q.Get("product_id"), 10, 64)
	if customerID == 0 || productID == 0 {
		response.Fail(w, http.StatusBadRequest, "bad_request", "customer_id and product_id are required")
		return
	}
	qty := q.Get("quantity")
	if qty == "" {
		qty = "1"
	}
	currency := q.Get("currency")
	if len(currency) != 3 {
		response.Fail(w, http.StatusBadRequest, "bad_request", "3-letter currency required")
		return
	}
	var websiteID *int64
	if v := q.Get("website_id"); v != "" {
		id, _ := strconv.ParseInt(v, 10, 64)
		websiteID = &id
	}
	row, err := h.q.ResolvePrice(r.Context(), gen.ResolvePriceParams{
		ID: customerID, ProductID: productID, Column3: qty, Currency: currency,
		WebsiteID: websiteID, ValidFrom: now(),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// No price resolves -> "price on request" (RFQ path), not free.
			response.JSON(w, http.StatusOK, map[string]any{"price_on_request": true})
			return
		}
		response.Fail(w, http.StatusInternalServerError, "internal", "could not resolve price")
		return
	}
	response.JSON(w, http.StatusOK, map[string]any{
		"price_on_request":     false,
		"value":                row.Value,
		"source_price_list_id": row.PriceListID,
		"currency":             currency,
	})
}

func (h *Handler) combinedForCustomer(w http.ResponseWriter, r *http.Request) {
	if _, ok := orgID(r); !ok {
		response.Fail(w, http.StatusUnauthorized, "unauthorized", "no claims")
		return
	}
	id, err := pathID(r)
	if err != nil {
		response.Fail(w, http.StatusBadRequest, "bad_request", "invalid id")
		return
	}
	currency := r.URL.Query().Get("currency")
	if len(currency) != 3 {
		response.Fail(w, http.StatusBadRequest, "bad_request", "3-letter currency required")
		return
	}
	rows, err := h.q.ListCombinedPricesForCustomer(r.Context(), gen.ListCombinedPricesForCustomerParams{CustomerID: id, Currency: currency})
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not load combined prices")
		return
	}
	if rows == nil {
		rows = []gen.CombinedPrice{}
	}
	response.JSON(w, http.StatusOK, map[string]any{"items": rows})
}

func (h *Handler) recompute(w http.ResponseWriter, r *http.Request) {
	org, ok := orgID(r)
	if !ok {
		response.Fail(w, http.StatusUnauthorized, "unauthorized", "no claims")
		return
	}
	if h.enq == nil {
		response.Fail(w, http.StatusServiceUnavailable, "unavailable", "recompute queue not configured")
		return
	}
	var req struct {
		CustomerID int64  `json:"customer_id"`
		WebsiteID  *int64 `json:"website_id"`
		Currency   string `json:"currency"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.CustomerID == 0 {
		response.Fail(w, http.StatusBadRequest, "bad_request", "customer_id is required")
		return
	}
	if req.Currency == "" || req.WebsiteID == nil {
		ws, err := h.q.GetDefaultWebsite(r.Context(), org)
		if err != nil {
			response.Fail(w, http.StatusBadRequest, "bad_request", "no website to resolve currency/fallback")
			return
		}
		if req.WebsiteID == nil {
			req.WebsiteID = &ws.ID
		}
		if req.Currency == "" {
			req.Currency = ws.DefaultCurrency
		}
	}
	if err := h.enq.EnqueueRecompute(r.Context(), req.CustomerID, req.WebsiteID, req.Currency); err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not enqueue recompute")
		return
	}
	response.JSON(w, http.StatusAccepted, map[string]any{"enqueued": true})
}

// ---- helpers -------------------------------------------------------------

func (h *Handler) loadList(w http.ResponseWriter, r *http.Request) (gen.PriceList, bool) {
	org, ok := orgID(r)
	if !ok {
		response.Fail(w, http.StatusUnauthorized, "unauthorized", "no claims")
		return gen.PriceList{}, false
	}
	id, err := pathID(r)
	if err != nil {
		response.Fail(w, http.StatusBadRequest, "bad_request", "invalid id")
		return gen.PriceList{}, false
	}
	pl, err := h.q.GetPriceList(r.Context(), gen.GetPriceListParams{OrganizationID: org, ID: id})
	if err != nil {
		response.Fail(w, http.StatusNotFound, "not_found", "price list not found")
		return gen.PriceList{}, false
	}
	return pl, true
}

// enqueueForList fans recompute out to every customer affected by a price list
// change (Pack 1 §4 AC). Best-effort: enqueue failures are not fatal to the
// request, and a nil enqueuer (e.g. in some tests) is a no-op.
func (h *Handler) enqueueForList(ctx context.Context, pl gen.PriceList) {
	if h.enq == nil {
		return
	}
	customers, err := h.q.CustomersAffectedByPriceList(ctx, pl.ID)
	if err != nil {
		return
	}
	ws, err := h.q.GetDefaultWebsite(ctx, pl.OrganizationID)
	var websiteID *int64
	if err == nil {
		websiteID = &ws.ID
	}
	for _, cid := range customers {
		_ = h.enq.EnqueueRecompute(ctx, cid, websiteID, pl.Currency)
	}
}

func tsPtr(t *time.Time) pgtype.Timestamptz {
	if t == nil {
		return pgtype.Timestamptz{}
	}
	return pgtype.Timestamptz{Time: *t, Valid: true}
}

func now() pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: time.Now(), Valid: true}
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
