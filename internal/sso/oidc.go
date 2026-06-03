// Package sso implements OpenID Connect SSO (PRD §15) using the OAuth2
// authorization-code flow. It depends only on the stdlib + our JWT library: the
// IdP's JWKS is fetched and parsed by hand, and the id_token is verified
// (signature, issuer, audience, expiry, nonce).
package sso

import (
	"context"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Config is an OIDC provider's resolved settings (from identity_providers.config).
type Config struct {
	Issuer                string   `json:"issuer"`
	AuthorizationEndpoint string   `json:"authorization_endpoint"`
	TokenEndpoint         string   `json:"token_endpoint"`
	JWKSURI               string   `json:"jwks_uri"`
	ClientID              string   `json:"client_id"`
	ClientSecret          string   `json:"client_secret"`
	Scopes                []string `json:"scopes"`
	RedirectURI           string   `json:"redirect_uri"`
}

// Identity is the verified subject from an id_token.
type Identity struct {
	Subject string
	Email   string
	Name    string
}

// AuthURL builds the authorization-endpoint redirect for the code flow.
func (c Config) AuthURL(state, nonce string) string {
	scopes := c.Scopes
	if len(scopes) == 0 {
		scopes = []string{"openid", "email", "profile"}
	}
	q := url.Values{}
	q.Set("response_type", "code")
	q.Set("client_id", c.ClientID)
	q.Set("redirect_uri", c.RedirectURI)
	q.Set("scope", strings.Join(scopes, " "))
	q.Set("state", state)
	q.Set("nonce", nonce)
	sep := "?"
	if strings.Contains(c.AuthorizationEndpoint, "?") {
		sep = "&"
	}
	return c.AuthorizationEndpoint + sep + q.Encode()
}

// Exchange swaps an authorization code for tokens at the token endpoint and
// returns the raw id_token.
func Exchange(ctx context.Context, client *http.Client, c Config, code string) (string, error) {
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", c.RedirectURI)
	form.Set("client_id", c.ClientID)
	form.Set("client_secret", c.ClientSecret)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.TokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := doClient(client).Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("token endpoint returned %d", resp.StatusCode)
	}
	var tok struct {
		IDToken string `json:"id_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tok); err != nil {
		return "", err
	}
	if tok.IDToken == "" {
		return "", fmt.Errorf("no id_token in token response")
	}
	return tok.IDToken, nil
}

// VerifyIDToken verifies the id_token signature against the IdP JWKS and checks
// issuer, audience (client_id), expiry, and the nonce. It returns the subject.
func VerifyIDToken(ctx context.Context, client *http.Client, c Config, rawIDToken, expectedNonce string) (Identity, error) {
	keys, err := fetchJWKS(ctx, client, c.JWKSURI)
	if err != nil {
		return Identity{}, fmt.Errorf("jwks: %w", err)
	}
	type claims struct {
		Nonce string `json:"nonce"`
		Email string `json:"email"`
		Name  string `json:"name"`
		jwt.RegisteredClaims
	}
	var cl claims
	_, err = jwt.ParseWithClaims(rawIDToken, &cl, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodRSA); !ok {
			return nil, fmt.Errorf("unexpected signing method %v", t.Header["alg"])
		}
		kid, _ := t.Header["kid"].(string)
		key, ok := keys[kid]
		if !ok {
			// Single-key JWKS: accept the only key when no kid match.
			if len(keys) == 1 {
				for _, k := range keys {
					return k, nil
				}
			}
			return nil, fmt.Errorf("no matching JWKS key for kid %q", kid)
		}
		return key, nil
	}, jwt.WithValidMethods([]string{"RS256"}), jwt.WithExpirationRequired())
	if err != nil {
		return Identity{}, fmt.Errorf("verify id_token: %w", err)
	}
	if c.Issuer != "" && cl.Issuer != c.Issuer {
		return Identity{}, fmt.Errorf("issuer mismatch")
	}
	if !audienceContains(cl.Audience, c.ClientID) {
		return Identity{}, fmt.Errorf("audience mismatch")
	}
	if expectedNonce != "" && cl.Nonce != expectedNonce {
		return Identity{}, fmt.Errorf("nonce mismatch")
	}
	if cl.Subject == "" {
		return Identity{}, fmt.Errorf("id_token has no subject")
	}
	return Identity{Subject: cl.Subject, Email: cl.Email, Name: cl.Name}, nil
}

func audienceContains(aud jwt.ClaimStrings, want string) bool {
	for _, a := range aud {
		if a == want {
			return true
		}
	}
	return false
}

// ---- JWKS (RSA only) -------------------------------------------------------

func fetchJWKS(ctx context.Context, client *http.Client, jwksURI string) (map[string]*rsa.PublicKey, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, jwksURI, nil)
	if err != nil {
		return nil, err
	}
	resp, err := doClient(client).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var doc struct {
		Keys []struct {
			Kty string `json:"kty"`
			Kid string `json:"kid"`
			N   string `json:"n"`
			E   string `json:"e"`
		} `json:"keys"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		return nil, err
	}
	out := make(map[string]*rsa.PublicKey)
	for _, k := range doc.Keys {
		if k.Kty != "RSA" {
			continue
		}
		nb, err := base64.RawURLEncoding.DecodeString(k.N)
		if err != nil {
			continue
		}
		eb, err := base64.RawURLEncoding.DecodeString(k.E)
		if err != nil {
			continue
		}
		e := 0
		for _, b := range eb {
			e = e<<8 | int(b)
		}
		out[k.Kid] = &rsa.PublicKey{N: new(big.Int).SetBytes(nb), E: e}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no usable RSA keys in JWKS")
	}
	return out, nil
}

func doClient(c *http.Client) *http.Client {
	if c != nil {
		return c
	}
	return &http.Client{Timeout: 15 * time.Second}
}
