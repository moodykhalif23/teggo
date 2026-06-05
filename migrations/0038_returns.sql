-- Returns / RMA + credit notes (pain point #4): the post-sale path the platform
-- lacked. A return moves requested -> approved -> received (restock + credit
-- note) or rejected. Credit notes record the value credited back to the buyer.
CREATE TABLE returns (
  id            BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  public_id     UUID NOT NULL DEFAULT gen_random_uuid() UNIQUE,
  order_id      BIGINT NOT NULL REFERENCES orders(id),
  customer_id   BIGINT NOT NULL REFERENCES customers(id),
  status        text NOT NULL DEFAULT 'requested'
                  CHECK (status IN ('requested','approved','rejected','received','closed')),
  reason        text,
  created_at    timestamptz NOT NULL DEFAULT now(),
  updated_at    timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX idx_returns_order ON returns(order_id);
CREATE INDEX idx_returns_customer ON returns(customer_id);
CREATE TRIGGER trg_returns_updated BEFORE UPDATE ON returns
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE return_items (
  id            BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  return_id     BIGINT NOT NULL REFERENCES returns(id) ON DELETE CASCADE,
  order_item_id BIGINT NOT NULL REFERENCES order_items(id),
  product_id    BIGINT NOT NULL REFERENCES products(id),
  quantity      NUMERIC(15,4) NOT NULL,
  reason        text
);
CREATE INDEX idx_return_items_return ON return_items(return_id);

CREATE TABLE credit_notes (
  id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  public_id   UUID NOT NULL DEFAULT gen_random_uuid() UNIQUE,
  return_id   BIGINT REFERENCES returns(id),
  invoice_id  BIGINT REFERENCES invoices(id),
  customer_id BIGINT NOT NULL REFERENCES customers(id),
  amount      NUMERIC(15,4) NOT NULL,
  currency    CHAR(3) NOT NULL,
  status      text NOT NULL DEFAULT 'issued' CHECK (status IN ('draft','issued','void')),
  created_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX idx_credit_notes_customer ON credit_notes(customer_id);

-- Grant the return permissions to the demo admin role.
INSERT INTO role_permissions (role_id, permission)
SELECT r.id, p.perm
  FROM roles r CROSS JOIN (VALUES ('return.view'), ('return.manage')) AS p(perm)
 WHERE r.organization_id = 1 AND r.name = 'admin'
ON CONFLICT DO NOTHING;
