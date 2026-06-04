---
title: Architecture
sidebar_position: 3
---

# Architecture

Teggo is a **modular monolith**: one Go binary serving an HTTP API, with strict package boundaries between feature modules. Not microservices — but each module is self-contained enough to reason about (and could be split later).

## Processes (`cmd/`)

- **`cmd/api`** — the HTTP server. Builds the pgx pool, the JWT issuer, the river insert-only client (to enqueue jobs), the DAM blob store, and wires every module via `internal/server`.
- **`cmd/worker`** — runs river: executes background jobs (email, invoice PDF, price recompute, rendition generation, report schedules, ERP sweep) and the periodic jobs.
- **`cmd/migrate`** — applies the embedded SQL migrations once, idempotently.

## Package map

```
internal/
  server/            wiring: builds the chi router, mounts every module, middleware stack
  server/middleware/ Authenticator, RequireAudience, RequirePermission, RequestLogger,
                     SecureHeaders, MaxBytes, RateLimit
  auth/              JWT issue/parse (2 audiences), bcrypt, signed capability URLs
  config/            env → Config (validated at startup)
  db/                pgx pool (configurable), embedded migrator
  store/             sqlc: queries/*.sql → gen/*.go (the typed query layer)
  money/             NUMERIC money math on math/big.Rat (never float)
  modules/<name>/    one feature module = handler + routes (catalog, sales, otc, crm, …)
  <engine pkgs>      pure logic reused by modules: cpq, tax, shipping, edi, cxml, erp,
                     sso, report, blob, imageproc, workflow, automation, changelog
  queue/             river client + jobs/ (one file per job kind)
migrations/          NNNN_*.sql, embedded via //go:embed
web/                 pnpm workspace: packages/api (OpenAPI + generated TS client),
                     admin (Vue SPA), storefront (Nuxt), docs (this site)
```

## Request lifecycle

1. **Middleware** (in `internal/server`): RequestID → RealIP → SecureHeaders → structured request logger (slog) → panic recovery → body-size cap → 30s timeout.
2. **Auth** — `Authenticator` parses the Bearer JWT into claims; `RequireAudience("admin"|"storefront")` and `RequirePermission("…")` gate each route.
3. **Handler** — validates input, calls the sqlc query layer (often inside a `pgx` transaction), returns a JSON envelope (`internal/server/response`).
4. **Side effects** — durable work (email, PDFs, recompute, ERP sync) is enqueued on river and runs in the worker, so a slow/failed integration never blocks the request.

## API-first

The OpenAPI 3.1 document (`web/packages/api/openapi.yaml`) is the **single source of truth**. From it:

- `openapi-typescript` generates `schema.d.ts`; an `openapi-fetch` client (`@teggo/api`) gives both frontends a fully typed surface.
- This site's **[API reference](/api)** is generated from the same file.

When you add or change an endpoint, you update the spec and regenerate — see **[Frontend](./frontend.md)**.

## Modules at a glance

Commerce spine: `catalog` (PIM), `pricing`, `cart`, `sales` (RFQ→Quote→Order), `otc` (shipments/invoices/payments), `inventory`, `customers`, `tenancy` (multi-org/website). Plus `crm`, `cms`, `wfadmin` (workflow/automation), `reporting`, `dam`, `integration` (punchout/EDI), `field` (offline sync), `cpq`, `tax`, `shipping`, `erp`, `sso`.

Each follows the same shape — see **[Adding a module](./module-pattern.md)**.
