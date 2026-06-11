-- Customer invite links — shareable buyer-onboarding tokens (0044).
-- Admin queries are organization-scoped via the customers join; the public
-- token lookup carries the org back so the accept flow can mint a token.

-- name: CreateCustomerInvite :one
INSERT INTO customer_invites (customer_id, role, spending_limit, expires_at, created_by)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: ListInvitesForCustomer :many
SELECT ci.*
FROM customer_invites ci
JOIN customers c ON c.id = ci.customer_id
WHERE ci.customer_id = $1 AND c.organization_id = $2
ORDER BY ci.id DESC;

-- GetInviteByToken resolves a live invite plus the company it joins. Validity
-- (expiry/revocation) is checked app-side so we can return precise errors.
-- name: GetInviteByToken :one
SELECT ci.*, c.name AS customer_name, c.organization_id, c.is_active AS customer_is_active
FROM customer_invites ci
JOIN customers c ON c.id = ci.customer_id
WHERE ci.token = $1 AND c.deleted_at IS NULL
LIMIT 1;

-- name: RevokeCustomerInvite :execrows
UPDATE customer_invites ci
SET revoked_at = now()
FROM customers c
WHERE ci.id = $1 AND ci.customer_id = c.id AND c.organization_id = $2
  AND ci.revoked_at IS NULL;

-- name: IncrementInviteUse :exec
UPDATE customer_invites SET use_count = use_count + 1 WHERE id = $1;
