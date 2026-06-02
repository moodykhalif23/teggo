# Teggo — In-House B2B Commerce Platform

A self-hosted, **API-first** B2B commerce platform for manufacturers, distributors, and
wholesalers — an in-house equivalent of OroCommerce. It models organizations buying from
organizations: per-customer catalogs and pricing, quote negotiation (RFQ → Quote → Order),
order-to-cash (shipments, invoices, payments), and an audited inventory ledger.

Built as a **modular monolith**. The Go service is the single source of truth; the two
frontends are pure API consumers.

| Layer | Choice |
|---|---|
| API | **Go** — `chi` router · `sqlc` (type-safe SQL) · `river` (Postgres-backed job queue) |
| Database | **PostgreSQL 16** — also carries the job queue (river) and search (FTS, planned) |
| Admin UI | **Vue 3** SPA (Vite · Pinia · Vue Router · **PrimeVue**) — login-gated back office |
| Storefront | **Nuxt** SSR (**PrimeVue**) — crawlable, customer-facing |
| Frontend ↔ API | **OpenAPI 3.1** contract → generated **TypeScript client** (`openapi-typescript` + `openapi-fetch`) |
| PDF / Edge / Deploy | Gotenberg (invoice PDFs) · Nginx (planned) · Docker Compose |

The full product specification lives in [`docs/`](docs/) (PRD v0.2 + Implementation Packs 1–3).
**Current state, implemented modules, gaps, and the phased roadmap are in [STATUS.md](STATUS.md).**

> **TL;DR status:** the MVP commerce spine is complete and integration-tested
> (Customers · Catalog/PIM · Pricing · Cart · RFQ→Quote→Order · Order-to-cash · Inventory).
> The frontends are scaffolded with a typed API client; the **Catalog** module is built on
> both. See [STATUS.md](STATUS.md).

## Prerequisites

- **Go** ≥ 1.22 · **Docker** (for Compose, and for integration tests via testcontainers)
- **Node** ≥ 20 + **pnpm** (`corepack enable pnpm`) — for the frontends
- **sqlc** (only if you change SQL): `go install github.com/sqlc-dev/sqlc/cmd/sqlc@latest`

---

## Run the backend

### Option A — Docker Compose (everything)

```bash
cp .env.example .env          # adjust JWT_SECRET
docker compose up --build
```

This starts Postgres, runs migrations + seed (one-shot `migrate` service), then the API and
worker. The API is on http://localhost:8080.

### Verify it's up

```bash
curl http://localhost:8080/healthz                      # {"status":"ok"}
curl 'http://localhost:8080/storefront/products'        # seeded products (public)

# Admin login → bearer token (seeded demo admin)
curl -s -X POST http://localhost:8080/admin/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"email":"admin@demo.test","password":"admin1234","org_id":1}'
```

**Seeded demo login:** `admin@demo.test` / `admin1234` (org `1`). The seed hash is a real
bcrypt of that password — change or remove the seed (`migrations/0003_seed.sql`) for production.

---

## Run the frontends

```bash
cd web
corepack enable pnpm
pnpm install
pnpm --filter @teggo/api generate     # generate the typed client from packages/api/openapi.yaml

pnpm dev:admin                      # admin SPA  → http://localhost:5173
pnpm dev:storefront                 # storefront → http://localhost:3000

pnpm build                          # production build of all packages
```

The Vite dev server proxies `/admin` and `/storefront` to the API at `localhost:8080`
(override with `VITE_API_BASE_URL`). The storefront reads `NUXT_PUBLIC_API_BASE`
(default `http://localhost:8080`). See [web/README.md](web/README.md).

---

## Tests

```bash
make test          
make vet            # go vet ./...
make fmt            # gofmt -w .
```

Set `TEST_DATABASE_URL` to run integration tests against an existing Postgres instead of a
throwaway container (e.g. in CI).

Frontend checks:

```bash
cd web
pnpm --filter @teggo/admin typecheck
pnpm --filter @teggo/storefront typecheck
```

---

## Code generation

| Generator | When | Command |
|---|---|---|
| **sqlc** — typed Go from `internal/store/queries/*.sql` into `internal/store/gen` | after editing SQL | `make generate` |
| **TypeScript client** — from `web/packages/api/openapi.yaml` | after editing the OpenAPI spec | `pnpm --filter @teggo/api generate` |

The OpenAPI file is the **single source of truth** for the API contract; both frontends
consume the generated types, so they cannot drift.

---

## Adding a backend module (the repeatable pattern)

1. Add a `migrations/00NN_<name>.sql` (copy DDL from the Implementation Packs in `docs/`).
2. Add `internal/store/queries/<name>.sql`, run `make generate`.
3. Create `internal/modules/<name>/handler.go` with a `Routes(r chi.Router, authMW ...)` method,
   org-scoped and permission/audience gated.
4. Mount it in `internal/server/server.go`.
5. Async work → add a job in `internal/queue/jobs/` and register it in `internal/queue/client.go`.
6. Add integration tests (real Postgres via `testsupport.NewDB(t)`): query-level + HTTP-level
   (assert the auth gate and tenant isolation).
7. Extend `web/packages/api/openapi.yaml`, regenerate, and build the screens.

Conventions (money as decimal strings, `public_id` in URLs, `organization_id` tenant scoping,
`text + CHECK` statuses, JSONB+GIN attributes) are documented in `docs/` Pack 1 §0.
