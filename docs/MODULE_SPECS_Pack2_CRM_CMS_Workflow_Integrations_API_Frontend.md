# Module Specifications — Pack 2
## CRM · CMS · Workflow Engine · Integration Adapters · OpenAPI · Vue/Nuxt

| Field | Value |
|---|---|
| Companion to | PRD v0.2 + Implementation Pack v0.1 (commerce spine) |
| Stack | Go (chi + sqlc + river) · PostgreSQL 16+ · Vue 3 (admin SPA) + Nuxt 3 (storefront SSR) |
| Scope | Deep specs for the deferred modules + API contract + frontend component breakdown |
| Status | Draft v0.1 |
| Last updated | 2 June 2026 |

This pack inherits all conventions from Implementation Pack §0 (BIGINT identity PKs, UUID `public_id` on customer-facing docs, `NUMERIC(15,4)` money, `text + CHECK` statuses, JSONB for flexible data, `set_updated_at()` trigger). Every FK below resolves to a table from Pack 1 or this pack.

---

# 1. CRM

The CRM links seller-side relationship data to the commerce `customers`. A CRM **account** is the same entity as a commerce `customer` (no duplication) — leads/opportunities/activities hang off it.

### 1.1 User stories
- As a **sales rep**, I capture leads (from storefront forms, RFQs, or manual entry) and qualify them into opportunities.
- As a **sales rep**, I track opportunities through a pipeline with stages and expected value.
- As a **sales rep**, I log activities (calls, emails, meetings, tasks, notes) against a customer/contact/opportunity and see a unified timeline.
- As a **sales manager**, I view the pipeline by stage with weighted forecast, and rep performance.

### 1.2 Acceptance criteria
- A lead can be converted to (customer + contact + opportunity) in one action; conversion is idempotent (a converted lead cannot convert twice).
- An opportunity always sits in exactly one pipeline stage; moving stages writes a history row.
- The activity timeline for a customer aggregates activities linked to the customer, its contacts, and its opportunities, ordered by `occurred_at` desc.
- Weighted forecast = Σ(`opportunity.amount` × `stage.probability`) over open opportunities.

### 1.3 Schema
```sql
CREATE TABLE contacts (
  id              BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  organization_id BIGINT NOT NULL REFERENCES organizations(id),
  customer_id     BIGINT REFERENCES customers(id),        -- null until linked
  customer_user_id BIGINT REFERENCES customer_users(id),  -- optional link to a login
  full_name       text NOT NULL,
  email           citext,
  phone           text,
  job_title       text,
  created_at      timestamptz NOT NULL DEFAULT now(),
  updated_at      timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX idx_contacts_customer ON contacts(customer_id);

CREATE TABLE leads (
  id              BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  public_id       UUID NOT NULL DEFAULT gen_random_uuid() UNIQUE,
  organization_id BIGINT NOT NULL REFERENCES organizations(id),
  source          text NOT NULL DEFAULT 'manual'
                    CHECK (source IN ('manual','storefront_form','rfq','import','referral')),
  company_name    text,
  contact_name    text,
  email           citext,
  phone           text,
  notes           text,
  status          text NOT NULL DEFAULT 'new'
                    CHECK (status IN ('new','working','qualified','disqualified','converted')),
  owner_user_id   BIGINT REFERENCES users(id),
  converted_customer_id BIGINT REFERENCES customers(id),  -- set on conversion
  created_at      timestamptz NOT NULL DEFAULT now(),
  updated_at      timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX idx_leads_owner ON leads(owner_user_id);

CREATE TABLE pipelines (
  id              BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  organization_id BIGINT NOT NULL REFERENCES organizations(id),
  name            text NOT NULL,
  is_default      boolean NOT NULL DEFAULT false
);

CREATE TABLE pipeline_stages (
  id           BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  pipeline_id  BIGINT NOT NULL REFERENCES pipelines(id) ON DELETE CASCADE,
  code         text NOT NULL,
  label        text NOT NULL,
  probability  NUMERIC(5,2) NOT NULL DEFAULT 0,   -- 0..100 for weighted forecast
  is_won       boolean NOT NULL DEFAULT false,
  is_lost      boolean NOT NULL DEFAULT false,
  sort_order   int NOT NULL DEFAULT 0,
  UNIQUE (pipeline_id, code)
);

CREATE TABLE opportunities (
  id              BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  public_id       UUID NOT NULL DEFAULT gen_random_uuid() UNIQUE,
  organization_id BIGINT NOT NULL REFERENCES organizations(id),
  customer_id     BIGINT NOT NULL REFERENCES customers(id),
  contact_id      BIGINT REFERENCES contacts(id),
  pipeline_id     BIGINT NOT NULL REFERENCES pipelines(id),
  stage_id        BIGINT NOT NULL REFERENCES pipeline_stages(id),
  name            text NOT NULL,
  amount          NUMERIC(15,4) NOT NULL DEFAULT 0,
  currency        CHAR(3) NOT NULL,
  expected_close  date,
  owner_user_id   BIGINT REFERENCES users(id),
  closed_at       timestamptz,
  created_at      timestamptz NOT NULL DEFAULT now(),
  updated_at      timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX idx_opps_customer ON opportunities(customer_id);
CREATE INDEX idx_opps_stage ON opportunities(stage_id);

CREATE TABLE opportunity_stage_history (
  id            BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  opportunity_id BIGINT NOT NULL REFERENCES opportunities(id) ON DELETE CASCADE,
  from_stage_id BIGINT REFERENCES pipeline_stages(id),
  to_stage_id   BIGINT NOT NULL REFERENCES pipeline_stages(id),
  changed_by    BIGINT REFERENCES users(id),
  created_at    timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE activities (
  id              BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  organization_id BIGINT NOT NULL REFERENCES organizations(id),
  type            text NOT NULL CHECK (type IN ('call','email','meeting','task','note')),
  subject         text NOT NULL,
  body            text,
  -- polymorphic association by explicit nullable FKs (one or more may be set)
  customer_id     BIGINT REFERENCES customers(id),
  contact_id      BIGINT REFERENCES contacts(id),
  opportunity_id  BIGINT REFERENCES opportunities(id),
  lead_id         BIGINT REFERENCES leads(id),
  owner_user_id   BIGINT REFERENCES users(id),
  status          text NOT NULL DEFAULT 'open'
                    CHECK (status IN ('open','done','cancelled')),  -- for tasks
  due_at          timestamptz,
  occurred_at     timestamptz NOT NULL DEFAULT now(),
  created_at      timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX idx_activities_customer ON activities(customer_id, occurred_at DESC);
CREATE INDEX idx_activities_opp ON activities(opportunity_id);
```

### 1.4 Key queries
```sql
-- Pipeline board with weighted forecast per stage
SELECT s.id, s.label, s.probability,
       count(o.id) AS open_count,
       COALESCE(sum(o.amount),0) AS total_amount,
       COALESCE(sum(o.amount * s.probability/100.0),0) AS weighted_amount
  FROM pipeline_stages s
  LEFT JOIN opportunities o
    ON o.stage_id = s.id AND o.closed_at IS NULL
 WHERE s.pipeline_id = $1
 GROUP BY s.id ORDER BY s.sort_order;

-- Unified activity timeline for a customer (incl. its contacts + opps)
SELECT a.* FROM activities a
 WHERE a.customer_id = $1
    OR a.contact_id IN (SELECT id FROM contacts WHERE customer_id = $1)
    OR a.opportunity_id IN (SELECT id FROM opportunities WHERE customer_id = $1)
 ORDER BY a.occurred_at DESC LIMIT 100;
```

### 1.5 API surface
`POST /admin/leads`, `POST /admin/leads/{id}/convert`, `GET /admin/pipelines/{id}/board`,
`POST /admin/opportunities`, `PATCH /admin/opportunities/{id}/stage`,
`POST /admin/activities`, `GET /admin/customers/{id}/timeline`.

---

# 2. CMS

A block-based content system for storefront pages, served via the storefront API to Nuxt. Pages are trees of typed blocks stored as JSONB, so new block types are additive without migrations.

### 2.1 User stories
- As a **content editor**, I build landing/content pages from reusable blocks (hero, text, product-grid, banner, CTA) without code.
- As a **content editor**, I manage navigation menus per website.
- As a **content editor**, I target content to a customer group (e.g. dealer-only banner).
- As **marketing**, I control SEO metadata and URL slugs, and set up redirects.

### 2.2 Acceptance criteria
- A page has a unique slug per (website, locale); publishing flips `status` to `published` and sets `published_at`.
- Draft pages are previewable via a signed preview token but never served publicly.
- Block payloads validate against the block type's schema (app-level) on save.
- A `product-grid` block references products by a saved query (category, attribute filter, or explicit SKU list) resolved at render time.
- Targeted content is filtered by the requesting customer's group; untargeted content is visible to all.

### 2.3 Schema
```sql
CREATE TABLE content_pages (
  id              BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  public_id       UUID NOT NULL DEFAULT gen_random_uuid() UNIQUE,
  website_id      BIGINT NOT NULL REFERENCES websites(id),
  locale          text NOT NULL DEFAULT 'en',
  slug            text NOT NULL,
  title           text NOT NULL,
  status          text NOT NULL DEFAULT 'draft'
                    CHECK (status IN ('draft','published','archived')),
  blocks          JSONB NOT NULL DEFAULT '[]'::jsonb,   -- ordered array of typed blocks
  seo             JSONB NOT NULL DEFAULT '{}'::jsonb,    -- {title, description, og, canonical, noindex}
  target_customer_group_id BIGINT REFERENCES customer_groups(id),  -- null = all
  published_at    timestamptz,
  created_at      timestamptz NOT NULL DEFAULT now(),
  updated_at      timestamptz NOT NULL DEFAULT now(),
  UNIQUE (website_id, locale, slug)
);
CREATE INDEX idx_content_pages_status ON content_pages(website_id, status);

CREATE TABLE menus (
  id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  website_id  BIGINT NOT NULL REFERENCES websites(id),
  code        text NOT NULL,            -- 'main','footer'
  name        text NOT NULL,
  UNIQUE (website_id, code)
);

CREATE TABLE menu_items (
  id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  menu_id     BIGINT NOT NULL REFERENCES menus(id) ON DELETE CASCADE,
  parent_id   BIGINT REFERENCES menu_items(id),
  label       text NOT NULL,
  url         text,                     -- or
  category_id BIGINT REFERENCES categories(id),
  page_id     BIGINT REFERENCES content_pages(id),
  sort_order  int NOT NULL DEFAULT 0
);
CREATE INDEX idx_menu_items_menu ON menu_items(menu_id);

CREATE TABLE media_assets (
  id              BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  organization_id BIGINT NOT NULL REFERENCES organizations(id),
  url             text NOT NULL,
  mime_type       text,
  width           int,
  height          int,
  alt             text,
  folder          text DEFAULT '/',
  created_at      timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE redirects (
  id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  website_id  BIGINT NOT NULL REFERENCES websites(id),
  from_path   text NOT NULL,
  to_path     text NOT NULL,
  status_code int NOT NULL DEFAULT 301 CHECK (status_code IN (301,302)),
  UNIQUE (website_id, from_path)
);
```

### 2.4 Block model (JSONB shape)
```jsonc
// content_pages.blocks
[
  { "type": "hero", "id": "b1",
    "props": { "heading": "...", "image_asset_id": 12, "cta": {"label":"Shop","href":"/c/valves"} } },
  { "type": "product-grid", "id": "b2",
    "props": { "source": {"kind":"category","category_id":5,"limit":8} } },
  { "type": "rich-text", "id": "b3", "props": { "html": "<p>...</p>" } }
]
```
Block types are registered in the frontend (renderer) and validated server-side against a registry of JSON schemas. Adding a block type = register schema + Nuxt component; no DB migration.

### 2.5 API surface
`GET /storefront/pages/{slug}` (resolves targeting + product-grid sources),
`GET /storefront/menus/{code}`, `GET /admin/pages`, `PUT /admin/pages/{id}`,
`POST /admin/pages/{id}/publish`, `POST /admin/media`.

---

# 3. Configurable workflow & automation engine

Two cooperating subsystems: a **state-machine engine** (governs entity lifecycles like order/quote/RFQ) and an **automation engine** (event → conditions → actions). Both are config-driven but extensible via Go-registered guards/actions, matching the PRD's "low-code where feasible, developer-extensible for complex logic."

### 3.1 User stories
- As a **system admin**, I define a workflow (states + transitions) for an entity type without code.
- As a **system admin**, I attach guards to transitions (e.g. "only if order total ≤ approver limit") and actions (e.g. "send email", "reserve inventory").
- As a **system admin**, I create automation rules: when X happens, if conditions, do actions (e.g. "when quote sent, schedule expiry job").
- As a **developer**, I register new guard/action types in Go that admins can then wire up by config.

### 3.2 Acceptance criteria
- A transition applies only if: the instance's current state matches the transition's `from_state` (or `from` is null = any), and all guards pass.
- Applying a transition is atomic: state update + `workflow_transition_log` row in one transaction; actions are enqueued (river) to run after commit (so a failed action never corrupts state).
- An entity has at most one active `workflow_instance` per definition.
- Automation rules fire on a named event; conditions are evaluated against the event payload; actions are enqueued. A rule that errors is recorded in `automation_executions` and retried per the queue's retry policy, never silently dropped.
- Final states accept no outgoing transitions.

### 3.3 Schema
```sql
CREATE TABLE workflow_definitions (
  id              BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  organization_id BIGINT NOT NULL REFERENCES organizations(id),
  code            text NOT NULL,            -- 'order_default','quote_default'
  entity_type     text NOT NULL,            -- 'order','quote','rfq'
  name            text NOT NULL,
  is_active       boolean NOT NULL DEFAULT true,
  UNIQUE (organization_id, code)
);

CREATE TABLE workflow_states (
  id            BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  definition_id BIGINT NOT NULL REFERENCES workflow_definitions(id) ON DELETE CASCADE,
  code          text NOT NULL,
  label         text NOT NULL,
  is_initial    boolean NOT NULL DEFAULT false,
  is_final      boolean NOT NULL DEFAULT false,
  sort_order    int NOT NULL DEFAULT 0,
  UNIQUE (definition_id, code)
);

CREATE TABLE workflow_transitions (
  id            BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  definition_id BIGINT NOT NULL REFERENCES workflow_definitions(id) ON DELETE CASCADE,
  code          text NOT NULL,
  label         text NOT NULL,
  from_state_id BIGINT REFERENCES workflow_states(id),  -- null = from any state
  to_state_id   BIGINT NOT NULL REFERENCES workflow_states(id),
  guards        JSONB NOT NULL DEFAULT '[]'::jsonb,   -- [{key, params}]
  actions       JSONB NOT NULL DEFAULT '[]'::jsonb,   -- [{key, params}]
  sort_order    int NOT NULL DEFAULT 0,
  UNIQUE (definition_id, code)
);

CREATE TABLE workflow_instances (
  id               BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  definition_id    BIGINT NOT NULL REFERENCES workflow_definitions(id),
  entity_type      text NOT NULL,
  entity_id        BIGINT NOT NULL,
  current_state_id BIGINT NOT NULL REFERENCES workflow_states(id),
  created_at       timestamptz NOT NULL DEFAULT now(),
  updated_at       timestamptz NOT NULL DEFAULT now(),
  UNIQUE (definition_id, entity_type, entity_id)
);
CREATE INDEX idx_wf_instances_entity ON workflow_instances(entity_type, entity_id);

CREATE TABLE workflow_transition_log (
  id            BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  instance_id   BIGINT NOT NULL REFERENCES workflow_instances(id) ON DELETE CASCADE,
  transition_id BIGINT NOT NULL REFERENCES workflow_transitions(id),
  from_state_id BIGINT,
  to_state_id   BIGINT NOT NULL,
  actor_type    text NOT NULL,
  actor_id      BIGINT,
  context       JSONB,
  created_at    timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE automation_rules (
  id              BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  organization_id BIGINT NOT NULL REFERENCES organizations(id),
  name            text NOT NULL,
  trigger_event   text NOT NULL,            -- 'order.status_changed','quote.created','schedule.hourly'
  conditions      JSONB NOT NULL DEFAULT '[]'::jsonb,  -- [{field, op, value}]
  actions         JSONB NOT NULL DEFAULT '[]'::jsonb,  -- [{key, params}]
  is_active       boolean NOT NULL DEFAULT true,
  created_at      timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX idx_automation_event ON automation_rules(trigger_event) WHERE is_active;

CREATE TABLE automation_executions (
  id            BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  rule_id       BIGINT NOT NULL REFERENCES automation_rules(id) ON DELETE CASCADE,
  event_payload JSONB NOT NULL,
  status        text NOT NULL CHECK (status IN ('ok','error')),
  result        JSONB,
  created_at    timestamptz NOT NULL DEFAULT now()
);
```

### 3.4 Go contracts
```go
// Guards decide whether a transition may proceed.
type Guard interface {
    Key() string
    Allow(ctx context.Context, in TransitionInput) (ok bool, reason string, err error)
}

// Actions run side effects after a transition commits (executed as queue jobs).
type Action interface {
    Key() string
    Run(ctx context.Context, in ActionInput) error
}

type TransitionInput struct {
    Instance   WorkflowInstance
    Transition WorkflowTransition
    Entity     any            // loaded order/quote/etc.
    Actor      Actor
    Context    map[string]any
    Params     map[string]any // from the guard config
}

// Registries populated at boot; admin config references guards/actions by Key().
type Registry struct {
    guards  map[string]Guard
    actions map[string]Action
}

// Engine.Apply: validate -> guards -> tx{update state + log} -> enqueue actions.
func (e *Engine) Apply(ctx context.Context, instanceID int64, transitionCode string,
    actor Actor, data map[string]any) error
```

Built-in guards (examples): `amount_lte_limit`, `has_permission`, `inventory_available`, `within_credit_limit`.
Built-in actions: `send_email`, `reserve_inventory`, `issue_invoice`, `emit_event`, `set_field`, `schedule_job`, `call_webhook`.

### 3.5 Automation event flow
1. Domain code emits a typed event after commit: `bus.Emit("order.status_changed", payload)`.
2. The dispatcher loads active `automation_rules` matching `trigger_event` (indexed lookup).
3. For each rule, evaluate `conditions` against the payload (`field op value`, e.g. `grand_total gt 100000`).
4. Matching rules enqueue their `actions` as river jobs; record an `automation_executions` row per run.
5. Scheduled events (`schedule.hourly`, `schedule.daily`) are emitted by a cron-style river periodic job — this is how quote-expiry and overdue-invoice rules fire.

### 3.6 Worked examples
- **Order approval**: transition `confirm` on `order_default` has guard `amount_lte_limit{field:grand_total, limit_source:customer_user.spending_limit}`; if it fails, a separate `request_approval` transition routes to `on_hold` and action `send_email{template:approval_request}`.
- **Quote expiry**: automation rule on `schedule.hourly`, condition `valid_until lt now`, action `set_field{entity:quote, field:status, value:expired}` + `send_email{template:quote_expired}`.

---

# 4. Integration adapters

A uniform adapter pattern: the core depends on small Go interfaces; each provider is a package implementing one. Config (non-secret) lives in `integration_connections.config`; secrets come from a secret store (env/Vault), never the DB. All outbound calls carry an idempotency key; all inbound webhooks are verified and de-duplicated.

### 4.1 Architecture
- **Registry**: `provider string → adapter`. Resolved per `integration_connections` row.
- **Outbound**: domain → adapter call (often via a river job for ret(ry/async); idempotency key = stable hash of (entity, operation)).
- **Inbound (webhooks)**: a single `POST /webhooks/{provider}` endpoint → verify signature → parse to a normalized `WebhookEvent` → enqueue handler job → dedupe on provider event id.
- **Observability**: every call writes `sync_logs`; failures hit the queue's retry + dead-letter.

### 4.2 Payment adapter
```go
type PaymentAdapter interface {
    Provider() string
    CreateCharge(ctx context.Context, r ChargeRequest) (ChargeResult, error) // authorize+capture or intent
    Capture(ctx context.Context, ref string, amount Money) (CaptureResult, error)
    Refund(ctx context.Context, ref string, amount Money) (RefundResult, error)
    VerifyWebhook(ctx context.Context, raw []byte, headers http.Header) (WebhookEvent, error)
}
```
**Stripe** — use PaymentIntents; `gateway_reference` = PaymentIntent id; webhook events `payment_intent.succeeded`/`.payment_failed`; verify via `Stripe-Signature` + signing secret. On `succeeded`, mark the matching `payments` row `captured` and flip the invoice to `paid` if covered.

**M-Pesa (Daraja)** — regionally primary for the Kenya context. Flow: OAuth token (cached until expiry) → STK Push (Lipa na M-Pesa Online) with the order/invoice amount → store `CheckoutRequestID` as `gateway_reference`, `payments.status = pending`, `method = mpesa`. Daraja calls back to `POST /webhooks/mpesa`; on `ResultCode = 0`, mark `captured` and store the M-Pesa receipt. Handle the validation/confirmation URLs for C2B if used. Timeouts/cancels → `failed`.

### 4.3 Shipping adapter
```go
type ShippingAdapter interface {
    Provider() string
    Rates(ctx context.Context, r RateRequest) ([]RateQuote, error)
    CreateLabel(ctx context.Context, r LabelRequest) (Label, error)
    Track(ctx context.Context, tracking string) (TrackingStatus, error)
}
```
Rates feed checkout; `CreateLabel` runs on fulfilment and writes `shipments.tracking_number`; tracking webhooks (or polling) update `shipments.status` and may drive the order workflow `ship`/`deliver` transitions.

### 4.4 Tax adapter
```go
type TaxAdapter interface {
    Provider() string
    Calculate(ctx context.Context, r TaxRequest) (TaxResult, error) // per-line tax
}
```
Two implementations: an external service (Avalara/TaxJar-style) and a **rules-based local provider** for VAT (e.g. Kenya VAT at the statutory rate, exemptions by product tax class). The local provider needs no external calls — config-driven rates per region/tax-class. Tax is calculated at quote/checkout and snapshotted onto `order_items.tax_amount`.

### 4.5 Email / transactional adapter
```go
type EmailAdapter interface {
    Provider() string
    Send(ctx context.Context, msg Email) (messageID string, err error)
}
```
Providers: SES, Mailgun, SMTP. All sends go through a river job (`send_email` action) with a template key + data. Templates: order confirmation, quote sent, quote accepted, invoice issued, approval request, password reset.

### 4.6 ERP / accounting sync
Pattern, not a single interface — most ERPs need bespoke mapping:
- **Outbound** (commerce → ERP): on `order.confirmed` and `invoice.issued`, enqueue a sync job that upserts into the ERP keyed by our `public_id` (idempotent). Record in `sync_logs`.
- **Inbound** (ERP → commerce): scheduled pulls (river periodic) for inventory levels and product/price master data; upsert keyed by an `external_id` mapping. Add an `external_refs` table when you wire the first ERP:
```sql
CREATE TABLE external_refs (
  id            BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  connection_id BIGINT NOT NULL REFERENCES integration_connections(id),
  entity_type   text NOT NULL,         -- 'order','customer','product','invoice'
  entity_id     BIGINT NOT NULL,
  external_id   text NOT NULL,
  synced_at     timestamptz,
  UNIQUE (connection_id, entity_type, entity_id),
  UNIQUE (connection_id, entity_type, external_id)
);
```

### 4.7 Acceptance criteria (cross-provider)
- Every outbound call is idempotent (safe to retry); duplicate webhooks (same provider event id) are no-ops.
- A failed integration call never blocks the user transaction — it's queued and retried, with state reflecting `pending` until confirmed.
- Switching providers (e.g. Stripe → another gateway) requires only a new adapter + a connection row, no domain changes.

---

# 5. OpenAPI contract (3.1)

Representative, directly extensible. Two security contexts: `adminAuth` (back-office bearer) and `storefrontAuth` (customer-user bearer/session). Generate the Go server interfaces and the TypeScript client for Vue/Nuxt from this file.

```yaml
openapi: 3.1.0
info:
  title: B2B Commerce Platform API
  version: 0.1.0
servers:
  - url: https://api.example.com
tags: [Auth, Catalog, Cart, RFQ, Quotes, Orders, Invoices, Payments]
security:
  - storefrontAuth: []

paths:
  /storefront/auth/login:
    post:
      tags: [Auth]
      security: []
      requestBody:
        required: true
        content:
          application/json:
            schema: { $ref: '#/components/schemas/LoginRequest' }
      responses:
        '200':
          description: OK
          content:
            application/json:
              schema: { $ref: '#/components/schemas/AuthToken' }
        '401': { $ref: '#/components/responses/Error' }

  /storefront/products:
    get:
      tags: [Catalog]
      parameters:
        - { name: category, in: query, schema: { type: string } }
        - { name: q, in: query, schema: { type: string } }
        - { name: filter, in: query, description: 'JSONB attr filter', schema: { type: string } }
        - { name: page, in: query, schema: { type: integer, default: 1 } }
        - { name: page_size, in: query, schema: { type: integer, default: 24 } }
      responses:
        '200':
          description: OK
          content:
            application/json:
              schema: { $ref: '#/components/schemas/ProductList' }

  /storefront/products/{slug}:
    get:
      tags: [Catalog]
      parameters:
        - { name: slug, in: path, required: true, schema: { type: string } }
      responses:
        '200':
          description: OK
          content:
            application/json:
              schema: { $ref: '#/components/schemas/Product' }
        '404': { $ref: '#/components/responses/Error' }

  /storefront/cart:
    get:
      tags: [Cart]
      responses:
        '200':
          description: OK
          content:
            application/json:
              schema: { $ref: '#/components/schemas/Cart' }
  /storefront/cart/items:
    post:
      tags: [Cart]
      requestBody:
        required: true
        content:
          application/json:
            schema: { $ref: '#/components/schemas/AddCartItem' }
      responses:
        '200':
          description: OK
          content:
            application/json:
              schema: { $ref: '#/components/schemas/Cart' }

  /storefront/rfqs:
    post:
      tags: [RFQ]
      requestBody:
        required: true
        content:
          application/json:
            schema: { $ref: '#/components/schemas/CreateRFQ' }
      responses:
        '201':
          description: Created
          content:
            application/json:
              schema: { $ref: '#/components/schemas/RFQ' }

  /storefront/quotes/{publicId}/accept:
    post:
      tags: [Quotes]
      parameters:
        - { name: publicId, in: path, required: true, schema: { type: string, format: uuid } }
      responses:
        '200':
          description: Quote accepted; order created
          content:
            application/json:
              schema: { $ref: '#/components/schemas/Order' }
        '409':
          description: Quote expired or already accepted
          content:
            application/json:
              schema: { $ref: '#/components/responses/Error' }

  /storefront/orders:
    get:
      tags: [Orders]
      responses:
        '200':
          description: OK
          content:
            application/json:
              schema: { $ref: '#/components/schemas/OrderList' }
    post:
      tags: [Orders]
      description: Place an order from the active cart
      requestBody:
        required: true
        content:
          application/json:
            schema: { $ref: '#/components/schemas/PlaceOrder' }
      responses:
        '201':
          description: Created
          content:
            application/json:
              schema: { $ref: '#/components/schemas/Order' }
        '402':
          description: Payment/credit gate failed
          content:
            application/json:
              schema: { $ref: '#/components/responses/Error' }

  /storefront/invoices:
    get:
      tags: [Invoices]
      responses:
        '200':
          description: OK
          content:
            application/json:
              schema: { $ref: '#/components/schemas/InvoiceList' }

components:
  securitySchemes:
    storefrontAuth: { type: http, scheme: bearer, bearerFormat: JWT }
    adminAuth:      { type: http, scheme: bearer, bearerFormat: JWT }
  responses:
    Error:
      description: Error
      content:
        application/json:
          schema: { $ref: '#/components/schemas/Error' }
  schemas:
    Error:
      type: object
      required: [code, message]
      properties:
        code:    { type: string }
        message: { type: string }
        details: { type: object, additionalProperties: true }
    Money:
      type: object
      required: [amount, currency]
      properties:
        amount:   { type: string, description: 'decimal as string' }
        currency: { type: string, minLength: 3, maxLength: 3 }
    LoginRequest:
      type: object
      required: [email, password]
      properties:
        email:    { type: string, format: email }
        password: { type: string }
    AuthToken:
      type: object
      required: [token, expires_at]
      properties:
        token:      { type: string }
        expires_at: { type: string, format: date-time }
    Product:
      type: object
      required: [public_id, sku, name, slug, status]
      properties:
        public_id:   { type: string, format: uuid }
        sku:         { type: string }
        name:        { type: string }
        slug:        { type: string }
        description: { type: string }
        status:      { type: string, enum: [draft, active, disabled] }
        attributes:  { type: object, additionalProperties: true }
        price:       { $ref: '#/components/schemas/Money' }
        available:   { type: number }
        media:
          type: array
          items: { type: object, properties: { url: { type: string }, alt: { type: string } } }
    ProductList:
      type: object
      properties:
        items: { type: array, items: { $ref: '#/components/schemas/Product' } }
        page:  { type: integer }
        total: { type: integer }
        facets:
          type: array
          items:
            type: object
            properties:
              attribute: { type: string }
              values:
                type: array
                items: { type: object, properties: { value: {type: string}, count: {type: integer} } }
    AddCartItem:
      type: object
      required: [product_public_id, quantity]
      properties:
        product_public_id: { type: string, format: uuid }
        quantity:          { type: number }
        unit:              { type: string, default: each }
    Cart:
      type: object
      properties:
        public_id: { type: string, format: uuid }
        currency:  { type: string }
        items:
          type: array
          items:
            type: object
            properties:
              product_public_id: { type: string, format: uuid }
              name:       { type: string }
              quantity:   { type: number }
              unit_price: { $ref: '#/components/schemas/Money' }
              row_total:  { $ref: '#/components/schemas/Money' }
        subtotal: { $ref: '#/components/schemas/Money' }
    CreateRFQ:
      type: object
      required: [items]
      properties:
        notes: { type: string }
        items:
          type: array
          items:
            type: object
            required: [product_public_id, quantity]
            properties:
              product_public_id: { type: string, format: uuid }
              quantity:          { type: number }
              target_price:      { type: string }
    RFQ:
      type: object
      properties:
        public_id: { type: string, format: uuid }
        status:    { type: string, enum: [draft, submitted, quoted, accepted, declined, expired, cancelled] }
        items:     { type: array, items: { type: object, additionalProperties: true } }
    PlaceOrder:
      type: object
      required: [billing_address, shipping_address, payment_method]
      properties:
        po_number:        { type: string }
        requested_delivery_date: { type: string, format: date }
        billing_address:  { $ref: '#/components/schemas/Address' }
        shipping_address: { $ref: '#/components/schemas/Address' }
        payment_method:   { type: string, enum: [card, ach, invoice, po, mpesa] }
    Address:
      type: object
      required: [line1, city, country]
      properties:
        line1:       { type: string }
        line2:       { type: string }
        city:        { type: string }
        region:      { type: string }
        postal_code: { type: string }
        country:     { type: string, minLength: 2, maxLength: 2 }
    Order:
      type: object
      properties:
        public_id:   { type: string, format: uuid }
        status:      { type: string, enum: [pending, confirmed, processing, shipped, delivered, closed, on_hold, cancelled] }
        currency:    { type: string }
        grand_total: { $ref: '#/components/schemas/Money' }
        items:       { type: array, items: { type: object, additionalProperties: true } }
    OrderList:
      type: object
      properties:
        items: { type: array, items: { $ref: '#/components/schemas/Order' } }
        page:  { type: integer }
        total: { type: integer }
    InvoiceList:
      type: object
      properties:
        items:
          type: array
          items:
            type: object
            properties:
              public_id:   { type: string, format: uuid }
              status:      { type: string, enum: [draft, issued, paid, overdue, void] }
              grand_total: { $ref: '#/components/schemas/Money' }
              due_at:      { type: string, format: date-time }
```
Admin paths (`/admin/...`) follow the same patterns under `adminAuth`; generate them from the same component schemas (orders, quotes, products, customers, pricing, CMS, workflow). Keep one OpenAPI file as the single source of truth — both the Go server stubs and the TS client are generated from it, so they can never drift.

---

# 6. Vue (admin) + Nuxt (storefront) component breakdown

Two separate frontend apps, one shared generated API client (`@app/api-client`, generated from §5) and a shared design-token package.

## 6.1 Admin SPA (Vue 3 + Vite + Pinia + Vue Router)

### App shell & cross-cutting
- `App.vue` → `AppLayout` (`SidebarNav` filtered by the user's permissions, `TopBar`, `<RouterView/>`).
- `router/` — route records carry `meta.permission`; a global `beforeEach` guard checks the auth store and redirects unauthorized routes.
- Stores (Pinia): `useAuthStore` (token, user, permissions), plus one store per domain (`useCustomers`, `useProducts`, `usePricing`, `useRfqs`, `useQuotes`, `useOrders`, `useInvoices`, `useCms`, `useWorkflow`, `useCrm`).
- API access via the generated client; a request interceptor attaches the bearer token and routes 401 → re-auth.

### Shared components
- `DataGrid` — server-side pagination/sort/filter; takes a `fetch(params)` fn + column defs; every list view uses it.
- `EntityForm` — schema-driven form (renders fields from a config); `AttributeFields` renders product attributes from the attribute family.
- `StatusBadge`, `MoneyDisplay`, `ConfirmDialog`, `AsyncButton`, `Drawer`.

### Module views (route → key components → endpoints)
| Route | Components | Endpoints |
|---|---|---|
| `/customers` | `CustomerList` (DataGrid) | `GET /admin/customers` |
| `/customers/:id` | `CustomerDetail`, `CustomerUsersTab`, `AddressesTab`, `HierarchyTree` | `GET/PUT /admin/customers/{id}` |
| `/products` | `ProductList` (DataGrid) | `GET /admin/products` |
| `/products/:id` | `ProductDetail`, `AttributeFields`, `VariantsTab`, `MediaTab`, `CategoryPicker` | `GET/PUT /admin/products/{id}` |
| `/pricing` | `PriceListList`, `PriceListEditor`, `AssignmentEditor` | `GET/PUT /admin/price-lists/*` |
| `/rfqs` | `RfqList`, `RfqDetail` | `GET /admin/rfqs` |
| `/quotes/:id` | **`QuoteEditor`** (line editor, discounting, version history viewer, "Send") | `PUT/POST /admin/quotes/{id}`, `/send` |
| `/orders` | `OrderList` | `GET /admin/orders` |
| `/orders/:id` | `OrderDetail`, `StatusTimeline`, `ShipmentsTab`, `InvoicesTab` | `GET /admin/orders/{id}`, `PATCH .../status` |
| `/crm` | **`PipelineBoard`** (kanban; drag = stage change), `OpportunityDrawer`, `LeadList` | `GET /admin/pipelines/{id}/board`, `PATCH .../stage` |
| `/cms/pages/:id` | **`PageEditor`** (`BlockList` + `BlockInspector`; block registry of editors), `SeoPanel` | `PUT /admin/pages/{id}`, `/publish` |
| `/workflow/:code` | **`WorkflowDesigner`** (state/transition graph editor, `GuardActionPicker`) | `GET/PUT /admin/workflows/{code}` |
| `/automation` | `AutomationRuleList`, `RuleBuilder` (event + conditions + actions) | `GET/PUT /admin/automation-rules/*` |

The three flagged views (`QuoteEditor`, `PageEditor`/block builder, `WorkflowDesigner`) are the build-effort hotspots — budget accordingly.

## 6.2 Storefront (Nuxt 3, SSR)

### Rendering & data
- SSR via `useAsyncData`/`useFetch` against the storefront API so product/category/CMS pages render server-side (SEO). Per-page `useSeoMeta` + JSON-LD via `useHead`.
- Consider route rules: cache/ISR catalog pages (`routeRules: { '/c/**': { swr: 600 } }`), but keep cart/account/checkout `ssr: true` + no-cache.
- Auth: customer-user JWT in an httpOnly cookie; a server middleware injects it into API calls; `definePageMeta({ middleware: 'auth' })` guards `/account/**`, `/checkout`, `/quotes/**`.

### Routes / pages → components → endpoints
| Route | Page → components | Endpoints |
|---|---|---|
| `/` | `index.vue` → `CmsRenderer` (renders blocks) | `GET /storefront/pages/home` |
| `/c/[slug]` | `category/[slug].vue` → `FacetSidebar`, `ProductGrid`(`ProductCard`), `Pagination` | `GET /storefront/products?category=` |
| `/p/[slug]` | `product/[slug].vue` → `Gallery`, `PriceBlock`, `QtyInput`, `AddToCart`, `AddToList` | `GET /storefront/products/{slug}` |
| `/search` | `search.vue` → reuse `ProductGrid` + `FacetSidebar` | `GET /storefront/products?q=` |
| `/cart` | `cart.vue` → `CartLineList`, `CartSummary` | `GET /storefront/cart`, `POST /cart/items` |
| `/checkout` | `checkout.vue` → `AddressStep`, `ShippingStep`, `PaymentStep`, `ReviewStep` | `POST /storefront/orders` |
| `/account/orders` | `OrderHistory`(reuse DataGrid-lite), `ReorderButton` | `GET /storefront/orders` |
| `/account/lists` | `ShoppingLists`, `ListEditor` | `GET/PUT /storefront/shopping-lists/*` |
| `/account/invoices` | `InvoiceList`, `PayInvoice` | `GET /storefront/invoices`, `POST /payments` |
| `/rfq` | `RfqBuilder` (from list or ad-hoc) | `POST /storefront/rfqs` |
| `/quotes/[publicId]` | **`QuoteThread`** (line view, revision history, Accept/Decline) | `GET /storefront/quotes/{id}`, `/accept` |

### Stores (Pinia, storefront)
- `useAuth` (customer user + session), `useCart` (optimistic add/update; re-validates price at checkout), `useCatalogFilters` (facet state synced to URL query).

### Performance notes
- `combined_prices` makes price reads O(1); the product page fetches price + availability in the single product call.
- Facets come from the `ProductList.facets` payload (computed server-side over the JSONB GIN index), so the sidebar needs no extra round-trips.

---

# 7. Build-order addendum (this pack)

Slot these after the Pack 1 commerce spine (its §13 steps 1–10):

11. **Workflow engine** (§3) — generic engine + registry; migrate the Pack 1 order/quote/RFQ hardcoded statuses onto workflow definitions once stable. Wire guards/actions you already need (approval, reserve inventory, send email).
12. **Automation engine** (§3.5) — event bus + dispatcher + river periodic jobs; move quote-expiry and overdue-invoice logic here.
13. **Integration framework** (§4) — registry + webhook endpoint + idempotency + `sync_logs`; then one payment adapter (M-Pesa or Stripe), then email, then tax (local VAT first).
14. **CRM** (§1) and **CMS** (§2) — independent of the commerce spine; can be built in parallel by a second track.
15. **OpenAPI as source of truth** (§5) — stand this up early in fact; generate Go stubs + TS client and keep both frontends consuming generated types.
16. **Frontends** (§6) — admin SPA and Nuxt storefront against the generated client; build the three hotspot views (QuoteEditor, PageEditor, WorkflowDesigner) last within their modules.

---

*End of Pack 2. Remaining for a Pack 3 if wanted: reporting/BI module, DAM with image transformation pipeline, punchout (OCI/cXML) + EDI specs, field-sales offline-sync design, and the full admin OpenAPI path inventory.*
