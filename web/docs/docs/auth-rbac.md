---
title: Auth & RBAC
sidebar_position: 6
---

# Authentication & RBAC

## Two audiences

Teggo issues two kinds of JWT, and they are **not interchangeable**:

| Audience | Who | Identity | Carries |
|---|---|---|---|
| `admin` | seller-side staff (`users`) | org-scoped, role-based | `org_id`, `perms[]`, subject = user id |
| `storefront` | B2B buyers (`customer_users`) | a buying company | `org_id`, `cust_id` (customer), subject = customer-user id |

`internal/auth` mints them (`Issue`, `IssueStorefront`) and verifies them (`Parse`, pinned to HS256, expiry required).

## Middleware

In `internal/server/middleware`:

- **`Authenticator`** — rejects requests without a valid Bearer token; stores claims in context.
- **`RequireAudience("admin"|"storefront")`** — enforces the token type for a route group.
- **`RequirePermission("thing.view")`** — checks the claim's `perms` (deny-by-default).

```go
ar.Use(authMW)
ar.Use(mw.RequireAudience("admin"))
ar.With(mw.RequirePermission("order.manage")).Post("/admin/orders", h.create)
```

`ClaimsFrom(ctx)` returns the claims (org, permissions, customer id). **Always take the org from claims, never the body.**

## RBAC model

`roles` → `role_permissions` (string perms like `order.view`, `cms.manage`) → `user_roles`. Admin tokens embed the resolved permission set at login. Storefront tokens carry no permissions — buyer endpoints authorize on `cust_id` + ownership.

## Login

- `POST /admin/auth/login` → admin JWT (looks up `users`, bcrypt-checks, loads perms). Rate-limited.
- `POST /storefront/auth/login` → storefront JWT (looks up `customer_users`).

## SSO (OIDC + SAML)

`internal/modules/sso` adds federated login (see the `identity_providers` model):

- **OIDC** — `/auth/sso/{id}/login` → IdP → `/auth/sso/{id}/callback` verifies the `id_token` against the IdP JWKS and issues our JWT.
- **SAML** — `/auth/sso/{id}/login` → IdP → `POST /auth/sso/{id}/acs` verifies the signed assertion.
- **JIT provisioning** links the IdP subject to a local identity: `admin`-audience providers provision a `user`; `storefront` providers provision a `customer_user` under the provider's mapped customer. Re-login reuses the link.

Sellers administer buyer accounts and configure buyer SSO, but never hold buyer secrets or impersonate them — the two session domains stay isolated.

## Capability URLs

Some assets are served via signed, time-limited URLs instead of bearer auth (so a browser can open them directly): **invoice PDFs** and **DAM transforms**. `auth.Issuer.SignURL` / `VerifyURL` (HMAC over path+expiry). A bare/guessed URL is rejected.
