package authmod

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"b2bcommerce/internal/auth"
	"b2bcommerce/internal/server/response"
	"b2bcommerce/internal/store"
	"b2bcommerce/internal/store/gen"
)

type Handler struct {
	store  *store.Store
	issuer *auth.Issuer
}

func New(s *store.Store, issuer *auth.Issuer) *Handler {
	return &Handler{store: s, issuer: issuer}
}

// Routes mounts the login endpoints. The limiter middleware throttles
// credential submission per client IP to blunt brute-force attempts. Invite
// endpoints are public (the token is the credential) and share the limiter.
func (h *Handler) Routes(r chi.Router, limiter func(http.Handler) http.Handler) {
	r.With(limiter).Post("/admin/auth/login", h.login)
	r.With(limiter).Post("/storefront/auth/login", h.storefrontLogin)
	r.With(limiter).Post("/vendor/auth/login", h.vendorLogin)
	r.With(limiter).Get("/storefront/invites/{token}", h.getInvite)
	r.With(limiter).Post("/storefront/invites/{token}/accept", h.acceptInvite)
}

// resolveOrgFromHost maps the request host to the org of the website serving it
// (PRD §4 multi-website), falling back to the demo org when no domain matches.
func (h *Handler) resolveOrgFromHost(r *http.Request) int64 {
	host := r.Host
	if i := strings.IndexByte(host, ':'); i >= 0 {
		host = host[:i]
	}
	if ws, err := h.store.Queries().GetWebsiteByDomain(r.Context(), host); err == nil {
		return ws.OrganizationID
	}
	return 1
}

type loginRequest struct {
	OrgID    int64  `json:"org_id"`
	Email    string `json:"email"`
	Password string `json:"password"`
}

type loginResponse struct {
	Token string `json:"token"`
}

func (h *Handler) login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Fail(w, http.StatusBadRequest, "bad_request", "invalid body")
		return
	}
	if req.OrgID == 0 {
		req.OrgID = 1 // demo convenience; require explicitly in production
	}

	u, err := h.store.GetUserByEmail(r.Context(), req.OrgID, req.Email)
	if err != nil || !auth.CheckPassword(u.PasswordHash, req.Password) {
		response.Fail(w, http.StatusUnauthorized, "unauthorized", "invalid credentials")
		return
	}

	perms, err := h.store.GetUserPermissions(r.Context(), u.ID)
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not load permissions")
		return
	}

	token, err := h.issuer.Issue(strconv.FormatInt(u.ID, 10), u.OrgID, "admin", perms)
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not issue token")
		return
	}
	response.JSON(w, http.StatusOK, loginResponse{Token: token})
}

// storefrontLogin authenticates a customer-user and issues a storefront token
// carrying their org and buying company (customer_id).
func (h *Handler) storefrontLogin(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Fail(w, http.StatusBadRequest, "bad_request", "invalid body")
		return
	}
	if req.OrgID == 0 {
		req.OrgID = h.resolveOrgFromHost(r) // multi-website: org follows the serving domain
	}

	cu, err := h.store.Queries().GetCustomerUserForLogin(r.Context(), gen.GetCustomerUserForLoginParams{
		OrganizationID: req.OrgID,
		Email:          req.Email,
	})
	if err != nil || !auth.CheckPassword(cu.PasswordHash, req.Password) {
		response.Fail(w, http.StatusUnauthorized, "unauthorized", "invalid credentials")
		return
	}

	token, err := h.issuer.IssueStorefront(cu.ID, cu.OrganizationID, cu.CustomerID)
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not issue token")
		return
	}
	response.JSON(w, http.StatusOK, loginResponse{Token: token})
}

// vendorLogin authenticates a vendor-user and issues a vendor-portal token
// carrying their org and selling vendor (vendor_id).
func (h *Handler) vendorLogin(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Fail(w, http.StatusBadRequest, "bad_request", "invalid body")
		return
	}
	if req.OrgID == 0 {
		req.OrgID = h.resolveOrgFromHost(r) // multi-website: org follows the serving domain
	}

	vu, err := h.store.Queries().GetVendorUserForLogin(r.Context(), gen.GetVendorUserForLoginParams{
		OrganizationID: req.OrgID,
		Email:          req.Email,
	})
	if err != nil || !auth.CheckPassword(vu.PasswordHash, req.Password) {
		response.Fail(w, http.StatusUnauthorized, "unauthorized", "invalid credentials")
		return
	}

	token, err := h.issuer.IssueVendor(vu.ID, vu.OrganizationID, vu.VendorID)
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not issue token")
		return
	}
	response.JSON(w, http.StatusOK, loginResponse{Token: token})
}
