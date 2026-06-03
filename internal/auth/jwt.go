package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Claims is the JWT payload for both admin and storefront contexts.
type Claims struct {
	OrgID       int64    `json:"org_id"`
	Audience    string   `json:"aud"` // "admin" or "storefront"
	Permissions []string `json:"perms,omitempty"`
	// CustomerID is set for storefront tokens: the buying company the
	// authenticated customer-user belongs to. Subject holds the customer_user id.
	CustomerID int64 `json:"cust_id,omitempty"`
	jwt.RegisteredClaims
}

// Issuer mints and verifies tokens with a shared secret.
type Issuer struct {
	secret []byte
	ttl    time.Duration
}

func NewIssuer(secret string, ttl time.Duration) *Issuer {
	return &Issuer{secret: []byte(secret), ttl: ttl}
}

// Issue creates a signed token for a subject (user id or customer-user id).
func (i *Issuer) Issue(subject string, orgID int64, audience string, perms []string) (string, error) {
	now := time.Now()
	claims := Claims{
		OrgID:       orgID,
		Audience:    audience,
		Permissions: perms,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   subject,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(i.ttl)),
		},
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return tok.SignedString(i.secret)
}

// IssueStorefront mints a storefront token for a customer-user. Subject is the
// customer_user id; CustomerID carries the buying company.
func (i *Issuer) IssueStorefront(customerUserID, orgID, customerID int64) (string, error) {
	now := time.Now()
	claims := Claims{
		OrgID:      orgID,
		Audience:   "storefront",
		CustomerID: customerID,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   strconv.FormatInt(customerUserID, 10),
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(i.ttl)),
		},
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return tok.SignedString(i.secret)
}

// Parse verifies a token string and returns its claims.
func (i *Issuer) Parse(tokenStr string) (*Claims, error) {
	claims := &Claims{}
	_, err := jwt.ParseWithClaims(tokenStr, claims, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return i.secret, nil
	},
		// Pin the algorithm (defence against alg-confusion) and reject tokens
		// without an expiry so a leaked unbounded token can't live forever.
		jwt.WithValidMethods([]string{"HS256"}),
		jwt.WithExpirationRequired(),
	)
	if err != nil {
		return nil, err
	}
	return claims, nil
}

// SignURL turns a bare path into a signed, time-limited capability URL of the
// form "<path>?exp=<unix>&sig=<hex-hmac>". The signature binds the path and
// expiry to the issuer secret, so only the server can mint a working link and
// it stops working after ttl. Used for invoice-PDF downloads, which a browser
// opens directly (no bearer token), but which must not be reachable by merely
// guessing the public_id.
func (i *Issuer) SignURL(path string, ttl time.Duration) string {
	exp := strconv.FormatInt(time.Now().Add(ttl).Unix(), 10)
	return fmt.Sprintf("%s?exp=%s&sig=%s", path, exp, i.urlMAC(path, exp))
}

// VerifyURL checks the exp+sig pair minted by SignURL against the given path.
// It returns false on a tampered signature or an elapsed expiry.
func (i *Issuer) VerifyURL(path, exp, sig string) bool {
	expUnix, err := strconv.ParseInt(exp, 10, 64)
	if err != nil || time.Now().Unix() > expUnix {
		return false
	}
	want := i.urlMAC(path, exp)
	return hmac.Equal([]byte(want), []byte(sig))
}

func (i *Issuer) urlMAC(path, exp string) string {
	m := hmac.New(sha256.New, i.secret)
	m.Write([]byte(path + "\n" + exp))
	return hex.EncodeToString(m.Sum(nil))
}
