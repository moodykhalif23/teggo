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

## Tier 2 — ✅ DONE (all three shipped: subscriptions, multi-currency, search merchandising)

### 4. Subscriptions / recurring & standing orders  ·  ✅ Done · Impact: High · Effort: L
Auto-replenishment is core to B2B repeat buying; today only manual reorder + lists.
- **Data**: `subscriptions` (customer, cadence, next_run, status) + `subscription_items` + `subscription_runs`. Migration `0047`.
- **Engine**: a daily River periodic job (`materialize_due_subscriptions`) turns due subscriptions
  into orders, priced from the customer's combined prices at run time, taxed + marketplace-split;
  records a run and advances the schedule (engine in `internal/subscriptions`, run per-subscription tx).
- **Surfaces**: storefront “Set up recurring” on a past order + an account → Recurring page
  (pause/skip/cancel); admin Subscriptions screen (list/detail/status/**run now**).
- **Shipped follow-ups:** edit a subscription's cadence/items after creation (admin + storefront);
  automatic promotions applied to subscription orders; a transactional “order placed” email to the
  buyer on each run (via the worker).
- **Deferred (future):** per-line price lock; a *pre-delivery reminder* email (vs. the order-placed one).

### 5. Multi-currency display & FX  ·  ✅ Done · Impact: Med · Effort: M
Currency columns exist per price-list, but there's no presentation/FX layer.
- **Data**: `fx_rates` (org, base, quote, rate, as_of — time series, latest wins). Migration `0048`.
  Orders gained `display_currency` / `fx_rate` / `display_grand_total` for the FX lock snapshot.
- **Engine**: `internal/fx` — `Rate(org, base, quote)` (latest, "1" if equal) + exact `Convert`
  via `internal/money`. Admin FX-rate CRUD (`fx.view`/`fx.manage`).
- **Surfaces**: storefront currency selector + `GET /storefront/currencies`; the cart returns an
  indicative `display` block via `?currency=`; checkout **locks** the rate + quoted total onto the
  order. Admin “Exchange rates” screen under Pricing.
- **Deferred (future):** settle orders fully in the buyer's currency (store line items converted),
  and convert product-card prices in the catalog (currently cart-level display).

### 6. Search merchandising  ·  ✅ Done · Impact: Med · Effort: M
Search is Postgres FTS only — no curation.
- **Data** (`0049`): `search_synonyms`, `search_redirects`, `merchandising_rules` (pin/boost/bury
  per query or category).
- **Engine**: `internal/merchandising` — `ExpandQuery` (synonym OR-expansion into websearch syntax)
  + `Reorder` (pin → boost → normal → bury). Applied in the storefront faceted catalog search:
  query redirect short-circuits, synonyms expand before FTS, rules reorder the result page.
- **Surfaces**: admin “Search merchandising” screen (synonyms / redirects / rules);
  `merchandising.view`/`merchandising.manage` perms.
- **Deferred (future):** cross-page pin / force-include of non-matching products; facet config;
  storefront search page honoring the `redirect` field (server returns it; reorder is transparent).

---

## Tier 3 — completeness, schedule opportunistically

### 7. Rebates / volume incentives  ·  ✅ Done · Impact: Med (vertical-dependent) · Effort: L
Retroactive/tiered rebates are big in distribution but niche elsewhere.
- **Data** (`0050`): `rebate_programs` (period, currency, scope), `rebate_tiers` (min-spend → rate),
  `rebate_settlements` (unique per program+customer+period). Accrual is **derived from orders**
  on-demand (no per-order write) for safety/simplicity.
- **Engine** (`internal/rebates`): `PeriodWindow` (monthly/quarterly/annual) + `Applicable`
  (retroactive top-tier selection) + `Rebate`. Unit-tested.
- **Surfaces**: admin Rebates screen — programs + tiers, an accrual **report** (per-customer
  qualifying total + tier + projected), and **Settle** (idempotent; snapshots the amount + issues a
  credit note via the existing credit-note path). Buyer **rebate statement** (current progress +
  earned/settled) at `/account/rebates`. Perms `rebate.view`/`rebate.manage`.
- **Deferred (future):** event-based accrual (per-order write) for scale; payout (vs. credit note);
  customer-group scope; multi-currency programs.

### 8. Storefront i18n (product/content translations)  ·  ✅ Done · Impact: Med (global sellers) · Effort: M
CMS/tenancy carry a `locale`; product/category content isn't translatable.
- **Data**: reuses the existing `product_translations` (product_id, locale, name, description) — no migration.
- **Behavior**: storefront reads resolve a per-locale name/description by `?locale=` (detail + faceted
  search, batch-resolved), falling back to the base product when none exists. `GET /storefront/locales`
  lists the website default + configured locales. Managed under `product.manage` (no new perm).
- **Surfaces**: admin Translations editor in the product dialog (per-locale name/description);
  storefront header **locale selector** (Nuxt `useLocale` state) → product detail localizes live.
- **Deferred (future):** category/attribute translations; localizing the legacy `/storefront/products`
  list + listing pages (faceted search + detail are localized); Nuxt locale-prefixed routing.

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
