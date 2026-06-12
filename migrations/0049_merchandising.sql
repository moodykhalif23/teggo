-- Search merchandising (Roadmap Tier 2 #6): curation on top of Postgres FTS —
-- synonyms (query expansion), redirects (a query jumps to a page), and
-- pin/boost/bury rules per query or category.
CREATE TABLE search_synonyms (
  id              BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  organization_id BIGINT NOT NULL REFERENCES organizations(id),
  term            text NOT NULL,   -- the query term (matched case-insensitively)
  synonyms        text NOT NULL,   -- expansion terms (space/comma separated)
  created_at      timestamptz NOT NULL DEFAULT now(),
  UNIQUE (organization_id, term)
);
CREATE INDEX idx_search_synonyms_org ON search_synonyms(organization_id);

CREATE TABLE search_redirects (
  id              BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  organization_id BIGINT NOT NULL REFERENCES organizations(id),
  query           text NOT NULL,   -- normalized query that triggers the redirect
  target          text NOT NULL,   -- storefront path, e.g. /c/clearance
  created_at      timestamptz NOT NULL DEFAULT now(),
  UNIQUE (organization_id, query)
);
CREATE INDEX idx_search_redirects_org ON search_redirects(organization_id);

CREATE TABLE merchandising_rules (
  id              BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  organization_id BIGINT NOT NULL REFERENCES organizations(id),
  scope_type      text NOT NULL CHECK (scope_type IN ('query','category')),
  scope_value     text NOT NULL,   -- the query string or category slug (normalized)
  product_id      BIGINT NOT NULL REFERENCES products(id) ON DELETE CASCADE,
  action          text NOT NULL CHECK (action IN ('pin','boost','bury')),
  position        int NOT NULL DEFAULT 0,  -- ordering among pins
  created_at      timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX idx_merch_rules_lookup ON merchandising_rules(organization_id, scope_type, scope_value);

-- Merchandising permissions for the demo admin role.
INSERT INTO role_permissions (role_id, permission)
SELECT r.id, p.permission
  FROM roles r
  CROSS JOIN (VALUES ('merchandising.view'), ('merchandising.manage')) AS p(permission)
 WHERE r.organization_id = 1 AND r.name = 'admin'
ON CONFLICT DO NOTHING;
