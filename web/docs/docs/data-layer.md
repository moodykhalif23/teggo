---
title: Data layer
sidebar_position: 7
---

# Data layer

## sqlc + pgx

Queries live in `internal/store/queries/*.sql` as annotated SQL; `sqlc generate` produces the typed Go in `internal/store/gen/`. Handlers call `h.q.SomeQuery(ctx, params)` — no hand-written SQL, no ORM.

```bash
go run github.com/sqlc-dev/sqlc/cmd/sqlc@latest generate
```

`internal/store` wraps the pool and exposes `Queries()` / `Pool()`. For multi-step writes, begin a transaction and run queries against the tx-bound `*gen.Queries`:

```go
func (h *Handler) tx(ctx context.Context, fn func(*gen.Queries) error) error {
    t, err := h.pool.Begin(ctx); if err != nil { return err }
    defer t.Rollback(ctx)
    if err := fn(gen.New(t)); err != nil { return err }
    return t.Commit(ctx)
}
```

## Migrations

`migrations/NNNN_*.sql`, embedded via `//go:embed *.sql` and applied **idempotently** by `cmd/migrate` (records applied versions in `schema_migrations`). Forward-only — to change schema, add the next-numbered migration. (Down-migrations are a documented follow-up.)

## Money

`NUMERIC(15,4)` ↔ Go `string` (sqlc override). Compute via `internal/money` (`math/big.Rat`): `LineTotal`, `RowTotal`, `Sum`, `Sub`, `Cmp`, `Parse`, `Format`. **Never** convert a price to `float64`.

## Connection pool

`db.NewPoolWithConfig` — `DB_MAX_CONNS` (default 20) and `DB_MAX_CONN_IDLE_TIME` (default 5m) are env-tunable; max lifetime 1h.

## Testing with real Postgres

Tests spin up a throwaway Postgres per package via **testcontainers**, with all migrations applied:

```go
pool := testsupport.NewDB(t)   // fresh DB, migrated; auto-torn-down
```

So tests exercise real SQL (CTEs, triggers, constraints, JSONB) — not a mock. Run `make test` or `go test ./...`.

## The tricky queries

Pricing resolution, customer/category recursive trees, available-to-promise, and JSONB facet counts are written as first-class sqlc methods (Pack 1 §12). When touching them, re-read the **[sqlc gotchas](./conventions.md#sqlc-gotchas-these-have-bitten-us)** — `narg`, `COALESCE` untyping, set-returning aliases, and `SELECT *` vs exotic columns.
