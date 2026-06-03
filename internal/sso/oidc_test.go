package sso

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// idpFixture spins up a mock IdP JWKS endpoint and signs id_tokens with a test
// RSA key (kid "k1").
type idpFixture struct {
	key    *rsa.PrivateKey
	server *httptest.Server
}

func newIDP(t *testing.T) *idpFixture {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("key: %v", err)
	}
	f := &idpFixture{key: key}
	mux := http.NewServeMux()
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": []map[string]string{{
			"kty": "RSA", "kid": "k1", "alg": "RS256", "use": "sig",
			"n": b64(key.N.Bytes()), "e": b64(big.NewInt(int64(key.E)).Bytes()),
		}}})
	})
	f.server = httptest.NewServer(mux)
	t.Cleanup(f.server.Close)
	return f
}

func b64(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

func (f *idpFixture) sign(t *testing.T, claims jwt.MapClaims) string {
	t.Helper()
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	tok.Header["kid"] = "k1"
	s, err := tok.SignedString(f.key)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	return s
}

func (f *idpFixture) cfg() Config {
	return Config{Issuer: "https://idp.test", ClientID: "client-1", JWKSURI: f.server.URL + "/jwks"}
}

func TestVerifyIDTokenHappy(t *testing.T) {
	idp := newIDP(t)
	tok := idp.sign(t, jwt.MapClaims{
		"iss": "https://idp.test", "aud": "client-1", "sub": "user-abc",
		"email": "buyer@acme.test", "name": "Buyer", "nonce": "nonce-1",
		"exp": time.Now().Add(time.Hour).Unix(),
	})
	id, err := VerifyIDToken(context.Background(), idp.server.Client(), idp.cfg(), tok, "nonce-1")
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if id.Subject != "user-abc" || id.Email != "buyer@acme.test" {
		t.Errorf("identity = %+v", id)
	}
}

func TestVerifyIDTokenRejects(t *testing.T) {
	idp := newIDP(t)
	base := func() jwt.MapClaims {
		return jwt.MapClaims{"iss": "https://idp.test", "aud": "client-1", "sub": "u1",
			"email": "a@b.c", "nonce": "n1", "exp": time.Now().Add(time.Hour).Unix()}
	}
	cases := map[string]struct {
		claims jwt.MapClaims
		nonce  string
	}{
		"bad nonce":    {base(), "wrong-nonce"},
		"bad audience": {mutate(base(), "aud", "someone-else"), "n1"},
		"bad issuer":   {mutate(base(), "iss", "https://evil.test"), "n1"},
		"expired":      {mutate(base(), "exp", time.Now().Add(-time.Hour).Unix()), "n1"},
	}
	for name, c := range cases {
		tok := idp.sign(t, c.claims)
		if _, err := VerifyIDToken(context.Background(), idp.server.Client(), idp.cfg(), tok, c.nonce); err == nil {
			t.Errorf("%s: expected verification failure", name)
		}
	}

	// A token signed by a different key must fail.
	other, _ := rsa.GenerateKey(rand.Reader, 2048)
	bad := jwt.NewWithClaims(jwt.SigningMethodRS256, base())
	bad.Header["kid"] = "k1"
	s, _ := bad.SignedString(other)
	if _, err := VerifyIDToken(context.Background(), idp.server.Client(), idp.cfg(), s, "n1"); err == nil {
		t.Error("token signed by a foreign key must fail")
	}
}

func TestAuthURL(t *testing.T) {
	c := Config{AuthorizationEndpoint: "https://idp.test/authorize", ClientID: "client-1", RedirectURI: "https://app/cb"}
	u := c.AuthURL("st1", "no1")
	for _, want := range []string{"response_type=code", "client_id=client-1", "state=st1", "nonce=no1", "scope=openid"} {
		if !contains(u, want) {
			t.Errorf("auth url missing %q: %s", want, u)
		}
	}
}

func mutate(m jwt.MapClaims, k string, v any) jwt.MapClaims {
	m[k] = v
	return m
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
