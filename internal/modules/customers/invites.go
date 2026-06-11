package customers

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	mw "b2bcommerce/internal/server/middleware"
	"b2bcommerce/internal/server/response"
	"b2bcommerce/internal/store/gen"
)

// Admin management of shareable buyer-onboarding links (0044). The public
// redeem flow lives in the auth module (/storefront/invites/...).

func inviteJSON(i gen.CustomerInvite) map[string]any {
	var revoked any
	if i.RevokedAt.Valid {
		revoked = i.RevokedAt.Time
	}
	return map[string]any{
		"id": i.ID, "token": i.Token.String(), "customer_id": i.CustomerID,
		"role": i.Role, "spending_limit": i.SpendingLimit, "expires_at": i.ExpiresAt,
		"use_count": i.UseCount, "revoked_at": revoked, "created_at": i.CreatedAt,
	}
}

func (h *Handler) listInvites(w http.ResponseWriter, r *http.Request) {
	org, ok := h.requireCustomer(w, r)
	if !ok {
		return
	}
	custID, _ := pathID(r)
	rows, err := h.q.ListInvitesForCustomer(r.Context(), gen.ListInvitesForCustomerParams{
		CustomerID: custID, OrganizationID: org,
	})
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not list invites")
		return
	}
	items := make([]map[string]any, 0, len(rows))
	for _, i := range rows {
		items = append(items, inviteJSON(i))
	}
	response.JSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *Handler) createInvite(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireCustomer(w, r); !ok {
		return
	}
	custID, _ := pathID(r)
	var req struct {
		Role          string  `json:"role"`
		SpendingLimit *string `json:"spending_limit"`
		ExpiresInDays int     `json:"expires_in_days"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Fail(w, http.StatusBadRequest, "bad_request", "invalid body")
		return
	}
	if req.Role == "" {
		req.Role = "buyer"
	}
	if req.Role != "buyer" && req.Role != "approver" && req.Role != "admin" {
		response.Fail(w, http.StatusBadRequest, "bad_request", "role must be buyer, approver or admin")
		return
	}
	if req.ExpiresInDays <= 0 {
		req.ExpiresInDays = 14
	}
	claims, _ := mw.ClaimsFrom(r.Context())
	var createdBy *int64
	if id, err := strconv.ParseInt(claims.Subject, 10, 64); err == nil {
		createdBy = &id
	}
	inv, err := h.q.CreateCustomerInvite(r.Context(), gen.CreateCustomerInviteParams{
		CustomerID:    custID,
		Role:          req.Role,
		SpendingLimit: req.SpendingLimit,
		ExpiresAt:     time.Now().AddDate(0, 0, req.ExpiresInDays),
		CreatedBy:     createdBy,
	})
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not create invite")
		return
	}
	response.JSON(w, http.StatusCreated, inviteJSON(inv))
}

func (h *Handler) revokeInvite(w http.ResponseWriter, r *http.Request) {
	org, ok := h.requireCustomer(w, r)
	if !ok {
		return
	}
	inviteID, err := strconv.ParseInt(chi.URLParam(r, "inviteID"), 10, 64)
	if err != nil {
		response.Fail(w, http.StatusBadRequest, "bad_request", "invalid invite id")
		return
	}
	n, err := h.q.RevokeCustomerInvite(r.Context(), gen.RevokeCustomerInviteParams{
		ID: inviteID, OrganizationID: org,
	})
	if err != nil || n == 0 {
		response.Fail(w, http.StatusNotFound, "not_found", "invite not found or already revoked")
		return
	}
	response.JSON(w, http.StatusOK, map[string]any{"revoked": true})
}
