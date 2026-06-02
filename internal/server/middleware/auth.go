package middleware

import (
	"context"
	"net/http"
	"strings"

	"b2bcommerce/internal/auth"
	"b2bcommerce/internal/server/response"
)

type ctxKey int

const claimsKey ctxKey = iota

// Authenticator parses the Bearer token and stores claims in the request context.
// It rejects requests without a valid token.
func Authenticator(issuer *auth.Issuer) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := r.Header.Get("Authorization")
			if !strings.HasPrefix(h, "Bearer ") {
				response.Fail(w, http.StatusUnauthorized, "unauthorized", "missing bearer token")
				return
			}
			claims, err := issuer.Parse(strings.TrimPrefix(h, "Bearer "))
			if err != nil {
				response.Fail(w, http.StatusUnauthorized, "unauthorized", "invalid token")
				return
			}
			ctx := context.WithValue(r.Context(), claimsKey, claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// ClaimsFrom returns the claims stored by Authenticator, if present.
func ClaimsFrom(ctx context.Context) (*auth.Claims, bool) {
	c, ok := ctx.Value(claimsKey).(*auth.Claims)
	return c, ok
}
