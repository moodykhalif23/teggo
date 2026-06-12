-- 0051_tenant_provisioning.sql — self-serve tenant signup (SAAS.md #1).
-- Organizations gain a lifecycle status; signups create a 'pending' org that a
-- verification email promotes to 'trial'. Platform operators (org 1, the
-- platform owner) can list and suspend tenant orgs.

ALTER TABLE organizations
  ADD COLUMN status text NOT NULL DEFAULT 'active'
    CHECK (status IN ('pending','trial','active','suspended'));

-- Email-verification tokens minted at signup. Single-use (consumed_at), expiring.
CREATE TABLE signup_verifications (
  id              BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  token           UUID NOT NULL DEFAULT gen_random_uuid() UNIQUE,
  organization_id BIGINT NOT NULL REFERENCES organizations(id),
  user_id         BIGINT NOT NULL REFERENCES users(id),
  expires_at      timestamptz NOT NULL,
  consumed_at     timestamptz,
  created_at      timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX signup_verifications_org_idx ON signup_verifications (organization_id);

-- Platform-operator permissions. Granted ONLY to the platform owner org's admin
-- role — tenant provisioning deliberately excludes platform.* from the template
-- it copies, so tenants can never see or manage other tenants.
INSERT INTO role_permissions (role_id, permission)
SELECT r.id, p.perm
FROM roles r,
     (VALUES ('platform.view'), ('platform.manage')) AS p(perm)
WHERE r.organization_id = 1 AND r.name = 'admin'
ON CONFLICT DO NOTHING;
