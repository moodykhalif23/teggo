-- Tenant provisioning + platform-operator queries (SAAS.md #1).

-- name: CreateOrganization :one
INSERT INTO organizations (name, status) VALUES ($1, $2) RETURNING *;

-- name: SetOrganizationStatus :one
UPDATE organizations SET status = $2 WHERE id = $1 RETURNING *;

-- ListOrganizationsWithCounts is the platform-operator overview.
-- name: ListOrganizationsWithCounts :many
SELECT o.*,
  (SELECT count(*) FROM users u    WHERE u.organization_id = o.id) AS user_count,
  (SELECT count(*) FROM websites w WHERE w.organization_id = o.id) AS website_count,
  COALESCE(p.code, '') AS plan_code
FROM organizations o
LEFT JOIN org_subscriptions s ON s.organization_id = o.id
LEFT JOIN plans p ON p.id = s.plan_id
ORDER BY o.id;

-- name: CreateRole :one
INSERT INTO roles (organization_id, name, description) VALUES ($1, $2, $3) RETURNING *;

-- SeedRolePermissionsFromTemplate copies the canonical permission matrix — org
-- 1's admin role, which every permission migration appends to — onto a freshly
-- provisioned role. platform.* is excluded (operator-only); pattern narrows the
-- copy for lesser roles ('' = everything, '%.view' = read-only, …).
-- name: SeedRolePermissionsFromTemplate :execrows
INSERT INTO role_permissions (role_id, permission)
SELECT sqlc.arg(role_id), rp.permission
FROM role_permissions rp
JOIN roles r ON r.id = rp.role_id
WHERE r.organization_id = 1 AND r.name = 'admin'
  AND rp.permission NOT LIKE 'platform.%'
  AND (sqlc.arg(pattern)::text = '' OR rp.permission LIKE sqlc.arg(pattern))
ON CONFLICT DO NOTHING;

-- name: AssignUserRole :exec
INSERT INTO user_roles (user_id, role_id) VALUES ($1, $2) ON CONFLICT DO NOTHING;

-- name: CreateSignupVerification :one
INSERT INTO signup_verifications (organization_id, user_id, expires_at)
VALUES ($1, $2, $3)
RETURNING *;

-- name: GetSignupVerification :one
SELECT * FROM signup_verifications
WHERE token = $1 AND consumed_at IS NULL AND expires_at > now();

-- name: ConsumeSignupVerification :execrows
UPDATE signup_verifications SET consumed_at = now()
WHERE id = $1 AND consumed_at IS NULL;

-- FindUserOrgsByEmail powers org-aware admin login: an email that exists in
-- exactly one org resolves it implicitly.
-- name: FindUserOrgsByEmail :many
SELECT organization_id FROM users
WHERE email = $1 AND is_active = true
ORDER BY organization_id;
