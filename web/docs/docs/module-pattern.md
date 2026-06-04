---
title: Adding a module
sidebar_position: 5
---

# Adding a module

Every feature module follows the same recipe. Here's the end-to-end path, from migration to admin GUI, with the order that avoids rework.

## 1. Migration

Add `migrations/NNNN_<name>.sql` (next number). Follow the **[Conventions](./conventions.md)**. Seed any RBAC permissions for the demo admin role:

```sql
INSERT INTO role_permissions (role_id, permission)
SELECT r.id, p.permission
  FROM roles r
  CROSS JOIN (VALUES ('thing.view'), ('thing.manage')) AS p(permission)
 WHERE r.organization_id = 1 AND r.name = 'admin'
ON CONFLICT DO NOTHING;
```

Migrations are embedded (`//go:embed *.sql`) and applied idempotently.

## 2. Queries → generate

Write `internal/store/queries/<name>.sql` with named sqlc queries, then:

```bash
go run github.com/sqlc-dev/sqlc/cmd/sqlc@latest generate
```

This regenerates `internal/store/gen/`. Mind the **sqlc gotchas** in Conventions.

## 3. Engine (optional, but preferred)

Put pure logic — validation, calculation, encoding — in its own package (`internal/<name>`), free of HTTP/DB so it's exhaustively unit-testable. Examples: `cpq`, `tax`, `edi`, `report`, `sso`.

## 4. Handler + routes

`internal/modules/<name>/handler.go`:

```go
type Handler struct { pool *pgxpool.Pool; q *gen.Queries /* + engine, issuer, … */ }

func New(pool *pgxpool.Pool) *Handler { return &Handler{pool: pool, q: gen.New(pool)} }

func (h *Handler) Routes(r chi.Router, authMW func(http.Handler) http.Handler) {
    r.Group(func(ar chi.Router) {
        ar.Use(authMW)
        ar.Use(mw.RequireAudience("admin"))
        ar.With(mw.RequirePermission("thing.view")).Get("/admin/things", h.list)
        ar.With(mw.RequirePermission("thing.manage")).Post("/admin/things", h.create)
    })
    // public/storefront routes (no bearer) go outside the group
}
```

Resolve org from claims (`mw.ClaimsFrom`), wrap multi-writes in `h.tx`.

## 5. Wire it

In `internal/server/server.go`, add `name.New(st.Pool()).Routes(r, authMW)` alongside the others. Pass `issuer` if the module mints/verifies tokens or signed URLs.

## 6. Contract + client

Extend `web/packages/api/openapi.yaml` (schemas + paths under the right tag), then:

```bash
pnpm --filter @teggo/api generate    # regenerates schema.d.ts
```

## 7. Tests

- Engine: pure unit tests (no DB).
- Handler: integration tests via `testsupport.NewDB(t)` (testcontainers Postgres, all migrations) — cover happy path, validation, **auth (wrong audience / missing permission → 403)**, and idempotency where relevant.

```bash
go test ./internal/modules/<name>/ ./internal/<name>/
go test ./...          # full suite before you call it done
```

## 8. Admin GUI

Add a view under `web/admin/src/views/<area>/`, a route in `router/index.ts` (with `meta.permission`), and a nav item in `AppLayout.vue` (same permission). Use the typed `api` client; for file uploads use a plain `fetch` with the bearer token.

## 9. Verify end-to-end

Build + vet + full tests, admin build, then a live smoke against the running stack (`docker compose up -d --build`). That's the bar for "done".

:::tip Order matters
migration → queries → `sqlc generate` → engine → handler → wire → OpenAPI → client → tests → GUI. Doing OpenAPI before the client, and tests before the GUI, avoids backtracking.
:::
