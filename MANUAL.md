# Teggo — In-House B2B Commerce Platform

*The operating system for selling to other businesses.*

---

## The pitch

Most "ecommerce platforms" are built for consumers — one shopper, one price, one card, one checkout. **B2B doesn't work that way.** A distributor sells the same SKU to 400 companies at 400 negotiated prices, on net‑30 terms, through approval chains, against purchase orders and budgets, often via the buyer's procurement system — and then has to invoice, chase payment, handle returns, and keep a sales rep in the loop the whole time.

Teggo is a **single, self‑hosted platform that runs that entire motion** — catalog → quote → order → fulfilment → invoice → cash → returns — with the B2B realities built in rather than bolted on: per‑customer pricing, company accounts with sub‑users and spending limits, RFQ/quote negotiation, approval workflows, multi‑warehouse stock, tax/shipping, EDI & punchout, ERP sync, and a revenue‑operations layer (AR aging, dunning, replenishment, churn signals, budgets).

It ships as **one Go backend, one Postgres database, and two front‑ends** (an admin back‑office and a buyer storefront) that talk to the same typed API contract. No microservice sprawl, no per‑seat SaaS lock‑in. You own the code and the data.

---

## Who it's for

- **Distributors, wholesalers, and manufacturers** selling to business customers.
- **Sellers** who need price lists, quotes, terms, approvals, and ERP/EDI — not just a "buy now" button.
- **Buying organizations** who want self‑service: reorder, quick‑order, requisitions, approvals, budgets, and their own user management.

---

## What it does — the capability tour

### 1. Catalog & product information (PIM)
A proper product catalog with categories (browsable as a tree), attributes and attribute families, and JSON‑backed faceted search/filtering. Products carry SKUs, units, descriptions, and arbitrary attributes. **Per‑customer/group catalog visibility** means you can hide restricted lines from buyers who shouldn't see them — enforced on listings, search, and product pages alike.

### 2. Pricing that matches reality
- **Price lists** with tiered (volume‑break) pricing, assigned to a customer, a customer group, or a website default.
- **Hierarchy inheritance** — a child company can inherit its parent's negotiated pricing.
- **Rule‑based adjustments** — percent or fixed markups/markdowns scoped by customer group and/or product attribute, applied on top of the resolved price.
- A precomputed **combined‑prices cache** keeps storefront pricing fast, and buyers see their **contract price tiers** ("buy 100+ at X") right on the product page.
- No price for a buyer? It cleanly becomes a **"price on request" → RFQ**.

### 3. The B2B sales motion: RFQ → Quote → Order → Invoice → Payment
- Buyers raise **RFQs**; reps turn them into **quotes**, negotiate, send, and the buyer **accepts** to create an order — all as an auditable lifecycle.
- **Configure‑Price‑Quote (CPQ)** for configurable products: option groups, options, rules, and price deltas, addable as configured lines on a quote.
- Orders flow through a **configurable workflow** (pending → confirmed → processing → shipped → delivered, with holds), confirming stock reservation along the way.
- **Invoices** are issued (with PDFs), **payments** recorded (card via gateway, or pay‑on‑terms), and credit limits enforced.

### 4. Company accounts & buyer self‑service
A customer is a **company**, not a person. Each has a hierarchy, **sub‑users with roles** (buyer / approver / admin) and **spending limits**, saved addresses, and shopping lists. Buyers get a real self‑service storefront:
- **Quick order** (paste/upload SKUs), **reorder** a past order in one click, **shopping lists**.
- **Multi‑address checkout**, PO numbers, requested delivery dates.
- **Order approvals** — over‑limit orders hold for a company approver; **tiered approval routing** can require a higher role for larger amounts.
- **Company administration** — invite/manage users, set roles and limits — without calling the seller.

### 5. Inventory & multi‑warehouse fulfilment
Stock is tracked per warehouse with a movement ledger as the source of truth. Orders reserve stock on confirm; **shipments** (full or partial) draw from an assigned warehouse and convert reservations to fulfilment. Buyers can see **per‑warehouse availability** on the product page.

### 6. Returns / RMA + credit notes
The post‑sale path: buyers request a return, admins approve → receive (which **restocks inventory and issues a credit note**) or reject — with per‑line returnable caps so you never credit more than was bought.

### 7. CRM for the sales team
Leads (including a **public storefront enquiry form**), a configurable opportunity **pipeline** with weighted forecasting, contacts, and a unified activity timeline. Plus **account‑health / churn‑risk signals** that flag accounts slipping below their own ordering pattern, so reps act before the account is lost.

### 8. Content (CMS)
Block‑based content pages, with **per‑customer‑group targeting** so different buyer segments can see different content.

### 9. Workflow & automation engine
Entity lifecycles (orders, shipments, quotes) are **config‑driven state machines** with guards (e.g. "amount within limit", "actor has permission") and actions — editable without a deploy. An **automation rule builder** fires actions on events/schedules: email the customer, expire stale quotes, **mark invoices overdue + dun**, **follow up on expiring quotes**, **recover abandoned carts**.

### 10. Revenue operations
The layer that protects cash and revenue:
- **AR aging + dunning** — invoices flip to overdue and chase themselves; an aging dashboard buckets receivables.
- **Replenishment** — reorder reminders inferred from each buyer's cadence.
- **Quote follow‑up & cart recovery** — automated nudges before quotes expire / carts go cold.
- **Procurement budgets** — per cost‑center spend caps, enforced at checkout, visible to the buyer.

### 11. Integrations
- **Punchout (cXML/OCI)** — buyers shop from inside their procurement system and transfer the cart back.
- **EDI (X12)** — inbound 850 (PO) → order; outbound 855/856/810 (ack/ASN/invoice).
- **ERP/accounting sync** — a signed‑webhook connector pushes orders/invoices out and ingests inventory updates, idempotently.
- **Tax & shipping** — rules‑based local providers for VAT and table‑rate shipping with label/tracking, behind adapter interfaces so a real Avalara/FedEx‑style provider drops in.
- **SSO** — OIDC and SAML, with JIT provisioning for both staff and buyers, and SP metadata.
- **DAM** — asset uploads with an image‑transformation pipeline and signed delivery URLs.
- **Field sales** — an offline‑sync protocol (cursor + idempotency + change‑log outbox) for reps in the field.

### 12. Reporting
A safe report builder (pick entities, dimensions, measures, filters), scheduled CSV exports, and dashboard metrics.

### 13. Multi‑vendor marketplace
Run the platform as a **marketplace**, not just a single seller. Vendors are first‑class: each has a profile, a commission rate, payout terms, and **its own self‑service portal** (a third login audience alongside admin and buyer). Vendors **list their own products**, which the operator **moderates** before they go live — unapproved listings never appear in catalog, search, or cart. A single buyer order **splits automatically into per‑vendor sub‑orders**, each with a frozen **commission snapshot** (gross → operator commission → net payable); vendors advance their own fulfilment (accept → ship → deliver). The operator **batches delivered sub‑orders into payouts** and marks them paid; vendors track their dashboard (orders, gross, commission, net) and payout history. Buyers see **"Sold by ‹vendor›"** on the product page. Operator‑owned ("house") products coexist untouched, so it's a marketplace and a first‑party store at once.

### 14. AI assistant (copilot)
A **deterministic, tool‑calling assistant** built into both experiences — a buyer copilot in the storefront and an ops copilot in the admin. **Safety is structural**, not aspirational: the agent can only ever invoke a **fixed catalog of typed, permission‑gated tools** that wrap the existing services, each running under the *caller's own* org/audience/permission scope — it never writes free‑form to the database and can do nothing the caller couldn't already do. Buyers ask "where's my order?", "what do I owe?", "what should I reorder?", "how's my budget?"; staff ask "show the receivables aging", "which accounts are at risk?", "look up order …" (each gated by the matching permission). The decision engine is **pluggable**: a local **deterministic** engine (intent + slots, fully reproducible, zero external calls — the default) or the **Anthropic Claude** API behind the same interface and the same tool guards, switched on only when an API key is configured. Same architecture as every other external seam.

---

## Two experiences, one platform

**The storefront (buyers).** Server‑rendered for SEO, fast, and self‑service: browse/search, see *their* prices and stock, quick‑order and reorder, manage lists and company users, request quotes and returns, track orders and invoices, approve orders, watch budgets.

**The admin (sellers).** A back‑office for the whole operation: catalog/PIM, pricing & rules, RFQs/quotes/orders/invoices, AR aging & returns, CRM & account health, inventory & shipments, CMS, workflow & automation builders, approval routing, integrations, SSO, reporting, and tenancy/settings.

---

## Architecture (for the technical evaluator)

- **Backend:** Go — `chi` HTTP router, `sqlc`‑generated type‑safe queries over `pgx`, a `river` background‑job queue, a custom workflow engine, and a domain‑event/automation dispatcher.
- **Database:** PostgreSQL. Money is exact decimal (no float drift). Schema evolves through ordered SQL migrations (currently 0001–0039).
- **Front‑ends:** a Vue 3 + PrimeVue admin SPA and a Nuxt storefront — both consuming a **single OpenAPI contract** from which the TypeScript client is generated, so the API and UI can never silently drift.
- **Multi‑tenancy & security:** organization‑scoped data taken from JWT claims (never the request body), audience‑gated tokens (admin vs storefront), permission‑gated admin routes, rate‑limited credential and public endpoints, signed capability URLs for documents, and HMAC‑signed integration webhooks.
- **Quality:** the Go suite runs against real Postgres via testcontainers; every feature lands with tests; the API client and both front‑ends typecheck clean.

---

## Status & roadmap

**Built and tested today:** everything above — the full buyer‑self‑service experience, the data‑model depth (catalog visibility, multi‑warehouse, rule‑based pricing, hierarchical config), the integration surfaces (EDI, punchout, ERP webhook, OIDC+SAML, DAM, field sync), the revenue‑ops layer (AR/dunning, replenishment, quote/cart recovery, returns, account health, budgets), the **full multi‑vendor marketplace** (vendor portal, order splitting, commission ledger, payouts, catalog moderation), and the **deterministic AI copilot** (buyer + staff, permission‑gated tool catalog, deterministic engine with an optional Claude adapter).

**Adapter seams ready, real provider pending credentials:** external tax (Avalara/TaxJar), carriers (FedEx/UPS/DHL), and bespoke ERPs ship as tested sandbox/mock adapters behind stable interfaces — wire a live account and they slot in.

**Deliberately deferred:** a **real payment processor adapter** (Stripe / M‑Pesa Daraja — the `gateway.Gateway` interface and a deterministic mock exist).

---

## Demo credentials (development seed)

On a freshly migrated database the seed data provisions one working login per surface:

| Surface | App | Email | Password |
|---|---|---|---|
| Admin back‑office | `web/admin` | `admin@demo.test` | `admin1234` |
| Buyer storefront | `web/storefront` | `buyer@demo.test` | `buyer1234` |
| Vendor portal | `web/vendor` | `vendor@demo.test` | `vendor1234` |

All belong to org `1`. Change or remove these seeds for production.

---

*Teggo is in‑house, self‑hosted, and contract‑first: your catalog, your customers, your pricing, your data — running the way B2B actually sells.*
