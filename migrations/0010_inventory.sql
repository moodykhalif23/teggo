-- Inventory — Implementation Pack 1 §8.
-- inventory_movements is the append-only source of truth; inventory_levels is a
-- cache of on-hand/reserved kept in step within the same transaction.
-- available = quantity_on_hand - quantity_reserved (never negative unless the
-- product/warehouse allows backorder).

CREATE TABLE warehouses (
  id              BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  organization_id BIGINT NOT NULL REFERENCES organizations(id),
  name            text NOT NULL,
  is_active       boolean NOT NULL DEFAULT true
);
CREATE INDEX idx_warehouses_org ON warehouses(organization_id);

CREATE TABLE inventory_levels (
  id                 BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  product_id         BIGINT NOT NULL REFERENCES products(id) ON DELETE CASCADE,
  warehouse_id       BIGINT NOT NULL REFERENCES warehouses(id),
  quantity_on_hand   NUMERIC(15,4) NOT NULL DEFAULT 0,
  quantity_reserved  NUMERIC(15,4) NOT NULL DEFAULT 0,
  reorder_threshold  NUMERIC(15,4),
  allow_backorder    boolean NOT NULL DEFAULT false,
  updated_at         timestamptz NOT NULL DEFAULT now(),
  UNIQUE (product_id, warehouse_id)
);
CREATE INDEX idx_inventory_levels_product ON inventory_levels(product_id);
CREATE TRIGGER trg_inventory_levels_updated BEFORE UPDATE ON inventory_levels
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE inventory_movements (
  id             BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  product_id     BIGINT NOT NULL REFERENCES products(id),
  warehouse_id   BIGINT NOT NULL REFERENCES warehouses(id),
  type           text NOT NULL CHECK (type IN
                   ('receipt','reservation','release','fulfillment','adjustment','return')),
  quantity       NUMERIC(15,4) NOT NULL,    -- signed
  reference_type text,                       -- 'order','shipment','manual'
  reference_id   BIGINT,
  created_by     text,
  created_at     timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX idx_inv_movements_product ON inventory_movements(product_id, warehouse_id);

-- Inventory permissions for the demo admin role.
INSERT INTO role_permissions (role_id, permission)
SELECT r.id, p.permission
  FROM roles r
  CROSS JOIN (VALUES ('inventory.view'), ('inventory.manage')) AS p(permission)
 WHERE r.organization_id = 1 AND r.name = 'admin'
ON CONFLICT DO NOTHING;
