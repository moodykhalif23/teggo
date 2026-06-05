-- CRM queries — Pack 2 §1. Admin/sales-rep facing; everything org-scoped.

-- ===== Leads ===============================================================

-- name: CreateLead :one
INSERT INTO leads (organization_id, source, company_name, contact_name, email, phone, notes, owner_user_id)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: GetLead :one
SELECT * FROM leads WHERE organization_id = $1 AND id = $2;

-- name: ListLeads :many
SELECT * FROM leads WHERE organization_id = $1 ORDER BY created_at DESC LIMIT $2 OFFSET $3;

-- name: SetLeadStatus :one
UPDATE leads SET status = $3 WHERE organization_id = $1 AND id = $2 RETURNING *;

-- MarkLeadConverted records the conversion result; only converts a not-yet-
-- converted lead (idempotency guard at the DB level).
-- name: MarkLeadConverted :one
UPDATE leads
SET status = 'converted', converted_customer_id = $3
WHERE organization_id = $1 AND id = $2 AND status <> 'converted'
RETURNING *;

-- ===== Contacts ============================================================

-- name: CreateContact :one
INSERT INTO contacts (organization_id, customer_id, customer_user_id, full_name, email, phone, job_title)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: GetContact :one
SELECT * FROM contacts WHERE organization_id = $1 AND id = $2;

-- name: ListContactsForCustomer :many
SELECT * FROM contacts WHERE organization_id = $1 AND customer_id = $2 ORDER BY full_name;

-- ===== Pipelines & stages ==================================================

-- name: GetDefaultPipeline :one
SELECT * FROM pipelines WHERE organization_id = $1 AND is_default ORDER BY id LIMIT 1;

-- name: GetPipeline :one
SELECT * FROM pipelines WHERE organization_id = $1 AND id = $2;

-- name: ListPipelineStages :many
SELECT * FROM pipeline_stages WHERE pipeline_id = $1 ORDER BY sort_order, id;

-- name: GetStage :one
SELECT * FROM pipeline_stages WHERE id = $1;

-- FirstStage returns the lowest-sort_order stage of a pipeline (the entry stage).
-- name: FirstStage :one
SELECT * FROM pipeline_stages WHERE pipeline_id = $1 ORDER BY sort_order, id LIMIT 1;

-- PipelineBoard: per-stage open count, total and probability-weighted amounts
-- (Pack 2 §1.4). Sums cast to text via the numeric override; count is bigint.
-- name: PipelineBoard :many
SELECT s.id, s.code, s.label, s.probability, s.is_won, s.is_lost, s.sort_order,
       count(o.id) AS open_count,
       COALESCE(sum(o.amount), 0)::numeric(15,4) AS total_amount,
       COALESCE(sum(o.amount * s.probability / 100.0), 0)::numeric(15,4) AS weighted_amount
FROM pipeline_stages s
LEFT JOIN opportunities o ON o.stage_id = s.id AND o.closed_at IS NULL
WHERE s.pipeline_id = $1
GROUP BY s.id
ORDER BY s.sort_order, s.id;

-- ===== Opportunities =======================================================

-- name: CreateOpportunity :one
INSERT INTO opportunities (organization_id, customer_id, contact_id, pipeline_id, stage_id, name, amount, currency, expected_close, owner_user_id)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
RETURNING *;

-- name: GetOpportunity :one
SELECT * FROM opportunities WHERE organization_id = $1 AND id = $2;

-- name: ListOpportunities :many
SELECT * FROM opportunities WHERE organization_id = $1 ORDER BY created_at DESC LIMIT $2 OFFSET $3;

-- name: SetOpportunityStage :one
UPDATE opportunities SET stage_id = $3, closed_at = $4
WHERE organization_id = $1 AND id = $2
RETURNING *;

-- name: AddOpportunityStageHistory :exec
INSERT INTO opportunity_stage_history (opportunity_id, from_stage_id, to_stage_id, changed_by)
VALUES ($1, $2, $3, $4);

-- ===== Activities ==========================================================

-- name: CreateActivity :one
INSERT INTO activities (organization_id, type, subject, body, customer_id, contact_id, opportunity_id, lead_id, owner_user_id, status, due_at, occurred_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
RETURNING *;

-- CustomerTimeline aggregates activities linked to a customer directly, via its
-- contacts, or via its opportunities (Pack 2 §1.4), newest first.
-- name: CustomerTimeline :many
SELECT a.* FROM activities a
WHERE a.organization_id = $1
  AND (
    a.customer_id = $2
    OR a.contact_id IN (SELECT c.id FROM contacts c WHERE c.customer_id = $2)
    OR a.opportunity_id IN (SELECT o.id FROM opportunities o WHERE o.customer_id = $2)
  )
ORDER BY a.occurred_at DESC
LIMIT 100;

-- AccountHealth aggregates each customer's order history (org-scoped, excluding
-- cancelled) into the signals a rep needs to spot a slipping account: lifetime
-- value, first/last order, and recent vs prior 90-day order counts.
-- name: AccountHealth :many
SELECT o.customer_id,
       max(c.name)::text                            AS name,
       c.assigned_sales_rep_id                       AS rep_id,
       count(*)                                      AS order_count,
       min(o.created_at)::timestamptz                AS first_ordered,
       max(o.created_at)::timestamptz                AS last_ordered,
       COALESCE(sum(o.grand_total), 0)::numeric(15,4) AS lifetime_value,
       count(*) FILTER (WHERE o.created_at >= now() - interval '90 days')                                          AS recent_count,
       count(*) FILTER (WHERE o.created_at >= now() - interval '180 days' AND o.created_at < now() - interval '90 days') AS prior_count
FROM orders o
JOIN customers c ON c.id = o.customer_id
WHERE c.organization_id = $1 AND o.status <> 'cancelled' AND c.deleted_at IS NULL
GROUP BY o.customer_id, c.assigned_sales_rep_id;
