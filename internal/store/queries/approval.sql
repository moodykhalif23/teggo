-- Approval routing rules (migration 0033).

-- name: ListApprovalRoutingRules :many
SELECT * FROM approval_routing_rules
WHERE organization_id = $1
ORDER BY sort_order, min_amount;

-- name: CreateApprovalRoutingRule :one
INSERT INTO approval_routing_rules (organization_id, min_amount, max_amount, required_role, sort_order)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: DeleteApprovalRoutingRule :execrows
DELETE FROM approval_routing_rules WHERE id = $1 AND organization_id = $2;
