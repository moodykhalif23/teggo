-- Customers & accounts (B2B core) — Implementation Pack 1 §2.
-- Hierarchy via customers.parent_id (cycle-safety enforced in app via the
-- ancestor CTE, §12.2). customers.default_price_list_id is added FK-less here;
-- the FK to price_lists is backfilled in the pricing migration (§5).

CREATE TABLE customer_groups (
  id              BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  organization_id BIGINT NOT NULL REFERENCES organizations(id),
  name            text NOT NULL,
  UNIQUE (organization_id, name)
);

CREATE TABLE customers (
  id                    BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  public_id             UUID NOT NULL DEFAULT gen_random_uuid() UNIQUE,
  organization_id       BIGINT NOT NULL REFERENCES organizations(id),
  parent_id             BIGINT REFERENCES customers(id),
  customer_group_id     BIGINT REFERENCES customer_groups(id),
  name                  text NOT NULL,
  tax_id                text,
  payment_terms_days    int NOT NULL DEFAULT 0,            -- 0 = prepay
  credit_limit          NUMERIC(15,4) NOT NULL DEFAULT 0,
  default_price_list_id BIGINT,                            -- FK added in pricing migration
  assigned_sales_rep_id BIGINT REFERENCES users(id),
  is_active             boolean NOT NULL DEFAULT true,
  created_at            timestamptz NOT NULL DEFAULT now(),
  updated_at            timestamptz NOT NULL DEFAULT now(),
  deleted_at            timestamptz
);
CREATE INDEX idx_customers_org ON customers(organization_id);
CREATE INDEX idx_customers_parent ON customers(parent_id);
CREATE TRIGGER trg_customers_updated BEFORE UPDATE ON customers
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE customer_users (
  id              BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  customer_id     BIGINT NOT NULL REFERENCES customers(id),
  email           citext NOT NULL,
  password_hash   text NOT NULL,
  full_name       text NOT NULL,
  role            text NOT NULL DEFAULT 'buyer'
                    CHECK (role IN ('buyer','approver','admin')),
  spending_limit  NUMERIC(15,4),                -- null = unlimited (subject to credit_limit)
  is_active       boolean NOT NULL DEFAULT true,
  created_at      timestamptz NOT NULL DEFAULT now(),
  updated_at      timestamptz NOT NULL DEFAULT now(),
  UNIQUE (customer_id, email)
);
CREATE INDEX idx_customer_users_customer ON customer_users(customer_id);
CREATE TRIGGER trg_customer_users_updated BEFORE UPDATE ON customer_users
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE customer_addresses (
  id           BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  customer_id  BIGINT NOT NULL REFERENCES customers(id),
  type         text NOT NULL CHECK (type IN ('billing','shipping')),
  is_default   boolean NOT NULL DEFAULT false,
  line1        text NOT NULL,
  line2        text,
  city         text NOT NULL,
  region       text,
  postal_code  text,
  country      CHAR(2) NOT NULL,
  created_at   timestamptz NOT NULL DEFAULT now(),
  updated_at   timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX idx_customer_addresses_cust ON customer_addresses(customer_id);
CREATE TRIGGER trg_customer_addresses_updated BEFORE UPDATE ON customer_addresses
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- Grant the new customer permissions to the demo admin role so the seeded admin
-- can exercise these endpoints. Safe on existing installs (this is a new migration).
INSERT INTO role_permissions (role_id, permission)
SELECT r.id, p.permission
  FROM roles r
  CROSS JOIN (VALUES ('customer.view'), ('customer.manage')) AS p(permission)
 WHERE r.organization_id = 1 AND r.name = 'admin'
ON CONFLICT DO NOTHING;
