---
title: Conventions
sidebar_position: 4
---

# Conventions

These bind every table and query. Following them keeps the codebase predictable — read before writing code.

## Schema (every table)

- **`BIGINT GENERATED ALWAYS AS IDENTITY`** primary keys.
- **`public_id UUID`** on customer-facing documents (orders, invoices, quotes, media, …). Never expose raw `id` externally.
- **Money is `NUMERIC(15,4)`**, currency `CHAR(3)`. **Never float.** In Go, money is a **`string`** (sqlc override on `pg_catalog.numeric`) — do arithmetic via `internal/money` (`math/big.Rat`).
- **Enums are `text + CHECK`**, not Postgres enum types.
- **Flexible attributes are `JSONB` + GIN** index.
- **`organization_id`** is the tenant boundary — filter it in every query. (Some children inherit org via a join, e.g. `invoices` → `orders`.)
- **Soft delete** only where noted (`products`, `customers`, `price_lists`) via `deleted_at`; filter `deleted_at IS NULL`.
- **`set_updated_at()` trigger** maintains `updated_at`.

## Money

```go
import "b2bcommerce/internal/money"

rt, _   := money.LineTotal(qty, unitPrice)     // qty × price, rounded
sub, _  := money.Sum(rowTotals...)             // add many
n, _    := money.Cmp(a, b)                     // compare
```

Never `float64` a price. `money.Parse` → `*big.Rat`, `money.Format` → 4-dp string.

## sqlc gotchas (these have bitten us)

- **`COALESCE($n, default)` in an INSERT untypes the param** → drop it, default in Go instead.
- **Positional `$n::text IS NULL OR …` makes the param non-nullable** → use `sqlc.narg('x')` for an optional `*string`.
- **Set-returning functions need an explicit column alias**: `jsonb_each_text(x) AS kv(key, value)`.
- **Recursive CTE columns must be fully qualified.**
- **Adding an exotic column (e.g. `tsvector`) breaks `SELECT *` scans at runtime** → add a column type override in `sqlc.yaml`.
- A batch param like `ANY($2::bigint[])` generates a field named `Column2` — pass the slice there.

## Go / HTTP

- Handlers return the JSON envelope via `internal/server/response` (`JSON`, `Fail`). Errors carry `{code, message}` — never leak SQL/stack traces.
- Multi-step mutations run in a `pgx` transaction (`h.tx(ctx, func(q *gen.Queries) error { … })`).
- Read the org from JWT claims, **never** from the request body.
- Fire-and-forget enqueues are logged on failure (`slog`), never silently dropped.

## Frontend (typed client)

When destructuring an `openapi-fetch` call, prefer `const { data, error } = await api.GET(...)`. Assigning the whole result and then reading `.error` narrows to `never` for 200-only operations.
