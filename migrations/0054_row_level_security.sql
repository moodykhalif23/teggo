-- 0054_row_level_security.sql — tenant isolation net (SAAS.md #3).
-- Every table carrying an organization_id gets a FORCEd row-level-security
-- policy keyed on the app.org_id session setting. The policy FAILS OPEN when
-- the setting is absent/empty, so workers, migrations and tests behave exactly
-- as before — queries keep their explicit org filters as the mechanism, and
-- RLS becomes the net wherever the API pool arms app.org_id (per request,
-- from the JWT claims).
--
-- The net only bites for a NON-superuser, NON-owner session: connection users
-- are typically superusers (docker POSTGRES_USER), which Postgres exempts from
-- RLS no matter what. So arming also does SET ROLE teggo_app — a dedicated
-- NOLOGIN, NOBYPASSRLS role created here with plain DML rights. FORCE keeps
-- the net up even if a deployment runs the app as the table owner.
--
-- Tables scoped indirectly (via customer_id etc.) are not covered here —
-- they are only reachable through org-scoped parents. Join-based policies for
-- them are a future hardening pass.

DO $$
BEGIN
  IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'teggo_app') THEN
    CREATE ROLE teggo_app NOLOGIN NOBYPASSRLS;
  END IF;
  -- The connecting user must be able to SET ROLE teggo_app.
  EXECUTE format('GRANT teggo_app TO %I', current_user);
END $$;

GRANT USAGE ON SCHEMA public TO teggo_app;
GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO teggo_app;
GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO teggo_app;
-- Tables created by later migrations (and river's own, when first installed
-- after this point) inherit the same DML rights.
ALTER DEFAULT PRIVILEGES IN SCHEMA public
  GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO teggo_app;
ALTER DEFAULT PRIVILEGES IN SCHEMA public
  GRANT USAGE, SELECT ON SEQUENCES TO teggo_app;

DO $$
DECLARE t record;
BEGIN
  FOR t IN
    SELECT c.table_name
    FROM information_schema.columns c
    JOIN information_schema.tables tb
      ON tb.table_name = c.table_name AND tb.table_schema = c.table_schema
    WHERE c.table_schema = 'public'
      AND c.column_name = 'organization_id'
      AND tb.table_type = 'BASE TABLE'
  LOOP
    EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', t.table_name);
    EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY', t.table_name);
    EXECUTE format(
      'CREATE POLICY org_isolation ON %I FOR ALL '
      || 'USING (COALESCE(current_setting(''app.org_id'', true), '''') = '''' '
      || '   OR organization_id = current_setting(''app.org_id'', true)::bigint) '
      || 'WITH CHECK (COALESCE(current_setting(''app.org_id'', true), '''') = '''' '
      || '   OR organization_id = current_setting(''app.org_id'', true)::bigint)',
      t.table_name);
  END LOOP;
END $$;
