-- Marketplace: vendors, vendor portal logins, per-vendor order splitting,
-- commission ledger and payouts. (migration 0041)

-- name: CreateVendor :one
INSERT INTO vendors (organization_id, name, slug, contact_email, status, commission_rate, payout_terms_days)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: UpdateVendor :one
UPDATE vendors
   SET name = $2, contact_email = $3, status = $4, commission_rate = $5, payout_terms_days = $6
 WHERE id = $1 AND organization_id = $7 AND deleted_at IS NULL
RETURNING *;

-- name: GetVendor :one
SELECT * FROM vendors
WHERE id = $1 AND organization_id = $2 AND deleted_at IS NULL;

-- name: ListVendors :many
SELECT * FROM vendors
WHERE organization_id = $1 AND deleted_at IS NULL
ORDER BY name;

-- name: SoftDeleteVendor :exec
UPDATE vendors SET deleted_at = now()
WHERE id = $1 AND organization_id = $2;

-- name: CreateVendorUser :one
INSERT INTO vendor_users (vendor_id, email, password_hash, full_name, role)
VALUES ($1, $2, $3, $4, $5)
RETURNING id, vendor_id, email, full_name, role, is_active, created_at, updated_at;

-- name: ListVendorUsers :many
SELECT id, vendor_id, email, full_name, role, is_active, created_at, updated_at
FROM vendor_users
WHERE vendor_id = $1
ORDER BY full_name;

-- ---- order splitting & commission ledger --------------------------------

-- ListOrderItemsWithVendor returns each line of an order with the owning vendor
-- of its product (NULL = operator-owned), for fanning an order into per-vendor
-- sub-orders at placement time.
-- name: ListOrderItemsWithVendor :many
SELECT oi.id, oi.row_total, p.vendor_id
FROM order_items oi
JOIN products p ON p.id = oi.product_id
WHERE oi.order_id = $1;

-- name: SetOrderItemVendor :exec
UPDATE order_items SET vendor_id = $2 WHERE id = $1;

-- name: CreateVendorOrder :one
INSERT INTO vendor_orders (organization_id, order_id, vendor_id, currency, gross_total, commission_rate, commission_total, net_total)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: ListVendorOrdersForOrder :many
SELECT * FROM vendor_orders WHERE order_id = $1 ORDER BY vendor_id;

-- GetVendorUserForLogin resolves a vendor-user by email within an org for vendor
-- portal authentication (email is citext, so case-insensitive).
-- name: GetVendorUserForLogin :one
SELECT vu.id, vu.vendor_id, v.organization_id, vu.password_hash, vu.is_active
FROM vendor_users vu
JOIN vendors v ON v.id = vu.vendor_id
WHERE v.organization_id = $1 AND vu.email = $2
  AND vu.is_active = true AND v.status <> 'suspended' AND v.deleted_at IS NULL
LIMIT 1;
