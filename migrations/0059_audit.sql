-- 0059_audit.sql — comprehensive admin/vendor audit trail (governance, SOC2,
-- funding due-diligence). Every state-changing request on a staff surface is
-- recorded automatically by the audit middleware: who (actor + audience), what
-- (action + entity), when, from where (IP/UA/request id), and the result
-- (status code). Handlers may enrich an entry with a before/after snapshot
-- (metadata.change) for sensitive entities. Append-only by convention — there
-- is no UPDATE/DELETE query for this table.

CREATE TABLE audit_log (
  id              BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  organization_id BIGINT NOT NULL REFERENCES organizations(id),
  actor_user_id   BIGINT,                          -- null only for unattributed system writes
  actor_audience  text NOT NULL DEFAULT '',        -- 'admin' | 'vendor'
  action          text NOT NULL,                   -- 'customers.update', 'orders.confirm', …
  entity_type     text NOT NULL DEFAULT '',        -- resource the action targeted
  entity_id       BIGINT,                          -- null for collection/non-addressable actions
  method          text NOT NULL,
  path            text NOT NULL,
  status_code     int  NOT NULL DEFAULT 0,
  ip              text NOT NULL DEFAULT '',
  user_agent      text NOT NULL DEFAULT '',
  request_id      text NOT NULL DEFAULT '',
  summary         text NOT NULL DEFAULT '',
  metadata        JSONB NOT NULL DEFAULT '{}'::jsonb,  -- optional {change:{before,after}}, etc.
  created_at      timestamptz NOT NULL DEFAULT now()
);
-- Recent-first listing per org (the default viewer query).
CREATE INDEX idx_audit_log_org_created ON audit_log (organization_id, created_at DESC);
-- "Everything that touched entity X" and "everything actor Y did".
CREATE INDEX idx_audit_log_org_entity ON audit_log (organization_id, entity_type, entity_id);
CREATE INDEX idx_audit_log_org_actor  ON audit_log (organization_id, actor_user_id);

-- Tenant-isolation net (mirrors 0054/0058): FORCEd, fail-open org_isolation.
ALTER TABLE audit_log ENABLE ROW LEVEL SECURITY;
ALTER TABLE audit_log FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS org_isolation ON audit_log;
CREATE POLICY org_isolation ON audit_log FOR ALL
  USING (COALESCE(current_setting('app.org_id', true), '') = ''
     OR organization_id = current_setting('app.org_id', true)::bigint)
  WITH CHECK (COALESCE(current_setting('app.org_id', true), '') = ''
     OR organization_id = current_setting('app.org_id', true)::bigint);

-- Viewing the audit trail is its own sensitive permission (granted to the demo
-- admin role; assign to compliance/admin roles per tenant).
INSERT INTO role_permissions (role_id, permission)
SELECT r.id, 'audit.view'
  FROM roles r
 WHERE r.organization_id = 1 AND r.name = 'admin'
ON CONFLICT DO NOTHING;
