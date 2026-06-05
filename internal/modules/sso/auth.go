package sso

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"b2bcommerce/internal/auth"
	"b2bcommerce/internal/server/response"
	ssoeng "b2bcommerce/internal/sso"
	"b2bcommerce/internal/store/gen"
)

// login starts the OIDC code flow: persists a one-time state+nonce and redirects
// the browser to the IdP's authorization endpoint.
func (h *Handler) login(w http.ResponseWriter, r *http.Request) {
	p, ok := h.publicProvider(w, r)
	if !ok {
		return
	}
	if p.Type == "saml" {
		h.samlLogin(w, r, p)
		return
	}
	cfg := h.providerConfig(r, p)
	if cfg.AuthorizationEndpoint == "" || cfg.ClientID == "" {
		response.Fail(w, http.StatusFailedDependency, "misconfigured", "provider is missing OIDC config")
		return
	}
	state, nonce := randToken(), randToken()
	if _, err := h.q.CreateSSOState(r.Context(), gen.CreateSSOStateParams{
		ProviderID: p.ID, State: state, Nonce: nonce,
		RedirectTo: optStr(r.URL.Query().Get("redirect")),
		ExpiresAt:  time.Now().Add(10 * time.Minute),
	}); err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not start login")
		return
	}
	http.Redirect(w, r, cfg.AuthURL(state, nonce), http.StatusFound)
}

// samlLogin builds a SAML AuthnRequest and redirects to the IdP (HTTP-Redirect
// binding). The relay state token lets the ACS recover the post-login redirect.
func (h *Handler) samlLogin(w http.ResponseWriter, r *http.Request, p gen.IdentityProvider) {
	cfg, acs := h.samlConfig(r, p)
	sp, err := ssoeng.NewSAMLSP(cfg, acs)
	if err != nil {
		response.Fail(w, http.StatusFailedDependency, "misconfigured", "invalid SAML config")
		return
	}
	relay := randToken()
	if _, err := h.q.CreateSSOState(r.Context(), gen.CreateSSOStateParams{
		ProviderID: p.ID, State: relay, Nonce: "saml",
		RedirectTo: optStr(r.URL.Query().Get("redirect")),
		ExpiresAt:  time.Now().Add(10 * time.Minute),
	}); err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not start login")
		return
	}
	authURL, err := ssoeng.SAMLAuthRedirect(sp, relay)
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not build SAML request")
		return
	}
	http.Redirect(w, r, authURL, http.StatusFound)
}

// acs is the SAML assertion consumer (HTTP-POST binding): it verifies the signed
// SAMLResponse, provisions/links the identity, and issues our JWT.
func (h *Handler) acs(w http.ResponseWriter, r *http.Request) {
	p, ok := h.publicProvider(w, r)
	if !ok {
		return
	}
	if p.Type != "saml" {
		response.Fail(w, http.StatusBadRequest, "bad_request", "provider is not SAML")
		return
	}
	if err := r.ParseForm(); err != nil {
		response.Fail(w, http.StatusBadRequest, "bad_request", "could not parse form")
		return
	}
	encoded := r.FormValue("SAMLResponse")
	if encoded == "" {
		response.Fail(w, http.StatusBadRequest, "bad_request", "missing SAMLResponse")
		return
	}
	cfg, acs := h.samlConfig(r, p)
	sp, err := ssoeng.NewSAMLSP(cfg, acs)
	if err != nil {
		response.Fail(w, http.StatusFailedDependency, "misconfigured", "invalid SAML config")
		return
	}
	identity, err := ssoeng.VerifySAMLResponse(sp, encoded)
	if err != nil {
		response.Fail(w, http.StatusUnauthorized, "invalid_assertion", "SAML assertion verification failed")
		return
	}
	if identity.Email == "" {
		response.Fail(w, http.StatusUnprocessableEntity, "no_email", "assertion has no email")
		return
	}

	// RelayState (if present + known) carries the post-login redirect; it's
	// one-time. Unknown/absent relay still authenticates (the assertion is the
	// trust anchor) but won't redirect.
	var redirectTo string
	if relay := r.FormValue("RelayState"); relay != "" {
		if st, err := h.q.GetSSOState(r.Context(), relay); err == nil && st.ProviderID == p.ID {
			_ = h.q.DeleteSSOState(r.Context(), st.ID)
			redirectTo = deref(st.RedirectTo)
		}
	}

	token, err := h.provision(r.Context(), p, identity)
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "provision_failed", err.Error())
		return
	}
	if redirectTo != "" && r.URL.Query().Get("format") != "json" {
		sep := "?"
		if strings.Contains(redirectTo, "?") {
			sep = "&"
		}
		http.Redirect(w, r, redirectTo+sep+"token="+token, http.StatusFound)
		return
	}
	response.JSON(w, http.StatusOK, map[string]any{"token": token, "audience": p.Audience})
}

// metadata serves this SP's SAML metadata XML for a SAML provider, so an IdP
// admin can register us by URL. Public (metadata is not secret).
func (h *Handler) metadata(w http.ResponseWriter, r *http.Request) {
	p, ok := h.publicProvider(w, r)
	if !ok {
		return
	}
	if p.Type != "saml" {
		response.Fail(w, http.StatusBadRequest, "bad_request", "provider is not SAML")
		return
	}
	cfg, acs := h.samlConfig(r, p)
	xmlBytes, err := ssoeng.SAMLMetadataXML(cfg, acs)
	if err != nil {
		response.Fail(w, http.StatusFailedDependency, "misconfigured", "invalid SAML config")
		return
	}
	w.Header().Set("Content-Type", "application/samlmetadata+xml")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(xmlBytes)
}

// callback completes the flow: validates state, exchanges the code, verifies the
// id_token, provisions/links the identity, and issues our JWT.
func (h *Handler) callback(w http.ResponseWriter, r *http.Request) {
	p, ok := h.publicProvider(w, r)
	if !ok {
		return
	}
	code := r.URL.Query().Get("code")
	state := r.URL.Query().Get("state")
	if code == "" || state == "" {
		response.Fail(w, http.StatusBadRequest, "bad_request", "missing code or state")
		return
	}
	st, err := h.q.GetSSOState(r.Context(), state)
	if err != nil || st.ProviderID != p.ID {
		response.Fail(w, http.StatusBadRequest, "bad_state", "unknown or mismatched state")
		return
	}
	_ = h.q.DeleteSSOState(r.Context(), st.ID) // one-time use
	if time.Now().After(st.ExpiresAt) {
		response.Fail(w, http.StatusBadRequest, "expired", "login state has expired")
		return
	}

	cfg := h.providerConfig(r, p)
	idToken, err := ssoeng.Exchange(r.Context(), h.client, cfg, code)
	if err != nil {
		response.Fail(w, http.StatusBadGateway, "exchange_failed", "token exchange failed")
		return
	}
	identity, err := ssoeng.VerifyIDToken(r.Context(), h.client, cfg, idToken, st.Nonce)
	if err != nil {
		response.Fail(w, http.StatusUnauthorized, "invalid_token", "id_token verification failed")
		return
	}
	if identity.Email == "" {
		response.Fail(w, http.StatusUnprocessableEntity, "no_email", "IdP did not provide an email claim")
		return
	}

	token, err := h.provision(r.Context(), p, identity)
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "provision_failed", err.Error())
		return
	}

	if dest := deref(st.RedirectTo); dest != "" && r.URL.Query().Get("format") != "json" {
		sep := "?"
		if strings.Contains(dest, "?") {
			sep = "&"
		}
		http.Redirect(w, r, dest+sep+"token="+token, http.StatusFound)
		return
	}
	response.JSON(w, http.StatusOK, map[string]any{"token": token, "audience": p.Audience})
}

// provision links the IdP subject to a local identity (creating one on first
// login) and returns our JWT for it.
func (h *Handler) provision(ctx context.Context, p gen.IdentityProvider, id ssoeng.Identity) (string, error) {
	// Returning user: reuse the linked identity.
	if ext, err := h.q.GetExternalIdentity(ctx, gen.GetExternalIdentityParams{ProviderID: p.ID, Subject: id.Subject}); err == nil {
		if ext.UserID != nil {
			return h.adminToken(ctx, p, *ext.UserID)
		}
		if ext.CustomerUserID != nil {
			return h.issuer.IssueStorefront(*ext.CustomerUserID, p.OrganizationID, derefID(p.CustomerID))
		}
	}

	hash, err := auth.HashPassword(randToken() + randToken()) // SSO users can't password-login
	if err != nil {
		return "", err
	}

	if p.Audience == "admin" {
		userID, err := h.resolveAdminUser(ctx, p.OrganizationID, id, hash)
		if err != nil {
			return "", err
		}
		if _, err := h.q.CreateExternalIdentity(ctx, gen.CreateExternalIdentityParams{
			ProviderID: p.ID, Subject: id.Subject, UserID: &userID, Email: &id.Email,
		}); err != nil {
			return "", err
		}
		return h.adminToken(ctx, p, userID)
	}

	// storefront (buyer)
	cuID, err := h.resolveBuyer(ctx, derefID(p.CustomerID), id, hash)
	if err != nil {
		return "", err
	}
	if _, err := h.q.CreateExternalIdentity(ctx, gen.CreateExternalIdentityParams{
		ProviderID: p.ID, Subject: id.Subject, CustomerUserID: &cuID, Email: &id.Email,
	}); err != nil {
		return "", err
	}
	return h.issuer.IssueStorefront(cuID, p.OrganizationID, derefID(p.CustomerID))
}

func (h *Handler) resolveAdminUser(ctx context.Context, org int64, id ssoeng.Identity, hash string) (int64, error) {
	if u, err := h.q.GetUserByEmail(ctx, gen.GetUserByEmailParams{OrganizationID: org, Email: id.Email}); err == nil {
		return u.ID, nil
	}
	u, err := h.q.CreateUser(ctx, gen.CreateUserParams{
		OrganizationID: org, Email: id.Email, PasswordHash: hash, FullName: defName(id.Name, id.Email),
	})
	if err != nil {
		return 0, err
	}
	return u.ID, nil
}

func (h *Handler) resolveBuyer(ctx context.Context, customerID int64, id ssoeng.Identity, hash string) (int64, error) {
	if cu, err := h.q.GetCustomerUserByEmail(ctx, gen.GetCustomerUserByEmailParams{CustomerID: customerID, Email: id.Email}); err == nil {
		return cu.ID, nil
	}
	cu, err := h.q.CreateCustomerUser(ctx, gen.CreateCustomerUserParams{
		CustomerID: customerID, Email: id.Email, PasswordHash: hash, FullName: defName(id.Name, id.Email), Role: "buyer",
	})
	if err != nil {
		return 0, err
	}
	return cu.ID, nil
}

// adminToken issues an admin JWT carrying the user's current permissions.
func (h *Handler) adminToken(ctx context.Context, p gen.IdentityProvider, userID int64) (string, error) {
	perms, _ := h.q.GetUserPermissions(ctx, userID)
	return h.issuer.Issue(strconv.FormatInt(userID, 10), p.OrganizationID, "admin", perms)
}

// publicProvider loads an active OIDC provider by path id (no auth).
func (h *Handler) publicProvider(w http.ResponseWriter, r *http.Request) (gen.IdentityProvider, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		response.Fail(w, http.StatusBadRequest, "bad_request", "invalid provider id")
		return gen.IdentityProvider{}, false
	}
	p, err := h.q.GetIdentityProviderByID(r.Context(), id)
	if err != nil || !p.IsActive {
		response.Fail(w, http.StatusNotFound, "not_found", "provider not found")
		return gen.IdentityProvider{}, false
	}
	return p, true
}

// samlConfig parses SAML config and resolves the ACS URL for this provider.
func (h *Handler) samlConfig(r *http.Request, p gen.IdentityProvider) (ssoeng.SAMLConfig, string) {
	var sc ssoeng.SAMLConfig
	_ = json.Unmarshal(nonEmptyJSON(p.Config), &sc)
	acs := scheme(r) + "://" + r.Host + "/auth/sso/" + strconv.FormatInt(p.ID, 10) + "/acs"
	return sc, acs
}

func nonEmptyJSON(b []byte) []byte {
	if len(b) == 0 {
		return []byte("{}")
	}
	return b
}

// ---- helpers --------------------------------------------------------------

func randToken() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func optStr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func derefID(p *int64) int64 {
	if p == nil {
		return 0
	}
	return *p
}

func defName(name, email string) string {
	if name != "" {
		return name
	}
	return email
}
