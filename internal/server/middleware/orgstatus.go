package middleware

import (
	"context"
	"net/http"

	"b2bcommerce/internal/server/response"
)

// RequireOrgActive blocks authenticated requests whose org is suspended or
// still pending email verification. status is a lookup (typically a
// tenant.StatusCache): ok=false means "unknown" and FAILS OPEN — a missing org
// row holds no data, and a transient DB error must not take every request down.
// Must run after Authenticator; requests without claims pass through untouched
// (public routes share the chain).
func RequireOrgActive(status func(ctx context.Context, orgID int64) (string, bool)) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims, ok := ClaimsFrom(r.Context())
			if !ok {
				next.ServeHTTP(w, r)
				return
			}
			switch s, known := status(r.Context(), claims.OrgID); {
			case known && s == "suspended":
				response.Fail(w, http.StatusForbidden, "org_suspended", "this organization is suspended")
			case known && s == "pending":
				response.Fail(w, http.StatusForbidden, "org_pending", "verify your email to activate this organization")
			default:
				next.ServeHTTP(w, r)
			}
		})
	}
}
