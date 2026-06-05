// Package crm implements seller-side relationship management (Pack 2 §1): leads,
// contacts, a configurable pipeline of opportunity stages, and a unified
// activity timeline. A CRM account IS a commerce customer (no duplication);
// leads/opportunities/activities hang off it. Admin-only (sales reps/managers).
package crm

import (
	"context"
	"net/http"
	"strconv"
	"time"

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
	// Public storefront enquiry form (unauthenticated, rate-limited to blunt spam).
	r.Group(func(pr chi.Router) {
		pr.Use(mw.RateLimit(20, time.Minute))
		pr.Post("/storefront/leads", h.submitLead)
	})

	r.Group(func(ar chi.Router) {
		ar.Use(authMW)
		ar.Use(mw.RequireAudience("admin"))

		// Leads
		ar.With(mw.RequirePermission("crm.view")).Get("/admin/leads", h.listLeads)
		ar.With(mw.RequirePermission("crm.manage")).Post("/admin/leads", h.createLead)
		ar.With(mw.RequirePermission("crm.view")).Get("/admin/leads/{id}", h.getLead)
		ar.With(mw.RequirePermission("crm.manage")).Post("/admin/leads/{id}/convert", h.convertLead)

		// Pipelines & opportunities
		ar.With(mw.RequirePermission("crm.view")).Get("/admin/pipelines", h.listPipelines)
		ar.With(mw.RequirePermission("crm.view")).Get("/admin/pipelines/{id}/board", h.pipelineBoard)
		ar.With(mw.RequirePermission("crm.view")).Get("/admin/opportunities", h.listOpportunities)
		ar.With(mw.RequirePermission("crm.manage")).Post("/admin/opportunities", h.createOpportunity)
		ar.With(mw.RequirePermission("crm.view")).Get("/admin/opportunities/{id}", h.getOpportunity)
		ar.With(mw.RequirePermission("crm.manage")).Patch("/admin/opportunities/{id}/stage", h.patchOpportunityStage)

		// Contacts & activities (hung off a customer)
		ar.With(mw.RequirePermission("crm.view")).Get("/admin/customers/{id}/contacts", h.listContacts)
		ar.With(mw.RequirePermission("crm.manage")).Post("/admin/customers/{id}/contacts", h.createContact)
		ar.With(mw.RequirePermission("crm.manage")).Post("/admin/activities", h.createActivity)
		ar.With(mw.RequirePermission("crm.view")).Get("/admin/customers/{id}/timeline", h.customerTimeline)
		ar.With(mw.RequirePermission("crm.view")).Get("/admin/accounts/health", h.accountHealth)
	})
}

// ---- actor + helpers ------------------------------------------------------

type adminCtx struct {
	orgID  int64
	userID *int64
}

func admin(r *http.Request) (adminCtx, bool) {
	c, ok := mw.ClaimsFrom(r.Context())
	if !ok {
		return adminCtx{}, false
	}
	a := adminCtx{orgID: c.OrgID}
	if id, err := strconv.ParseInt(c.Subject, 10, 64); err == nil && id != 0 {
		a.userID = &id
	}
	return a, true
}

func pathID(r *http.Request) (int64, error) {
	return strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
}

// tx runs fn inside a transaction with a tx-bound Queries; commit on success.
func (h *Handler) tx(ctx context.Context, fn func(*gen.Queries) error) error {
	t, err := h.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer t.Rollback(ctx) //nolint:errcheck // no-op after commit
	if err := fn(gen.New(t)); err != nil {
		return err
	}
	return t.Commit(ctx)
}

func unauthorized(w http.ResponseWriter) {
	response.Fail(w, http.StatusUnauthorized, "unauthorized", "no valid context")
}

func notFound(w http.ResponseWriter, what string) {
	response.Fail(w, http.StatusNotFound, "not_found", what+" not found")
}

// defaultCurrency resolves the org's default website currency, or "USD".
func (h *Handler) defaultCurrency(ctx context.Context, orgID int64) string {
	if ws, err := h.q.GetDefaultWebsite(ctx, orgID); err == nil && ws.DefaultCurrency != "" {
		return ws.DefaultCurrency
	}
	return "USD"
}

// ---- pipelines ------------------------------------------------------------

func (h *Handler) listPipelines(w http.ResponseWriter, r *http.Request) {
	a, ok := admin(r)
	if !ok {
		unauthorized(w)
		return
	}
	// The default pipeline plus its stages is what the board UI needs.
	pl, err := h.q.GetDefaultPipeline(r.Context(), a.orgID)
	if err != nil {
		response.JSON(w, http.StatusOK, map[string]any{"items": []any{}})
		return
	}
	stages, err := h.q.ListPipelineStages(r.Context(), pl.ID)
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not load stages")
		return
	}
	if stages == nil {
		stages = []gen.PipelineStage{}
	}
	response.JSON(w, http.StatusOK, map[string]any{
		"items": []map[string]any{{"id": pl.ID, "name": pl.Name, "is_default": pl.IsDefault, "stages": stages}},
	})
}

func (h *Handler) pipelineBoard(w http.ResponseWriter, r *http.Request) {
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
	// Confirm the pipeline belongs to the caller's org before reporting on it.
	if _, err := h.q.GetPipeline(r.Context(), gen.GetPipelineParams{OrganizationID: a.orgID, ID: id}); err != nil {
		notFound(w, "pipeline")
		return
	}
	rows, err := h.q.PipelineBoard(r.Context(), id)
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not load board")
		return
	}
	if rows == nil {
		rows = []gen.PipelineBoardRow{}
	}
	response.JSON(w, http.StatusOK, map[string]any{"items": rows})
}
