-- Multi-currency display & FX (Roadmap Tier 2 #5). Org-scoped exchange rates as a
-- time series (latest by as_of wins). Used to present prices/totals in a buyer's
-- chosen currency, and to lock the rate onto an order at placement for audit.
CREATE TABLE fx_rates (
  id               BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  organization_id  BIGINT NOT NULL REFERENCES organizations(id),
  base_currency    CHAR(3) NOT NULL,
  quote_currency   CHAR(3) NOT NULL,
  rate             NUMERIC(18,8) NOT NULL CHECK (rate > 0),
  as_of            timestamptz NOT NULL DEFAULT now(),
  created_at       timestamptz NOT NULL DEFAULT now(),
  CHECK (base_currency <> quote_currency)
);
CREATE INDEX idx_fx_rates_lookup ON fx_rates(organization_id, base_currency, quote_currency, as_of DESC);

-- FX snapshot locked onto an order when the buyer transacted in a display currency.
-- The order's `currency` stays the transactional (base) currency; these record what
-- the buyer was quoted and the rate used.
ALTER TABLE orders
  ADD COLUMN IF NOT EXISTS display_currency    CHAR(3),
  ADD COLUMN IF NOT EXISTS fx_rate             NUMERIC(18,8),
  ADD COLUMN IF NOT EXISTS display_grand_total NUMERIC(15,4);

-- FX permissions for the demo admin role.
INSERT INTO role_permissions (role_id, permission)
SELECT r.id, p.permission
  FROM roles r
  CROSS JOIN (VALUES ('fx.view'), ('fx.manage')) AS p(permission)
 WHERE r.organization_id = 1 AND r.name = 'admin'
ON CONFLICT DO NOTHING;
