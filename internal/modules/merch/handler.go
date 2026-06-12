// Package merch is the admin CRUD surface for search merchandising — synonyms,
// redirects, and pin/boost/bury rules. The curation engine lives in
// internal/merchandising; the storefront applies it in catalog search. Gated by
// merchandising.view / merchandising.manage.
package merch

import (
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
	q *gen.Queries
}

func New(pool *pgxpool.Pool) *Handler { return &Handler{q: gen.New(pool)} }

func (h *Handler) Routes(r chi.Router, authMW func(http.Handler) http.Handler) {
	r.Group(func(ar chi.Router) {
		ar.Use(authMW)
		ar.Use(mw.RequireAudience("admin"))
		v := mw.RequirePermission("merchandising.view")
		m := mw.RequirePermission("merchandising.manage")

		ar.With(v).Get("/admin/search-synonyms", h.listSynonyms)
		ar.With(m).Post("/admin/search-synonyms", h.upsertSynonym)
		ar.With(m).Delete("/admin/search-synonyms/{id}", h.deleteSynonym)

		ar.With(v).Get("/admin/search-redirects", h.listRedirects)
		ar.With(m).Post("/admin/search-redirects", h.upsertRedirect)
		ar.With(m).Delete("/admin/search-redirects/{id}", h.deleteRedirect)

		ar.With(v).Get("/admin/merchandising-rules", h.listRules)
		ar.With(m).Post("/admin/merchandising-rules", h.createRule)
		ar.With(m).Delete("/admin/merchandising-rules/{id}", h.deleteRule)
	})
}

func orgID(r *http.Request) (int64, bool) {
	c, ok := mw.ClaimsFrom(r.Context())
	if !ok {
		return 0, false
	}
	return c.OrgID, true
}
func pathID(r *http.Request) (int64, error) { return strconv.ParseInt(chi.URLParam(r, "id"), 10, 64) }

// ---- synonyms ----

func (h *Handler) listSynonyms(w http.ResponseWriter, r *http.Request) {
	org, ok := orgID(r)
	if !ok {
		response.Fail(w, http.StatusUnauthorized, "unauthorized", "no claims")
		return
	}
	rows, err := h.q.ListSearchSynonyms(r.Context(), org)
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not list synonyms")
		return
	}
	type dto struct {
		ID       int64  `json:"id"`
		Term     string `json:"term"`
		Synonyms string `json:"synonyms"`
	}
	items := make([]dto, 0, len(rows))
	for _, s := range rows {
		items = append(items, dto{ID: s.ID, Term: s.Term, Synonyms: s.Synonyms})
	}
	response.JSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *Handler) upsertSynonym(w http.ResponseWriter, r *http.Request) {
	org, ok := orgID(r)
	if !ok {
		response.Fail(w, http.StatusUnauthorized, "unauthorized", "no claims")
		return
	}
	var req struct {
		Term     string `json:"term"`
		Synonyms string `json:"synonyms"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Term) == "" || strings.TrimSpace(req.Synonyms) == "" {
		response.Fail(w, http.StatusBadRequest, "bad_request", "term and synonyms are required")
		return
	}
	row, err := h.q.UpsertSearchSynonym(r.Context(), gen.UpsertSearchSynonymParams{OrganizationID: org, Term: strings.TrimSpace(req.Term), Synonyms: strings.TrimSpace(req.Synonyms)})
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not save synonym")
		return
	}
	response.JSON(w, http.StatusCreated, map[string]any{"id": row.ID, "term": row.Term, "synonyms": row.Synonyms})
}

func (h *Handler) deleteSynonym(w http.ResponseWriter, r *http.Request) {
	h.deleteScoped(w, r, func(org, id int64) (int64, error) {
		return h.q.DeleteSearchSynonym(r.Context(), gen.DeleteSearchSynonymParams{OrganizationID: org, ID: id})
	})
}

// ---- redirects ----

func (h *Handler) listRedirects(w http.ResponseWriter, r *http.Request) {
	org, ok := orgID(r)
	if !ok {
		response.Fail(w, http.StatusUnauthorized, "unauthorized", "no claims")
		return
	}
	rows, err := h.q.ListSearchRedirects(r.Context(), org)
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not list redirects")
		return
	}
	type dto struct {
		ID     int64  `json:"id"`
		Query  string `json:"query"`
		Target string `json:"target"`
	}
	items := make([]dto, 0, len(rows))
	for _, s := range rows {
		items = append(items, dto{ID: s.ID, Query: s.Query, Target: s.Target})
	}
	response.JSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *Handler) upsertRedirect(w http.ResponseWriter, r *http.Request) {
	org, ok := orgID(r)
	if !ok {
		response.Fail(w, http.StatusUnauthorized, "unauthorized", "no claims")
		return
	}
	var req struct {
		Query  string `json:"query"`
		Target string `json:"target"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Query) == "" || strings.TrimSpace(req.Target) == "" {
		response.Fail(w, http.StatusBadRequest, "bad_request", "query and target are required")
		return
	}
	row, err := h.q.UpsertSearchRedirect(r.Context(), gen.UpsertSearchRedirectParams{OrganizationID: org, Query: strings.TrimSpace(req.Query), Target: strings.TrimSpace(req.Target)})
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not save redirect")
		return
	}
	response.JSON(w, http.StatusCreated, map[string]any{"id": row.ID, "query": row.Query, "target": row.Target})
}

func (h *Handler) deleteRedirect(w http.ResponseWriter, r *http.Request) {
	h.deleteScoped(w, r, func(org, id int64) (int64, error) {
		return h.q.DeleteSearchRedirect(r.Context(), gen.DeleteSearchRedirectParams{OrganizationID: org, ID: id})
	})
}

// ---- rules ----

func (h *Handler) listRules(w http.ResponseWriter, r *http.Request) {
	org, ok := orgID(r)
	if !ok {
		response.Fail(w, http.StatusUnauthorized, "unauthorized", "no claims")
		return
	}
	rows, err := h.q.ListMerchandisingRules(r.Context(), org)
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not list rules")
		return
	}
	type dto struct {
		ID         int64  `json:"id"`
		ScopeType  string `json:"scope_type"`
		ScopeValue string `json:"scope_value"`
		ProductID  int64  `json:"product_id"`
		SKU        string `json:"sku"`
		Name       string `json:"name"`
		Action     string `json:"action"`
		Position   int32  `json:"position"`
	}
	items := make([]dto, 0, len(rows))
	for _, s := range rows {
		items = append(items, dto{ID: s.ID, ScopeType: s.ScopeType, ScopeValue: s.ScopeValue, ProductID: s.ProductID, SKU: s.Sku, Name: s.Name, Action: s.Action, Position: s.Position})
	}
	response.JSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *Handler) createRule(w http.ResponseWriter, r *http.Request) {
	org, ok := orgID(r)
	if !ok {
		response.Fail(w, http.StatusUnauthorized, "unauthorized", "no claims")
		return
	}
	var req struct {
		ScopeType  string `json:"scope_type"`
		ScopeValue string `json:"scope_value"`
		ProductID  int64  `json:"product_id"`
		Action     string `json:"action"`
		Position   int32  `json:"position"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Fail(w, http.StatusBadRequest, "bad_request", "invalid body")
		return
	}
	req.ScopeValue = strings.TrimSpace(req.ScopeValue)
	if (req.ScopeType != "query" && req.ScopeType != "category") || req.ScopeValue == "" || req.ProductID == 0 {
		response.Fail(w, http.StatusBadRequest, "bad_request", "scope_type (query|category), scope_value and product_id are required")
		return
	}
	if req.Action != "pin" && req.Action != "boost" && req.Action != "bury" {
		response.Fail(w, http.StatusBadRequest, "bad_request", "action must be pin, boost or bury")
		return
	}
	row, err := h.q.CreateMerchandisingRule(r.Context(), gen.CreateMerchandisingRuleParams{
		OrganizationID: org, ScopeType: req.ScopeType, ScopeValue: req.ScopeValue, ProductID: req.ProductID, Action: req.Action, Position: req.Position,
	})
	if err != nil {
		response.Fail(w, http.StatusBadRequest, "bad_request", "could not create rule (unknown product?)")
		return
	}
	response.JSON(w, http.StatusCreated, map[string]any{"id": row.ID})
}

func (h *Handler) deleteRule(w http.ResponseWriter, r *http.Request) {
	h.deleteScoped(w, r, func(org, id int64) (int64, error) {
		return h.q.DeleteMerchandisingRule(r.Context(), gen.DeleteMerchandisingRuleParams{OrganizationID: org, ID: id})
	})
}

// deleteScoped is the shared 204/404 delete helper.
func (h *Handler) deleteScoped(w http.ResponseWriter, r *http.Request, del func(org, id int64) (int64, error)) {
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
	n, err := del(org, id)
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not delete")
		return
	}
	if n == 0 {
		response.Fail(w, http.StatusNotFound, "not_found", "not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
