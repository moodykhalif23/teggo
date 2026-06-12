-- Rebates / volume incentives (Roadmap Tier 3 #7). A program defines tiered
-- (retroactive) rebate rates over a period; qualifying order spend is summed per
-- customer per period (derived from orders — no per-order write), and a period-end
-- settlement snapshots the amount and issues a credit note.
CREATE TABLE rebate_programs (
  id               BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  public_id        UUID NOT NULL DEFAULT gen_random_uuid() UNIQUE,
  organization_id  BIGINT NOT NULL REFERENCES organizations(id),
  name             text NOT NULL,
  description      text,
  customer_id      BIGINT REFERENCES customers(id),  -- NULL = all customers
  period           text NOT NULL CHECK (period IN ('monthly','quarterly','annual')),
  basis            text NOT NULL DEFAULT 'order_subtotal' CHECK (basis IN ('order_subtotal')),
  currency         CHAR(3) NOT NULL,
  is_active        boolean NOT NULL DEFAULT true,
  created_at       timestamptz NOT NULL DEFAULT now(),
  updated_at       timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX idx_rebate_programs_org ON rebate_programs(organization_id, is_active);

CREATE TABLE rebate_tiers (
  id           BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  program_id   BIGINT NOT NULL REFERENCES rebate_programs(id) ON DELETE CASCADE,
  min_amount   NUMERIC(15,4) NOT NULL,        -- qualify at/above this period total
  rate_percent NUMERIC(7,4) NOT NULL CHECK (rate_percent >= 0),
  sort_order   int NOT NULL DEFAULT 0
);
CREATE INDEX idx_rebate_tiers_program ON rebate_tiers(program_id, min_amount);

CREATE TABLE rebate_settlements (
  id               BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  program_id       BIGINT NOT NULL REFERENCES rebate_programs(id) ON DELETE CASCADE,
  customer_id      BIGINT NOT NULL REFERENCES customers(id),
  period_key       text NOT NULL,             -- e.g. 2026-Q2, 2026-06, 2026
  qualifying_total NUMERIC(15,4) NOT NULL,
  rate_percent     NUMERIC(7,4) NOT NULL,
  rebate_amount    NUMERIC(15,4) NOT NULL,
  currency         CHAR(3) NOT NULL,
  status           text NOT NULL DEFAULT 'issued' CHECK (status IN ('pending','issued','void')),
  credit_note_id   BIGINT REFERENCES credit_notes(id),
  created_at       timestamptz NOT NULL DEFAULT now(),
  UNIQUE (program_id, customer_id, period_key)  -- a period settles once
);
CREATE INDEX idx_rebate_settlements_customer ON rebate_settlements(customer_id, created_at DESC);
CREATE INDEX idx_rebate_settlements_program ON rebate_settlements(program_id, created_at DESC);

-- Rebate permissions for the demo admin role.
INSERT INTO role_permissions (role_id, permission)
SELECT r.id, p.permission
  FROM roles r
  CROSS JOIN (VALUES ('rebate.view'), ('rebate.manage')) AS p(permission)
 WHERE r.organization_id = 1 AND r.name = 'admin'
ON CONFLICT DO NOTHING;
