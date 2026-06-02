# Module Specifications — Pack 3
## Reporting & BI · DAM · Punchout/EDI · Field-Sales Offline Sync · Admin API Inventory

| Field | Value |
|---|---|
| Companion to | PRD v0.2 + Implementation Packs 1 & 2 |
| Stack | Go (chi + sqlc + river) · PostgreSQL 16+ · Vue 3 (admin) + Nuxt 3 (storefront) |
| Scope | The remaining V2 modules + the complete admin API path inventory |
| Status | Draft v0.1 |
| Last updated | 2 June 2026 |

Inherits all conventions from Pack 1 §0. Every FK resolves to a table from Packs 1–2 or this pack. This pack completes the specification series; after it, the next step is scaffolding the Go skeleton.

---

# 1. Reporting & analytics / BI

Principle: **users never write raw SQL.** Reports are a constrained model (entity + dimensions + measures + filters) compiled server-side to safe, parameterized SQL. Heavy rollups are precomputed as materialized views refreshed by a periodic job. External BI reads from a read replica / reporting views, not the live OLTP path.

### 1.1 User stories
- As a **manager**, I view operational dashboards (sales, orders, top products, customer activity) that refresh on a schedule.
- As an **analyst**, I build a custom report by picking an entity, dimensions, measures, and filters — no SQL.
- As an **analyst**, I schedule a report to export (CSV/XLSX) and email on a cadence.
- As a **BI engineer**, I connect a BI tool to a read replica / reporting schema without touching production tables.

### 1.2 Acceptance criteria
- A report definition compiles only to whitelisted columns/measures for its entity; an unknown field is rejected at save, not at run.
- Dashboard widgets read from materialized views (or cached report runs), never ad-hoc heavy scans, so a dashboard load issues bounded, fast queries.
- A scheduled report run produces a file artifact (object storage URL) and a `report_runs` row; failures are retried by the queue and surfaced, never silently skipped.
- Materialized views refresh on a defined cadence (`river` periodic job) using `REFRESH MATERIALIZED VIEW CONCURRENTLY`.

### 1.3 Schema
```sql
CREATE TABLE dashboards (
  id              BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  organization_id BIGINT NOT NULL REFERENCES organizations(id),
  owner_user_id   BIGINT REFERENCES users(id),
  name            text NOT NULL,
  layout          JSONB NOT NULL DEFAULT '[]'::jsonb,  -- grid positions
  is_shared       boolean NOT NULL DEFAULT false,
  created_at      timestamptz NOT NULL DEFAULT now(),
  updated_at      timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE dashboard_widgets (
  id            BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  dashboard_id  BIGINT NOT NULL REFERENCES dashboards(id) ON DELETE CASCADE,
  type          text NOT NULL CHECK (type IN ('kpi','line','bar','pie','table')),
  title         text NOT NULL,
  source        JSONB NOT NULL,         -- {view:'mv_daily_sales', dims:[...], measures:[...], filters:[...]}
  sort_order    int NOT NULL DEFAULT 0
);

CREATE TABLE report_definitions (
  id              BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  organization_id BIGINT NOT NULL REFERENCES organizations(id),
  name            text NOT NULL,
  entity          text NOT NULL,         -- 'orders','invoices','opportunities',...
  dimensions      JSONB NOT NULL DEFAULT '[]'::jsonb,  -- ['customer','month']
  measures        JSONB NOT NULL DEFAULT '[]'::jsonb,  -- [{field:'grand_total',agg:'sum'}]
  filters         JSONB NOT NULL DEFAULT '[]'::jsonb,  -- [{field,op,value}]
  created_by      BIGINT REFERENCES users(id),
  created_at      timestamptz NOT NULL DEFAULT now(),
  updated_at      timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE report_schedules (
  id                    BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  report_definition_id  BIGINT NOT NULL REFERENCES report_definitions(id) ON DELETE CASCADE,
  cadence               text NOT NULL CHECK (cadence IN ('daily','weekly','monthly')),
  format                text NOT NULL DEFAULT 'csv' CHECK (format IN ('csv','xlsx')),
  recipients            JSONB NOT NULL DEFAULT '[]'::jsonb,  -- email addresses
  is_active             boolean NOT NULL DEFAULT true
);

CREATE TABLE report_runs (
  id                    BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  report_definition_id  BIGINT NOT NULL REFERENCES report_definitions(id),
  status                text NOT NULL DEFAULT 'running'
                          CHECK (status IN ('running','ok','error')),
  row_count             int,
  file_url              text,
  error                 text,
  started_at            timestamptz NOT NULL DEFAULT now(),
  finished_at           timestamptz
);
```

### 1.4 Materialized views (precomputed rollups)
```sql
CREATE MATERIALIZED VIEW mv_daily_sales AS
SELECT o.organization_id,
       date_trunc('day', o.created_at)::date AS day,
       o.currency,
       count(*)                AS order_count,
       sum(o.grand_total)      AS revenue
  FROM orders o
 WHERE o.status NOT IN ('cancelled')
 GROUP BY 1,2,3;
CREATE UNIQUE INDEX uq_mv_daily_sales ON mv_daily_sales(organization_id, day, currency);

CREATE MATERIALIZED VIEW mv_top_products AS
SELECT oi.product_id, o.organization_id,
       date_trunc('month', o.created_at)::date AS month,
       sum(oi.quantity)  AS qty,
       sum(oi.row_total) AS revenue
  FROM order_items oi JOIN orders o ON o.id = oi.order_id
 WHERE o.status NOT IN ('cancelled')
 GROUP BY 1,2,3;
CREATE UNIQUE INDEX uq_mv_top_products ON mv_top_products(product_id, organization_id, month);

-- Periodic river job: REFRESH MATERIALIZED VIEW CONCURRENTLY mv_daily_sales; (etc.)
```

### 1.5 Report compiler (Go contract)
```go
// Compiles a constrained ReportDefinition into safe parameterized SQL.
type ReportCompiler interface {
    Compile(def ReportDefinition) (sql string, args []any, err error)
}
// Each entity registers an allow-list of dimensions/measures + their SQL fragments.
// Unknown fields -> error. Aggregations limited to sum/avg/count/min/max.
```

### 1.6 BI integration
Provision a Postgres **read replica**; expose a `reporting` schema of stable views (versioned column contracts) for external BI tools. Alternatively, scheduled export of the materialized views to object storage (CSV/Parquet) for a warehouse. Never point BI at the OLTP primary.

### 1.7 API surface
`GET /admin/dashboards/{id}`, `PUT /admin/dashboards/{id}`,
`POST /admin/reports` (definition), `POST /admin/reports/{id}/run`, `GET /admin/reports/{id}/runs`,
`POST /admin/reports/{id}/schedule`.

---

# 2. Digital Asset Management (DAM) + image pipeline

Extends `media_assets` (Pack 2 §2.3). Upload once, derive responsive renditions asynchronously, serve optimized formats. Supports both **pre-generated presets** and **signed on-the-fly transforms** with edge caching.

### 2.1 User stories
- As a **content editor**, I upload an image once and get responsive renditions (thumb, card, hero) in modern formats (WebP/AVIF) automatically.
- As a **content editor**, I organize assets in folders and tag them for reuse across products and CMS.
- As the **storefront**, I request an image at a needed size/format and get an optimized, cached rendition.

### 2.2 Acceptance criteria
- An upload stores the original, computes a checksum (dedupe), and enqueues rendition jobs; the asset is usable immediately (original) and progressively gains renditions.
- Each `(asset, preset)` produces at most one rendition (idempotent regeneration).
- On-the-fly transform URLs are signed (HMAC over params) so the transform endpoint can't be abused to generate arbitrary sizes; results are cached.
- Re-uploading an identical checksum returns the existing asset rather than duplicating storage.

### 2.3 Schema (extends Pack 2 media_assets)
```sql
ALTER TABLE media_assets
  ADD COLUMN checksum   text,
  ADD COLUMN size_bytes bigint,
  ADD COLUMN status     text NOT NULL DEFAULT 'ready'
               CHECK (status IN ('uploading','processing','ready','error'));
CREATE UNIQUE INDEX uq_media_checksum ON media_assets(organization_id, checksum) WHERE checksum IS NOT NULL;

CREATE TABLE transformation_presets (
  id              BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  organization_id BIGINT NOT NULL REFERENCES organizations(id),
  name            text NOT NULL,         -- 'thumb','card','hero'
  width           int,
  height          int,
  fit             text NOT NULL DEFAULT 'cover' CHECK (fit IN ('cover','contain','fill','inside')),
  format          text NOT NULL DEFAULT 'webp' CHECK (format IN ('webp','avif','jpeg','png')),
  quality         int NOT NULL DEFAULT 82,
  UNIQUE (organization_id, name)
);

CREATE TABLE media_renditions (
  id              BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  media_asset_id  BIGINT NOT NULL REFERENCES media_assets(id) ON DELETE CASCADE,
  preset          text NOT NULL,
  url             text NOT NULL,
  width           int,
  height          int,
  format          text,
  size_bytes      bigint,
  created_at      timestamptz NOT NULL DEFAULT now(),
  UNIQUE (media_asset_id, preset)
);

CREATE TABLE media_tags (
  media_asset_id  BIGINT NOT NULL REFERENCES media_assets(id) ON DELETE CASCADE,
  tag             text NOT NULL,
  PRIMARY KEY (media_asset_id, tag)
);
```

### 2.4 Go contracts
```go
type BlobStore interface {            // S3-compatible or local FS behind the same interface
    Put(ctx context.Context, key string, r io.Reader, contentType string) (url string, err error)
    Get(ctx context.Context, key string) (io.ReadCloser, error)
    Delete(ctx context.Context, key string) error
}

type ImageProcessor interface {       // recommended impl: govips (libvips) for speed
    Transform(ctx context.Context, src io.Reader, p Preset) (out []byte, w, h int, err error)
}
```
Pipeline: `POST /admin/media` → store original (BlobStore) → set `status=processing` → enqueue one `generate_rendition` river job per active preset → each job runs `ImageProcessor.Transform`, stores the rendition, inserts `media_renditions`, and flips asset `status=ready` when all complete. Optionally run `jpegoptim`/`pngquant` as a post step (matches the reference platform). On-the-fly endpoint: `GET /media/{assetPublicId}/t/{signedParams}` → check signature → return cached rendition or generate-and-cache.

### 2.5 API surface
`POST /admin/media` (multipart), `GET /admin/media` (filter by folder/tag), `PUT /admin/media/{id}` (tags/alt/folder), `POST /admin/transformation-presets`, `GET /media/{publicId}/t/{signed}`.

---

# 3. Punchout (OCI / cXML) + EDI

For buyers whose procurement systems (SAP Ariba, Coupa, SAP ERP) integrate directly. **Punchout** lets a buyer shop our storefront from inside their procurement app and return a cart. **EDI** exchanges purchase orders, acknowledgements, ASNs, and invoices as structured documents.

### 3.1 Punchout — user stories & flow
- As a **procurement buyer**, I click "punchout" in my procurement system, land authenticated in our storefront, build a cart, and transfer it back as a requisition.

Flow (cXML PunchOut; OCI is the SAP equivalent with form-field POSTs):
1. Procurement system POSTs a `PunchOutSetupRequest` (cXML) to our setup endpoint with shared-secret credentials and a `buyer_cookie` + `return_url`.
2. We validate the trading partner, create a `punchout_sessions` row + a storefront session bound to the mapped `customer`, and respond with a start URL.
3. Buyer shops in **punchout mode** (a flag on the session; checkout is replaced by a "Transfer cart" action).
4. On transfer, we POST a `PunchOutOrderMessage` (cXML) back to `return_url` containing the cart lines and our pricing.
5. The buyer's PO later arrives as a cXML `OrderRequest` or EDI 850 → becomes an `order`.

### 3.2 EDI — user stories & document set
- As **operations**, inbound EDI **850** (purchase order) creates an order automatically; we return **855** (PO acknowledgement); on shipment we send **856** (ASN); finance sends **810** (invoice).

| X12 | EDIFACT | Direction | Maps to |
|---|---|---|---|
| 850 | ORDERS | inbound | `orders` (create) |
| 855 | ORDRSP | outbound | order acknowledgement |
| 856 | DESADV | outbound | `shipments` (ASN) |
| 810 | INVOIC | outbound | `invoices` |
Transport: AS2, SFTP, or VAN; per-partner config.

### 3.3 Acceptance criteria
- A `PunchOutSetupRequest` with invalid credentials or unknown partner is rejected (401) and logged.
- A punchout session expires after a configured TTL; an expired session cannot transfer a cart.
- An inbound 850 is parsed, validated against the partner's catalog/pricing, and either creates an order or is flagged `error` with a reason — never partially applied.
- Every EDI document is stored raw (`edi_documents.raw_payload`) before parsing, so re-processing is possible.
- Outbound documents carry a unique control number; duplicates are idempotent.

### 3.4 Schema
```sql
CREATE TABLE trading_partners (
  id              BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  organization_id BIGINT NOT NULL REFERENCES organizations(id),
  customer_id     BIGINT REFERENCES customers(id),     -- mapped commerce customer
  name            text NOT NULL,
  protocol        text NOT NULL CHECK (protocol IN ('cxml','oci','edi_x12','edifact')),
  transport       text CHECK (transport IN ('https','as2','sftp','van')),
  config          JSONB NOT NULL DEFAULT '{}'::jsonb,   -- endpoints, identifiers (non-secret)
  is_active       boolean NOT NULL DEFAULT true,
  created_at      timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE punchout_sessions (
  id                 BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  public_id          UUID NOT NULL DEFAULT gen_random_uuid() UNIQUE,
  trading_partner_id BIGINT NOT NULL REFERENCES trading_partners(id),
  customer_id        BIGINT NOT NULL REFERENCES customers(id),
  buyer_cookie       text NOT NULL,        -- echoed back in PunchOutOrderMessage
  operation          text NOT NULL DEFAULT 'create'
                       CHECK (operation IN ('create','edit','inspect')),
  return_url         text NOT NULL,
  cart_id            BIGINT REFERENCES carts(id),
  status             text NOT NULL DEFAULT 'active'
                       CHECK (status IN ('active','returned','expired')),
  created_at         timestamptz NOT NULL DEFAULT now(),
  expires_at         timestamptz NOT NULL
);

CREATE TABLE edi_documents (
  id                 BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  organization_id    BIGINT NOT NULL REFERENCES organizations(id),
  trading_partner_id BIGINT NOT NULL REFERENCES trading_partners(id),
  direction          text NOT NULL CHECK (direction IN ('inbound','outbound')),
  doc_type           text NOT NULL,        -- '850','855','856','810' / 'ORDERS' etc.
  status             text NOT NULL DEFAULT 'received'
                       CHECK (status IN ('received','parsed','mapped','processed','sent','acknowledged','error')),
  control_number     text,
  raw_payload        text NOT NULL,
  parsed             JSONB,
  related_entity_type text,                -- 'order','invoice','shipment'
  related_entity_id   BIGINT,
  error              text,
  created_at         timestamptz NOT NULL DEFAULT now(),
  processed_at       timestamptz,
  UNIQUE (trading_partner_id, direction, doc_type, control_number)
);
```

### 3.5 API / endpoints
`POST /punchout/setup` (cXML PunchOutSetupRequest), `GET /punchout/start/{publicId}`,
`POST /punchout/transfer/{publicId}` (emits PunchOutOrderMessage),
`POST /edi/inbound/{partnerId}` (or SFTP/AS2 ingest worker) → enqueues parse job.

---

# 4. Field-sales offline sync

Reps work where connectivity is poor. The field app (a Nuxt PWA or native client) holds a **scoped local subset** — the rep's assigned customers, the catalog, resolved pricing, and their own draft orders/quotes/activities — and syncs via a cursor-based delta protocol. This is the most distributed-systems-sensitive design in the platform; keep the rules simple and explicit.

### 4.1 Design principles
- **Scoped data**: a device only ever pulls data for `assigned_sales_rep_id = rep` plus shared read-only catalog/pricing. Never the whole org.
- **Read-only vs. writable**: catalog, pricing, customers are **read-only on device** (pulled, never pushed). Orders, quotes, and activities are **writable** (created/edited offline, pushed up).
- **Client-generated IDs**: every locally created record gets a client UUID, used as the idempotency key on push, so a retried push never duplicates.
- **Conflict policy**:
  - New documents (orders/quotes/activities) are **append-only** → always accepted after server validation; no conflict possible.
  - Edits to a rep-owned draft → **rep wins**.
  - Edits to shared mutable data → **last-write-wins by `updated_at`**; if the server copy is newer, reject and return the server version for the client to reconcile.
- **Cursor**: a monotonic `change_log.id` is the sync watermark. The client stores the last cursor it has seen.

### 4.2 User stories
- As a **field rep**, I open the app offline and see my customers, the catalog, and their pricing.
- As a **field rep**, I create an order/quote offline; it syncs and is confirmed when I reconnect.
- As the **system**, I reconcile concurrent edits deterministically without losing the rep's new documents.

### 4.3 Acceptance criteria
- A pull with `since=<cursor>` returns only changes after that cursor, scoped to the rep, plus the new high-water cursor.
- A push of N changes is idempotent: replaying the same `client_change_id`s applies nothing new and returns the same id→server mapping.
- A rejected edit (server newer) returns `409` with the current server record; the client surfaces it for re-entry, never silently overwrites.
- Catalog/pricing pushes are refused (`403`) — those are read-only on device.

### 4.4 Schema
```sql
CREATE TABLE field_devices (
  id               BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  user_id          BIGINT NOT NULL REFERENCES users(id),    -- the sales rep
  device_uuid      UUID NOT NULL,
  platform         text,
  last_sync_cursor BIGINT NOT NULL DEFAULT 0,
  last_seen_at     timestamptz,
  created_at       timestamptz NOT NULL DEFAULT now(),
  UNIQUE (user_id, device_uuid)
);

-- Append-only outbox; its id IS the sync cursor. Written by triggers or the repo layer
-- on every change to a syncable entity.
CREATE TABLE change_log (
  id           BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  organization_id BIGINT NOT NULL REFERENCES organizations(id),
  scope_rep_id BIGINT,                 -- which rep this change is visible to (null = global, e.g. catalog)
  entity_type  text NOT NULL,
  entity_id    BIGINT NOT NULL,
  op           text NOT NULL CHECK (op IN ('upsert','delete')),
  payload      JSONB NOT NULL,
  created_at   timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX idx_change_log_scope ON change_log(scope_rep_id, id);
CREATE INDEX idx_change_log_global ON change_log(id) WHERE scope_rep_id IS NULL;

-- Idempotency + audit for client pushes.
CREATE TABLE sync_push_log (
  id               BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  device_id        BIGINT NOT NULL REFERENCES field_devices(id),
  client_change_id UUID NOT NULL,
  entity_type      text NOT NULL,
  op               text NOT NULL,
  status           text NOT NULL CHECK (status IN ('applied','conflict','rejected')),
  server_entity_id BIGINT,
  detail           JSONB,
  created_at       timestamptz NOT NULL DEFAULT now(),
  UNIQUE (device_id, client_change_id)
);
```

### 4.5 Sync protocol (endpoints)
```
GET  /field/sync/pull?since={cursor}
     -> { cursor: <new high water>, changes: [{entity_type, op, payload}, ...] }
        (scoped: change_log WHERE (scope_rep_id = rep OR scope_rep_id IS NULL) AND id > since
                 ORDER BY id LIMIT batch)

POST /field/sync/push
     body: { changes: [{client_change_id, entity_type, op, payload, base_updated_at?}, ...] }
     -> { results: [{client_change_id, status, server_public_id?, server_record?}], cursor }
        per change: dedupe on (device, client_change_id); validate; apply per conflict policy.
```
The pull is bounded (batch + LIMIT) and resumable. The push is a single transaction per change with the `sync_push_log` unique constraint guaranteeing idempotency.

---

# 5. Full admin OpenAPI path inventory

The complete admin surface (`adminAuth` security), by tag. Schemas reuse the components defined in Pack 2 §5 (extend that single OpenAPI file — do not fork it). Storefront paths are in Pack 2 §5; this is the back-office complement.

### Auth & identity
| Method | Path | Purpose |
|---|---|---|
| POST | `/admin/auth/login` | Back-office login → bearer |
| POST | `/admin/auth/refresh` | Refresh token |
| GET | `/admin/me` | Current user + permissions |
| GET/POST | `/admin/users` | List / create users |
| GET/PUT/DELETE | `/admin/users/{id}` | Read / update / deactivate |
| POST/DELETE | `/admin/users/{id}/roles` | Assign / remove role |
| GET/POST | `/admin/roles` | List / create roles |
| PUT | `/admin/roles/{id}/permissions` | Set role permissions |

### Organization & settings
| Method | Path | Purpose |
|---|---|---|
| GET/POST | `/admin/organizations` | List / create |
| GET/POST | `/admin/websites` | List / create |
| GET/PUT | `/admin/websites/{id}` | Read / update |
| GET/PUT | `/admin/settings` | Hierarchical config get/set |

### Customers & accounts
| Method | Path | Purpose |
|---|---|---|
| GET/POST | `/admin/customers` | List / create |
| GET/PUT/DELETE | `/admin/customers/{id}` | Read / update / soft-delete |
| GET | `/admin/customers/{id}/hierarchy` | Ancestor/descendant tree |
| GET/POST | `/admin/customers/{id}/users` | Customer users |
| GET/POST | `/admin/customers/{id}/addresses` | Addresses |
| GET/POST | `/admin/customer-groups` | Groups |

### Catalog & PIM
| Method | Path | Purpose |
|---|---|---|
| GET/POST | `/admin/products` | List / create |
| GET/PUT/DELETE | `/admin/products/{id}` | Read / update / soft-delete |
| GET/POST | `/admin/products/{id}/variants` | Variant management |
| POST | `/admin/products/import` | Bulk import (async) |
| GET/POST | `/admin/attributes` | Attributes |
| GET/POST | `/admin/attribute-families` | Families |
| GET/POST | `/admin/categories` | Category tree |
| PUT | `/admin/catalog-visibility` | Per-customer/group visibility |

### Pricing
| Method | Path | Purpose |
|---|---|---|
| GET/POST | `/admin/price-lists` | List / create |
| GET/PUT | `/admin/price-lists/{id}` | Read / update |
| GET/POST | `/admin/price-lists/{id}/prices` | Prices (tiers) |
| GET/POST | `/admin/price-list-assignments` | Assignments |
| POST | `/admin/pricing/recompute` | Trigger combined-price rebuild |

### RFQ / Quotes / Orders
| Method | Path | Purpose |
|---|---|---|
| GET | `/admin/rfqs` | List RFQs |
| GET | `/admin/rfqs/{id}` | RFQ detail |
| POST | `/admin/rfqs/{id}/quote` | Create quote from RFQ |
| GET/POST | `/admin/quotes` | List / create (seller-initiated) |
| GET/PUT | `/admin/quotes/{id}` | Read / edit lines |
| POST | `/admin/quotes/{id}/send` | Send / revise |
| GET/POST | `/admin/orders` | List / create (on-behalf-of) |
| GET | `/admin/orders/{id}` | Detail |
| PATCH | `/admin/orders/{id}/status` | Transition status |

### Fulfilment & finance
| Method | Path | Purpose |
|---|---|---|
| GET/POST | `/admin/orders/{id}/shipments` | Create shipment / list |
| PATCH | `/admin/shipments/{id}/status` | Update shipment |
| GET/POST | `/admin/orders/{id}/invoices` | Issue / list invoices |
| POST | `/admin/invoices/{id}/pdf` | (Re)generate PDF |
| GET/POST | `/admin/payments` | Record / list payments |
| POST | `/admin/payments/{id}/refund` | Refund |

### Inventory
| Method | Path | Purpose |
|---|---|---|
| GET/POST | `/admin/warehouses` | Warehouses |
| GET/PUT | `/admin/inventory/{productId}` | Levels per warehouse |
| POST | `/admin/inventory/adjustments` | Manual adjustment (movement) |
| GET | `/admin/inventory/movements` | Movement ledger |

### CRM
| Method | Path | Purpose |
|---|---|---|
| GET/POST | `/admin/leads` | Leads |
| POST | `/admin/leads/{id}/convert` | Convert to customer+opp |
| GET/POST | `/admin/opportunities` | Opportunities |
| PATCH | `/admin/opportunities/{id}/stage` | Move stage |
| GET | `/admin/pipelines/{id}/board` | Pipeline board + forecast |
| GET/POST | `/admin/activities` | Activities |
| GET | `/admin/customers/{id}/timeline` | Unified timeline |

### CMS & DAM
| Method | Path | Purpose |
|---|---|---|
| GET/POST | `/admin/pages` | Content pages |
| PUT | `/admin/pages/{id}` | Edit blocks/SEO |
| POST | `/admin/pages/{id}/publish` | Publish |
| GET/POST | `/admin/menus` | Menus + items |
| GET/POST | `/admin/media` | Asset library |
| PUT | `/admin/media/{id}` | Tags/alt/folder |
| GET/POST | `/admin/transformation-presets` | Rendition presets |
| GET/POST | `/admin/redirects` | URL redirects |

### Workflow & automation
| Method | Path | Purpose |
|---|---|---|
| GET/POST | `/admin/workflows` | Definitions |
| GET/PUT | `/admin/workflows/{code}` | States/transitions |
| GET | `/admin/workflows/registry` | Available guards/actions |
| GET/POST | `/admin/automation-rules` | Rules |
| GET | `/admin/automation-rules/{id}/executions` | Run history |

### Integrations
| Method | Path | Purpose |
|---|---|---|
| GET/POST | `/admin/integrations` | Connections |
| PUT | `/admin/integrations/{id}` | Update config |
| POST | `/admin/integrations/{id}/test` | Test connection |
| GET | `/admin/integrations/{id}/logs` | Sync logs |
| GET/POST | `/admin/webhooks` | Subscriptions |
| GET | `/admin/webhooks/{id}/deliveries` | Delivery log |
| GET/POST | `/admin/trading-partners` | Punchout/EDI partners |
| GET | `/admin/edi/documents` | EDI document log |

### Reporting & DAM admin
| Method | Path | Purpose |
|---|---|---|
| GET/PUT | `/admin/dashboards/{id}` | Dashboards |
| GET/POST | `/admin/reports` | Report definitions |
| POST | `/admin/reports/{id}/run` | Run now |
| GET | `/admin/reports/{id}/runs` | Run history |
| POST | `/admin/reports/{id}/schedule` | Schedule |

---

# 6. Build-order addendum (this pack)

Slot after Packs 1–2 (commerce spine + the deferred modules). All Pack 3 items are V2 — do them only when the business need is real:

17. **Reporting** (§1) — start with the two materialized views + a couple of dashboard widgets; add the report builder later.
18. **DAM pipeline** (§2) — wire when CMS/product imagery volume justifies it; presets + async renditions first, on-the-fly transforms second.
19. **Punchout/EDI** (§3) — only when you onboard a procurement-integrated customer; build the one protocol that customer needs (cXML *or* OCI *or* X12), not all.
20. **Field-sales offline sync** (§4) — highest complexity; build last and only if reps genuinely work offline. The cursor + idempotency design is the whole game — get §4.1 rules right before any code.
21. **Admin API** (§5) — fold into the single OpenAPI file as each module lands; never let it drift from the implementation.

---

*End of Pack 3 — specification series complete. PRD v0.2 + Implementation Packs 1–3 now cover the full product: the commerce spine, the deferred modules, and the V2 surface, with one consistent Postgres schema and one OpenAPI contract throughout. Next: scaffold the Go project skeleton (package layout, chi router, sqlc + migrations, river worker, config) to drop all of this into.*
