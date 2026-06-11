package authmod

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"b2bcommerce/internal/auth"
	"b2bcommerce/internal/server/response"
	"b2bcommerce/internal/store/gen"
)

// Public buyer-onboarding endpoints for shareable invite links (0044): a buyer
// opens /join/<token> on the storefront, which validates the token here and
// self-registers a customer_user under the invite's company/role. No auth —
// the token IS the credential — so both routes ride the login rate limiter.

type inviteInfo struct {
	CompanyName string    `json:"company_name"`
	Role        string    `json:"role"`
	ExpiresAt   time.Time `json:"expires_at"`
}

// liveInvite loads the invite and enforces validity, writing the error response
// itself when the link is unusable.
func (h *Handler) liveInvite(w http.ResponseWriter, r *http.Request) (gen.GetInviteByTokenRow, bool) {
	tok, err := uuid.Parse(chi.URLParam(r, "token"))
	if err != nil {
		response.Fail(w, http.StatusNotFound, "not_found", "invite not found")
		return gen.GetInviteByTokenRow{}, false
	}
	inv, err := h.store.Queries().GetInviteByToken(r.Context(), tok)
	if err != nil {
		response.Fail(w, http.StatusNotFound, "not_found", "invite not found")
		return gen.GetInviteByTokenRow{}, false
	}
	switch {
	case inv.RevokedAt.Valid:
		response.Fail(w, http.StatusGone, "revoked", "this invite link has been revoked")
	case time.Now().After(inv.ExpiresAt):
		response.Fail(w, http.StatusGone, "expired", "this invite link has expired")
	case !inv.CustomerIsActive:
		response.Fail(w, http.StatusGone, "inactive", "this company account is not active")
	default:
		return inv, true
	}
	return gen.GetInviteByTokenRow{}, false
}

// getInvite lets the storefront join page show who the buyer is joining before
// they fill in the form.
func (h *Handler) getInvite(w http.ResponseWriter, r *http.Request) {
	inv, ok := h.liveInvite(w, r)
	if !ok {
		return
	}
	response.JSON(w, http.StatusOK, inviteInfo{CompanyName: inv.CustomerName, Role: inv.Role, ExpiresAt: inv.ExpiresAt})
}

type inviteAcceptRequest struct {
	Email    string `json:"email"`
	FullName string `json:"full_name"`
	Password string `json:"password"`
}

// acceptInvite registers the buyer and signs them straight in (returns a
// storefront token, same shape as login).
func (h *Handler) acceptInvite(w http.ResponseWriter, r *http.Request) {
	inv, ok := h.liveInvite(w, r)
	if !ok {
		return
	}
	var req inviteAcceptRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Fail(w, http.StatusBadRequest, "bad_request", "invalid body")
		return
	}
	if req.Email == "" || req.FullName == "" || len(req.Password) < 8 {
		response.Fail(w, http.StatusBadRequest, "bad_request", "email, full_name and a password of at least 8 characters are required")
		return
	}
	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not hash password")
		return
	}
	cu, err := h.store.Queries().CreateCustomerUser(r.Context(), gen.CreateCustomerUserParams{
		CustomerID:    inv.CustomerID,
		Email:         req.Email,
		PasswordHash:  hash,
		FullName:      req.FullName,
		Role:          inv.Role,
		SpendingLimit: inv.SpendingLimit,
	})
	if err != nil {
		// UNIQUE (customer_id, email) — almost certainly already registered.
		response.Fail(w, http.StatusConflict, "conflict", "that email is already registered for this company — try signing in")
		return
	}
	_ = h.store.Queries().IncrementInviteUse(r.Context(), inv.ID)

	token, err := h.issuer.IssueStorefront(cu.ID, inv.OrganizationID, inv.CustomerID)
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "account created — please sign in")
		return
	}
	response.JSON(w, http.StatusCreated, loginResponse{Token: token})
}
