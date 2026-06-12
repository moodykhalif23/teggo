// Package tenantctx carries the authenticated request's organization through
// context, decoupled from JWT claims so low-level layers (the database pool's
// RLS arming — SAAS.md #3) can read it without importing HTTP middleware.
package tenantctx

import "context"

type ctxKey struct{}
type bypassKey struct{}

// WithOrg returns a context carrying the org id (0 is treated as absent).
func WithOrg(ctx context.Context, orgID int64) context.Context {
	if orgID == 0 {
		return ctx
	}
	return context.WithValue(ctx, ctxKey{}, orgID)
}

// Bypass marks the context as deliberately cross-tenant: OrgFrom reports no
// org, so the RLS net stands down for queries made with this context. Reserve
// it for platform-operator paths that legitimately touch other orgs' rows
// (already permission-gated) — it is the auditable escape hatch, grep-able.
func Bypass(ctx context.Context) context.Context {
	return context.WithValue(ctx, bypassKey{}, true)
}

// OrgFrom returns the org id stored by WithOrg, or (0, false) when absent or
// explicitly bypassed.
func OrgFrom(ctx context.Context) (int64, bool) {
	if b, _ := ctx.Value(bypassKey{}).(bool); b {
		return 0, false
	}
	v, ok := ctx.Value(ctxKey{}).(int64)
	return v, ok && v != 0
}
