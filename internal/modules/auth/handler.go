package authmod

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"b2bcommerce/internal/auth"
	"b2bcommerce/internal/server/response"
	"b2bcommerce/internal/store"
)

type Handler struct {
	store  *store.Store
	issuer *auth.Issuer
}

func New(s *store.Store, issuer *auth.Issuer) *Handler {
	return &Handler{store: s, issuer: issuer}
}

func (h *Handler) Routes(r chi.Router) {
	r.Post("/admin/auth/login", h.login)
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
