// Package marketplace implements the multi-vendor marketplace (migration 0041):
// vendor management for the operator (admin audience) and the vendor self-service
// portal (vendor audience). All admin routes are organization-scoped from the
// JWT claims, never the request body, so tenant isolation cannot be spoofed.
package marketplace

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"b2bcommerce/internal/auth"
	mw "b2bcommerce/internal/server/middleware"
	"b2bcommerce/internal/server/response"
	"b2bcommerce/internal/store/gen"
)

type Handler struct {
	pool *pgxpool.Pool
	q    *gen.Queries
}

func New(pool *pgxpool.Pool) *Handler { return &Handler{pool: pool, q: gen.New(pool)} }

// tx runs fn in a single transaction bound to a fresh *gen.Queries.
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

// Routes mounts the operator-facing vendor-management endpoints. The vendor
// self-service portal is mounted separately (see RoutesVendor).
func (h *Handler) Routes(r chi.Router, authMW func(http.Handler) http.Handler) {
	r.Group(func(ar chi.Router) {
		ar.Use(authMW)
		ar.Use(mw.RequireAudience("admin"))

		ar.With(mw.RequirePermission("vendor.view")).Get("/admin/vendors", h.list)
		ar.With(mw.RequirePermission("vendor.manage")).Post("/admin/vendors", h.create)
		ar.With(mw.RequirePermission("vendor.view")).Get("/admin/vendors/{id}", h.get)
		ar.With(mw.RequirePermission("vendor.manage")).Put("/admin/vendors/{id}", h.update)
		ar.With(mw.RequirePermission("vendor.manage")).Delete("/admin/vendors/{id}", h.softDelete)

		ar.With(mw.RequirePermission("vendor.view")).Get("/admin/vendors/{id}/users", h.listUsers)
		ar.With(mw.RequirePermission("vendor.manage")).Post("/admin/vendors/{id}/users", h.createUser)

		ar.With(mw.RequirePermission("vendor.view")).Get("/admin/vendors/{id}/payouts", h.listPayouts)
		ar.With(mw.RequirePermission("vendor.manage")).Post("/admin/vendors/{id}/payouts", h.generatePayout)
		ar.With(mw.RequirePermission("vendor.manage")).Post("/admin/payouts/{id}/pay", h.markPayoutPaid)
	})
}

func orgID(r *http.Request) (int64, bool) {
	claims, ok := mw.ClaimsFrom(r.Context())
	if !ok {
		return 0, false
	}
	return claims.OrgID, true
}

func pathID(r *http.Request) (int64, error) {
	return strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
}

// ---- DTOs ----------------------------------------------------------------

type vendorDTO struct {
	ID              int64   `json:"id"`
	PublicID        string  `json:"public_id"`
	Name            string  `json:"name"`
	Slug            string  `json:"slug"`
	ContactEmail    *string `json:"contact_email"`
	Status          string  `json:"status"`
	CommissionRate  string  `json:"commission_rate"`
	PayoutTermsDays int32   `json:"payout_terms_days"`
}

func toVendorDTO(v gen.Vendor) vendorDTO {
	return vendorDTO{
		ID:              v.ID,
		PublicID:        v.PublicID.String(),
		Name:            v.Name,
		Slug:            v.Slug,
		ContactEmail:    v.ContactEmail,
		Status:          v.Status,
		CommissionRate:  v.CommissionRate,
		PayoutTermsDays: v.PayoutTermsDays,
	}
}

type vendorRequest struct {
	Name            string  `json:"name"`
	Slug            string  `json:"slug"`
	ContactEmail    *string `json:"contact_email"`
	Status          string  `json:"status"`
	CommissionRate  string  `json:"commission_rate"`
	PayoutTermsDays int32   `json:"payout_terms_days"`
}

var slugRe = regexp.MustCompile(`[^a-z0-9]+`)

func slugify(s string) string {
	return strings.Trim(slugRe.ReplaceAllString(strings.ToLower(s), "-"), "-")
}

// ---- Vendors -------------------------------------------------------------

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	org, ok := orgID(r)
	if !ok {
		response.Fail(w, http.StatusUnauthorized, "unauthorized", "no claims")
		return
	}
	rows, err := h.q.ListVendors(r.Context(), org)
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not list vendors")
		return
	}
	items := make([]vendorDTO, 0, len(rows))
	for _, v := range rows {
		items = append(items, toVendorDTO(v))
	}
	response.JSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	org, ok := orgID(r)
	if !ok {
		response.Fail(w, http.StatusUnauthorized, "unauthorized", "no claims")
		return
	}
	var req vendorRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Fail(w, http.StatusBadRequest, "bad_request", "invalid body")
		return
	}
	if req.Name == "" {
		response.Fail(w, http.StatusBadRequest, "bad_request", "name is required")
		return
	}
	if req.Slug == "" {
		req.Slug = slugify(req.Name)
	}
	if req.Status == "" {
		req.Status = "active"
	}
	if !validStatus(req.Status) {
		response.Fail(w, http.StatusBadRequest, "bad_request", "status must be pending, active or suspended")
		return
	}
	if req.CommissionRate == "" {
		req.CommissionRate = "0"
	}
	if req.PayoutTermsDays == 0 {
		req.PayoutTermsDays = 30
	}
	v, err := h.q.CreateVendor(r.Context(), gen.CreateVendorParams{
		OrganizationID:  org,
		Name:            req.Name,
		Slug:            req.Slug,
		ContactEmail:    req.ContactEmail,
		Status:          req.Status,
		CommissionRate:  req.CommissionRate,
		PayoutTermsDays: req.PayoutTermsDays,
	})
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not create vendor")
		return
	}
	response.JSON(w, http.StatusCreated, toVendorDTO(v))
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
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
	v, err := h.q.GetVendor(r.Context(), gen.GetVendorParams{ID: id, OrganizationID: org})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			response.Fail(w, http.StatusNotFound, "not_found", "vendor not found")
			return
		}
		response.Fail(w, http.StatusInternalServerError, "internal", "could not load vendor")
		return
	}
	response.JSON(w, http.StatusOK, toVendorDTO(v))
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
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
	var req vendorRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Fail(w, http.StatusBadRequest, "bad_request", "invalid body")
		return
	}
	if req.Name == "" {
		response.Fail(w, http.StatusBadRequest, "bad_request", "name is required")
		return
	}
	if req.Status == "" {
		req.Status = "active"
	}
	if !validStatus(req.Status) {
		response.Fail(w, http.StatusBadRequest, "bad_request", "status must be pending, active or suspended")
		return
	}
	if req.CommissionRate == "" {
		req.CommissionRate = "0"
	}
	if req.PayoutTermsDays == 0 {
		req.PayoutTermsDays = 30
	}
	v, err := h.q.UpdateVendor(r.Context(), gen.UpdateVendorParams{
		ID:              id,
		Name:            req.Name,
		ContactEmail:    req.ContactEmail,
		Status:          req.Status,
		CommissionRate:  req.CommissionRate,
		PayoutTermsDays: req.PayoutTermsDays,
		OrganizationID:  org,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			response.Fail(w, http.StatusNotFound, "not_found", "vendor not found")
			return
		}
		response.Fail(w, http.StatusInternalServerError, "internal", "could not update vendor")
		return
	}
	response.JSON(w, http.StatusOK, toVendorDTO(v))
}

func (h *Handler) softDelete(w http.ResponseWriter, r *http.Request) {
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
	if err := h.q.SoftDeleteVendor(r.Context(), gen.SoftDeleteVendorParams{ID: id, OrganizationID: org}); err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not delete vendor")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---- Vendor users --------------------------------------------------------

func (h *Handler) listUsers(w http.ResponseWriter, r *http.Request) {
	org, ok := h.requireVendor(w, r)
	if !ok {
		return
	}
	_ = org
	id, _ := pathID(r)
	rows, err := h.q.ListVendorUsers(r.Context(), id)
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not list vendor users")
		return
	}
	if rows == nil {
		rows = []gen.ListVendorUsersRow{}
	}
	response.JSON(w, http.StatusOK, map[string]any{"items": rows})
}

func (h *Handler) createUser(w http.ResponseWriter, r *http.Request) {
	_, ok := h.requireVendor(w, r)
	if !ok {
		return
	}
	id, _ := pathID(r)
	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
		FullName string `json:"full_name"`
		Role     string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Fail(w, http.StatusBadRequest, "bad_request", "invalid body")
		return
	}
	if req.Email == "" || req.Password == "" || req.FullName == "" {
		response.Fail(w, http.StatusBadRequest, "bad_request", "email, password, full_name required")
		return
	}
	if req.Role == "" {
		req.Role = "member"
	}
	if req.Role != "member" && req.Role != "admin" {
		response.Fail(w, http.StatusBadRequest, "bad_request", "role must be member or admin")
		return
	}
	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not hash password")
		return
	}
	u, err := h.q.CreateVendorUser(r.Context(), gen.CreateVendorUserParams{
		VendorID: id, Email: req.Email, PasswordHash: hash, FullName: req.FullName, Role: req.Role,
	})
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not create vendor user")
		return
	}
	response.JSON(w, http.StatusCreated, u)
}

// requireVendor verifies the path vendor exists within the caller's org so
// sub-resource routes can't read across tenants. Returns the org id.
func (h *Handler) requireVendor(w http.ResponseWriter, r *http.Request) (int64, bool) {
	org, ok := orgID(r)
	if !ok {
		response.Fail(w, http.StatusUnauthorized, "unauthorized", "no claims")
		return 0, false
	}
	id, err := pathID(r)
	if err != nil {
		response.Fail(w, http.StatusBadRequest, "bad_request", "invalid id")
		return 0, false
	}
	if _, err := h.q.GetVendor(r.Context(), gen.GetVendorParams{ID: id, OrganizationID: org}); err != nil {
		response.Fail(w, http.StatusNotFound, "not_found", "vendor not found")
		return 0, false
	}
	return org, true
}

func validStatus(s string) bool {
	return s == "pending" || s == "active" || s == "suspended"
}
