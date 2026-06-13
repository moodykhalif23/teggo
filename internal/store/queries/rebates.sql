-- Rebates / volume incentives .

-- ===== Programs ============================================================

-- name: CreateRebateProgram :one
INSERT INTO rebate_programs (organization_id, name, description, customer_id, period, currency, is_active)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: UpdateRebateProgram :one
UPDATE rebate_programs
SET name = $3, description = $4, customer_id = $5, period = $6, currency = $7, is_active = $8
WHERE organization_id = $1 AND id = $2
RETURNING *;

-- name: GetRebateProgram :one
SELECT * FROM rebate_programs WHERE organization_id = $1 AND id = $2;

-- GetRebateProgramByID loads a program without org scoping, for the trusted
-- background settlement worker (the enqueue was already org-authorized).
-- name: GetRebateProgramByID :one
SELECT * FROM rebate_programs WHERE id = $1;

-- name: ListRebatePrograms :many
SELECT * FROM rebate_programs WHERE organization_id = $1
ORDER BY is_active DESC, name;

-- name: ListActiveRebateProgramsForCustomer :many
-- Programs that apply to a customer: org-scoped, active, and either all-customer
-- (customer_id NULL) or this specific customer.
SELECT * FROM rebate_programs
WHERE organization_id = $1 AND is_active AND (customer_id IS NULL OR customer_id = $2)
ORDER BY name;

-- name: DeleteRebateProgram :execrows
DELETE FROM rebate_programs WHERE organization_id = $1 AND id = $2;

-- ===== Tiers ===============================================================

-- name: CreateRebateTier :one
INSERT INTO rebate_tiers (program_id, min_amount, rate_percent, sort_order)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: ListRebateTiers :many
SELECT * FROM rebate_tiers WHERE program_id = $1 ORDER BY min_amount;

-- name: DeleteRebateTier :execrows
DELETE FROM rebate_tiers t USING rebate_programs p
WHERE t.id = $2 AND t.program_id = p.id AND p.organization_id = $1;

-- ===== Accrual (derived from orders) =======================================

-- RebateQualifyingTotals sums qualifying (non-cancelled) order subtotal per
-- customer for a program's currency within a period window. Optional customer scope.
-- name: RebateQualifyingTotals :many
SELECT o.customer_id, SUM(o.subtotal)::numeric(15,4) AS total, count(*)::bigint AS orders
FROM orders o
WHERE o.organization_id = $1
  AND o.currency = $2
  AND o.created_at >= $3 AND o.created_at < $4
  AND o.status <> 'cancelled'
  AND (sqlc.narg('customer')::bigint IS NULL OR o.customer_id = sqlc.narg('customer'))
GROUP BY o.customer_id
ORDER BY o.customer_id;

-- ===== Settlements =========================================================

-- name: CreateRebateSettlement :one
INSERT INTO rebate_settlements (program_id, customer_id, period_key, qualifying_total, rate_percent, rebate_amount, currency, status, credit_note_id)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING *;

-- name: ListRebateSettlementsForProgram :many
SELECT * FROM rebate_settlements WHERE program_id = $1 ORDER BY created_at DESC, id DESC;

-- name: ListRebateSettlementsForCustomer :many
SELECT rs.*, rp.name AS program_name
FROM rebate_settlements rs
JOIN rebate_programs rp ON rp.id = rs.program_id
WHERE rs.customer_id = $1 AND rp.organization_id = $2
ORDER BY rs.created_at DESC, rs.id DESC;
