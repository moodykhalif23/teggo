-- 0058_insights_rls.sql — extend the tenant-isolation net (SAAS.md #3) to the
-- insight_digests table added in 0057. Mirrors the FORCEd, fail-open
-- org_isolation policy that 0054 applied to every other org-scoped table: it
-- bites only when app.org_id is armed (per-request in the API), and is a no-op
-- for the worker/migrate/test sessions that keep their explicit org filters.

ALTER TABLE insight_digests ENABLE ROW LEVEL SECURITY;
ALTER TABLE insight_digests FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS org_isolation ON insight_digests;
CREATE POLICY org_isolation ON insight_digests FOR ALL
  USING (COALESCE(current_setting('app.org_id', true), '') = ''
     OR organization_id = current_setting('app.org_id', true)::bigint)
  WITH CHECK (COALESCE(current_setting('app.org_id', true), '') = ''
     OR organization_id = current_setting('app.org_id', true)::bigint);
