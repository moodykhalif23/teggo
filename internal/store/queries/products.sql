-- name: ListActiveProducts :many
SELECT public_id, sku, name, slug, description, status, attributes, unit
FROM products
WHERE organization_id = $1
  AND status = 'active'
  AND deleted_at IS NULL
ORDER BY name
LIMIT $2 OFFSET $3;

-- name: GetProductBySlug :one
SELECT public_id, sku, name, slug, description, status, attributes, unit
FROM products
WHERE organization_id = $1 AND slug = $2 AND deleted_at IS NULL;

-- name: CountActiveProducts :one
SELECT count(*) FROM products
WHERE organization_id = $1 AND status = 'active' AND deleted_at IS NULL;
