package middleware

import (
	"context"
	"net/http"
	"strings"

	"b2bcommerce/internal/auth"
	"b2bcommerce/internal/server/response"
	"b2bcommerce/internal/tenantctx"
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

			ctx = tenantctx.WithOrg(ctx, claims.OrgID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// OptionalAuthenticator parses a Bearer token when present and stores the
// claims in context, but does NOT reject anonymous requests. Used on public
// storefront reads (e.g. catalog) that personalize for a signed-in buyer
// (per-customer catalog visibility) yet must still serve anonymous visitors.
func OptionalAuthenticator(issuer *auth.Issuer) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := r.Header.Get("Authorization")
			if strings.HasPrefix(h, "Bearer ") {
				if claims, err := issuer.Parse(strings.TrimPrefix(h, "Bearer ")); err == nil {
					ctx := context.WithValue(r.Context(), claimsKey, claims)
					r = r.WithContext(tenantctx.WithOrg(ctx, claims.OrgID))
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

// ClaimsFrom returns the claims stored by Authenticator, if present.
func ClaimsFrom(ctx context.Context) (*auth.Claims, bool) {
	c, ok := ctx.Value(claimsKey).(*auth.Claims)
	return c, ok
}
