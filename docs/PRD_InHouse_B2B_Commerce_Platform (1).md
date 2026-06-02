# Product Requirements Document
## In-House B2B Commerce Platform ("Oro-equivalent")

| Field | Value |
|---|---|
| Document status | Draft v0.2 |
| Owner | Brian |
| Last updated | 2 June 2026 |
| Reference benchmark | OroCommerce / OroCRM / OroPlatform |
| Chosen stack | Go (API) · PostgreSQL · Vue (admin SPA) + Nuxt (storefront SSR) |
| Target deployment | Self-hosted (Linux, VPS/cloud; API-first / headless) |

> **v0.2 changes:** Stack decided — Go + PostgreSQL + Vue/Nuxt (replaces the earlier PHP/Symfony-vs-Laravel options). Architecture is now **API-first / headless**, so the storefront API moves from V1 to MVP. Postgres absorbs the job queue and search in MVP, deferring Redis and Elasticsearch. See §17, §20.5, §21, §24, §27.

---

## 0. How to read this document

This PRD specifies the **complete** product — everything the reference platform (Oro) does, plus integrations and extras. It is intentionally large. It is **not** meant to be built all at once.

Each functional area is tagged with a build phase:

- **[MVP]** — minimum to run a single B2B store end-to-end (browse → quote → order → invoice).
- **[V1]** — the "real product": multi-customer, pricing depth, CRM, workflow.
- **[V2]** — enterprise/scale features (marketplace, advanced BI, deep ERP sync).

Read Section 12 (Roadmap) first if you want the pragmatic build order rather than the full inventory.

---

## 1. Executive summary

We are building a self-hosted B2B digital commerce platform for manufacturers, distributors, and wholesalers. Unlike B2C platforms (one buyer, one price, instant card payment, linear checkout), B2B commerce requires modelling **organizations buying from organizations**: multi-level account hierarchies, customer-specific catalogs and pricing, quote negotiation (RFQ/CPQ), approval workflows, credit-terms/invoice payment, and tight integration with back-office ERP/accounting.

The platform combines three capabilities that the reference product keeps natively integrated rather than stitched together: **Commerce** (storefront, catalog, checkout, order-to-cash), **CRM** (accounts, contacts, leads, opportunities), and a **low-code workflow/automation engine** that ties them together.

### 1.1 Goals

1. Let a B2B seller run all commerce operations from one system.
2. Model customer organizations with hierarchy, roles, and per-customer pricing/catalogs.
3. Support the full B2B sales motion: quote → negotiation → order → fulfilment → invoice → payment.
4. Be self-hostable and operable by a small team without enterprise licensing.
5. Be extensible (modular) so new features and integrations are additive, not rewrites.

### 1.2 Non-goals (explicitly out of scope)

- We are **not** building a general-purpose ERP (no payroll, no general ledger, no manufacturing MRP). We *integrate* with those.
- We are **not** targeting consumer/DTC retail as the primary use case (B2C is a secondary mode, not the design centre).
- We are **not** building a headless-only product in MVP; the storefront is server-rendered for SEO, with an API for later headless use.
- We are **not** matching Oro's full enterprise scale (multi-region active-active, etc.) in V1.

### 1.3 Reality check (build vs. buy)

The reference platform represents ~10 years of work by a large team. A faithful full clone is a multi-year, multi-engineer program. This PRD is viable only if phased. Before committing, weigh: adopting OroCommerce Community Edition (open-source, same feature set, self-hostable) or ERPNext/Frappe (lighter, Python, hackable) may deliver 80% of this faster. Build in-house when control, IP ownership, domain-specific workflows, or licensing economics justify the cost. This document assumes that decision is already made.

---

## 2. Target users & personas

| Persona | Role | Primary needs |
|---|---|---|
| **Buyer (purchasing agent)** | Customer-side employee placing orders | Fast reorder, see contract pricing, request quotes, submit POs, track shipments |
| **Buyer admin** | Customer-side account manager | Manage sub-users, set spending limits, approve orders, view org-wide order history |
| **Sales rep** | Seller-side | Build quotes, negotiate, manage accounts/opportunities, place orders on behalf of customers, field-sales (incl. offline) |
| **Catalog/merchandising manager** | Seller-side | Manage products, attributes, categories, content, pricing |
| **Operations / fulfilment** | Seller-side | Process orders, manage inventory, shipping, returns |
| **Finance** | Seller-side | Invoicing, payments, credit terms, reconciliation with ERP |
| **System admin** | Seller-side | Users, roles, permissions, configuration, integrations |
| **Developer/integrator** | Internal or partner | Extend modules, build integrations, customize storefront |

---

## 3. Glossary

- **Account / Customer** — a buying organization (company).
- **Customer User** — an individual login belonging to a Customer.
- **Account hierarchy** — parent/child structure of customer organizations (e.g. HQ → regional branches).
- **Price List** — a named set of prices, assignable to customers/groups/websites by priority.
- **RFQ** — Request for Quote (buyer-initiated price request).
- **CPQ** — Configure, Price, Quote (seller-side quote building, incl. configurable products).
- **Shopping List** — a saved, reusable cart / requisition list.
- **Website** — a storefront instance; one installation can serve many.
- **Organization** — top-level tenant boundary (seller side) for multi-org/multi-brand setups.
- **Order-to-cash** — the full flow from order placement through invoicing and payment.
- **Punchout** — buyer's procurement system (e.g. SAP Ariba) connects into our catalog (OCI/cXML).
- **EDI** — Electronic Data Interchange for orders/invoices (e.g. X12 850/810).

---

## 4. System foundation & multi-tenancy [MVP→V1]

### 4.1 Multi-organization / multi-website
- One installation supports multiple seller **Organizations** (brands/business units). [V1]
- Each Organization can run multiple **Websites** (distinct domains, themes, catalogs, currencies, locales). [V1]
- A single Website end-to-end is the MVP target. [MVP]

### 4.2 Localization & currency
- Multi-language content and UI (translation catalogs, RTL support). [V1]
- Multi-currency pricing, with per-website default and customer-level overrides. [V1]
- Locale-aware formatting (dates, numbers, addresses, tax IDs). [MVP basic / V1 full]

### 4.3 Configuration system
- Hierarchical config: global → organization → website → customer-group → customer, with inheritance and override at each level. [V1]
- Admin-editable settings without code deploys. [MVP for core settings]

---

## 5. B2B customer & account model [MVP→V1]

### 5.1 Accounts
- Customer (company) records with billing/shipping addresses, tax IDs, payment terms, assigned price lists, assigned catalog visibility. [MVP]
- **Account hierarchy**: parent/child companies; pricing, catalog, and permissions can inherit down the tree. [V1]

### 5.2 Customer users & roles
- Multiple users per customer, each with a role (Buyer, Approver, Admin). [MVP basic / V1 full]
- Per-user and per-role permissions (who can see prices, place orders, request quotes, manage users, set budgets). [V1]
- Self-registration with seller approval workflow; or sales-rep-created accounts. [V1]

### 5.3 Account-level controls
- Spending limits / budgets per user, with approval routing above threshold. [V1]
- Order approval workflows (buyer submits → approver authorizes → order placed). [V1]
- Assigned sales rep / account manager visibility on seller side. [V1]

---

## 6. Catalog & Product Information Management (PIM) [MVP→V1]

### 6.1 Products
- Simple, configurable (variants via attributes), kit/bundle, and digital product types. [MVP simple+configurable / V1 kit+digital]
- SKU, name, descriptions (short/long, rich text), images/media, datasheets/attachments. [MVP]
- Per-website and per-localization product content. [V1]

### 6.2 Attributes & families
- Custom attribute system (text, number, boolean, select, multiselect, date, file, price, entity reference). [MVP core types / V1 full]
- **Attribute families/sets** so different product categories have different fields. [V1]
- Attributes drive faceted search filters and variant generation. [V1]

### 6.3 Categories & catalog structure
- Hierarchical category tree; products in multiple categories. [MVP]
- Category-level content, sort order, and visibility rules. [V1]
- **Per-customer catalog visibility** (a customer sees only the products/categories assigned to them). [V1]

### 6.4 PIM operations
- Bulk import/export (CSV/XLSX) with validation and async processing via the queue. [MVP import / V1 full pipeline]
- Product data versioning / draft-publish. [V2]
- Inventory levels per product/warehouse (see §10). [MVP single-warehouse / V1 multi]

---

## 7. Pricing engine [MVP→V1]

The pricing engine is the most B2B-distinctive subsystem and the most likely to be under-scoped. Treat it as first-class.

### 7.1 Price lists
- Named price lists assignable to customer, customer group, or website, with **priority/merge strategy**. [V1]
- A single default price list is the MVP. [MVP]

### 7.2 Pricing rules
- Tiered / volume pricing (price breaks by quantity). [MVP]
- Customer-specific and customer-group-specific prices. [V1]
- Rule-based pricing (formulas referencing product attributes, cost, margin). [V1]
- Currency-specific and unit-of-measure-specific prices. [V1]
- Time-bound pricing (promotions, contract validity windows). [V1]

### 7.3 Price calculation
- Deterministic resolution: given (customer, product, quantity, currency, website, date) → final price + applied rule trace. [V1]
- Cached/precomputed combined price lists for storefront performance (recompute via queue on change). [V1]
- Show/hide prices by permission (e.g. guests see no prices). [MVP]

---

## 8. Quotes, RFQ & CPQ [V1]

- **RFQ (buyer-initiated)**: buyer adds items, requests a quote, optionally with target prices/notes. [V1]
- **Quote management (seller-side)**: rep reviews RFQ, adjusts line prices/quantities/discounts, sets validity, sends back. [V1]
- **Negotiation loop**: multi-round back-and-forth with version history. [V1]
- **Quote → Order**: accepted quote converts to order with no re-entry. [V1]
- **CPQ for configurable products**: guided configuration, BOM-level options, price recalculated per configuration. [V2]
- Quote expiry, reminders, and PDF generation. [V1]

---

## 9. Cart, checkout & order management [MVP→V1]

### 9.1 Shopping lists / requisitions
- Multiple named, savable, shareable shopping lists per customer (reorder templates). [V1]
- Quick order by SKU / paste-a-list / CSV upload. [V1]
- Add-to-cart from list; convert list to order or RFQ. [V1]

### 9.2 Cart & checkout
- Single-page or multi-step B2B checkout: shipping address, shipping method, payment method, PO number, requested delivery date. [MVP]
- Save cart, multiple carts, persistent cart across sessions. [V1]
- Guest vs. authenticated behaviour (B2B usually auth-only). [MVP]

### 9.3 Order management (seller-side)
- Order lifecycle states (pending → confirmed → processing → shipped → delivered → closed; plus cancelled/on-hold). [MVP core / V1 full]
- Edit orders pre-fulfilment; split shipments; partial fulfilment. [V1]
- Order placed **on behalf of** a customer by a sales rep. [V1]
- Returns / RMA. [V2]
- Order history, reorder, invoices, tracking visible to buyer. [MVP history / V1 full]

### 9.4 Order-to-cash
- Order → invoice → payment → reconciliation, as one traceable flow. [V1]
- Backorder handling and inventory allocation. [V1]

---

## 10. Inventory & fulfilment [MVP→V1]

- Stock tracking per product (and per warehouse in V1). [MVP single / V1 multi-warehouse]
- Inventory statuses (in stock, out of stock, backorder, discontinued) with storefront display rules. [MVP]
- Allocation/reservation on order; decrement on fulfilment. [V1]
- Low-stock thresholds and notifications. [V1]
- Shipping integration: rate quotes, label/tracking (carrier APIs). [V1]
- Warehouse Management System (WMS) integration hooks. [V2]

---

## 11. Payments & finance [MVP→V1]

- Payment methods: card (gateway), ACH/bank transfer, **invoice / credit terms (pay later)**, purchase order. [MVP card+PO / V1 invoice+ACH]
- Gateway abstraction so multiple processors plug in (see §16). [V1]
- Stored payment methods / tokenization per customer. [V1]
- Credit limit enforcement at checkout. [V1]
- Invoice generation (PDF) and dunning/reminders. [V1]
- Real-time reconciliation with ERP/accounting. [V2]
- PCI-DSS scope minimization (no raw card data stored; gateway tokens only). [MVP — compliance requirement]

---

## 12. CRM [V1]

- Accounts, Contacts, Leads, Opportunities with pipeline stages. [V1]
- Activity tracking (calls, emails, tasks, notes) against records. [V1]
- Unified customer view linking CRM account ↔ commerce customer. [V1]
- Sales rep assignment, territories, and dashboards. [V1]
- Field-sales app access, including offline mode. [V2]
- Lead capture from storefront (registration, RFQ, contact forms). [V1]

---

## 13. Content management (CMS) [V1]

- Landing pages and content pages with a block/widget builder. [V1]
- WYSIWYG editor with media embedding (reuse patterns you already know from your Contracts editor work). [V1]
- Menus/navigation management per website. [V1]
- Per-customer-group content targeting. [V2]
- SEO controls: meta, slugs, canonical, sitemap.xml, robots, structured data. [MVP basics / V1 full]
- Digital Asset Management (DAM): central media library, reuse, transformations. [V2]

---

## 14. Search & navigation [MVP→V1]

- Full-text product/content search backed by **PostgreSQL FTS** (`tsvector`/`tsquery` with GIN indexes) in MVP → OpenSearch/Elasticsearch only if scale or relevance tuning demands it. [MVP Postgres FTS / V1 external index if needed]
- Faceted filtering driven by product attributes (JSONB attributes + indexed columns). [V1]
- Autocomplete/suggestions, synonyms, typo tolerance. [V1]
- Per-customer catalog visibility respected in search results. [V1]
- Sorting (price, relevance, name, newest) and pagination. [MVP]

---

## 15. Workflow & automation engine [V1]

This is the "glue" that distinguishes the platform — a configurable engine rather than hardcoded flows.

- Entity workflows: define states, transitions, guards, and actions for any entity (order, quote, RFQ, lead). [V1]
- Approval processes with routing rules and escalations. [V1]
- Event-driven automation: triggers (entity created/updated, threshold crossed) → conditions → actions (email, status change, webhook, task). [V1]
- Low-code/admin-configurable where feasible; developer-extensible for complex logic. [V1 admin-config is V2]
- Scheduled jobs (cron-like) via the Postgres-backed queue (price recompute, reindex, reminders, ERP sync). [MVP infra / V1 features]

---

## 16. Integrations [V1→V2]

A pluggable integration framework (adapters + a normalized internal event/webhook bus) so each integration is an additive module.

| Integration class | Examples | Phase |
|---|---|---|
| **Payment gateways** | Stripe, PayPal, regional gateways (e.g. M-Pesa/Daraja, Flutterwave, Pesapal for the East Africa context) | [MVP one / V1 several] |
| **Shipping carriers** | Carrier rate/label/tracking APIs; regional couriers | [V1] |
| **Tax** | Avalara/TaxJar-style services; configurable tax rules engine for local VAT (e.g. KRA-compatible) | [V1] |
| **ERP / accounting** | SAP, Microsoft Dynamics, NetSuite, Epicor, QuickBooks, Zoho; or your own systems | [V2] |
| **CRM (external)** | Salesforce, HubSpot (if not using built-in CRM) | [V2] |
| **Email / marketing** | SMTP/transactional (SES, Mailgun), marketing automation | [MVP transactional / V1 marketing] |
| **Identity / SSO** | OAuth2/OIDC, SAML, LDAP for B2B buyer SSO | [V2] |
| **Procurement punchout** | OCI / cXML for SAP Ariba, Coupa | [V2] |
| **EDI** | X12 850 (PO) / 810 (invoice), EDIFACT | [V2] |
| **Search** | PostgreSQL FTS (MVP); OpenSearch/Elasticsearch only if needed | [MVP / V1 if needed] |
| **PDF** | Headless-Chromium service (Gotenberg-style) for invoices/quotes | [MVP] |

Integration framework requirements: retry with backoff, idempotency keys, dead-letter handling, per-integration audit log, sandbox/test mode, and a webhook subscription system for outbound events.

---

## 17. API & extensibility [MVP→V1]

> **Architecture note:** With a Go backend and Vue/Nuxt frontends, the platform is **API-first / headless from day one**. The frontends cannot render without the API, so the storefront API is an MVP necessity (it was V1 in v0.1). The Go service is the single source of truth; both the Nuxt storefront and the Vue admin are pure API consumers.

- REST API (JSON:API style) covering all major entities; documented (OpenAPI). [MVP core entities / V1 full]
- **Storefront API** (catalog, cart, checkout, customer account) consumed by the Nuxt SSR storefront and any future mobile client. [MVP]
- Outbound webhooks for integration events. [V1]
- Authentication: token/OAuth2 for the admin SPA and API clients; session/cookie or token strategy for the SSR storefront; scoped permissions throughout. [MVP]
- Modular architecture (clear Go package/module boundaries) so features and integrations are additive. [MVP foundational decision]
- Theming/branding driven by the Nuxt layer (components + design tokens), no core API edits required. [V1]

---

## 18. Administration & access control [MVP→V1]

- Role-based access control (RBAC) with granular, entity-level permissions (ACL). [MVP roles / V1 full ACL]
- Back-office admin UI: dashboards, data grids (filter/sort/export), entity CRUD, bulk actions. [MVP core / V1 polished]
- Audit log of admin and data changes. [V1]
- Impersonation (admin/rep "view as customer") with audit trail. [V1]
- System health: queue monitoring, job status, integration status. [V1]

---

## 19. Reporting & analytics [V1→V2]

- Operational dashboards (sales, orders, top products, customer activity). [V1]
- Custom report builder (pick entity, columns, filters, group/aggregate). [V2]
- Segmentation (customer segments for targeting/pricing). [V2]
- Scheduled report exports/emails. [V2]
- BI tool integration (data export / read replica for external BI). [V2]

---

## 20. Non-functional requirements

### 20.1 Performance
- Storefront product/category pages: rendered server-side by Nuxt (SSR), cached, target < 500 ms TTFB under normal load. [MVP target]
- Price/catalog precomputation moved off the request path (Postgres-backed queue + workers). [V1]
- Search and faceting responsive at catalog sizes of 100k+ SKUs (Postgres GIN indexes; external index if needed). [V1]
- Go's concurrency model keeps integration sync and batch jobs cheap to parallelize. [inherent]

### 20.2 Scalability
- Stateless Go API behind a load balancer; horizontally scalable (single static binary per instance). [V1]
- Async work (email, import, reindex, integration sync) via the Postgres-backed queue + scalable workers. [MVP infra]
- Postgres read replicas (and an external search node) added as load grows. [V2]
- Nuxt storefront scales independently of the API tier. [V1]

### 20.3 Availability & operations
- Zero-downtime deploys (or maintenance-window strategy in MVP). [V1]
- Backups (DB + media) with tested restore; documented RPO/RTO. [MVP]
- Observability: structured logs, metrics, error tracking, queue/job dashboards. [V1]
- Cache-busting strategy for static assets (versioned/hashed filenames). [MVP — you already use md5sum-based hashing; reuse it]

### 20.4 Security
- PCI-DSS scope minimized (tokenized payments, no PAN storage). [MVP]
- OWASP Top 10 mitigations; CSRF on the storefront, XSS protection in Nuxt, parameterized SQL (sqlc/sqlx) to prevent injection. [MVP]
- Encryption in transit (TLS) and at rest for sensitive fields. [MVP]
- Secrets management (no secrets in repo). [MVP]
- Rate limiting and brute-force protection on auth/API (chi middleware). [V1]
- Per-tenant/customer data isolation enforced at the query layer. [V1]

### 20.5 Compliance & accessibility
- GDPR-style data handling (consent, export, deletion). [V1]
- Local tax/e-invoicing compliance (e.g. KRA requirements, VAT). [V1]
- WCAG 2.1 AA storefront accessibility. [V1]
- SEO: clean URLs, sitemaps, structured data, **Nuxt SSR (not a client-only SPA) so storefront pages are crawlable**. The admin is a pure Vue SPA — no SEO concern, login-gated. [MVP]

---

## 21. Technology stack (decided)

Stack is committed: **Go** (API) · **PostgreSQL** · **Vue** (admin SPA) + **Nuxt** (storefront SSR). The architecture is API-first / headless — see §17.

| Layer | Choice | Notes |
|---|---|---|
| Backend | **Go** | Single static binary, cheap concurrency for workers/sync. Trades framework leverage for control (see §24). |
| HTTP router | **chi** | Stdlib-`http.Handler` based, no lock-in, explicit middleware. (Echo is the batteries-included alternative.) |
| DB access | **sqlc** (or sqlx) | Type-safe Go generated from hand-written SQL. Keeps the pricing/hierarchy queries explicit and avoids ORM-induced N+1. |
| Database | **PostgreSQL 16+** | JSONB for product attributes, recursive CTEs for account/category trees, FTS for search. |
| Migrations | **goose** or golang-migrate | Versioned schema. |
| Job queue | **river** (Postgres-backed) | No extra infra — jobs live in Postgres. (asynq is the Redis-backed alternative.) |
| Search | **Postgres FTS** (`tsvector` + GIN) | Defer OpenSearch/Elasticsearch until scale/relevance demands it. |
| Cache & sessions | **Redis (optional)** | Add when caching/session needs justify it; not required for MVP. |
| Admin frontend | **Vue 3 SPA** | Login-gated, no SEO concern. Data grids, CRUD, dashboards. |
| Storefront frontend | **Nuxt (Vue SSR)** | Server-rendered for crawlability; consumes the storefront API. |
| PDF | Gotenberg (headless Chromium) | Invoices, quotes. |
| Web/edge | Nginx (reverse proxy / TLS) | Fronts the Go API and Nuxt server. |
| Deploy | Docker Compose (single host MVP) → orchestrated later | |

### Why this shape works for you
- **Lean MVP infra.** Postgres pulls triple duty — data, job queue (river), and search (FTS) — so MVP is essentially Go + Postgres + Nuxt. Three moving parts instead of six. Redis and a search cluster are added back only when load demands.
- **Explicit over magic.** `sqlc` + chi keep handlers and queries readable, which matters most in the pricing engine (§7) and hierarchy queries (§5, §6) — the places an ORM quietly hurts.
- **SEO is preserved** by splitting the frontend: Nuxt SSR for the storefront, plain Vue SPA for the admin. This keeps the reference platform's deliberate "storefront is not a client-only SPA" property (§20.5) while letting you work in Vue.

### Architectural guidance
- **Modular monolith first** — clear Go package boundaries inside one service, not microservices. Microservices are a V2+ concern, if ever.
- Treat the Go service as the single source of truth; both frontends are API consumers with no business logic of their own.
- Reuse your hard-won build patterns (md5-based cache-busting, scripted build steps) in the Nuxt/Vite asset pipeline.

---

## 22. High-level data model (core entities)

```
Organization 1───* Website 1───* (Catalog, PriceList, Currency, Locale)

Customer (Account) 1───* CustomerUser
Customer *───* PriceList (with priority)
Customer 1───* Address
Customer ──parent/child── Customer        (hierarchy)

Product *───* Category
Product 1───* ProductVariant
Product *───* Attribute (via AttributeFamily)
Product 1───* InventoryLevel ───* Warehouse
Product 1───* Price ───* PriceList

ShoppingList 1───* LineItem ───* Product
RFQ 1───* RFQLineItem ; RFQ ──> Quote ──> Order
Order 1───* OrderLineItem ; Order 1───* Shipment ; Order 1───* Invoice ; Invoice 1───* Payment

Lead, Opportunity, Activity ──> linked to Customer/Contact   (CRM)
Workflow ─ states/transitions ── applied to any entity
User ─ Role ─ Permission (ACL)
IntegrationConnection 1───* SyncLog ; WebhookSubscription
```

A formal ERD should be produced as a follow-up deliverable per module before implementation. Two Postgres-specific modeling notes: product attributes (§6.2) are a good fit for **JSONB** with GIN indexes rather than a relational EAV sprawl, and the parent/child **Customer** and **Category** relationships are queried with **recursive CTEs** rather than application-side tree-walking.

---

## 23. Phased roadmap

### Phase 0 — Foundations (infra & skeleton)
Modular monolith skeleton, auth, RBAC roles, admin shell, data grids, queue + worker, Redis cache, Docker deploy, CI, backups. *No business features yet — this is the platform spine.*

### Phase 1 — MVP: single store, end-to-end B2B
Customers + customer users, catalog (simple + configurable products, categories, attributes core), single price list with tiered pricing, search (FULLTEXT), cart, B2B checkout (card + PO), order lifecycle (core states), single-warehouse inventory, invoice PDF, transactional email, core REST API, server-rendered storefront with SEO basics. *Outcome: a customer can log in, see their prices, order, and get an invoice.*

### Phase 2 — V1: the real product
Multi-website/multi-org, account hierarchy, full price-list engine (customer-specific, rule-based, multi-currency), RFQ + quote management + negotiation, shopping lists/requisitions, approval workflows, workflow/automation engine, CRM, CMS + WYSIWYG, indexed faceted search, multi-warehouse inventory + shipping integration, invoice/ACH payments + credit limits, reporting dashboards, full ACL + audit, theming, storefront API. *Outcome: a competitive B2B platform.*

### Phase 3 — V2: enterprise & scale
Marketplace/multi-vendor, CPQ for configurable products, deep ERP/accounting sync, punchout (OCI/cXML), EDI, SSO/SAML, DAM, custom report builder + BI, field-sales offline app, returns/RMA, advanced scalability (replicas, multi-node), content versioning. *Outcome: parity with the reference enterprise edition.*

---

## 24. Team & effort (order-of-magnitude)

This is a planning estimate, not a commitment — actuals depend heavily on scope discipline and reuse.

> **Go stack adjustment:** because Go has no batteries-included commerce/admin framework (unlike Symfony+Oro or Laravel), the **admin back-office (§18), CMS (§13), and data-access scaffolding are more hand-built**. Budget extra time in Phase 0–1 for foundations you'd otherwise inherit: the data-grid/CRUD admin layer, the workflow engine, and the integration framework. The runtime and concurrency benefits are real, but the framework leverage is lower — net effect nudges the estimates below upward, not down.

| Phase | Indicative effort | Suggested team |
|---|---|---|
| Phase 0 | 2–4 months | 1–2 senior engineers |
| Phase 1 (MVP) | 6–10 months | 2–3 engineers + part-time PM/QA |
| Phase 2 (V1) | 12–18 months | 4–6 engineers + PM + QA + designer |
| Phase 3 (V2) | 18+ months | larger team, possibly multiple squads |

A single developer building the full scope solo should expect a multi-year timeline and should ruthlessly prioritize MVP, or seriously reconsider adopting an existing open-source base.

---

## 25. Risks & assumptions

| Risk | Impact | Mitigation |
|---|---|---|
| Scope underestimation (pricing engine, workflow engine) | High | Treat these as first-class; build incrementally; phase aggressively |
| Building everything before shipping anything | High | Enforce MVP gate; ship Phase 1 to a real customer before Phase 2 |
| Integration sprawl | Medium | Build one solid integration framework; add adapters one at a time |
| Performance at catalog scale | Medium | Move pricing/catalog computation off request path early; index search |
| Solo/small-team burnout | High | Honest roadmap; consider open-source base for non-differentiating modules |
| PCI/compliance gaps | High | Tokenized payments only; never store card data; design compliance in from MVP |

### Assumptions
- Self-hosted Linux deployment; ops handled by the same small team.
- B2B is the design centre; B2C is secondary.
- An existing accounting/ERP and payroll system exist and are integrated with, not rebuilt.
- The team is comfortable with Go, PostgreSQL, and Vue/Nuxt, and with an API-first (headless) architecture.

---

## 26. Success metrics (KPIs)

- **Adoption**: % of orders placed through the platform vs. legacy channels (phone/email).
- **Self-service rate**: orders completed without sales-rep intervention.
- **Quote velocity**: median RFQ → accepted-quote time.
- **Order accuracy**: % orders without manual correction.
- **Storefront performance**: p95 page TTFB; search latency.
- **Reorder rate**: % of orders from saved shopping lists.
- **Integration health**: sync success rate; mean time to recover failed syncs.
- **Time-to-launch**: a new website/brand stood up in days, not months.

---

## 27. Open questions (to resolve before build)

1. Build in-house vs. adopt OroCommerce CE / ERPNext as a base — is this decision final? (Stack is now Go/Postgres/Vue, which only makes sense on the build-in-house path.)
2. chi vs. Echo for the Go HTTP layer — chi is recommended (stdlib-based, no lock-in); confirm before scaffolding handlers.
3. Which single payment gateway anchors the MVP (regional context suggests M-Pesa/Daraja or Stripe)?
4. Is the built-in CRM required in V1, or can an external CRM (HubSpot/Zoho) cover it initially?
5. What is the target first customer/use case the MVP must satisfy? (This should prune the MVP scope further.)
6. Multi-website needed at launch, or single store acceptable for V1?

---

*End of PRD v0.2. Each module in Sections 4–19 can be expanded into its own detailed spec (user stories, acceptance criteria, wireframes, ERD) as a follow-up.*
