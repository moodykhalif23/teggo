-- Approval routing tiers (Pack 2 §3.6 extension). Beyond a buyer's individual
-- spending_limit, an organization can define amount tiers that require a given
-- approver role to release a held order — e.g. orders ≥ 10k need an 'admin',
-- smaller ones an 'approver'. With no tiers configured the buyer-approval flow
-- is unchanged (any approver/admin may approve), so this is purely additive.
CREATE TABLE approval_routing_rules (
  id              BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  organization_id BIGINT NOT NULL REFERENCES organizations(id),
  min_amount      NUMERIC(15,4) NOT NULL DEFAULT 0,
  max_amount      NUMERIC(15,4),                       -- null = no upper bound
  required_role   text NOT NULL CHECK (required_role IN ('approver','admin')),
  sort_order      int NOT NULL DEFAULT 0,
  created_at      timestamptz NOT NULL DEFAULT now(),
  CHECK (max_amount IS NULL OR max_amount >= min_amount)
);
CREATE INDEX idx_approval_routing_org ON approval_routing_rules(organization_id, sort_order, min_amount);
