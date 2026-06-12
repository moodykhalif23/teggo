// Package fxadmin is the admin CRUD surface for FX rates (Roadmap Tier 2 #5).
// Rates are org-scoped and time-series; posting a pair records a new current rate.
// Gated by fx.view / fx.manage.
package fxadmin

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"b2bcommerce/internal/money"
	mw "b2bcommerce/internal/server/middleware"
	"b2bcommerce/internal/server/response"
	"b2bcommerce/internal/store/gen"
)

type Handler struct {
	q *gen.Queries
}

func New(pool *pgxpool.Pool) *Handler { return &Handler{q: gen.New(pool)} }

func (h *Handler) Routes(r chi.Router, authMW func(http.Handler) http.Handler) {
	r.Group(func(ar chi.Router) {
		ar.Use(authMW)
		ar.Use(mw.RequireAudience("admin"))
		ar.With(mw.RequirePermission("fx.view")).Get("/admin/fx-rates", h.list)
		ar.With(mw.RequirePermission("fx.manage")).Post("/admin/fx-rates", h.create)
		ar.With(mw.RequirePermission("fx.manage")).Delete("/admin/fx-rates/{id}", h.delete)
	})
}

func orgID(r *http.Request) (int64, bool) {
	c, ok := mw.ClaimsFrom(r.Context())
	if !ok {
		return 0, false
	}
	return c.OrgID, true
}

type fxRateDTO struct {
	ID            int64     `json:"id"`
	BaseCurrency  string    `json:"base_currency"`
	QuoteCurrency string    `json:"quote_currency"`
	Rate          string    `json:"rate"`
	AsOf          time.Time `json:"as_of"`
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	org, ok := orgID(r)
	if !ok {
		response.Fail(w, http.StatusUnauthorized, "unauthorized", "no claims")
		return
	}
	rows, err := h.q.ListLatestFxRates(r.Context(), org)
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not list rates")
		return
	}
	items := make([]fxRateDTO, 0, len(rows))
	for _, x := range rows {
		items = append(items, fxRateDTO{ID: x.ID, BaseCurrency: x.BaseCurrency, QuoteCurrency: x.QuoteCurrency, Rate: x.Rate, AsOf: x.AsOf})
	}
	response.JSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	org, ok := orgID(r)
	if !ok {
		response.Fail(w, http.StatusUnauthorized, "unauthorized", "no claims")
		return
	}
	var req struct {
		BaseCurrency  string `json:"base_currency"`
		QuoteCurrency string `json:"quote_currency"`
		Rate          string `json:"rate"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Fail(w, http.StatusBadRequest, "bad_request", "invalid body")
		return
	}
	base := strings.ToUpper(strings.TrimSpace(req.BaseCurrency))
	quote := strings.ToUpper(strings.TrimSpace(req.QuoteCurrency))
	if len(base) != 3 || len(quote) != 3 {
		response.Fail(w, http.StatusBadRequest, "bad_request", "base and quote must be 3-letter currency codes")
		return
	}
	if base == quote {
		response.Fail(w, http.StatusBadRequest, "bad_request", "base and quote must differ")
		return
	}
	v, err := money.Parse(req.Rate)
	if err != nil || v.Sign() <= 0 {
		response.Fail(w, http.StatusBadRequest, "bad_request", "rate must be a positive number")
		return
	}
	row, err := h.q.CreateFxRate(r.Context(), gen.CreateFxRateParams{OrganizationID: org, BaseCurrency: base, QuoteCurrency: quote, Rate: req.Rate})
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not save rate")
		return
	}
	response.JSON(w, http.StatusCreated, fxRateDTO{ID: row.ID, BaseCurrency: row.BaseCurrency, QuoteCurrency: row.QuoteCurrency, Rate: row.Rate, AsOf: row.AsOf})
}

func (h *Handler) delete(w http.ResponseWriter, r *http.Request) {
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
	n, err := h.q.DeleteFxRate(r.Context(), gen.DeleteFxRateParams{OrganizationID: org, ID: id})
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not delete rate")
		return
	}
	if n == 0 {
		response.Fail(w, http.StatusNotFound, "not_found", "rate not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
