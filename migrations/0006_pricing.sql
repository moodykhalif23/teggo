-- Pricing engine — Implementation Pack 1 §4 + §12.1.
-- Deterministic resolution (customer > group > website default; higher priority
-- wins within a level; most-specific qty tier <= requested). Storefront reads
-- hit the precomputed combined_prices cache; a river job recomputes it.

CREATE TABLE price_lists (
  id              BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  organization_id BIGINT NOT NULL REFERENCES organizations(id),
  name            text NOT NULL,
  currency        CHAR(3) NOT NULL,
  is_default      boolean NOT NULL DEFAULT false,
  is_active       boolean NOT NULL DEFAULT true,
  created_at      timestamptz NOT NULL DEFAULT now(),
  updated_at      timestamptz NOT NULL DEFAULT now(),
  deleted_at      timestamptz,
  UNIQUE (organization_id, name)
);
CREATE TRIGGER trg_price_lists_updated BEFORE UPDATE ON price_lists
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- Backfill the FK declared FK-less in §2 (customers.default_price_list_id).
ALTER TABLE customers
  ADD CONSTRAINT fk_customers_default_price_list
  FOREIGN KEY (default_price_list_id) REFERENCES price_lists(id);

CREATE TABLE prices (
  id            BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  price_list_id BIGINT NOT NULL REFERENCES price_lists(id) ON DELETE CASCADE,
  product_id    BIGINT NOT NULL REFERENCES products(id) ON DELETE CASCADE,
  unit          text NOT NULL DEFAULT 'each',
  min_quantity  NUMERIC(15,4) NOT NULL DEFAULT 1,      -- tier threshold
  value         NUMERIC(15,4) NOT NULL,
  valid_from    timestamptz,
  valid_to      timestamptz,
  created_at    timestamptz NOT NULL DEFAULT now(),
  UNIQUE (price_list_id, product_id, unit, min_quantity)
);
CREATE INDEX idx_prices_product ON prices(product_id);

CREATE TABLE price_list_assignments (
  id                BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  price_list_id     BIGINT NOT NULL REFERENCES price_lists(id) ON DELETE CASCADE,
  customer_id       BIGINT REFERENCES customers(id) ON DELETE CASCADE,
  customer_group_id BIGINT REFERENCES customer_groups(id) ON DELETE CASCADE,
  website_id        BIGINT REFERENCES websites(id) ON DELETE CASCADE,
  priority          int NOT NULL DEFAULT 0,            -- higher wins within a level
  CHECK ( (customer_id IS NOT NULL)::int
        + (customer_group_id IS NOT NULL)::int
        + (website_id IS NOT NULL)::int = 1 )
);
CREATE INDEX idx_pla_customer ON price_list_assignments(customer_id);
CREATE INDEX idx_pla_group ON price_list_assignments(customer_group_id);
CREATE INDEX idx_pla_website ON price_list_assignments(website_id);
CREATE INDEX idx_pla_list ON price_list_assignments(price_list_id);

-- Precomputed resolved prices (the storefront read path). Group/website
-- fallbacks are flattened into per-customer rows by the recompute job.
CREATE TABLE combined_prices (
  customer_id   BIGINT NOT NULL REFERENCES customers(id) ON DELETE CASCADE,
  product_id    BIGINT NOT NULL REFERENCES products(id) ON DELETE CASCADE,
  unit          text NOT NULL DEFAULT 'each',
  min_quantity  NUMERIC(15,4) NOT NULL DEFAULT 1,
  currency      CHAR(3) NOT NULL,
  value         NUMERIC(15,4) NOT NULL,
  source_price_list_id BIGINT REFERENCES price_lists(id),  -- trace
  computed_at   timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (customer_id, product_id, unit, min_quantity, currency)
);

-- Pricing read permission for the demo admin role (manage already seeded).
INSERT INTO role_permissions (role_id, permission)
SELECT r.id, 'price_list.view'
  FROM roles r
 WHERE r.organization_id = 1 AND r.name = 'admin'
ON CONFLICT DO NOTHING;
