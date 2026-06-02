---
name: oro-module
description: >
  Steer any module of the oro-folk B2B commerce platform (the in-house Oro-equivalent:
  Go/chi/sqlc/river + Postgres + Vue/Nuxt). Use when adding or extending a backend
  module (catalog, pricing, customers, RFQ/quotes/orders, inventory, CRM, CMS, workflow,
  integrations, reporting, DAM, punchout/EDI, field-sales) or its API/frontend slice.
  Encodes the repeatable enterprise pattern and the spec/convention guardrails so every
  module lands consistently.
---

# Steering an oro-folk module

This platform is an **API-first / headless modular monolith**. The Go service is the single
source of truth; Vue (admin) and Nuxt (storefront) are pure API consumers. Build
**module-by-module, MVP-first** — never scaffold V1/V2 breadth before the MVP slice ships.

## 0. Before writing any code

1. **Read the spec section for the module.** Source of truth is `docs/` (see the
   `reference-spec-docs` memory). Pack 1 = commerce spine + conventions §0 + the tricky
   queries §12 + build order §13. Pack 2 = CRM/CMS/workflow/integrations + OpenAPI §5 +
   Vue/Nuxt breakdown §6. Pack 3 = reporting/DAM/punchout-EDI/field-sales + full admin API §5.
2. **Respect the phase tags.** Each feature is tagged [MVP]/[V1]/[V2] in the PRD. Build only
   the current phase's slice. Confirm with Brian before pulling V1/V2 scope forward.
3. **Check the build order** (Pack 1 §13 + Pack 2 §7 + Pack 3 §6). Modules have dependency
   order; don't build pricing before catalog, cart before pricing, etc.

## 1. Schema conventions (bind EVERY table — Pack 1 §0)

- PKs: `BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY`.
- Customer-facing docs (orders, quotes, rfqs, invoices, payments, carts) also carry
  `public_id UUID NOT NULL DEFAULT gen_random_uuid() UNIQUE`. **Never expose raw `id`** in
  URLs/APIs — use `public_id`.
- Timestamps: `created_at`/`updated_at timestamptz NOT NULL DEFAULT now()`; bump `updated_at`
  via the shared `set_updated_at()` trigger.
- Soft delete (`deleted_at timestamptz`) ONLY where business needs it (products, customers,
  price_lists). Hard-delete everything else.
- Money: `NUMERIC(15,4)` (never float). Currency: `CHAR(3)` (ISO 4217). Quantities:
  `NUMERIC(15,4)` (B2B sells fractional/UOM).
- Status vocabularies: `text + CHECK (... IN (...))`, never native ENUM.
- Flexible data: `JSONB` + `GIN` index (product attributes, document snapshots, CMS blocks).
- **Multi-tenant boundary:** most tables carry `organization_id`; EVERY query filters by it.
  Enforce tenant isolation at the query layer.
- Naming: tables plural snake_case; FK columns `<singular>_id`; join tables `<a>_<b>`.

## 2. The repeatable add-a-module pattern (from README)

1. **Migration:** add `migrations/000N_<name>.sql` (copy DDL from the Pack, in dependency
   order; load any needed extensions first). The migrator is idempotent and embeds via
   `migrations/embed.go`; it tracks applied files in `schema_migrations`.
2. **Queries:** add `internal/store/queries/<name>.sql` and run `make generate` (sqlc →
   typed layer in `internal/store/gen`). Write the **tricky queries (Pack 1 §12) as sqlc
   methods first**: price resolution, recursive CTEs (customer ancestors / category subtree),
   available-to-promise, JSONB facet filter. New code should prefer generated sqlc methods;
   `internal/store/store.go` is the legacy hand-written pgx layer to shrink over time.
3. **Handler:** create `internal/modules/<name>/handler.go` with
   `New(...) *Handler` and `func (h *Handler) Routes(r chi.Router, authMW ...)`. Two security
   contexts: `/storefront/*` is public/customer; `/admin/*` is bearer-gated. Gate admin routes
   with `mw.RequirePermission("<entity>.<action>")` inside an `r.Group` that `Use`s the
   Authenticator. Return via `response.JSON` / `response.Fail` (matches the OpenAPI Error
   envelope `{code,message,details}`).
4. **Mount it** in `internal/server/server.go` alongside the existing modules.
5. **Async work:** add a job in `internal/queue/jobs/` and register the worker in
   `internal/queue/client.go` (`river.AddWorker`). The API enqueues via the insert-only
   client; the worker processes. Use jobs for: price recompute → `combined_prices`, invoice
   PDF (Gotenberg), email sends, integration sync, reindex, scheduled (quote-expiry,
   overdue-invoice) via river periodic jobs.

Each module = one Go package + one set of sqlc queries + (usually) one OpenAPI tag.

## 3. Cross-cutting rules that are easy to miss

- **Pricing is first-class** (PRD §7, Pack 1 §4). Resolution is deterministic:
  customer > customer_group > website default, higher `priority` wins, then most-specific
  qty tier ≤ requested. Storefront reads hit `combined_prices` (the precomputed cache) only;
  any price/assignment/group change enqueues a recompute job. No price resolved = "price on
  request" (RFQ path), not free.
- **Order placement gate** (Pack 1 §10): in one tx — resolve totals → if over
  `spending_limit` create `approval_requests` + set order `on_hold` → enforce `credit_limit`
  for invoice terms → on pass reserve inventory + set `confirmed` + enqueue invoice/email jobs.
- **Orders are immutable records:** snapshot SKU, name, unit_price, addresses onto the order
  at creation — don't live-join back to products/customers.
- **Inventory:** `inventory_movements` (signed, append-only) is the source of truth;
  `inventory_levels` is the cache. available = on_hand − reserved.
- **OpenAPI is the single source of truth** (Pack 2 §5, Pack 3 §5). When a module's API
  changes, update the one OpenAPI file and regenerate the Go stubs + TS client so backend and
  frontends never drift.
- **Integrations** are an adapter pattern: small Go interface per class (Payment/Shipping/
  Tax/Email), provider packages register into a registry; secrets from env/Vault NEVER the DB;
  outbound calls idempotent; inbound webhooks verified + de-duped; every call logs to
  `sync_logs`; failures go to the queue retry/dead-letter. Regional defaults: M-Pesa/Daraja
  payment, local rules-based VAT (KRA) tax.
- **Workflow engine** (Pack 2 §3): config-driven state machine + automation (event→
  conditions→actions). Transitions are atomic (state update + log in one tx); actions run as
  river jobs AFTER commit so a failed action never corrupts state.

## 4. Definition of done for a module slice

- Tenant isolation (`organization_id`) enforced in every query.
- Permission-gated on `/admin/*`; correct security context.
- Money as decimal strings over the wire (sqlc maps `numeric`→`string`); no floats.
- Tricky queries done as sqlc methods, not application-side tree-walking.
- Async/side-effecting work goes through river, not inline in the request.
- OpenAPI updated; `make vet` / `make build` clean; `gofmt` applied.
- Only the current phase's scope built; no silent V1/V2 creep.
