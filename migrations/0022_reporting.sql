-- Reporting & analytics — Pack 3 §1 (V1 operational dashboards). Heavy rollups
-- are precomputed as materialized views so dashboard widgets issue bounded, fast
-- queries (never ad-hoc scans of the OLTP tables). A river periodic job refreshes
-- them with REFRESH MATERIALIZED VIEW CONCURRENTLY (needs the unique indexes).
-- The custom report builder + schedules/runs are V2 and not built yet.

CREATE MATERIALIZED VIEW mv_daily_sales AS
SELECT o.organization_id,
       date_trunc('day', o.created_at)::date AS day,
       o.currency,
       count(*)                             AS order_count,
       sum(o.grand_total)::numeric(15, 4)   AS revenue
  FROM orders o
 WHERE o.status NOT IN ('cancelled')
 GROUP BY 1, 2, 3;
CREATE UNIQUE INDEX uq_mv_daily_sales ON mv_daily_sales (organization_id, day, currency);

CREATE MATERIALIZED VIEW mv_top_products AS
SELECT oi.product_id,
       o.organization_id,
       date_trunc('month', o.created_at)::date AS month,
       sum(oi.quantity)::numeric(15, 4)        AS qty,
       sum(oi.row_total)::numeric(15, 4)       AS revenue
  FROM order_items oi
  JOIN orders o ON o.id = oi.order_id
 WHERE o.status NOT IN ('cancelled')
 GROUP BY 1, 2, 3;
CREATE UNIQUE INDEX uq_mv_top_products ON mv_top_products (product_id, organization_id, month);

-- Reporting permission for the demo admin role.
INSERT INTO role_permissions (role_id, permission)
SELECT r.id, 'report.view'
  FROM roles r
 WHERE r.organization_id = 1 AND r.name = 'admin'
ON CONFLICT DO NOTHING;
