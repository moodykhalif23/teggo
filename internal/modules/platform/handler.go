// Package platform is the HTTP surface for tenant provisioning and platform
// operation (SAAS.md #1): public self-serve signup + email verification, and
// operator endpoints (org 1 only, platform.* permissions) to list and suspend
// tenant organizations. Provisioning itself lives in internal/tenant.
package platform

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"b2bcommerce/internal/billing"
	mw "b2bcommerce/internal/server/middleware"
	"b2bcommerce/internal/server/response"
	"b2bcommerce/internal/store/gen"
	"b2bcommerce/internal/tenant"
	"b2bcommerce/internal/tenantctx"
)

// Emailer schedules the verification email; nil disables sending (the token
// still lands in signup_verifications, so ops can hand-verify).
type Emailer interface {
	EnqueueEmail(ctx context.Context, to, template string, data map[string]any) error
}

type Handler struct {
	pool       *pgxpool.Pool
	q          *gen.Queries
	mailer     Emailer
	statuses   *tenant.StatusCache
	billing    *billing.Service
	baseDomain string
	verifyURL  string // page the email links to; the token rides as ?token=
}

func New(pool *pgxpool.Pool, mailer Emailer, statuses *tenant.StatusCache, baseDomain, verifyURL string) *Handler {
	if baseDomain == "" {
		baseDomain = "teggo.local"
	}
	if verifyURL == "" {
		verifyURL = "http://localhost:5173/verify-signup"
	}
	return &Handler{pool: pool, q: gen.New(pool), mailer: mailer, statuses: statuses, baseDomain: baseDomain, verifyURL: verifyURL}
}

// Routes mounts public signup endpoints (rate-limited — they're unauthenticated
// writes) and the operator surface.
func (h *Handler) Routes(r chi.Router, authMW, limiter func(http.Handler) http.Handler) {
	r.With(limiter).Post("/signup", h.signup)
	r.With(limiter).Post("/signup/verify", h.verify)

	r.Group(func(ar chi.Router) {
		ar.Use(authMW)
		ar.Use(mw.RequireAudience("admin"))
		ar.With(mw.RequirePermission("platform.view")).Get("/admin/platform/organizations", h.listOrgs)
		ar.With(mw.RequirePermission("platform.manage")).Post("/admin/platform/organizations/{id}/status", h.setOrgStatus)
	})

	if h.billing != nil {
		h.billingRoutes(r, authMW)
	}
}

// ---- Public signup ---------------------------------------------------------

type signupRequest struct {
	Organization string `json:"organization"`
	FullName     string `json:"full_name"`
	Email        string `json:"email"`
	Password     string `json:"password"`
	Subdomain    string `json:"subdomain"`
	Currency     string `json:"currency"`
	Locale       string `json:"locale"`
}

func (h *Handler) signup(w http.ResponseWriter, r *http.Request) {
	var req signupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Fail(w, http.StatusBadRequest, "bad_request", "invalid body")
		return
	}
	res, err := tenant.Provision(r.Context(), h.pool, tenant.Input{
		OrgName:    req.Organization,
		FullName:   req.FullName,
		Email:      req.Email,
		Password:   req.Password,
		Subdomain:  req.Subdomain,
		BaseDomain: h.baseDomain,
		Currency:   req.Currency,
		Locale:     req.Locale,
	})
	var ve tenant.ValidationError
	switch {
	case errors.As(err, &ve):
		response.Fail(w, http.StatusBadRequest, "invalid", ve.Error())
		return
	case errors.Is(err, tenant.ErrDomainTaken):
		response.Fail(w, http.StatusConflict, "domain_taken", "that subdomain is already in use")
		return
	case err != nil:
		response.Fail(w, http.StatusInternalServerError, "internal", "could not create the organization")
		return
	}

	// Best-effort: the token row is the source of truth, the email a courtesy.
	if h.mailer != nil {
		_ = h.mailer.EnqueueEmail(r.Context(), req.Email, "signup_verify", map[string]any{
			"organization": req.Organization,
			"name":         req.FullName,
			"link":         h.verifyURL + "?token=" + res.Token,
			"expires_at":   res.ExpiresAt.Format("2006-01-02 15:04 MST"),
		})
	}
	response.JSON(w, http.StatusCreated, map[string]any{
		"domain":  res.Domain,
		"message": "check your email to verify the organization",
	})
}

type verifyRequest struct {
	Token string `json:"token"`
}

func (h *Handler) verify(w http.ResponseWriter, r *http.Request) {
	var req verifyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Token == "" {
		response.Fail(w, http.StatusBadRequest, "bad_request", "token is required")
		return
	}
	tokenUUID, err := uuid.Parse(req.Token)
	if err != nil {
		response.Fail(w, http.StatusBadRequest, "invalid_token", "that verification link is not valid")
		return
	}
	ver, err := h.q.GetSignupVerification(r.Context(), tokenUUID)
	if err != nil {
		response.Fail(w, http.StatusBadRequest, "invalid_token", "that verification link is invalid or has expired")
		return
	}
	if n, err := h.q.ConsumeSignupVerification(r.Context(), ver.ID); err != nil || n == 0 {
		response.Fail(w, http.StatusBadRequest, "invalid_token", "that verification link is invalid or has expired")
		return
	}
	if _, err := h.q.SetOrganizationStatus(r.Context(), gen.SetOrganizationStatusParams{ID: ver.OrganizationID, Status: tenant.StatusTrial}); err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not activate the organization")
		return
	}
	if h.statuses != nil {
		h.statuses.Invalidate(ver.OrganizationID)
	}
	response.JSON(w, http.StatusOK, map[string]any{"message": "organization verified — you can sign in now"})
}

// ---- Platform operator -----------------------------------------------------

type orgDTO struct {
	ID           int64     `json:"id"`
	Name         string    `json:"name"`
	Status       string    `json:"status"`
	PlanCode     string    `json:"plan_code"`
	UserCount    int64     `json:"user_count"`
	WebsiteCount int64     `json:"website_count"`
	CreatedAt    time.Time `json:"created_at"`
}

func (h *Handler) listOrgs(w http.ResponseWriter, r *http.Request) {
	// Operator overview is deliberately cross-tenant (platform.view-gated):
	// stand the RLS net down so per-org user/website counts are real.
	rows, err := h.q.ListOrganizationsWithCounts(tenantctx.Bypass(r.Context()))
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not list organizations")
		return
	}
	items := make([]orgDTO, 0, len(rows))
	for _, o := range rows {
		items = append(items, orgDTO{
			ID: o.ID, Name: o.Name, Status: o.Status, PlanCode: o.PlanCode,
			UserCount: o.UserCount, WebsiteCount: o.WebsiteCount, CreatedAt: o.CreatedAt,
		})
	}
	response.JSON(w, http.StatusOK, map[string]any{"items": items})
}

type statusRequest struct {
	Status string `json:"status"`
}

func (h *Handler) setOrgStatus(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		response.Fail(w, http.StatusBadRequest, "bad_request", "invalid id")
		return
	}
	var req statusRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Fail(w, http.StatusBadRequest, "bad_request", "invalid body")
		return
	}
	switch req.Status {
	case tenant.StatusTrial, tenant.StatusActive, tenant.StatusSuspended:
	default:
		response.Fail(w, http.StatusBadRequest, "bad_request", "status must be trial, active or suspended")
		return
	}
	if id == 1 {
		// Suspending the platform owner org would lock every operator out.
		response.Fail(w, http.StatusBadRequest, "bad_request", "the platform organization cannot be modified")
		return
	}
	org, err := h.q.SetOrganizationStatus(r.Context(), gen.SetOrganizationStatusParams{ID: id, Status: req.Status})
	if err != nil {
		response.Fail(w, http.StatusNotFound, "not_found", "organization not found")
		return
	}
	if h.statuses != nil {
		h.statuses.Invalidate(id)
	}
	response.JSON(w, http.StatusOK, map[string]any{"id": org.ID, "status": org.Status})
}
