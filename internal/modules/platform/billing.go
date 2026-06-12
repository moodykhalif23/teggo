package platform

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"b2bcommerce/internal/billing"
	mw "b2bcommerce/internal/server/middleware"
	"b2bcommerce/internal/server/response"
	"b2bcommerce/internal/store/gen"
	"b2bcommerce/internal/tenantctx"
)

// WithBilling wires the plan/metering service; returns the handler for chaining
// at the mount site. Without it the billing routes are not mounted.
func (h *Handler) WithBilling(svc *billing.Service) *Handler {
	h.billing = svc
	return h
}

// billingRoutes mounts the tenant-facing billing view and the operator plan
// management endpoints. Called from Routes when a billing service is wired.
func (h *Handler) billingRoutes(r chi.Router, authMW func(http.Handler) http.Handler) {
	r.Group(func(ar chi.Router) {
		ar.Use(authMW)
		ar.Use(mw.RequireAudience("admin"))
		// Any signed-in admin may see their own org's plan + usage.
		ar.Get("/admin/billing", h.getBilling)

		ar.With(mw.RequirePermission("platform.view")).Get("/admin/platform/plans", h.listPlans)
		ar.With(mw.RequirePermission("platform.manage")).Put("/admin/platform/plans/{code}", h.updatePlan)
		ar.With(mw.RequirePermission("platform.manage")).Post("/admin/platform/organizations/{id}/plan", h.setOrgPlan)
	})
}

// ---- Tenant: my plan + usage ------------------------------------------------

func (h *Handler) getBilling(w http.ResponseWriter, r *http.Request) {
	c, ok := mw.ClaimsFrom(r.Context())
	if !ok {
		response.Fail(w, http.StatusUnauthorized, "unauthorized", "no claims")
		return
	}
	out := map[string]any{
		"plan": nil, "status": "", "features": []string{}, "limits": map[string]int64{},
		"usage": map[string]int64{},
	}
	if row, err := h.q.GetOrgEntitlements(r.Context(), c.OrgID); err == nil {
		var feats []string
		_ = json.Unmarshal(row.Features, &feats)
		if feats == nil {
			feats = []string{}
		}
		limits := map[string]int64{}
		_ = json.Unmarshal(row.Limits, &limits)
		out["plan"] = map[string]any{"code": row.Code, "name": row.Name, "price": row.Price, "currency": row.Currency}
		out["status"] = row.Status
		out["features"] = feats
		out["limits"] = limits
	}
	now := time.Now()
	usage := map[string]int64{}
	rows, err := h.q.ListUsageForPeriods(r.Context(), gen.ListUsageForPeriodsParams{
		OrganizationID: c.OrgID,
		PeriodKeys:     []string{billing.PeriodKeyFor(billing.MetricOrders, now), "all"},
	})
	if err == nil {
		for _, u := range rows {
			usage[u.Metric] = u.Value
		}
	}
	out["usage"] = usage
	response.JSON(w, http.StatusOK, out)
}

// ---- Operator: plan management ----------------------------------------------

type planDTO struct {
	Code     string          `json:"code"`
	Name     string          `json:"name"`
	Price    string          `json:"price"`
	Currency string          `json:"currency"`
	Features json.RawMessage `json:"features"`
	Limits   json.RawMessage `json:"limits"`
	Position int32           `json:"position"`
}

func (h *Handler) listPlans(w http.ResponseWriter, r *http.Request) {
	rows, err := h.q.ListPlans(r.Context())
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not list plans")
		return
	}
	items := make([]planDTO, 0, len(rows))
	for _, p := range rows {
		items = append(items, planDTO{
			Code: p.Code, Name: p.Name, Price: p.Price, Currency: p.Currency,
			Features: json.RawMessage(p.Features), Limits: json.RawMessage(p.Limits), Position: p.Position,
		})
	}
	response.JSON(w, http.StatusOK, map[string]any{"items": items})
}

// updatePlan overlays the provided fields onto the plan (absent = unchanged)
// and flushes every cached entitlement — a plan edit affects all orgs on it.
func (h *Handler) updatePlan(w http.ResponseWriter, r *http.Request) {
	code := chi.URLParam(r, "code")
	cur, err := h.q.GetPlanByCode(r.Context(), code)
	if err != nil {
		response.Fail(w, http.StatusNotFound, "not_found", "plan not found")
		return
	}
	var req struct {
		Name     *string         `json:"name"`
		Price    *string         `json:"price"`
		Currency *string         `json:"currency"`
		Features json.RawMessage `json:"features"`
		Limits   json.RawMessage `json:"limits"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Fail(w, http.StatusBadRequest, "bad_request", "invalid body")
		return
	}
	upd := gen.UpdatePlanParams{
		Code: code, Name: cur.Name, Price: cur.Price, Currency: cur.Currency,
		Features: cur.Features, Limits: cur.Limits,
	}
	if req.Name != nil {
		upd.Name = *req.Name
	}
	if req.Price != nil {
		upd.Price = *req.Price
	}
	if req.Currency != nil {
		upd.Currency = *req.Currency
	}
	if len(req.Features) > 0 {
		if !json.Valid(req.Features) {
			response.Fail(w, http.StatusBadRequest, "bad_request", "features must be a JSON array")
			return
		}
		upd.Features = req.Features
	}
	if len(req.Limits) > 0 {
		if !json.Valid(req.Limits) {
			response.Fail(w, http.StatusBadRequest, "bad_request", "limits must be a JSON object")
			return
		}
		upd.Limits = req.Limits
	}
	p, err := h.q.UpdatePlan(r.Context(), upd)
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not update plan")
		return
	}
	if h.billing != nil {
		h.billing.InvalidateAll()
	}
	response.JSON(w, http.StatusOK, planDTO{
		Code: p.Code, Name: p.Name, Price: p.Price, Currency: p.Currency,
		Features: json.RawMessage(p.Features), Limits: json.RawMessage(p.Limits), Position: p.Position,
	})
}

func (h *Handler) setOrgPlan(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		response.Fail(w, http.StatusBadRequest, "bad_request", "invalid id")
		return
	}
	var req struct {
		PlanCode string `json:"plan_code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.PlanCode == "" {
		response.Fail(w, http.StatusBadRequest, "bad_request", "plan_code is required")
		return
	}
	plan, err := h.q.GetPlanByCode(r.Context(), req.PlanCode)
	if err != nil {
		response.Fail(w, http.StatusNotFound, "not_found", "plan not found")
		return
	}
	if _, err := h.q.GetOrganization(r.Context(), id); err != nil {
		response.Fail(w, http.StatusNotFound, "not_found", "organization not found")
		return
	}
	// Writing another org's plan row is the operator's job — the RLS net must
	// stand down for this one platform.manage-gated insert.
	if _, err := h.q.SetOrgPlan(tenantctx.Bypass(r.Context()), gen.SetOrgPlanParams{OrganizationID: id, PlanID: plan.ID}); err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not assign plan")
		return
	}
	if h.billing != nil {
		h.billing.Invalidate(id)
	}
	response.JSON(w, http.StatusOK, map[string]any{"organization_id": id, "plan_code": plan.Code})
}
