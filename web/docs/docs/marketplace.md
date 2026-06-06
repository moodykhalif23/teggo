---
title: Marketplace
sidebar_position: 12
---

# Multi-vendor marketplace

Teggo runs as a **marketplace** as well as a first-party store: third-party vendors sell through the same catalog, orders fan out to them automatically, and the operator settles them via commissions and payouts. It is fully additive — products with no vendor stay **operator-owned ("house")** and behave exactly as before.

`internal/modules/marketplace` (+ `internal/store/queries/marketplace.sql`, migrations **0041–0042**).

## Vendors and the third audience

A **vendor** has a profile, a `commission_rate` (the operator's take, percent), `payout_terms_days`, and a status (`pending` / `active` / `suspended`). Vendors get their **own portal login**: a **third JWT audience** — `vendor` — alongside `admin` and `storefront` (`auth.Issuer.IssueVendor`, `Claims.VendorID`). `POST /vendor/auth/login` authenticates `vendor_users`; `mw.RequireAudience("vendor")` gates the portal routes.

| Audience | Subject | Carries |
|---|---|---|
| `admin` | user id | permissions |
| `storefront` | customer-user id | `cust_id` |
| `vendor` | vendor-user id | `vendor_id` |

## Order splitting + commission ledger

When an order is created (cart checkout, quote acceptance, or rep on-behalf), `marketplace.SplitOrder` runs **inside the same transaction**: it groups the order's lines by their product's vendor, tags each line with its owning vendor, and creates one **`vendor_orders`** row per vendor with a **frozen commission snapshot**:

```
gross      = Σ line row_total (ex-tax) for that vendor
commission = gross × rate / 100      (exact, money.Percent)
net        = gross − commission       (payable to the vendor)
```

Operator-owned lines are skipped, so a pure single-seller order produces no vendor sub-orders. The snapshot is frozen at split time, so later rate changes never rewrite history.

## Vendor self-service portal

Audience `vendor`, every route scoped to the vendor in the token:

- `GET /vendor/me`, `GET /vendor/dashboard` (lifetime gross / commission / net)
- `GET /vendor/products`, `POST /vendor/products`, `PUT /vendor/products/{id}`, `POST /vendor/products/{id}/submit`
- `GET /vendor/orders`, `GET /vendor/orders/{id}`, `PATCH /vendor/orders/{id}/status` (fulfilment: `pending → accepted → shipped → delivered`, or `cancelled`)
- `GET /vendor/payouts`

The portal ships as a dedicated app: **`web/vendor`** (Vite + Vue 3 + PrimeVue), `make vendor` → `http://localhost:5174`.

## Catalog ownership + operator moderation

Vendors list their own products; the operator moderates before they go live. Products carry `vendor_id` (NULL = house) and `approval_status` (`draft` / `pending` / `approved` / `rejected`; house products default `approved`).

The **storefront approval guard** is the key invariant: every public product read (search, faceted search, facets, category browse, by-slug, list/count) and both cart-add paths require `approval_status = 'approved'`. An unapproved vendor listing can **never** appear in catalog, search, or a cart.

Operator endpoints (`vendor.view` / `vendor.manage`):

- `GET /admin/products/pending` — moderation queue
- `POST /admin/products/{id}/approve` · `POST /admin/products/{id}/reject`

Buyers see **“Sold by ‹vendor›”** on the product page (`StorefrontProduct.sold_by`).

## Payouts

The operator batches a vendor's **delivered, not-yet-paid** sub-orders into a single payout (`amount = Σ net_total`) — totalled and attached in one transaction so the settled set equals the totalled set:

- `POST /admin/vendors/{id}/payouts` — generate (`422` when nothing is due)
- `POST /admin/payouts/{id}/pay` — mark paid (idempotent)
- `GET /admin/vendors/{id}/payouts` · `GET /vendor/payouts`

## Admin management

Under **Marketplace** in the admin SPA (`vendor.view` / `vendor.manage`):

- `GET/POST/PUT/DELETE /admin/vendors` (CRUD, commission, payout terms, status)
- `GET/POST /admin/vendors/{id}/users` (portal logins)
- the **Vendors** and **Catalog moderation** views

## Demo

The seed (`migration 0042`) provisions a working vendor out of the box:

| | |
|---|---|
| Vendor portal | `vendor@demo.test` / `vendor1234` |

It owns the seeded `PIPE-200` product (approved), so the storefront shows “Sold by Demo Vendor Co” and the portal has a live catalog.
