# Teggo — Feature Roadmap

Prioritized plan for closing the gaps between Teggo and the day-to-day feature set
of mature B2B commerce suites (OroCommerce, Salesforce B2B Commerce). The MVP spine
and most operational modules already exist (see [README.md](README.md)); this doc
covers what's **missing or thin**, sequenced by impact ÷ effort.

Every item follows the repeatable module pattern in README "Adding a backend module":
`migration → internal/store/queries → make generate → internal/modules/<name>/handler.go
→ mount in server.go → (async jobs) → integration tests → OpenAPI → typed client → screens`.

Legend — **Impact**: how much day-to-day B2B value. **Effort**: rough build size.

---

## Tier 1 — ✅ DONE

> All three Tier 1 items shipped. Coverage: engine unit tests + integration tests
> (cart coupon, checkout persistence, admin CRUD), OpenAPI + typed client, admin
> screens, storefront coupon field. Migration `0046_promotions` applied.
>
> **Promotions v1 scope shipped:** percent / fixed-amount cart discounts, optional
> coupon code, minimum-subtotal threshold, schedule window, global redemption cap,
> single best-value promotion applied at checkout, redemption tracking.
> **Deferred (future):** stacking with priority/exclusivity, buy-X-get-Y, free
> shipping, per-customer redemption caps, discount reducing the taxable base
> (v1 applies discount post-tax).

### 1. Promotions / discounts / coupons  ·  ✅ Done · Impact: High · Effort: M–L
The single biggest missing pillar. Today pricing is static (price lists + adjustment
rules); there's no cart-level promotion or coupon concept.
- **Data**: `promotions` (type, scope, conditions JSONB, action, schedule, usage limits),
  `coupon_codes` (code, promotion_id, per-customer/global caps, redemptions).
- **Engine**: evaluate active promotions at cart calculation time — order-percentage,
  fixed-amount, buy-X-get-Y, free shipping, threshold discounts. Stackable rules with
  priority + exclusivity.
- **Surfaces**: admin Promotions CRUD + a rule builder (reuse the automation flow-builder
  pattern); storefront coupon-code field in cart/checkout; show applied discounts on the
  cart, quote, and order.
- **Touches**: cart calc, quote totals, order-to-cash, invoices.

### 2. Sales rep "order / quote on behalf of"  ·  ✅ Done · Impact: High · Effort: S–M
Partial today (admin can create an order on behalf). Make it first-class for assisted selling.
- **Data**: record `placed_by_user` / acting-rep on orders & quotes (some fields exist).
- **Behavior**: rep impersonation/“act as customer” session scoped to a customer, with an
  audit trail; carts and pricing resolve as that customer.
- **Surfaces**: a customer-context switcher in admin; “Create order/quote for {customer}”
  flows; clearly badge rep-placed orders.

### 3. KPI / inventory insight endpoint  ·  ✅ Done · Impact: Med-High · Effort: S
The dashboard wants an **org-wide low-stock list** but inventory is per-product
(`/admin/inventory/{productId}`) — there's no “levels at/under reorder threshold” query.
- Add `GET /admin/inventory/low-stock` (join stock levels → products, org-scoped,
  `on_hand <= reorder_threshold`), then add a Low-stock widget to the dashboard.
- Cheap, unblocks a daily-ops widget already designed for.

---

## Tier 2 — strong value, medium effort

### 4. Subscriptions / recurring & standing orders  ·  Impact: High · Effort: L
Auto-replenishment is core to B2B repeat buying; today only manual reorder + lists.
- **Data**: `subscriptions` (customer, cadence, next_run, line items, status), `subscription_runs`.
- **Engine**: a river job that materializes due subscriptions into orders on schedule.
- **Surfaces**: storefront “subscribe / set up recurring” on a list or past order; admin
  management + skip/pause/cancel.

### 5. Multi-currency display & FX  ·  Impact: Med · Effort: M
Currency columns exist per price-list, but there's no presentation/FX layer.
- **Data**: `fx_rates` (base, quote, rate, as_of); website/customer default currency
  (website already has `default_currency`).
- **Behavior**: resolve display currency per website/customer; convert where a native
  price list isn't defined; lock FX at order placement.
- **Surfaces**: currency selector; show prices/totals in buyer currency end-to-end.

### 6. Search merchandising  ·  Impact: Med · Effort: M
Search is Postgres FTS only — no curation.
- **Data**: `search_synonyms`, `search_redirects`, `merchandising_rules` (boost/bury/pin
  per query or category).
- **Surfaces**: admin merchandising screens; apply rules + synonyms in the storefront
  search/catalog query; facet config.

---

## Tier 3 — completeness, schedule opportunistically

### 7. Rebates / volume incentives  ·  Impact: Med (vertical-dependent) · Effort: L
Retroactive/tiered rebates are big in distribution but niche elsewhere.
- **Data**: `rebate_programs` (tiers, period, accrual basis), `rebate_accruals`.
- **Engine**: accrue against qualifying orders; period-end settlement → credit note / payout.
- **Surfaces**: admin program setup + accrual report; buyer rebate statement.

### 8. Storefront i18n (product/content translations)  ·  Impact: Med (global sellers) · Effort: M
CMS/tenancy carry a `locale`; product/category content isn't translatable.
- **Data**: translation tables (or JSONB locale maps) for product name/description, category, attributes.
- **Surfaces**: admin per-locale editors; Nuxt locale routing + content resolution.

### 9. Loyalty / rewards  ·  Impact: Low–Med · Effort: L
Less common in pure B2B; consider only if a target vertical needs it. Likely overlaps
with rebates (Tier 3 #7) — evaluate together.

---

## Suggested sequence
1. **Promotions/coupons** (Tier 1 #1) — biggest gap, unlocks marketing-driven selling.
2. **Low-stock endpoint + widget** (Tier 1 #3) — small, finishes the dashboard.
3. **Order/quote on behalf** (Tier 1 #2) — assisted selling, mostly UI + audit.
4. **Subscriptions** (Tier 2 #4) — recurring revenue.
5. **Multi-currency** (Tier 2 #5) and **search merchandising** (Tier 2 #6) in parallel if capacity allows.
6. Tier 3 as vertical needs dictate.

## Cross-cutting (do alongside whatever ships)
- Keep the **OpenAPI spec** the source of truth; regenerate the typed client so the
  two frontends can't drift.
- Add **integration tests** (real Postgres via `testsupport.NewDB(t)`): query-level +
  HTTP-level asserting the auth gate and tenant isolation, for every new module.
- Money as decimal strings; `organization_id` tenant scoping; `public_id` in URLs;
  `text + CHECK` statuses; JSONB+GIN for flexible attributes — per the conventions in `docs/` Pack 1 §0.
