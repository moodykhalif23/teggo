package sso_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"b2bcommerce/internal/auth"
	"b2bcommerce/internal/server"
	"b2bcommerce/internal/store"
	"b2bcommerce/internal/store/gen"
	"b2bcommerce/internal/testsupport"
)

const testSecret = "sso-test-secret"

// mockIDP is a minimal OIDC IdP: a JWKS endpoint and a token endpoint that
// returns whatever id_token the test most recently staged.
type mockIDP struct {
	key     *rsa.PrivateKey
	server  *httptest.Server
	idToken string
}

func newIDP(t *testing.T) *mockIDP {
	t.Helper()
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	m := &mockIDP{key: key}
	mux := http.NewServeMux()
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": []map[string]string{{
			"kty": "RSA", "kid": "k1",
			"n": base64.RawURLEncoding.EncodeToString(key.N.Bytes()),
			"e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes()),
		}}})
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"id_token": m.idToken})
	})
	m.server = httptest.NewServer(mux)
	t.Cleanup(m.server.Close)
	return m
}

func (m *mockIDP) stage(t *testing.T, sub, email, nonce string) {
	t.Helper()
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
		"iss": "https://idp.test", "aud": "client-1", "sub": sub, "email": email,
		"nonce": nonce, "exp": time.Now().Add(time.Hour).Unix(),
	})
	tok.Header["kid"] = "k1"
	m.idToken, _ = tok.SignedString(m.key)
}

func (m *mockIDP) config() map[string]any {
	return map[string]any{
		"issuer": "https://idp.test", "authorization_endpoint": m.server.URL + "/authorize",
		"token_endpoint": m.server.URL + "/token", "jwks_uri": m.server.URL + "/jwks",
		"client_id": "client-1", "client_secret": "secret",
	}
}

func newServer(t *testing.T) (http.Handler, *auth.Issuer, *pgxpool.Pool) {
	t.Helper()
	pool := testsupport.NewDB(t)
	st := store.New(pool)
	issuer := auth.NewIssuer(testSecret, time.Hour)
	return server.New(st, issuer), issuer, pool
}

func tok(t *testing.T, issuer *auth.Issuer) string {
	t.Helper()
	s, _ := issuer.Issue("1", 1, "admin", []string{"sso.view", "sso.manage", "customer.view", "customer.manage"})
	return s
}

func do(t *testing.T, h http.Handler, method, path, token string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(method, path, &buf)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	return rr
}

func createProvider(t *testing.T, h http.Handler, at string, body map[string]any) int64 {
	t.Helper()
	rr := do(t, h, http.MethodPost, "/admin/identity-providers", at, body)
	if rr.Code != http.StatusCreated {
		t.Fatalf("create provider: %d (%s)", rr.Code, rr.Body.String())
	}
	var p struct {
		ID int64 `json:"id"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &p)
	return p.ID
}

// runFlow drives login → (stage id_token with the stored nonce) → callback and
// returns our issued JWT.
func runFlow(t *testing.T, h http.Handler, pool *pgxpool.Pool, idp *mockIDP, providerID int64, sub, email string) string {
	t.Helper()
	ps := strconv.FormatInt(providerID, 10)
	lr := do(t, h, http.MethodGet, "/auth/sso/"+ps+"/login", "", nil)
	if lr.Code != http.StatusFound {
		t.Fatalf("login: want 302, got %d (%s)", lr.Code, lr.Body.String())
	}
	loc, _ := url.Parse(lr.Header().Get("Location"))
	state := loc.Query().Get("state")
	if state == "" {
		t.Fatalf("login redirect missing state: %s", lr.Header().Get("Location"))
	}
	var nonce string
	if err := pool.QueryRow(context.Background(), `SELECT nonce FROM sso_states WHERE state=$1`, state).Scan(&nonce); err != nil {
		t.Fatalf("read nonce: %v", err)
	}
	idp.stage(t, sub, email, nonce)

	cr := do(t, h, http.MethodGet, "/auth/sso/"+ps+"/callback?format=json&code=authcode&state="+state, "", nil)
	if cr.Code != http.StatusOK {
		t.Fatalf("callback: %d (%s)", cr.Code, cr.Body.String())
	}
	var out struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal(cr.Body.Bytes(), &out)
	if out.Token == "" {
		t.Fatalf("callback returned no token: %s", cr.Body.String())
	}
	return out.Token
}

func TestSSOAdminProvisioningAndLinkReuse(t *testing.T) {
	h, issuer, pool := newServer(t)
	at := tok(t, issuer)
	idp := newIDP(t)

	pid := createProvider(t, h, at, map[string]any{
		"type": "oidc", "name": "Okta", "audience": "admin", "config": idp.config(),
	})

	// First login provisions a seller-side user and issues an admin JWT.
	token := runFlow(t, h, pool, idp, pid, "okta|123", "alice@corp.test")
	claims, err := issuer.Parse(token)
	if err != nil || claims.Audience != "admin" {
		t.Fatalf("token claims: %+v err=%v", claims, err)
	}

	var users int
	_ = pool.QueryRow(context.Background(), `SELECT count(*) FROM users WHERE email='alice@corp.test'`).Scan(&users)
	if users != 1 {
		t.Fatalf("expected 1 provisioned user, got %d", users)
	}

	// Second login (same subject) reuses the link — no duplicate user.
	token2 := runFlow(t, h, pool, idp, pid, "okta|123", "alice@corp.test")
	c2, _ := issuer.Parse(token2)
	if c2.Subject != claims.Subject {
		t.Errorf("link not reused: subjects %s vs %s", claims.Subject, c2.Subject)
	}
	_ = pool.QueryRow(context.Background(), `SELECT count(*) FROM users WHERE email='alice@corp.test'`).Scan(&users)
	if users != 1 {
		t.Errorf("relogin created a duplicate user (%d)", users)
	}
}

func TestSSOBuyerProvisioning(t *testing.T) {
	h, issuer, pool := newServer(t)
	at := tok(t, issuer)
	idp := newIDP(t)

	cust, _ := gen.New(pool).CreateCustomer(context.Background(), gen.CreateCustomerParams{OrganizationID: 1, Name: "Buyer Co", CreditLimit: "0"})
	pid := createProvider(t, h, at, map[string]any{
		"type": "oidc", "name": "Buyer IdP", "audience": "storefront", "customer_id": cust.ID, "config": idp.config(),
	})

	token := runFlow(t, h, pool, idp, pid, "okta|buyer-7", "bob@buyer.test")
	claims, err := issuer.Parse(token)
	if err != nil || claims.Audience != "storefront" {
		t.Fatalf("buyer token: %+v err=%v", claims, err)
	}
	if claims.CustomerID != cust.ID {
		t.Errorf("buyer token customer = %d, want %d", claims.CustomerID, cust.ID)
	}
	var cu int
	_ = pool.QueryRow(context.Background(), `SELECT count(*) FROM customer_users WHERE email='bob@buyer.test' AND customer_id=$1`, cust.ID).Scan(&cu)
	if cu != 1 {
		t.Errorf("expected 1 provisioned customer_user, got %d", cu)
	}
}

func TestSSOCallbackRejectsBadState(t *testing.T) {
	h, issuer, _ := newServer(t)
	at := tok(t, issuer)
	idp := newIDP(t)
	pid := createProvider(t, h, at, map[string]any{"type": "oidc", "name": "X", "audience": "admin", "config": idp.config()})
	ps := strconv.FormatInt(pid, 10)
	if rr := do(t, h, http.MethodGet, "/auth/sso/"+ps+"/callback?code=x&state=unknown", "", nil); rr.Code != http.StatusBadRequest {
		t.Errorf("unknown state: want 400, got %d", rr.Code)
	}
}

func TestSSOAdminAuth(t *testing.T) {
	h, issuer, _ := newServer(t)
	cust, _ := issuer.IssueStorefront(0, 1, 1)
	if rr := do(t, h, http.MethodGet, "/admin/identity-providers", cust, nil); rr.Code != http.StatusForbidden {
		t.Errorf("storefront token: want 403, got %d", rr.Code)
	}
}

// genCertPEM returns a throwaway self-signed cert (PEM) for SAML provider config.
func genCertPEM(t *testing.T) string {
	t.Helper()
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	tmpl := x509.Certificate{SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: "idp"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour)}
	der, _ := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &key.PublicKey, key)
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
}

func TestSSOSamlProviderAndACS(t *testing.T) {
	h, issuer, _ := newServer(t)
	at := tok(t, issuer)

	// Create a SAML provider (requires idp_sso_url + idp_certificate).
	if rr := do(t, h, http.MethodPost, "/admin/identity-providers", at, map[string]any{
		"type": "saml", "name": "Bad", "audience": "admin", "config": map[string]any{"idp_sso_url": "https://idp/sso"},
	}); rr.Code != http.StatusBadRequest {
		t.Fatalf("saml without cert: want 400, got %d", rr.Code)
	}
	pid := createProvider(t, h, at, map[string]any{
		"type": "saml", "name": "Okta SAML", "audience": "admin",
		"config": map[string]any{"idp_entity_id": "idp", "idp_sso_url": "https://idp/sso", "idp_certificate": genCertPEM(t), "sp_entity_id": "teggo"},
	})
	ps := strconv.FormatInt(pid, 10)

	// login builds a SAML AuthnRequest redirect to the IdP SSO URL.
	lr := do(t, h, http.MethodGet, "/auth/sso/"+ps+"/login", "", nil)
	if lr.Code != http.StatusFound {
		t.Fatalf("saml login: want 302, got %d (%s)", lr.Code, lr.Body.String())
	}
	loc, _ := url.Parse(lr.Header().Get("Location"))
	if loc.Host == "" || loc.Query().Get("SAMLRequest") == "" {
		t.Errorf("saml login redirect missing SAMLRequest: %s", lr.Header().Get("Location"))
	}

	// ACS rejects a garbage (unsigned) SAMLResponse.
	form := bytes.NewBufferString("SAMLResponse=" + url.QueryEscape("PG5vdC1zYW1sLz4=")) // base64 "<not-saml/>"
	req := httptest.NewRequest(http.MethodPost, "/auth/sso/"+ps+"/acs", form)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("garbage SAMLResponse: want 401, got %d (%s)", rr.Code, rr.Body.String())
	}

	// Metadata endpoint serves SP metadata XML carrying our ACS URL.
	md := do(t, h, http.MethodGet, "/auth/sso/"+ps+"/metadata", "", nil)
	if md.Code != http.StatusOK {
		t.Fatalf("saml metadata: want 200, got %d (%s)", md.Code, md.Body.String())
	}
	body := md.Body.String()
	if !strings.Contains(body, "EntityDescriptor") || !strings.Contains(body, "/auth/sso/"+ps+"/acs") {
		t.Errorf("metadata missing EntityDescriptor/ACS URL: %s", body)
	}
}

func TestSSOMetadataRejectsOIDC(t *testing.T) {
	h, issuer, _ := newServer(t)
	at := tok(t, issuer)
	idp := newIDP(t)
	pid := createProvider(t, h, at, map[string]any{"type": "oidc", "name": "X", "audience": "admin", "config": idp.config()})
	if rr := do(t, h, http.MethodGet, "/auth/sso/"+strconv.FormatInt(pid, 10)+"/metadata", "", nil); rr.Code != http.StatusBadRequest {
		t.Errorf("metadata on OIDC provider: want 400, got %d", rr.Code)
	}
}
