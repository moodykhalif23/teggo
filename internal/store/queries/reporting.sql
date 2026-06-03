-- Reporting queries — Pack 3 §1. All read from the precomputed materialized
-- views, org-scoped.

-- DailySales returns the daily revenue/order series within a date range
-- (aggregated across currencies for the dashboard line chart).
-- name: DailySales :many
SELECT day, sum(order_count)::bigint AS order_count, sum(revenue)::numeric(15,4) AS revenue
FROM mv_daily_sales
WHERE organization_id = $1 AND day >= $2 AND day <= $3
GROUP BY day
ORDER BY day;

-- SalesSummary is the headline KPI rollup since a date.
-- name: SalesSummary :one
SELECT COALESCE(sum(order_count), 0)::bigint AS order_count,
       COALESCE(sum(revenue), 0)::numeric(15,4) AS revenue
FROM mv_daily_sales
WHERE organization_id = $1 AND day >= $2;

-- TopProducts ranks products by revenue in a month, joined to product names.
-- name: TopProducts :many
SELECT t.product_id, p.sku, p.name,
       t.qty::numeric(15,4) AS qty,
       t.revenue::numeric(15,4) AS revenue
FROM mv_top_products t
JOIN products p ON p.id = t.product_id
WHERE t.organization_id = $1 AND t.month = $2
ORDER BY t.revenue DESC, p.name
LIMIT $3;
