-- RFQ -> Quote -> Order — Implementation Pack 1 §6.
-- State machines (enforced in app):
--   RFQ:   draft -> submitted -> quoted -> accepted | declined | expired | cancelled
--   QUOTE: draft -> sent -> (revised -> sent)* -> accepted | declined | expired
--   ORDER: pending -> confirmed -> processing -> shipped -> delivered -> closed
--                              \-> on_hold ; any -> cancelled (pre-fulfilment)
-- Orders are immutable records: SKUs, names, prices, and addresses are snapshotted.

CREATE TABLE rfqs (
  id               BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  public_id        UUID NOT NULL DEFAULT gen_random_uuid() UNIQUE,
  organization_id  BIGINT NOT NULL REFERENCES organizations(id),
  website_id       BIGINT NOT NULL REFERENCES websites(id),
  customer_id      BIGINT NOT NULL REFERENCES customers(id),
  customer_user_id BIGINT REFERENCES customer_users(id),
  status           text NOT NULL DEFAULT 'draft'
                     CHECK (status IN ('draft','submitted','quoted','accepted','declined','expired','cancelled')),
  notes            text,
  created_at       timestamptz NOT NULL DEFAULT now(),
  updated_at       timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX idx_rfqs_customer ON rfqs(customer_id);
CREATE TRIGGER trg_rfqs_updated BEFORE UPDATE ON rfqs
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE rfq_items (
  id           BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  rfq_id       BIGINT NOT NULL REFERENCES rfqs(id) ON DELETE CASCADE,
  product_id   BIGINT NOT NULL REFERENCES products(id),
  quantity     NUMERIC(15,4) NOT NULL,
  unit         text NOT NULL DEFAULT 'each',
  target_price NUMERIC(15,4),
  notes        text
);
CREATE INDEX idx_rfq_items_rfq ON rfq_items(rfq_id);

CREATE TABLE quotes (
  id                 BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  public_id          UUID NOT NULL DEFAULT gen_random_uuid() UNIQUE,
  organization_id    BIGINT NOT NULL REFERENCES organizations(id),
  website_id         BIGINT NOT NULL REFERENCES websites(id),
  customer_id        BIGINT NOT NULL REFERENCES customers(id),
  rfq_id             BIGINT REFERENCES rfqs(id),           -- null = seller-initiated
  sales_rep_user_id  BIGINT REFERENCES users(id),
  status             text NOT NULL DEFAULT 'draft'
                       CHECK (status IN ('draft','sent','revised','accepted','declined','expired')),
  currency           CHAR(3) NOT NULL,
  version            int NOT NULL DEFAULT 1,
  valid_until        timestamptz,
  subtotal           NUMERIC(15,4) NOT NULL DEFAULT 0,
  created_at         timestamptz NOT NULL DEFAULT now(),
  updated_at         timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX idx_quotes_customer ON quotes(customer_id);
CREATE INDEX idx_quotes_rfq ON quotes(rfq_id);
CREATE TRIGGER trg_quotes_updated BEFORE UPDATE ON quotes
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE quote_items (
  id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  quote_id    BIGINT NOT NULL REFERENCES quotes(id) ON DELETE CASCADE,
  product_id  BIGINT NOT NULL REFERENCES products(id),
  quantity    NUMERIC(15,4) NOT NULL,
  unit        text NOT NULL DEFAULT 'each',
  unit_price  NUMERIC(15,4) NOT NULL,
  discount    NUMERIC(15,4) NOT NULL DEFAULT 0,
  row_total   NUMERIC(15,4) NOT NULL
);
CREATE INDEX idx_quote_items_quote ON quote_items(quote_id);

-- Immutable negotiation history: one snapshot per sent version.
CREATE TABLE quote_revisions (
  id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  quote_id    BIGINT NOT NULL REFERENCES quotes(id) ON DELETE CASCADE,
  version     int NOT NULL,
  snapshot    JSONB NOT NULL,            -- full quote + items at send time
  created_by  text NOT NULL,             -- 'rep:{id}' or 'customer_user:{id}'
  created_at  timestamptz NOT NULL DEFAULT now(),
  UNIQUE (quote_id, version)
);

CREATE TABLE orders (
  id                      BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  public_id               UUID NOT NULL DEFAULT gen_random_uuid() UNIQUE,
  organization_id         BIGINT NOT NULL REFERENCES organizations(id),
  website_id              BIGINT NOT NULL REFERENCES websites(id),
  customer_id             BIGINT NOT NULL REFERENCES customers(id),
  customer_user_id        BIGINT REFERENCES customer_users(id),
  quote_id                BIGINT REFERENCES quotes(id),
  placed_by_sales_rep_id  BIGINT REFERENCES users(id),   -- on-behalf-of
  status                  text NOT NULL DEFAULT 'pending'
                            CHECK (status IN ('pending','confirmed','processing','shipped','delivered','closed','on_hold','cancelled')),
  currency                CHAR(3) NOT NULL,
  po_number               text,
  requested_delivery_date date,
  billing_address         JSONB NOT NULL DEFAULT '{}'::jsonb,   -- snapshot
  shipping_address        JSONB NOT NULL DEFAULT '{}'::jsonb,   -- snapshot
  subtotal                NUMERIC(15,4) NOT NULL DEFAULT 0,
  tax_total               NUMERIC(15,4) NOT NULL DEFAULT 0,
  shipping_total          NUMERIC(15,4) NOT NULL DEFAULT 0,
  grand_total             NUMERIC(15,4) NOT NULL DEFAULT 0,
  created_at              timestamptz NOT NULL DEFAULT now(),
  updated_at              timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX idx_orders_customer ON orders(customer_id);
CREATE INDEX idx_orders_status ON orders(status);
CREATE TRIGGER trg_orders_updated BEFORE UPDATE ON orders
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE order_items (
  id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  order_id    BIGINT NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
  product_id  BIGINT NOT NULL REFERENCES products(id),
  sku         text NOT NULL,            -- snapshot
  name        text NOT NULL,            -- snapshot
  quantity    NUMERIC(15,4) NOT NULL,
  unit        text NOT NULL DEFAULT 'each',
  unit_price  NUMERIC(15,4) NOT NULL,   -- snapshot
  tax_amount  NUMERIC(15,4) NOT NULL DEFAULT 0,
  row_total   NUMERIC(15,4) NOT NULL
);
CREATE INDEX idx_order_items_order ON order_items(order_id);

CREATE TABLE order_status_history (
  id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  order_id    BIGINT NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
  from_status text,
  to_status   text NOT NULL,
  changed_by  text NOT NULL,
  note        text,
  created_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX idx_order_status_history_order ON order_status_history(order_id);

-- Sales permissions for the demo admin role.
INSERT INTO role_permissions (role_id, permission)
SELECT r.id, p.permission
  FROM roles r
  CROSS JOIN (VALUES
    ('rfq.view'), ('rfq.manage'), ('quote.view'), ('quote.manage'), ('order.manage')
  ) AS p(permission)
 WHERE r.organization_id = 1 AND r.name = 'admin'
ON CONFLICT DO NOTHING;
