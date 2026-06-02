# b2bcommerce — Go skeleton

Runnable starting point for the in-house B2B commerce platform (PRD v0.2 + Implementation Packs 1–3).
Stack: **Go (chi + sqlc + river)**, **PostgreSQL 16**, with **Vue/Nuxt** frontends added later.

## Quick start (Docker)

```bash
cp .env.example .env          # adjust JWT_SECRET
docker compose up --build
```

This brings up Postgres, runs migrations + seed (one-shot `migrate` service), then starts the API and the worker.

- API:        http://localhost:8080
- Health:     `GET /healthz`, `GET /readyz`
- Storefront: `GET /storefront/products`
- Admin login:`POST /admin/auth/login`  body `{"email":"admin@demo.test","password":"admin1234","org_id":1}`
- Admin list: `GET /admin/products`  with header `Authorization: Bearer <token>`

> The seeded admin password hash in `migrations/0003_seed.sql` is a placeholder.
> Generate a real one and replace it before logging in:
> ```bash
> # any Go scratch: bcrypt.GenerateFromPassword([]byte("admin1234"), bcrypt.DefaultCost)
> ```
> Or register a proper user-creation endpoint and drop the seed.

## Quick start (local, no Docker)

```bash
# Postgres running locally; set DATABASE_URL to localhost
export $(grep -v '^#' .env | xargs)
go mod tidy
make migrate
make run-api      # terminal 1
make run-worker   # terminal 2
```

## Project layout

```
cmd/
  api/        HTTP server entrypoint (graceful shutdown)
  worker/     river worker entrypoint
  migrate/    one-shot: app migrations + river migrations
migrations/   *.sql applied in filename order + embed.go
internal/
  config/     env-based configuration
  db/         pgx pool + embedded migrator
  auth/       JWT issue/parse, bcrypt password helpers
  server/
    server.go         chi router assembly (mounts modules)
    middleware/        auth (bearer->claims), permission gate
    response/          JSON + error envelope (matches OpenAPI Error)
  modules/    one package per domain area; each owns its routes
    health/   liveness/readiness
    auth/     admin login
    catalog/  storefront + admin product listing (example)
  queue/      river client builders + jobs/
  store/      hand-written pgx data access (pre-sqlc)
    queries/  *.sql for sqlc -> generates internal/store/gen
sqlc.yaml     sqlc config
```

## How it's wired

- **Migrations** run from the dedicated `migrate` service/binary before api/worker start (compose `depends_on: condition: service_completed_successfully`). The migrator is idempotent and tracks applied files in `schema_migrations`. River's own tables are created via `rivermigrate`.
- **Auth**: `POST /admin/auth/login` verifies bcrypt and issues a JWT carrying `org_id`, audience, and the user's permissions. The `Authenticator` middleware parses the bearer token into context; `RequirePermission("...")` gates routes.
- **Queue**: the API uses an insert-only river client to enqueue; the worker registers workers and processes. The sample `send_email` job shows the pattern for real actions (invoice PDF, ERP sync, price recompute).
- **Two security contexts**: `/storefront/*` is public/customer-facing; `/admin/*` is bearer + permission gated. Catalog shows both off the same handler.

## Adding a module (the repeatable pattern)

1. Add tables as a new `migrations/000N_<name>.sql` (copy DDL from the Implementation Packs).
2. Add queries in `internal/store/queries/<name>.sql`, run `make generate`.
3. Create `internal/modules/<name>/handler.go` with a `Routes(r chi.Router, ...)` method.
4. Mount it in `internal/server/server.go`.
5. For async work, add a job in `internal/queue/jobs/` and register it in `queue/client.go`.

## Moving from hand-written store to sqlc

`internal/store/store.go` is hand-written pgx so the app runs immediately. As you
add `queries/*.sql` and run `make generate`, the typed layer appears in
`internal/store/gen`. Migrate handlers to the generated methods and shrink the
hand-written store over time. `sqlc.yaml` is already configured for pgx/v5.

## Notes

- Versions in `go.mod` are starting pins; run `go mod tidy` to resolve `go.sum`
  and indirect dependencies. Adjust river/pgx versions to the latest you trust.
- The full schema (Packs 1–3) is not all migrated here — only foundation +
  a minimal `products` table — so the skeleton boots with something to query.
  Add the rest module-by-module.
- Keep the OpenAPI file (Pack 2 §5 + Pack 3 §5) as the source of truth and
  generate the TS client for Vue/Nuxt from it.
```
