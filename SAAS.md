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

## 1. Tenant provisioning  ·  Impact: Critical (the biggest code gap) · Effort: M–L
There's no signup. Org 1 is seeded by migrations, and every permission seed is
`WHERE organization_id = 1` — a freshly created org would have **no roles, no
permissions, no website**. Until this exists, "SaaS" means onboarding tenants by
hand in SQL.
- **Data**: a `permission_templates` notion — either re-derive role/permission seeds
  from a code-level template at org creation, or restructure seeds so they apply per-org.
  Org gains `status` (trial/active/suspended) + `created_at` provenance.
- **Engine**: a single transactional `ProvisionOrganization(name, adminEmail, domain)`:
  create org → seed roles (admin/staff/viewer) + the full permission matrix from the
  template → create default website (domain) → create the first admin user + invite
  email → seed org defaults (currency, tax zone, locale).
- **Surfaces**: a public `/signup` flow (org name, admin email, subdomain) with email
  verification; a platform-operator screen to list/suspend orgs. Admin login becomes
  org-aware (resolve org by email or subdomain).
- **Touches**: migrations seeding strategy (the `WHERE organization_id=1` pattern),
  auth, websites/tenancy, email worker.
- **Tests**: provision a fresh org in an integration test, then assert a full
  golden-path (login → create product → storefront order) works inside it AND that
  org 1's data is invisible to it.

## 2. Platform billing & metering  ·  Impact: Critical · Effort: L
Teggo bills its *buyers* (invoices, AR); nothing bills its *tenants*.
- **Data**: `plans` (name, price, currency, limits JSONB, features JSONB),
  `org_subscriptions` (org, plan, status, period), `usage_counters`
  (org, metric, period, value) — metrics: orders/month, storage bytes, AI calls,
  admin seats.
- **Engine**: plan-based **feature flags** (e.g. rebates/subscriptions/AI on higher
  tiers) checked server-side (middleware or per-module guard, mirrored in the admin
  nav); **quota enforcement** at the write path (order create, media upload, assistant
  call) with soft-warn → hard-block; a River periodic job to roll up usage and close
  billing periods.
- **Payments**: Stripe Billing (or local equivalent — M-Pesa for KES markets) for the
  platform subscription itself; webhooks → `org_subscriptions.status`; dunning =
  org suspended, data retained.
- **Surfaces**: platform-operator plan management; tenant-facing "Billing & usage"
  screen in admin (current plan, usage meters, upgrade).
- **Tests**: quota exceeded → 403 with a clear code; feature off-plan → hidden in nav
  AND blocked at the API.

## 3. Isolation hardening  ·  Impact: High (risk reduction) · Effort: M
Shared-schema isolation rests on every query remembering its `org_id` filter; one
missed filter is a cross-tenant leak. The test convention covers this well — add
defense-in-depth:
- [ ] **Postgres row-level security**: `ALTER TABLE ... ENABLE ROW LEVEL SECURITY` +
  a `current_setting('app.org_id')` policy per tenant table; the API sets
  `app.org_id` per request/tx. Queries keep their explicit filters — RLS is the net,
  not the mechanism.
- [ ] A migration-lint check (CI) that every new tenant table gets the RLS policy.
- [ ] Cross-tenant probe tests: for each module, a second org's token attempts every
  read/write against org 1's resources (some exist already — make it a convention
  gate for new modules).
- [ ] Keep **DB-per-tenant in the back pocket** as a premium/enterprise tier — the
  sqlc + pgx pool layer makes a per-org connection string feasible later; don't
  build it now.

## 4. Per-tenant config  ·  Impact: High · Effort: M
SMTP from-address, payments gateway, and Pusher are global env vars. Tenants are
their own merchants of record.
- **Data**: extend org `settings` (JSONB or typed columns) with: payment gateway +
  credentials (encrypted at rest — pgcrypto or app-level AES with a platform KMS key),
  email sender identity (from-name/address, optionally per-tenant SMTP/provider
  subaccount), branding (logo media id, brand color, storefront theme knobs).
- **Engine**: gateway resolution per-org at charge time (the `PaymentGateway`
  interface already exists — make the mock/real selection org-scoped); the email
  worker reads the org's sender identity per message; storefront SSR reads branding
  by website.
- **Deliverability**: per-tenant sending domains need SPF/DKIM — use a provider with
  subaccounts/domains API (SES/Postmark/Resend) instead of raw SMTP; platform-managed
  fallback sender for tenants who don't bring a domain.
- **Surfaces**: admin Settings grows "Payments", "Email", "Branding" sections
  (org-scoped, `settings.manage`).

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
1. **Security pre-flight** (#0) — hours, removes standing risk.
2. **Tenant provisioning** (#1) — everything else assumes orgs can exist.
3. **Per-tenant config** (#4) — makes a second real tenant actually usable.
4. **Platform billing & metering** (#2) — monetize once tenants can self-serve.
5. **Isolation hardening** (#3) — land RLS before opening signup to strangers.
6. **Infra swaps** (#5) — opportunistically; object storage + TLS first.

## Cross-cutting (do alongside whatever ships)
- Keep the **OpenAPI spec** the source of truth; regenerate the typed client.
- Integration tests (real Postgres via `testsupport.NewDB(t)`) for every new module,
  always including the cross-tenant probe.
- The AI assistant ships with the OpenAI-compatible provider (Groq/Llama via
  `AI_PROVIDER=openai` + `AI_CHAT_*`) — platform keys, metered per org (workstream 2).
- Money as decimal strings; `organization_id` scoping; `public_id` in URLs;
  `text + CHECK` statuses — per the conventions in `docs/` Pack 1 §0.
