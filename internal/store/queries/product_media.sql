-- Product images — gallery photos for a product, linked to DAM media_assets.
-- A product is capped at 5 images (enforced in the handler). Only type='image'
-- rows are treated as gallery photos here.

-- name: ListProductImages :many
SELECT pm.id, pm.product_id, pm.media_asset_id, pm.url, pm.alt, pm.sort_order,
       ma.status AS asset_status, ma.width, ma.height
FROM product_media pm
LEFT JOIN media_assets ma ON ma.id = pm.media_asset_id
WHERE pm.product_id = $1 AND pm.type = 'image'
ORDER BY pm.sort_order, pm.id;

-- name: CountProductImages :one
SELECT count(*) FROM product_media WHERE product_id = $1 AND type = 'image';

-- name: MaxProductImageSort :one
SELECT COALESCE(MAX(sort_order), -1)::int FROM product_media WHERE product_id = $1 AND type = 'image';

-- name: CreateProductImage :one
INSERT INTO product_media (product_id, media_asset_id, url, type, alt, sort_order)
VALUES ($1, $2, $3, 'image', $4, $5)
RETURNING id, product_id, media_asset_id, url, alt, sort_order;

-- name: DeleteProductImage :execrows
DELETE FROM product_media WHERE id = $1 AND product_id = $2 AND type = 'image';

-- GetMediaAssetForOrg validates that a media asset belongs to the caller's org
-- and returns its servable URL (so the product image can denormalize it).
-- name: GetMediaAssetForOrg :one
SELECT id, url FROM media_assets WHERE id = $1 AND organization_id = $2;

-- ExportProductsAdmin streams the full (non-deleted) catalog for CSV export.
-- name: ExportProductsAdmin :many
SELECT sku, type, name, slug, description, status, unit, attributes, cost_price
FROM products
WHERE organization_id = $1 AND deleted_at IS NULL
ORDER BY sku;
