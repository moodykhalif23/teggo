# Project Status & Roadmap — Teggo

_Last updated: 2026-06-02._ Companion to [README.md](README.md) and the specs in [`docs/`](docs/)
(PRD v0.2 + Implementation Packs 1–3). This document is the precise, honest record of **what
is built, what is deliberately stubbed/deferred, and the ordered path forward** — including the
gaps inside otherwise-"done" modules.

---

## 1. Snapshot

- **MVP commerce spine: COMPLETE and integration-tested.** The full motion works end-to-end
  against real PostgreSQL: **browse → request quote → negotiate → accept → order → confirm
  (reserve stock) → ship (fulfil) → invoice (PDF job) → pay (settle)**.
- **Frontend foundation: in place.** pnpm workspace, PrimeVue, and a **typed client generated
  from an OpenAPI 3.1 contract**. The **Catalog** module is built on both the admin SPA and the
  Nuxt storefront. Remaining admin/storefront modules are the active workstream.
- Everything below the spine (CRM, CMS, workflow engine, integrations, reporting, DAM,
  punchout/EDI, field-sales) is **specified but not yet built**.

---

## 2. Architecture & conventions (in force)

- **Modular monolith** in Go: one package per domain under `internal/modules/` (+ `internal/inventory`
  which exposes domain functions other modules call). Mounted in `internal/server/server.go`.
- **Data access:** `sqlc` generates type-safe Go from `internal/store/queries/*.sql` into
  `internal/store/gen`. Money/quantities (`NUMERIC(15,4)`) map to **Go `string`**; exact decimal
  math via `internal/money` (`math/big.Rat`) — never floats.
- **Migrations:** embedded `migrations/00NN_*.sql`, applied in order by an idempotent migrator;
  river's own tables are migrated alongside.
- **Auth:** JWT with two **audiences** — `admin` (RBAC permission-gated) and `storefront`
  (customer-user, scoped to their `customer_id`). `mw.RequireAudience` + `mw.RequirePermission`.
- **Tenancy:** every query is scoped by `organization_id`; customer-facing routes by `customer_id`.
- **Async:** river job queue; the API enqueues via an insert-only client, the worker processes.
- **Testing:** integration tests run against a real Postgres 16 via **testcontainers**
  (`testsupport.NewDB(t)`); each module has query-level and HTTP-level tests asserting the auth
  gate and tenant isolation.
- **Contract:** `web/packages/api/openapi.yaml` is the single source of truth → generated TS client.

---

## 3. Implemented — backend modules

| Module | Migration | Surface | Highlights |
|---|---|---|---|
| Foundation / RBAC | `0001` | — | organizations, websites, users, roles, role_permissions, user_roles; `set_updated_at()` trigger |
| Auth | — | `POST /admin/auth/login`, `POST /storefront/auth/login` | bcrypt; admin & storefront token issue; `RequireAudience` |
| Customers & accounts (§2) | `0004` | `/admin/customers*`, `/admin/customer-groups` | hierarchy (ancestor CTE §12.2), **cycle-safe re-parenting**, customer users, addresses, groups |
| Catalog & PIM (§3) | `0002`,`0005` | `/admin/products*`, `/admin/categories`, `/admin/attributes`, `/admin/attribute-families`, `/storefront/products*` | product CRUD, attribute families, **category subtree CTE (§12.3)**, **JSONB facet filter (§12.5)**, product↔category assignment |
| Pricing engine (§4) | `0006` | `/admin/price-lists*`, `/admin/price-list-assignments`, `/admin/pricing/*` | **deterministic resolution (§12.1)**: customer>group>website, priority, qty tier, currency, time-bounds; **`combined_prices` cache** recomputed by a river job; auto-enqueue on change |
| Cart & shopping lists (§5) | `0007` | `/storefront/cart*`, `/storefront/shopping-lists*` | price snapshot from `combined_prices` at add-time; **no price → 409 price-on-request**; revalidate (price drift); convert list→cart |
| RFQ → Quote → Order (§6) | `0008` | `/storefront/rfqs*`, `/storefront/quotes/{id}/accept|decline`, `/storefront/orders*`, `/admin/rfqs*`, `/admin/quotes*`, `/admin/orders*` | three state machines; quote-from-RFQ; edit/send with **versioned revisions**; **accept → order in one tx** (immutable snapshots); on-behalf-of order placement; order status PATCH + history |
| Order-to-cash (§7) | `0009` | `/admin/orders/{id}/shipments`, `/admin/shipments/{id}/status`, `/admin/orders/{id}/invoices`, `/admin/invoices/{id}*`, `/admin/payments*`, `/storefront/invoices*` | shipments capped at ordered qty; invoice issue freezes amounts + async PDF; payments with **paid-flip** (captured ≥ total) and invoice-method credit check; refund |
| Inventory (§8) | `0010` | `/admin/warehouses`, `/admin/inventory/*` | append-only **movement ledger** as source of truth; `inventory_levels` cache; manual receipt/adjustment/return; **ATP (§12.4)**; **Reserve-on-confirm** + **Fulfil-on-ship** wired into the order/shipment flows (graceful when untracked) |

### Cross-cutting infrastructure (built)
- 3 river jobs: `send_email` (stub), `recompute_combined_prices`, `generate_invoice_pdf` (stub).
- `internal/money`: `Parse/Format/Sum/Sub/Cmp/RowTotal/LineTotal`.
- testcontainers harness + per-test isolated DB; migration-compatibility gate test.
- JSON error envelope `{code, message, details}`; structured request logging (chi middleware).

---

## 4. Implemented — frontend

- **Workspace** (`web/`, pnpm): `@teggo/api` (OpenAPI + generated `openapi-fetch` client),
  `admin` (Vue 3), `storefront` (Nuxt). Both build; admin type-checks clean.
- **Typed client:** `web/packages/api/openapi.yaml` covers **auth + catalog**; `pnpm --filter
  @teggo/api generate` produces the TS types both apps import.
- **Admin:** auth store + login, app shell with permission-filtered sidebar, and the **Catalog**
  screens — Products (DataTable + create/edit/delete dialog), Categories, Attributes.
- **Storefront:** home, category browse (`/c/[slug]`, subtree), product detail (`/p/[slug]`),
  layout — all on the typed client + SSR `useAsyncData`.
- Verified with a live smoke test against the running API.

---

## 5. Known gaps & stubs (inside "done" areas — do not lose track of these)

These are real omissions in the current spine, important to close before calling the MVP
production-ready:

**Order flow / finance**
- **Checkout from cart (§9):** a cart cannot yet be placed directly as an order; orders are
  created only via quote-accept or admin on-behalf-of. `POST /storefront/orders` (place from
  active cart) is not implemented.
- **Credit & approval gate at placement (§10):** only a partial check exists (invoice-method
  payment verifies terms + open-invoice-vs-credit-limit). Missing: `spending_limit` enforcement,
  approval routing/`approval_requests` (table not migrated), credit hold at order time.
- **Tax:** never computed — `tax_total`/`tax_amount` are always `0` (no tax engine / local VAT).
- **Payments are manual records only:** no gateway adapters (no Stripe / M-Pesa-Daraja);
  `refund` flips state without calling a processor.
- **Invoice PDF — real (Gotenberg).** The async job renders an HTML invoice → PDF via Gotenberg
  (stub PDF when `GOTENBERG_URL` is unset), stores the bytes in `invoice_documents`, and the API
  streams them at a capability URL `GET /files/invoices/{publicID}.pdf` (unguessable UUID, no auth —
  works as a plain browser download). Remaining: object storage instead of DB bytea; signed/expiring
  URLs if capability URLs aren't acceptable; quote PDFs.
- **Quote expiry** is enforced at accept-time but there is **no scheduled job** to auto-flip
  expired quotes; no quote PDF.
- **Email** (`send_email` job) just logs — no SES/SMTP/Mailgun adapter; no order/quote/invoice emails.

**Catalog / search**
- **Full-text search** (`?q=`) is accepted by the OpenAPI/storefront but **not implemented**
  (only category-subtree and JSONB-facet filtering work). No Postgres FTS yet; `ProductList.facets`
  not returned.
- **`catalog_visibility`** table exists but is **not enforced** in product queries.
- **Variants** (configurable products) and **product media/translations** tables exist but have
  no handler/UI flows.

**Platform**
- **Multi-website / multi-org:** schema supports it, but the storefront and several demo paths
  **hardcode `organization_id = 1`** and the default website; no host→website resolution.
- **No audit log** (`audit_logs` not migrated), **no webhooks/integration framework**, **no
  rate limiting / brute-force protection**, **no refresh tokens / logout / revocation**.
- **Storefront auth** works via bearer token, but there is **no httpOnly-cookie + Nuxt server
  middleware** yet, and **no customer self-registration / approval**.
- **RBAC is coarse** (permission strings only) — no entity-level ACL, no impersonation.
- **State machines are hardcoded** in Go (order/quote/RFQ/shipment) — not yet the configurable
  workflow engine (Pack 2 §3).
- **Inventory** assumes a single default warehouse in reserve/fulfil; no low-stock notifications;
  multi-warehouse allocation not modelled.
- **Observability:** structured request logs only — no metrics, tracing, or error tracking.
- **i18n / localization, multi-currency display, config hierarchy** not implemented.

---

## 6. Roadmap — next phases (ordered)

### Phase G — Finish the GUI (current workstream)
Build both surfaces module-by-module on the typed client, growing `openapi.yaml` and checking in
after each. Order:
1. **Customers** (admin: grid, CRUD, hierarchy view, users, addresses).
2. **Pricing** (admin: price lists, tiers, assignments, recompute trigger, resolve preview).
3. **Cart & Checkout** (storefront: cart page, **customer login via httpOnly cookie + Nuxt
   middleware**, checkout — depends on §9 below).
4. **RFQ / Quote / Order** (storefront RFQ builder + quote accept/`QuoteThread`; admin
   **`QuoteEditor`** + order list/detail + status timeline).
5. **Invoices / Payments / Shipments** (admin finance + storefront invoice views).
6. **Inventory** (admin: warehouses, levels, adjustments, movement ledger, ATP).
7. **Dashboard** widgets; shared `DataGrid`/`EntityForm`/design-token package.

### Phase MVP-hardening — make the single store production-ready
Close the §5 gaps that block a real launch:
- **Checkout from cart (§9)** → order in one tx (reuse the snapshot/reserve logic).
- **Full credit & approval gate (§10)**: migrate `approval_requests`; enforce `spending_limit`
  + `credit_limit` at placement with approval routing.
- **One payment gateway** end-to-end (M-Pesa/Daraja or Stripe) via an adapter interface.
- **Invoice PDF object storage** (move `invoice_documents` bytea → S3/MinIO) + signed URLs;
  quote PDFs reusing the renderer. (Gotenberg rendering itself is done.) **Transactional email**
  adapter + templates.
- **Tax**: rules-based local VAT provider; snapshot onto order/invoice lines.
- **Full-text search** (Postgres FTS `tsvector`/GIN) + facets in `ProductList`; enforce
  `catalog_visibility`.
- **Audit log**, **rate limiting**, **storefront cookie auth + self-registration**,
  **quote-expiry scheduled job**.

### Phase V1 — the real product (Pack 2 + parts of Pack 1 depth)
Multi-website/multi-org; account-hierarchy inheritance in pricing; approval workflows;
**configurable workflow + automation engine** (Pack 2 §3, replacing hardcoded state machines);
**CRM** (Pack 2 §1); **CMS + block builder** (Pack 2 §2); **integration framework + adapters**
(Pack 2 §4); indexed faceted search; multi-warehouse + shipping integration; reporting dashboards
(Pack 3 §1); full ACL + audit; theming; storefront API completeness.

### Phase V2 — enterprise & scale (Pack 3)
Marketplace/multi-vendor; CPQ; deep ERP/accounting sync; **punchout (OCI/cXML) + EDI** (Pack 3 §3);
**DAM + image pipeline** (Pack 3 §2); **field-sales offline sync** (Pack 3 §4); custom report
builder + BI; read replicas / multi-node.

---

## 7. How to extend

- **Specs:** `docs/` — PRD (phasing, NFRs), Pack 1 (commerce spine + §0 conventions + §12 tricky
  queries), Pack 2 (CRM/CMS/workflow/integrations + OpenAPI + Vue/Nuxt breakdown), Pack 3 (V2 +
  full admin API inventory).
- **Backend module recipe:** see [README.md](README.md) → "Adding a backend module". The
  `teggo-module` project skill encodes the conventions, the test pattern, and the per-slice
  definition-of-done.
- **API contract:** edit `web/packages/api/openapi.yaml`, regenerate, build screens.
