-- CRM — Pack 2 §1. Seller-side relationship data hangs off the commerce
-- `customers` (a CRM account IS a customer — no duplication). Leads qualify into
-- opportunities that move through a pipeline of stages; activities form a
-- unified timeline. Admin-only (sales reps/managers).

CREATE TABLE contacts (
  id               BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  organization_id  BIGINT NOT NULL REFERENCES organizations(id),
  customer_id      BIGINT REFERENCES customers(id),        -- null until linked
  customer_user_id BIGINT REFERENCES customer_users(id),   -- optional login link
  full_name        text NOT NULL,
  email            citext,
  phone            text,
  job_title        text,
  created_at       timestamptz NOT NULL DEFAULT now(),
  updated_at       timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX idx_contacts_customer ON contacts(customer_id);
CREATE TRIGGER trg_contacts_updated BEFORE UPDATE ON contacts
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE leads (
  id                    BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  public_id             UUID NOT NULL DEFAULT gen_random_uuid() UNIQUE,
  organization_id       BIGINT NOT NULL REFERENCES organizations(id),
  source                text NOT NULL DEFAULT 'manual'
                          CHECK (source IN ('manual','storefront_form','rfq','import','referral')),
  company_name          text,
  contact_name          text,
  email                 citext,
  phone                 text,
  notes                 text,
  status                text NOT NULL DEFAULT 'new'
                          CHECK (status IN ('new','working','qualified','disqualified','converted')),
  owner_user_id         BIGINT REFERENCES users(id),
  converted_customer_id BIGINT REFERENCES customers(id),   -- set on conversion
  created_at            timestamptz NOT NULL DEFAULT now(),
  updated_at            timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX idx_leads_owner ON leads(owner_user_id);
CREATE INDEX idx_leads_org ON leads(organization_id);
CREATE TRIGGER trg_leads_updated BEFORE UPDATE ON leads
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE pipelines (
  id              BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  organization_id BIGINT NOT NULL REFERENCES organizations(id),
  name            text NOT NULL,
  is_default      boolean NOT NULL DEFAULT false
);

CREATE TABLE pipeline_stages (
  id           BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  pipeline_id  BIGINT NOT NULL REFERENCES pipelines(id) ON DELETE CASCADE,
  code         text NOT NULL,
  label        text NOT NULL,
  probability  NUMERIC(5,2) NOT NULL DEFAULT 0,   -- 0..100 for weighted forecast
  is_won       boolean NOT NULL DEFAULT false,
  is_lost      boolean NOT NULL DEFAULT false,
  sort_order   int NOT NULL DEFAULT 0,
  UNIQUE (pipeline_id, code)
);

CREATE TABLE opportunities (
  id              BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  public_id       UUID NOT NULL DEFAULT gen_random_uuid() UNIQUE,
  organization_id BIGINT NOT NULL REFERENCES organizations(id),
  customer_id     BIGINT NOT NULL REFERENCES customers(id),
  contact_id      BIGINT REFERENCES contacts(id),
  pipeline_id     BIGINT NOT NULL REFERENCES pipelines(id),
  stage_id        BIGINT NOT NULL REFERENCES pipeline_stages(id),
  name            text NOT NULL,
  amount          NUMERIC(15,4) NOT NULL DEFAULT 0,
  currency        CHAR(3) NOT NULL,
  expected_close  date,
  owner_user_id   BIGINT REFERENCES users(id),
  closed_at       timestamptz,
  created_at      timestamptz NOT NULL DEFAULT now(),
  updated_at      timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX idx_opps_customer ON opportunities(customer_id);
CREATE INDEX idx_opps_stage ON opportunities(stage_id);
CREATE TRIGGER trg_opps_updated BEFORE UPDATE ON opportunities
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE opportunity_stage_history (
  id             BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  opportunity_id BIGINT NOT NULL REFERENCES opportunities(id) ON DELETE CASCADE,
  from_stage_id  BIGINT REFERENCES pipeline_stages(id),
  to_stage_id    BIGINT NOT NULL REFERENCES pipeline_stages(id),
  changed_by     BIGINT REFERENCES users(id),
  created_at     timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE activities (
  id              BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  organization_id BIGINT NOT NULL REFERENCES organizations(id),
  type            text NOT NULL CHECK (type IN ('call','email','meeting','task','note')),
  subject         text NOT NULL,
  body            text,
  -- polymorphic association by explicit nullable FKs (one or more may be set)
  customer_id     BIGINT REFERENCES customers(id),
  contact_id      BIGINT REFERENCES contacts(id),
  opportunity_id  BIGINT REFERENCES opportunities(id),
  lead_id         BIGINT REFERENCES leads(id),
  owner_user_id   BIGINT REFERENCES users(id),
  status          text NOT NULL DEFAULT 'open'
                    CHECK (status IN ('open','done','cancelled')),
  due_at          timestamptz,
  occurred_at     timestamptz NOT NULL DEFAULT now(),
  created_at      timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX idx_activities_customer ON activities(customer_id, occurred_at DESC);
CREATE INDEX idx_activities_opp ON activities(opportunity_id);

-- A default pipeline + stages for the demo organization so the board works
-- out of the box. Probabilities drive the weighted forecast.
INSERT INTO pipelines (organization_id, name, is_default) VALUES (1, 'Default', true);
INSERT INTO pipeline_stages (pipeline_id, code, label, probability, is_won, is_lost, sort_order)
SELECT p.id, s.code, s.label, s.probability, s.is_won, s.is_lost, s.sort_order
  FROM pipelines p
  CROSS JOIN (VALUES
    ('new',         'New',         10.0, false, false, 1),
    ('qualified',   'Qualified',   25.0, false, false, 2),
    ('proposal',    'Proposal',    50.0, false, false, 3),
    ('negotiation', 'Negotiation', 75.0, false, false, 4),
    ('won',         'Won',        100.0, true,  false, 5),
    ('lost',        'Lost',         0.0, false, true,  6)
  ) AS s(code, label, probability, is_won, is_lost, sort_order)
 WHERE p.organization_id = 1 AND p.is_default;

-- CRM permissions for the demo admin role.
INSERT INTO role_permissions (role_id, permission)
SELECT r.id, p.permission
  FROM roles r
  CROSS JOIN (VALUES ('crm.view'), ('crm.manage')) AS p(permission)
 WHERE r.organization_id = 1 AND r.name = 'admin'
ON CONFLICT DO NOTHING;
