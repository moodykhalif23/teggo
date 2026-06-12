-- 0053_billing.sql — platform billing & metering (SAAS.md #2).
-- Plans carry feature flags (premium modules) + usage limits; each org holds one
-- plan subscription; usage_counters accumulate per metric per period. Payment
-- collection (Stripe/M-Pesa) is deferred — operators assign plans manually and
-- dunning is the org-status suspension from SAAS.md #1.

CREATE TABLE plans (
  id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  code        text NOT NULL UNIQUE,
  name        text NOT NULL,
  price       NUMERIC(15,4) NOT NULL DEFAULT 0,
  currency    CHAR(3) NOT NULL DEFAULT 'USD',
  period      text NOT NULL DEFAULT 'monthly' CHECK (period IN ('monthly','annual')),
  -- features: JSON array of premium feature keys this plan enables
  -- (subscriptions, rebates, fx, merchandising, assistant).
  features    JSONB NOT NULL DEFAULT '[]',
  -- limits: {metric: cap} — orders/ai_calls per month, storage_bytes lifetime.
  -- A missing metric = unlimited.
  limits      JSONB NOT NULL DEFAULT '{}',
  is_active   boolean NOT NULL DEFAULT true,
  position    INT NOT NULL DEFAULT 0,
  created_at  timestamptz NOT NULL DEFAULT now(),
  updated_at  timestamptz NOT NULL DEFAULT now()
);
CREATE TRIGGER trg_plans_updated BEFORE UPDATE ON plans
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE org_subscriptions (
  organization_id       BIGINT PRIMARY KEY REFERENCES organizations(id),
  plan_id               BIGINT NOT NULL REFERENCES plans(id),
  status                text NOT NULL DEFAULT 'active'
                          CHECK (status IN ('active','past_due','canceled')),
  current_period_start  timestamptz NOT NULL DEFAULT now(),
  updated_at            timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE usage_counters (
  organization_id BIGINT NOT NULL REFERENCES organizations(id),
  metric          text NOT NULL,     -- 'orders' | 'ai_calls' | 'storage_bytes'
  period_key      text NOT NULL,     -- '2026-06' monthly, 'all' for lifetime gauges
  value           BIGINT NOT NULL DEFAULT 0,
  updated_at      timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (organization_id, metric, period_key)
);

-- The three launch tiers. Core commerce (catalog, orders, quotes, promotions,
-- CRM, …) is in every plan; features gate the premium modules.
INSERT INTO plans (code, name, price, currency, features, limits, position) VALUES
  ('free',   'Free',   0,     'USD', '[]',
   '{"orders": 50, "ai_calls": 50, "storage_bytes": 1073741824}', 1),
  ('growth', 'Growth', 99,    'USD', '["assistant","merchandising","fx"]',
   '{"orders": 1000, "ai_calls": 2000, "storage_bytes": 21474836480}', 2),
  ('scale',  'Scale',  299,   'USD', '["assistant","merchandising","fx","subscriptions","rebates"]',
   '{}', 3);

-- Existing orgs (the platform org and any tenant provisioned before billing)
-- land on Scale so nothing they already use goes dark.
INSERT INTO org_subscriptions (organization_id, plan_id)
SELECT o.id, p.id FROM organizations o, plans p
WHERE p.code = 'scale'
ON CONFLICT DO NOTHING;
