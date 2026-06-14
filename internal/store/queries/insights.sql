-- Insights analytics + digest persistence.
--
-- The analytics queries are all date-windowed GROUP BY/aggregate over
-- orders/order_items, riding idx_orders_org_created (migration 0056) — the same
-- live, no-materialization philosophy as reporting. They take an explicit
-- [from_ts, to_ts) window so the engine can compute period-over-period deltas by
-- running the same query for the current and the immediately-preceding window.

-- RevenueWindow is the headline rollup for one window: order count + gross
-- revenue (excluding cancelled). Run twice (current + prior) for growth.
-- name: RevenueWindow :one
SELECT count(*)::bigint AS order_count,
       COALESCE(sum(o.grand_total), 0)::numeric(15,4) AS revenue
FROM orders o
WHERE o.organization_id = $1
  AND o.status <> 'cancelled'
  AND o.created_at >= sqlc.arg(from_ts)
  AND o.created_at < sqlc.arg(to_ts);

-- RevenueByCustomerWindow ranks customers by spend in the window. The engine
-- uses it for revenue concentration (top-account dependency risk) and for the
-- biggest movers narrative.
-- name: RevenueByCustomerWindow :many
SELECT o.customer_id,
       c.name,
       count(*)::bigint AS order_count,
       COALESCE(sum(o.grand_total), 0)::numeric(15,4) AS revenue
FROM orders o
JOIN customers c ON c.id = o.customer_id
WHERE o.organization_id = $1
  AND o.status <> 'cancelled'
  AND o.created_at >= sqlc.arg(from_ts)
  AND o.created_at < sqlc.arg(to_ts)
GROUP BY o.customer_id, c.name
ORDER BY revenue DESC;

-- MarginWindow returns line-item revenue and cost of goods over the window, for
-- gross-margin analysis. Cost is at the product's CURRENT cost_price (v1 — no
-- per-line cost snapshot yet); products with cost 0 contribute no cost, so margin
-- reads as 100% until costs are entered. Run twice (current + prior) for the trend.
-- name: MarginWindow :one
SELECT
  COALESCE(sum(oi.row_total), 0)::numeric(15,4)         AS revenue,
  COALESCE(sum(oi.quantity * p.cost_price), 0)::numeric(15,4) AS cost
FROM order_items oi
JOIN orders o   ON o.id = oi.order_id
JOIN products p ON p.id = oi.product_id
WHERE o.organization_id = $1
  AND o.status <> 'cancelled'
  AND o.created_at >= sqlc.arg(from_ts)
  AND o.created_at <  sqlc.arg(to_ts);

-- NewCustomerCountWindow counts accounts whose FIRST ever non-cancelled order
-- falls inside the window — new-logo acquisition for the period.
-- name: NewCustomerCountWindow :one
SELECT count(*)::bigint AS new_customers FROM (
  SELECT o.customer_id
  FROM orders o
  WHERE o.organization_id = $1 AND o.status <> 'cancelled'
  GROUP BY o.customer_id
  HAVING min(o.created_at) >= sqlc.arg(from_ts)
     AND min(o.created_at) <  sqlc.arg(to_ts)
) firsts;

-- ===== digest persistence ==================================================

-- name: CreateInsightDigest :one
INSERT INTO insight_digests (
  organization_id, period_start, period_end, source, trigger, narrative, kpis, anomalies
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: ListInsightDigests :many
SELECT * FROM insight_digests
WHERE organization_id = $1
ORDER BY generated_at DESC
LIMIT $2 OFFSET $3;

-- name: GetInsightDigest :one
SELECT * FROM insight_digests
WHERE id = $1 AND organization_id = $2;

-- name: LatestInsightDigest :one
SELECT * FROM insight_digests
WHERE organization_id = $1
ORDER BY generated_at DESC
LIMIT 1;

-- ListActiveOrganizationIDs enumerates active tenants for the weekly digest
-- sweep (one digest job is fanned out per id).
-- name: ListActiveOrganizationIDs :many
SELECT id FROM organizations WHERE status = 'active' ORDER BY id;
