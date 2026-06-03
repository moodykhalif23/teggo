-- B2B integration: Punchout (cXML/OCI) + EDI (X12) — Pack 3 §3. Trading partners
-- map a procurement system to a commerce customer; punchout sessions bind a
-- buyer's storefront cart to a return URL; EDI documents are stored raw before
-- parsing so re-processing is always possible.

CREATE TABLE trading_partners (
  id              BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  organization_id BIGINT NOT NULL REFERENCES organizations(id),
  customer_id     BIGINT REFERENCES customers(id),
  name            text NOT NULL,
  protocol        text NOT NULL CHECK (protocol IN ('cxml','oci','edi_x12','edifact')),
  transport       text CHECK (transport IN ('https','as2','sftp','van')),
  identity        text,                                 -- cXML Sender identity / EDI ISA id
  shared_secret   text,                                 -- punchout shared secret (self-hosted)
  config          JSONB NOT NULL DEFAULT '{}'::jsonb,    -- endpoints, identifiers (non-secret)
  is_active       boolean NOT NULL DEFAULT true,
  created_at      timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX idx_trading_partners_org ON trading_partners(organization_id);
-- Identity is how an inbound punchout/EDI document finds its partner.
CREATE UNIQUE INDEX uq_trading_partners_identity
  ON trading_partners(organization_id, identity) WHERE identity IS NOT NULL;

CREATE TABLE punchout_sessions (
  id                 BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  public_id          UUID NOT NULL DEFAULT gen_random_uuid() UNIQUE,
  trading_partner_id BIGINT NOT NULL REFERENCES trading_partners(id),
  customer_id        BIGINT NOT NULL REFERENCES customers(id),
  buyer_cookie       text NOT NULL,
  operation          text NOT NULL DEFAULT 'create'
                       CHECK (operation IN ('create','edit','inspect')),
  return_url         text NOT NULL,
  cart_id            BIGINT REFERENCES carts(id),
  status             text NOT NULL DEFAULT 'active'
                       CHECK (status IN ('active','returned','expired')),
  created_at         timestamptz NOT NULL DEFAULT now(),
  expires_at         timestamptz NOT NULL
);
CREATE INDEX idx_punchout_sessions_partner ON punchout_sessions(trading_partner_id);

CREATE TABLE edi_documents (
  id                  BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  organization_id     BIGINT NOT NULL REFERENCES organizations(id),
  trading_partner_id  BIGINT NOT NULL REFERENCES trading_partners(id),
  direction           text NOT NULL CHECK (direction IN ('inbound','outbound')),
  doc_type            text NOT NULL,
  status              text NOT NULL DEFAULT 'received'
                        CHECK (status IN ('received','parsed','mapped','processed','sent','acknowledged','error')),
  control_number      text,
  raw_payload         text NOT NULL,
  parsed              JSONB,
  related_entity_type text,
  related_entity_id   BIGINT,
  error               text,
  created_at          timestamptz NOT NULL DEFAULT now(),
  processed_at        timestamptz,
  UNIQUE (trading_partner_id, direction, doc_type, control_number)
);
CREATE INDEX idx_edi_documents_partner ON edi_documents(trading_partner_id, created_at DESC);

-- Integration admin permissions for the demo admin role.
INSERT INTO role_permissions (role_id, permission)
SELECT r.id, p.permission
  FROM roles r
  CROSS JOIN (VALUES ('integration.view'), ('integration.manage')) AS p(permission)
 WHERE r.organization_id = 1 AND r.name = 'admin'
ON CONFLICT DO NOTHING;
