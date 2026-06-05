// Package sso (module) wires OIDC SSO (PRD §15): admin-managed identity
// providers, the /auth/sso login + callback endpoints, and JIT provisioning
// that links an IdP subject to a seller-side user (audience 'admin') or a buyer
// customer_user (audience 'storefront'), then issues our own JWT.
package sso

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	mw "b2bcommerce/internal/server/middleware"
	"b2bcommerce/internal/server/response"
	ssoeng "b2bcommerce/internal/sso"
	"b2bcommerce/internal/store/gen"
)

// TokenIssuer mints our JWTs after a successful SSO login. *auth.Issuer fits.
type TokenIssuer interface {
	Issue(subject string, orgID int64, audience string, perms []string) (string, error)
	IssueStorefront(customerUserID, orgID, customerID int64) (string, error)
}

type Handler struct {
	pool   *pgxpool.Pool
	q      *gen.Queries
	issuer TokenIssuer
	client *http.Client
}

func New(pool *pgxpool.Pool, issuer TokenIssuer) *Handler {
	return &Handler{pool: pool, q: gen.New(pool), issuer: issuer, client: &http.Client{Timeout: 15 * time.Second}}
}

func (h *Handler) Routes(r chi.Router, authMW func(http.Handler) http.Handler) {
	// Public SSO endpoints (the IdP redirects the browser here).
	r.Get("/auth/sso/{id}/login", h.login)
	r.Get("/auth/sso/{id}/callback", h.callback) // OIDC
	r.Post("/auth/sso/{id}/acs", h.acs)          // SAML assertion consumer
	r.Get("/auth/sso/{id}/metadata", h.metadata) // SAML SP metadata (XML)

	r.Group(func(ar chi.Router) {
		ar.Use(authMW)
		ar.Use(mw.RequireAudience("admin"))

		ar.With(mw.RequirePermission("sso.view")).Get("/admin/identity-providers", h.list)
		ar.With(mw.RequirePermission("sso.manage")).Post("/admin/identity-providers", h.create)
		ar.With(mw.RequirePermission("sso.view")).Get("/admin/identity-providers/{id}", h.get)
		ar.With(mw.RequirePermission("sso.manage")).Put("/admin/identity-providers/{id}", h.update)
	})
}

func orgID(r *http.Request) (int64, bool) {
	c, ok := mw.ClaimsFrom(r.Context())
	if !ok {
		return 0, false
	}
	return c.OrgID, true
}

// renderProvider omits secret config values (returns whether a client_secret is set).
func renderProvider(p gen.IdentityProvider) map[string]any {
	var cfg map[string]any
	_ = json.Unmarshal(nonEmpty(p.Config), &cfg)
	hasSecret := false
	if cfg != nil {
		if s, ok := cfg["client_secret"].(string); ok && s != "" {
			hasSecret = true
		}
		delete(cfg, "client_secret")
	}
	return map[string]any{
		"id": p.ID, "type": p.Type, "name": p.Name, "audience": p.Audience,
		"customer_id": p.CustomerID, "is_active": p.IsActive, "has_secret": hasSecret,
		"config": cfg, "created_at": p.CreatedAt.Format(time.RFC3339),
	}
}

func nonEmpty(b []byte) []byte {
	if len(b) == 0 {
		return []byte("{}")
	}
	return b
}

type providerInput struct {
	Type       string          `json:"type"`
	Name       string          `json:"name"`
	Audience   string          `json:"audience"`
	CustomerID *int64          `json:"customer_id"`
	Config     json.RawMessage `json:"config"`
	IsActive   *bool           `json:"is_active"`
}

func validAudience(a string) bool { return a == "admin" || a == "storefront" }

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	org, ok := orgID(r)
	if !ok {
		response.Fail(w, http.StatusUnauthorized, "unauthorized", "no claims")
		return
	}
	rows, err := h.q.ListIdentityProviders(r.Context(), org)
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not list providers")
		return
	}
	items := make([]map[string]any, 0, len(rows))
	for _, p := range rows {
		items = append(items, renderProvider(p))
	}
	response.JSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	org, ok := orgID(r)
	if !ok {
		response.Fail(w, http.StatusUnauthorized, "unauthorized", "no claims")
		return
	}
	var in providerInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.Name == "" || !validAudience(in.Audience) {
		response.Fail(w, http.StatusBadRequest, "bad_request", "name and audience (admin|storefront) are required")
		return
	}
	if in.Type == "" {
		in.Type = "oidc"
	}
	if in.Type != "oidc" && in.Type != "saml" {
		response.Fail(w, http.StatusBadRequest, "bad_request", "type must be oidc or saml")
		return
	}
	if in.Type == "saml" {
		var sc ssoeng.SAMLConfig
		_ = json.Unmarshal(nonEmpty(in.Config), &sc)
		if sc.IDPSSOURL == "" || sc.IDPCertificate == "" {
			response.Fail(w, http.StatusBadRequest, "bad_request", "SAML requires idp_sso_url and idp_certificate in config")
			return
		}
	}
	if in.Audience == "storefront" && in.CustomerID == nil {
		response.Fail(w, http.StatusBadRequest, "bad_request", "storefront providers require a customer_id")
		return
	}
	cfg := []byte("{}")
	if len(in.Config) > 0 {
		cfg = in.Config
	}
	active := true
	if in.IsActive != nil {
		active = *in.IsActive
	}
	p, err := h.q.CreateIdentityProvider(r.Context(), gen.CreateIdentityProviderParams{
		OrganizationID: org, Type: in.Type, Name: in.Name, Audience: in.Audience,
		CustomerID: in.CustomerID, Config: cfg, IsActive: active,
	})
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not create provider")
		return
	}
	response.JSON(w, http.StatusCreated, renderProvider(p))
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	p, ok := h.loadProvider(w, r)
	if !ok {
		return
	}
	response.JSON(w, http.StatusOK, renderProvider(p))
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	cur, ok := h.loadProvider(w, r)
	if !ok {
		return
	}
	org, _ := orgID(r)
	var in providerInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.Name == "" || !validAudience(in.Audience) {
		response.Fail(w, http.StatusBadRequest, "bad_request", "name and audience are required")
		return
	}
	if in.Type == "" {
		in.Type = cur.Type
	}
	cfg := cur.Config
	if len(in.Config) > 0 {
		cfg = in.Config
	}
	active := cur.IsActive
	if in.IsActive != nil {
		active = *in.IsActive
	}
	p, err := h.q.UpdateIdentityProvider(r.Context(), gen.UpdateIdentityProviderParams{
		OrganizationID: org, ID: cur.ID, Type: in.Type, Name: in.Name, Audience: in.Audience,
		CustomerID: in.CustomerID, Config: cfg, IsActive: active,
	})
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not update provider")
		return
	}
	response.JSON(w, http.StatusOK, renderProvider(p))
}

func (h *Handler) loadProvider(w http.ResponseWriter, r *http.Request) (gen.IdentityProvider, bool) {
	org, ok := orgID(r)
	if !ok {
		response.Fail(w, http.StatusUnauthorized, "unauthorized", "no claims")
		return gen.IdentityProvider{}, false
	}
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		response.Fail(w, http.StatusBadRequest, "bad_request", "invalid id")
		return gen.IdentityProvider{}, false
	}
	p, err := h.q.GetIdentityProvider(r.Context(), gen.GetIdentityProviderParams{OrganizationID: org, ID: id})
	if err != nil {
		response.Fail(w, http.StatusNotFound, "not_found", "provider not found")
		return gen.IdentityProvider{}, false
	}
	return p, true
}

// providerConfig unmarshals the OIDC config, defaulting redirect_uri to this
// server's callback when unset.
func (h *Handler) providerConfig(r *http.Request, p gen.IdentityProvider) ssoeng.Config {
	var cfg ssoeng.Config
	_ = json.Unmarshal(nonEmpty(p.Config), &cfg)
	if cfg.RedirectURI == "" {
		cfg.RedirectURI = scheme(r) + "://" + r.Host + "/auth/sso/" + strconv.FormatInt(p.ID, 10) + "/callback"
	}
	return cfg
}

func scheme(r *http.Request) string {
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		return "https"
	}
	return "http"
}
