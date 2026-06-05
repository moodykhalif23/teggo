-- Inventory queries — Implementation Pack 1 §8 + §12.4 (ATP).

-- ===== Warehouses ==========================================================

-- name: CreateWarehouse :one
INSERT INTO warehouses (organization_id, name, is_active)
VALUES ($1, $2, true)
RETURNING *;

-- name: ListWarehouses :many
SELECT * FROM warehouses WHERE organization_id = $1 ORDER BY name;

-- name: GetDefaultWarehouse :one
SELECT * FROM warehouses WHERE organization_id = $1 AND is_active
ORDER BY id LIMIT 1;

-- name: GetWarehouse :one
SELECT * FROM warehouses WHERE organization_id = $1 AND id = $2;

-- ===== Levels ==============================================================

-- name: EnsureInventoryLevel :exec
INSERT INTO inventory_levels (product_id, warehouse_id, quantity_on_hand, quantity_reserved)
VALUES ($1, $2, 0, 0)
ON CONFLICT (product_id, warehouse_id) DO NOTHING;

-- name: SetInventoryLevelConfig :one
INSERT INTO inventory_levels (product_id, warehouse_id, reorder_threshold, allow_backorder)
VALUES ($1, $2, $3, $4)
ON CONFLICT (product_id, warehouse_id)
DO UPDATE SET reorder_threshold = EXCLUDED.reorder_threshold, allow_backorder = EXCLUDED.allow_backorder
RETURNING *;

-- name: GetInventoryLevel :one
SELECT * FROM inventory_levels WHERE product_id = $1 AND warehouse_id = $2;

-- name: ListInventoryLevelsForProduct :many
SELECT il.*, (il.quantity_on_hand - il.quantity_reserved)::numeric(15,4) AS available
FROM inventory_levels il WHERE il.product_id = $1
ORDER BY il.warehouse_id;

-- AdjustInventoryLevel applies signed deltas to on-hand and reserved.
-- name: AdjustInventoryLevel :one
UPDATE inventory_levels
SET quantity_on_hand  = quantity_on_hand  + $3::numeric,
    quantity_reserved = quantity_reserved + $4::numeric
WHERE product_id = $1 AND warehouse_id = $2
RETURNING *;

-- AvailableToPromise sums available across warehouses for a set of products (§12.4).
-- name: AvailableToPromise :many
SELECT product_id, SUM(quantity_on_hand - quantity_reserved)::numeric(15,4) AS available
FROM inventory_levels
WHERE product_id = ANY($1::bigint[])
GROUP BY product_id;

-- ===== Movements ===========================================================

-- name: AddInventoryMovement :one
INSERT INTO inventory_movements (product_id, warehouse_id, type, quantity, reference_type, reference_id, created_by)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: ListInventoryMovements :many
SELECT * FROM inventory_movements
WHERE product_id = $1 AND warehouse_id = $2
ORDER BY created_at DESC, id DESC
LIMIT 200;

-- ListShipmentItemProducts resolves a shipment's lines to product + quantity
-- (for converting reservations to fulfilment on ship).
-- name: ListShipmentItemProducts :many
SELECT si.order_item_id, oi.product_id, si.quantity
FROM shipment_items si
JOIN order_items oi ON oi.id = si.order_item_id
WHERE si.shipment_id = $1;

-- ProductAvailabilityByWarehouse lists per-warehouse available qty (on_hand -
-- reserved) for a product, for storefront per-location availability display.
-- name: ProductAvailabilityByWarehouse :many
SELECT w.id AS warehouse_id, w.name AS warehouse_name,
       (il.quantity_on_hand - il.quantity_reserved)::numeric AS available
FROM inventory_levels il
JOIN warehouses w ON w.id = il.warehouse_id
WHERE il.product_id = $1 AND w.organization_id = $2 AND w.is_active = true
ORDER BY w.name;
