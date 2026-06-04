---
title: Integrations
sidebar_position: 9
---

# Integrations

Teggo integrates with payment processors, tax/shipping providers, procurement systems, identity providers, and ERPs. The pattern: a **Go interface** (the adapter contract) with a **rules-based local implementation** that needs no external service, so the platform is fully functional self-hosted — and a real provider drops in behind the same interface + a connection row.

## Payments

`internal/payments/gateway` — `Gateway` interface (`CreateCharge`, `Provider`). Built-in **`Mock`** (deterministic; declines on "decline" tokens). Storefront card pay records a `payments` row idempotently (unique `(gateway, gateway_reference)`). Real Stripe / M-Pesa (Daraja) adapters are the documented next step.

## Tax & shipping

- **`internal/tax`** — `LocalVAT` computes per-line tax from `(country, tax_class)` rates; wired into order creation (per-line `tax_amount` + order `tax_total`). 0 when no rates configured — so untaxed orgs are unaffected.
- **`internal/shipping`** — `Local` table-rate provider (`Rates` with free-over threshold, `CreateLabel`, `Track`); rate quotes feed checkout, labels write `shipments.tracking_number`.

Admin manages rates under **Tax & Shipping**.

## Punchout + EDI

`internal/cxml` + `internal/edi` + `internal/modules/integration`:

- **Punchout (cXML)** — `setup` (shared-secret auth) → `start` (mints a storefront session + cart) → `transfer` (returns a `PunchOutOrderMessage`).
- **EDI (X12)** — inbound **850** → order (all-or-nothing) + **855** ack; outbound **810** (invoice) / **856** (ASN). Trading partners + an EDI document log live under **Integrations**.

## ERP / accounting sync

`internal/erp` + `internal/modules/erp` — a generic **signed-webhook** connector:

- **Outbound** — an idempotent sweep posts confirmed orders + issued invoices to the connection endpoint (recorded in `external_refs` / `sync_logs`).
- **Inbound** — `POST /webhooks/erp/{id}` (HMAC-verified) applies master data (e.g. inventory), deduped by event id.

## SSO

`internal/sso` — OIDC (authorization-code, JWKS verify) and SAML (signed-assertion verify via gosaml2), with JIT provisioning. See **[Auth & RBAC](./auth-rbac.md#sso-oidc--saml)**.

## The adapter principle

Every integration is **swappable**: a new provider is a new adapter + a connection/config row, with **no changes to domain code**. Outbound calls are idempotent and queued; inbound webhooks are signature-verified and deduped.
