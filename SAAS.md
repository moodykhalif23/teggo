# Teggo — SaaS Readiness Roadmap

What stands between today's Teggo (self-hosted, org 1 seeded by migrations) and a
multi-tenant SaaS where organizations sign up, pay, and run their own storefronts.
Companion to [ROADMAP.md](ROADMAP.md) (feature gaps); this doc covers **platform
gaps**, sequenced so each workstream lands independently — tackle them one by one.

Already working in our favor: every table/query is `organization_id`-scoped with
integration tests asserting tenant isolation; the storefront resolves
**host → website → organization** (`catalog.resolveOrg`); JWTs carry org +
audience; SSO is per-org; River jobs, multi-currency, and i18n are org-scoped;
the API is stateless (scales horizontally); media goes through the S3-shaped
`blob.Store` interface.

Each item follows the repeatable module pattern in README "Adding a backend module":
`migration → internal/store/queries → make generate → internal/modules/<name>/handler.go
→ mount in server.go → integration tests → OpenAPI → typed client → screens`.

Legend — **Impact**: how much it unblocks a real SaaS launch. **Effort**: rough build size.

---

## 1. Tenant provisioning  ·  ✅ Done · Impact: Critical (was the biggest code gap) · Effort: M–L
Self-serve signup is live: a new org provisions itself, verifies by email, and
signs in with a fully-seeded permission set — no SQL onboarding.
- **Data** (`0051`): `organizations.status` (pending/trial/active/suspended) +
  `signup_verifications` (single-use UUID tokens, expiring). `platform.view`/
  `platform.manage` granted ONLY to org 1 (the platform owner).
- **Engine** (`internal/tenant`): transactional `Provision` — org ('pending') →
  roles admin/staff/viewer seeded from the canonical template (org 1's admin role,
  which every permission migration appends to; `platform.*` excluded; staff =
  view+edit, viewer = view) → default website at `<subdomain>.<PLATFORM_BASE_DOMAIN>`
  → first admin user → verification token. Fails loudly if the template is empty.
- **Surfaces**: public `POST /signup` + `POST /signup/verify` (rate-limited) with a
  `signup_verify` email; admin SPA `/signup` + `/verify-signup` pages; operator
  "Platform" screen (list orgs, suspend/reactivate — org 1 untouchable). Admin login
  is org-aware (unique email resolves its org; ambiguity asks for org_id) and every
  login checks org status.
- **Enforcement**: an org-status gate composed under both authenticators — a
  suspended tenant's existing tokens die within the 30s cache TTL (instantly on the
  node that suspended); login refused for pending/suspended across admin, storefront
  and vendor audiences.
- **Tests**: golden path (signup → pending blocks login → verify → org-aware login →
  create product → tenant isolation vs org 1 → operator suspends → live token blocked
  → reactivate); validation + subdomain collision (409); single-use token; operator
  guards. Unit tests for validation rules.
- **Deferred (future):** verification-email resend; per-tenant tax-zone defaults at
  signup; storefront-wide gate for *unauthenticated* reads of suspended tenants'
  catalogs (sign-ins and authed calls are blocked today); new-permission migrations
  should now grant to **every** org's admin role, not just org 1 (new convention —
  provisioned orgs only inherit the template at creation time).

## 2. Platform billing & metering  ·  ✅ Done · Impact: Critical · Effort: L
Plans, feature flags and usage quotas are live; tenants are metered, operators
manage tiers.
- **Data** (`0053`): `plans` (price, features JSONB, limits JSONB — seeded
  free/growth/scale), `org_subscriptions` (one per org; existing orgs landed on
  scale so nothing went dark), `usage_counters` (org × metric × period —
  orders + ai_calls monthly, storage_bytes lifetime).
- **Engine** (`internal/billing`): cached per-org **entitlements** + one
  path-rule **Gate middleware** under the authenticators — premium modules
  (subscriptions, rebates, fx, merchandising, assistant) 403 `feature_not_in_plan`
  off-plan; metered writes (order create ×3 paths, media upload by bytes,
  assistant calls) 403 `quota_exceeded` at the cap, recording usage only on 2xx
  so failed requests never burn quota. Recurring orders are counted but never
  blocked. Orgs without a plan row are unmetered by design (the platform org on
  legacy DBs); provisioning always assigns `free`.
- **Surfaces**: tenant **Billing & usage** screen (plan card, usage meters,
  feature list); the sidebar hides off-plan modules (server enforces regardless);
  operator plan list/edit (`PUT /admin/platform/plans/{code}` — applies to every
  org on the plan) and per-org plan assignment on the Platform screen.
- **Tests**: free tenant 403-blocked from all four premium modules while core
  commerce works; operator upgrade opens access without re-login; order cap of 1
  → second order 403 `quota_exceeded`; `/admin/billing` reflects plan, limits
  and consumption.
- **Deferred (future):** Stripe/M-Pesa collection for the platform subscription
  (webhooks → `org_subscriptions.status`, dunning → org suspension — the status
  fields exist); admin-seat metering; period close-out/invoice job; storage
  decrement on media delete (no delete endpoint exists yet); soft-warn
  thresholds before the hard block.

## 3. Isolation hardening  ·  ✅ Done · Impact: High (risk reduction) · Effort: M
Queries keep their explicit `org_id` filters as the mechanism; Postgres RLS is
now the net underneath — a query that forgets its WHERE clause returns nothing
foreign instead of everything.
- **Net** (`0054`): every table with `organization_id` (50 at ship time) carries
  a FORCEd `org_isolation` policy keyed on the `app.org_id` session setting,
  failing OPEN when unset (workers, migrations, tests unchanged). Because docker
  `POSTGRES_USER` connections are superusers (which bypass RLS no matter what),
  arming also `SET ROLE teggo_app` — a NOLOGIN/NOBYPASSRLS role with plain DML
  rights created by the migration.
- **Arming** (`internal/db` + `internal/tenantctx`): the API pool's
  `BeforeAcquire` hook reads the request's org (stashed in context by the auth
  middlewares) and pins role + setting per connection, with a per-conn cache so
  the round-trip only happens when the org changes. The worker runs unarmed
  (cross-org sweeps). NOTE: needs session pooling — PgBouncer transaction mode
  would break it.
- **Bypass**: `tenantctx.Bypass(ctx)` is the grep-able escape hatch, used only
  by the platform-operator endpoints that are legitimately cross-tenant (org
  list counts, plan assignment).
- **Convention gates** (`internal/isolation`): a lint test fails the suite if
  any NEW table with `organization_id` lacks the policy; net tests prove an
  armed session scopes UNFILTERED SQL and that cross-org INSERT dies on WITH
  CHECK; an end-to-end probe runs the real HTTP stack on an armed pool — a
  foreign org's fully-permissioned token sweeps 16 admin read endpoints without
  leaking an org-1 marker, object fetch/update of foreign rows 404s, and the
  operator endpoints still work through their bypass.
- **Deferred (future):** join-based policies for tables scoped indirectly via
  `customer_id` (carts, invoices, customer_users — reachable only through
  org-scoped parents today); arming the worker per-job; DB-per-tenant stays in
  the back pocket as a premium tier.

## 4. Per-tenant config  ·  ✅ Done · Impact: High · Effort: M
Payments, email identity and branding are org-scoped settings, not env vars.
- **Data** (`0052`): `org_payment_configs` (gateway + credentials sealed with
  AES-256-GCM via `internal/secretbox`, key from `CONFIG_ENCRYPTION_KEY` — a
  dedicated table so the settings list endpoint can never echo secrets). Branding
  + email identity live in `config_settings` (`branding.*`, `email.*` keys).
- **Engine**: charges resolve their gateway per org at charge time
  (`internal/payments/tenantgw` — adapter registry, platform default as fallback);
  the send_email worker resolves `email.from_name`/`email.from_address` per
  message at SEND time (queued mail picks up identity changes); the SMTP envelope
  sender stays the platform's so SPF/DKIM never depend on tenant input. All
  buyer-facing mail (orders, quotes, invoices, dunning, recurring, automation)
  carries its org.
- **Surfaces**: admin Settings → "Store identity" panel (branding, email sender,
  gateway + write-only credentials showing stored key names only); public
  `GET /storefront/branding` resolves by serving host and the storefront layout
  applies name/color/logo server-side on first paint.
- **Deferred (future):** real gateway adapters (stripe/mpesa — the registry +
  encrypted credential plumbing is ready); per-tenant sending domains with
  SPF/DKIM via a provider subaccount API; per-tenant Pusher (realtime stays
  platform-level); logo picker wired to the Media library (URL paste today).

## 5. Infra swaps  ·  Impact: Medium-High (all anticipated by the code) · Effort: M, mostly ops
- [ ] **Object storage**: implement `blob.Store` against S3/R2 and select by env
  (`MEDIA_STORE=s3`); `FSStore` stays for dev. (The interface is already S3-shaped.)
- [ ] **Managed Postgres** (RDS/Cloud SQL/Neon) + automated backups + PITR; pgbouncer
  if conn counts grow.
- [ ] **Custom-domain TLS**: Caddy on-demand TLS or Cloudflare for SaaS in front of
  the storefront; a `websites.domain` verification flow (DNS TXT) in admin.
- [ ] **Per-org rate limiting** on the API (middleware keyed by org claim; stricter
  unauthenticated limits per IP/host).
- [ ] **Org-tagged observability**: org_id attribute on OTel spans/metrics; per-org
  request/error dashboards (noisy-neighbor detection).
- [ ] **AI metering**: assistant calls counted into `usage_counters` (workstream 2);
  platform-level provider keys; per-plan caps.
- [ ] **Tenant offboarding**: GDPR-grade export (per-org dump of all rows + media)
  and delete (cascade with audit record); org suspension already covered by
  provisioning status.

---

## Suggested sequence
1. ~~Security pre-flight (#0)~~ — done (secrets rotated, .env untracked).
2. ~~Tenant provisioning (#1)~~ — ✅ shipped.
3. ~~Per-tenant config (#4)~~ — ✅ shipped.
4. ~~Platform billing & metering (#2)~~ — ✅ shipped (payment collection deferred).
5. ~~Isolation hardening (#3)~~ — ✅ shipped (RLS net armed in the API).
6. **Infra swaps** (#5) — opportunistically; object storage + TLS first.

## Cross-cutting (do alongside whatever ships)
- Keep the **OpenAPI spec** the source of truth; regenerate the typed client.
- Integration tests (real Postgres via `testsupport.NewDB(t)`) for every new module,
  always including the cross-tenant probe.
- The AI assistant ships with the OpenAI-compatible provider (Groq/Llama via
  `AI_PROVIDER=openai` + `AI_CHAT_*`) — platform keys, metered per org (workstream 2).
- Money as decimal strings; `organization_id` scoping; `public_id` in URLs;
  `text + CHECK` statuses — per the conventions in `docs/` Pack 1 §0.
