// Package subscription is the HTTP surface for recurring orders — admin
// management (list/detail/status/run-now) and storefront self-service
// (subscribe/list/pause/skip/cancel). The materialization engine lives in
// internal/subscriptions; this module manages the records it runs on.
package subscription

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	mw "b2bcommerce/internal/server/middleware"
	"b2bcommerce/internal/server/response"
	"b2bcommerce/internal/store/gen"
	"b2bcommerce/internal/subscriptions"
)

type Handler struct {
	pool *pgxpool.Pool
	q    *gen.Queries
}

func New(pool *pgxpool.Pool) *Handler { return &Handler{pool: pool, q: gen.New(pool)} }

func (h *Handler) Routes(r chi.Router, authMW func(http.Handler) http.Handler) {
	// Admin.
	r.Group(func(ar chi.Router) {
		ar.Use(authMW)
		ar.Use(mw.RequireAudience("admin"))
		ar.With(mw.RequirePermission("subscription.view")).Get("/admin/subscriptions", h.adminList)
		ar.With(mw.RequirePermission("subscription.view")).Get("/admin/subscriptions/{id}", h.adminGet)
		ar.With(mw.RequirePermission("subscription.manage")).Put("/admin/subscriptions/{id}", h.adminUpdate)
		ar.With(mw.RequirePermission("subscription.manage")).Post("/admin/subscriptions/{id}/status", h.adminSetStatus)
		ar.With(mw.RequirePermission("subscription.manage")).Post("/admin/subscriptions/{id}/run", h.adminRunNow)
	})
	// Storefront (the buying company self-manages).
	r.Group(func(sr chi.Router) {
		sr.Use(authMW)
		sr.Use(mw.RequireAudience("storefront"))
		sr.Get("/storefront/subscriptions", h.myList)
		sr.Post("/storefront/subscriptions", h.create)
		sr.Get("/storefront/subscriptions/{id}", h.myGet)
		sr.Put("/storefront/subscriptions/{id}", h.myUpdate)
		sr.Post("/storefront/subscriptions/{id}/status", h.mySetStatus)
		sr.Post("/storefront/subscriptions/{id}/skip", h.mySkip)
	})
}

// ---- helpers -------------------------------------------------------------

func validCadence(c string) bool {
	switch c {
	case "weekly", "biweekly", "monthly", "quarterly":
		return true
	}
	return false
}

func dateStr(d pgtype.Date) string {
	if !d.Valid {
		return ""
	}
	return d.Time.Format("2006-01-02")
}
func tsPtr(t pgtype.Timestamptz) *time.Time {
	if !t.Valid {
		return nil
	}
	v := t.Time
	return &v
}

type subscriptionDTO struct {
	ID          int64      `json:"id"`
	PublicID    string     `json:"public_id"`
	CustomerID  int64      `json:"customer_id"`
	Name        *string    `json:"name,omitempty"`
	Currency    string     `json:"currency"`
	Cadence     string     `json:"cadence"`
	NextRunDate string     `json:"next_run_date"`
	Status      string     `json:"status"`
	PoNumber    *string    `json:"po_number,omitempty"`
	LastRunAt   *time.Time `json:"last_run_at,omitempty"`
	Items       []itemDTO  `json:"items,omitempty"`
	Runs        []runDTO   `json:"runs,omitempty"`
}
type itemDTO struct {
	ID        int64  `json:"id"`
	ProductID int64  `json:"product_id"`
	SKU       string `json:"sku"`
	Name      string `json:"name"`
	Quantity  string `json:"quantity"`
	Unit      string `json:"unit"`
}
type runDTO struct {
	ID        int64     `json:"id"`
	OrderID   *int64    `json:"order_id,omitempty"`
	RunDate   string    `json:"run_date"`
	Status    string    `json:"status"`
	Note      *string   `json:"note,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

func toDTO(s gen.Subscription) subscriptionDTO {
	return subscriptionDTO{
		ID: s.ID, PublicID: s.PublicID.String(), CustomerID: s.CustomerID, Name: s.Name,
		Currency: s.Currency, Cadence: s.Cadence, NextRunDate: dateStr(s.NextRunDate),
		Status: s.Status, PoNumber: s.PoNumber, LastRunAt: tsPtr(s.LastRunAt),
	}
}

func (h *Handler) loadDetail(r *http.Request, s gen.Subscription) subscriptionDTO {
	dto := toDTO(s)
	if its, err := h.q.ListSubscriptionItems(r.Context(), s.ID); err == nil {
		for _, it := range its {
			dto.Items = append(dto.Items, itemDTO{ID: it.ID, ProductID: it.ProductID, SKU: it.Sku, Name: it.Name, Quantity: it.Quantity, Unit: it.Unit})
		}
	}
	if runs, err := h.q.ListSubscriptionRuns(r.Context(), s.ID); err == nil {
		for _, rn := range runs {
			dto.Runs = append(dto.Runs, runDTO{ID: rn.ID, OrderID: rn.OrderID, RunDate: dateStr(rn.RunDate), Status: rn.Status, Note: rn.Note, CreatedAt: rn.CreatedAt})
		}
	}
	return dto
}

func pathID(r *http.Request) (int64, error) {
	return strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
}

// ---- admin ---------------------------------------------------------------

func (h *Handler) adminList(w http.ResponseWriter, r *http.Request) {
	c, ok := mw.ClaimsFrom(r.Context())
	if !ok {
		response.Fail(w, http.StatusUnauthorized, "unauthorized", "no claims")
		return
	}
	rows, err := h.q.ListSubscriptionsAdmin(r.Context(), c.OrgID)
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not list subscriptions")
		return
	}
	items := make([]subscriptionDTO, 0, len(rows))
	for _, s := range rows {
		items = append(items, toDTO(s))
	}
	response.JSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *Handler) adminGet(w http.ResponseWriter, r *http.Request) {
	c, ok := mw.ClaimsFrom(r.Context())
	if !ok {
		response.Fail(w, http.StatusUnauthorized, "unauthorized", "no claims")
		return
	}
	id, err := pathID(r)
	if err != nil {
		response.Fail(w, http.StatusBadRequest, "bad_request", "invalid id")
		return
	}
	s, err := h.q.GetSubscription(r.Context(), gen.GetSubscriptionParams{OrganizationID: c.OrgID, ID: id})
	if err != nil {
		response.Fail(w, http.StatusNotFound, "not_found", "subscription not found")
		return
	}
	response.JSON(w, http.StatusOK, h.loadDetail(r, s))
}

func (h *Handler) adminSetStatus(w http.ResponseWriter, r *http.Request) {
	c, ok := mw.ClaimsFrom(r.Context())
	if !ok {
		response.Fail(w, http.StatusUnauthorized, "unauthorized", "no claims")
		return
	}
	id, err := pathID(r)
	if err != nil {
		response.Fail(w, http.StatusBadRequest, "bad_request", "invalid id")
		return
	}
	status, ok := decodeStatus(w, r)
	if !ok {
		return
	}
	s, err := h.q.SetSubscriptionStatus(r.Context(), gen.SetSubscriptionStatusParams{OrganizationID: c.OrgID, ID: id, Status: status})
	if err != nil {
		response.Fail(w, http.StatusNotFound, "not_found", "subscription not found")
		return
	}
	response.JSON(w, http.StatusOK, toDTO(s))
}

func (h *Handler) adminRunNow(w http.ResponseWriter, r *http.Request) {
	c, ok := mw.ClaimsFrom(r.Context())
	if !ok {
		response.Fail(w, http.StatusUnauthorized, "unauthorized", "no claims")
		return
	}
	id, err := pathID(r)
	if err != nil {
		response.Fail(w, http.StatusBadRequest, "bad_request", "invalid id")
		return
	}
	s, err := h.q.GetSubscription(r.Context(), gen.GetSubscriptionParams{OrganizationID: c.OrgID, ID: id})
	if err != nil {
		response.Fail(w, http.StatusNotFound, "not_found", "subscription not found")
		return
	}
	if s.Status != "active" {
		response.Fail(w, http.StatusConflict, "not_active", "only active subscriptions can be run")
		return
	}
	created, err := subscriptions.RunNow(r.Context(), h.pool, s, time.Now().UTC(), nil)
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "run failed")
		return
	}
	response.JSON(w, http.StatusOK, map[string]any{"order_created": created})
}

// lineInput is a subscription line in create/edit payloads.
type lineInput struct {
	ProductID int64  `json:"product_id"`
	Quantity  string `json:"quantity"`
	Unit      string `json:"unit"`
}

// editInput is the create/edit body (cadence + items, plus metadata).
type editInput struct {
	Name     *string     `json:"name"`
	Cadence  string      `json:"cadence"`
	PoNumber *string     `json:"po_number"`
	Items    []lineInput `json:"items"`
}

// replaceItems swaps a subscription's items for the given set (within a tx).
func replaceItems(r *http.Request, q *gen.Queries, subID int64, items []lineInput) error {
	if err := q.DeleteSubscriptionItems(r.Context(), subID); err != nil {
		return err
	}
	for _, it := range items {
		if it.ProductID == 0 {
			continue
		}
		qty := it.Quantity
		if qty == "" {
			qty = "1"
		}
		unit := it.Unit
		if unit == "" {
			unit = "each"
		}
		if _, err := q.CreateSubscriptionItem(r.Context(), gen.CreateSubscriptionItemParams{
			SubscriptionID: subID, ProductID: it.ProductID, Quantity: qty, Unit: unit,
		}); err != nil {
			return err
		}
	}
	return nil
}

// decodeEdit reads + validates an edit body.
func decodeEdit(w http.ResponseWriter, r *http.Request) (editInput, bool) {
	var in editInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		response.Fail(w, http.StatusBadRequest, "bad_request", "invalid body")
		return in, false
	}
	if !validCadence(in.Cadence) {
		response.Fail(w, http.StatusBadRequest, "bad_request", "cadence must be weekly, biweekly, monthly or quarterly")
		return in, false
	}
	if len(in.Items) == 0 {
		response.Fail(w, http.StatusBadRequest, "bad_request", "at least one item is required")
		return in, false
	}
	return in, true
}

func (h *Handler) adminUpdate(w http.ResponseWriter, r *http.Request) {
	c, ok := mw.ClaimsFrom(r.Context())
	if !ok {
		response.Fail(w, http.StatusUnauthorized, "unauthorized", "no claims")
		return
	}
	id, err := pathID(r)
	if err != nil {
		response.Fail(w, http.StatusBadRequest, "bad_request", "invalid id")
		return
	}
	in, ok := decodeEdit(w, r)
	if !ok {
		return
	}
	cur, err := h.q.GetSubscription(r.Context(), gen.GetSubscriptionParams{OrganizationID: c.OrgID, ID: id})
	if err != nil {
		response.Fail(w, http.StatusNotFound, "not_found", "subscription not found")
		return
	}
	if cur.Status == "cancelled" {
		response.Fail(w, http.StatusConflict, "cancelled", "a cancelled subscription cannot be edited")
		return
	}
	tx, err := h.pool.Begin(r.Context())
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not start")
		return
	}
	defer tx.Rollback(r.Context()) //nolint:errcheck
	q := gen.New(tx)
	s, err := q.UpdateSubscription(r.Context(), gen.UpdateSubscriptionParams{OrganizationID: c.OrgID, ID: id, Name: in.Name, Cadence: in.Cadence, PoNumber: in.PoNumber})
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not update subscription")
		return
	}
	if err := replaceItems(r, q, id, in.Items); err != nil {
		response.Fail(w, http.StatusBadRequest, "bad_request", "invalid item")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not save")
		return
	}
	response.JSON(w, http.StatusOK, h.loadDetail(r, s))
}

// ---- storefront ----------------------------------------------------------

type sfActor struct {
	orgID, customerID int64
	customerUserID    *int64
}

func storefrontActor(r *http.Request) (sfActor, bool) {
	c, ok := mw.ClaimsFrom(r.Context())
	if !ok || c.CustomerID == 0 {
		return sfActor{}, false
	}
	a := sfActor{orgID: c.OrgID, customerID: c.CustomerID}
	if id, err := strconv.ParseInt(c.Subject, 10, 64); err == nil && id != 0 {
		a.customerUserID = &id
	}
	return a, true
}

func (h *Handler) myList(w http.ResponseWriter, r *http.Request) {
	a, ok := storefrontActor(r)
	if !ok {
		response.Fail(w, http.StatusUnauthorized, "unauthorized", "no customer context")
		return
	}
	rows, err := h.q.ListSubscriptionsForCustomer(r.Context(), a.customerID)
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not list subscriptions")
		return
	}
	items := make([]subscriptionDTO, 0, len(rows))
	for _, s := range rows {
		items = append(items, toDTO(s))
	}
	response.JSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *Handler) myGet(w http.ResponseWriter, r *http.Request) {
	a, ok := storefrontActor(r)
	if !ok {
		response.Fail(w, http.StatusUnauthorized, "unauthorized", "no customer context")
		return
	}
	id, err := pathID(r)
	if err != nil {
		response.Fail(w, http.StatusBadRequest, "bad_request", "invalid id")
		return
	}
	s, err := h.q.GetSubscriptionForCustomer(r.Context(), gen.GetSubscriptionForCustomerParams{CustomerID: a.customerID, ID: id})
	if err != nil {
		response.Fail(w, http.StatusNotFound, "not_found", "subscription not found")
		return
	}
	response.JSON(w, http.StatusOK, h.loadDetail(r, s))
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	a, ok := storefrontActor(r)
	if !ok {
		response.Fail(w, http.StatusUnauthorized, "unauthorized", "no customer context")
		return
	}
	var req struct {
		Name      *string `json:"name"`
		Cadence   string  `json:"cadence"`
		StartDate string  `json:"start_date"` // YYYY-MM-DD; optional
		PoNumber  *string `json:"po_number"`
		Items     []struct {
			ProductID int64  `json:"product_id"`
			Quantity  string `json:"quantity"`
			Unit      string `json:"unit"`
		} `json:"items"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Fail(w, http.StatusBadRequest, "bad_request", "invalid body")
		return
	}
	if !validCadence(req.Cadence) {
		response.Fail(w, http.StatusBadRequest, "bad_request", "cadence must be weekly, biweekly, monthly or quarterly")
		return
	}
	if len(req.Items) == 0 {
		response.Fail(w, http.StatusBadRequest, "bad_request", "at least one item is required")
		return
	}

	ws, err := h.q.GetDefaultWebsite(r.Context(), a.orgID)
	if err != nil {
		response.Fail(w, http.StatusBadRequest, "bad_request", "no website configured")
		return
	}

	// First run: an explicit start date, or one cadence from today.
	today := time.Now().UTC()
	next := subscriptions.AdvanceDate(req.Cadence, today)
	if req.StartDate != "" {
		if d, perr := time.Parse("2006-01-02", req.StartDate); perr == nil {
			next = d
		} else {
			response.Fail(w, http.StatusBadRequest, "bad_request", "start_date must be YYYY-MM-DD")
			return
		}
	}

	var createdBy *string
	if a.customerUserID != nil {
		s := "customer_user:" + strconv.FormatInt(*a.customerUserID, 10)
		createdBy = &s
	}

	tx, err := h.pool.Begin(r.Context())
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not start")
		return
	}
	defer tx.Rollback(r.Context()) //nolint:errcheck
	q := gen.New(tx)

	sub, err := q.CreateSubscription(r.Context(), gen.CreateSubscriptionParams{
		OrganizationID: a.orgID, WebsiteID: ws.ID, CustomerID: a.customerID, CustomerUserID: a.customerUserID,
		Name: req.Name, Currency: ws.DefaultCurrency, Cadence: req.Cadence,
		NextRunDate: pgtype.Date{Time: next, Valid: true}, PoNumber: req.PoNumber, CreatedBy: createdBy,
	})
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not create subscription")
		return
	}
	for _, it := range req.Items {
		if it.ProductID == 0 {
			continue
		}
		qty := it.Quantity
		if qty == "" {
			qty = "1"
		}
		unit := it.Unit
		if unit == "" {
			unit = "each"
		}
		if _, err := q.CreateSubscriptionItem(r.Context(), gen.CreateSubscriptionItemParams{
			SubscriptionID: sub.ID, ProductID: it.ProductID, Quantity: qty, Unit: unit,
		}); err != nil {
			response.Fail(w, http.StatusBadRequest, "bad_request", "invalid item")
			return
		}
	}
	if err := tx.Commit(r.Context()); err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not save subscription")
		return
	}
	response.JSON(w, http.StatusCreated, h.loadDetail(r, sub))
}

func (h *Handler) mySetStatus(w http.ResponseWriter, r *http.Request) {
	a, ok := storefrontActor(r)
	if !ok {
		response.Fail(w, http.StatusUnauthorized, "unauthorized", "no customer context")
		return
	}
	id, err := pathID(r)
	if err != nil {
		response.Fail(w, http.StatusBadRequest, "bad_request", "invalid id")
		return
	}
	status, ok := decodeStatus(w, r)
	if !ok {
		return
	}
	s, err := h.q.SetSubscriptionStatusForCustomer(r.Context(), gen.SetSubscriptionStatusForCustomerParams{CustomerID: a.customerID, ID: id, Status: status})
	if err != nil {
		response.Fail(w, http.StatusNotFound, "not_found", "subscription not found")
		return
	}
	response.JSON(w, http.StatusOK, toDTO(s))
}

func (h *Handler) myUpdate(w http.ResponseWriter, r *http.Request) {
	a, ok := storefrontActor(r)
	if !ok {
		response.Fail(w, http.StatusUnauthorized, "unauthorized", "no customer context")
		return
	}
	id, err := pathID(r)
	if err != nil {
		response.Fail(w, http.StatusBadRequest, "bad_request", "invalid id")
		return
	}
	in, ok := decodeEdit(w, r)
	if !ok {
		return
	}
	cur, err := h.q.GetSubscriptionForCustomer(r.Context(), gen.GetSubscriptionForCustomerParams{CustomerID: a.customerID, ID: id})
	if err != nil {
		response.Fail(w, http.StatusNotFound, "not_found", "subscription not found")
		return
	}
	if cur.Status == "cancelled" {
		response.Fail(w, http.StatusConflict, "cancelled", "a cancelled subscription cannot be edited")
		return
	}
	tx, err := h.pool.Begin(r.Context())
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not start")
		return
	}
	defer tx.Rollback(r.Context()) //nolint:errcheck
	q := gen.New(tx)
	s, err := q.UpdateSubscriptionForCustomer(r.Context(), gen.UpdateSubscriptionForCustomerParams{CustomerID: a.customerID, ID: id, Name: in.Name, Cadence: in.Cadence, PoNumber: in.PoNumber})
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not update subscription")
		return
	}
	if err := replaceItems(r, q, id, in.Items); err != nil {
		response.Fail(w, http.StatusBadRequest, "bad_request", "invalid item")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not save")
		return
	}
	response.JSON(w, http.StatusOK, h.loadDetail(r, s))
}

// mySkip advances the next run by one cadence without creating an order.
func (h *Handler) mySkip(w http.ResponseWriter, r *http.Request) {
	a, ok := storefrontActor(r)
	if !ok {
		response.Fail(w, http.StatusUnauthorized, "unauthorized", "no customer context")
		return
	}
	id, err := pathID(r)
	if err != nil {
		response.Fail(w, http.StatusBadRequest, "bad_request", "invalid id")
		return
	}
	s, err := h.q.GetSubscriptionForCustomer(r.Context(), gen.GetSubscriptionForCustomerParams{CustomerID: a.customerID, ID: id})
	if err != nil {
		response.Fail(w, http.StatusNotFound, "not_found", "subscription not found")
		return
	}
	base := time.Now().UTC()
	if s.NextRunDate.Valid && s.NextRunDate.Time.After(base) {
		base = s.NextRunDate.Time
	}
	next := subscriptions.AdvanceDate(s.Cadence, base)
	if err := h.q.SetSubscriptionNextRun(r.Context(), gen.SetSubscriptionNextRunParams{ID: s.ID, NextRunDate: pgtype.Date{Time: next, Valid: true}}); err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not skip")
		return
	}
	s.NextRunDate = pgtype.Date{Time: next, Valid: true}
	response.JSON(w, http.StatusOK, toDTO(s))
}

// decodeStatus reads {status} and validates it against the allowed set.
func decodeStatus(w http.ResponseWriter, r *http.Request) (string, bool) {
	var req struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Fail(w, http.StatusBadRequest, "bad_request", "invalid body")
		return "", false
	}
	switch req.Status {
	case "active", "paused", "cancelled":
		return req.Status, true
	}
	response.Fail(w, http.StatusBadRequest, "bad_request", "status must be active, paused or cancelled")
	return "", false
}
