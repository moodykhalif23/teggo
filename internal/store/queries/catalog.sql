-- Catalog & PIM queries — Implementation Pack 1 §3, §12.3 (subtree), §12.5 (facets).
-- Storefront product reads live in products.sql; this file is the admin PIM surface
-- plus the category-subtree and JSONB-facet reads used by the storefront listing.

-- ===== Products (admin) ====================================================

-- name: CreateProduct :one
INSERT INTO products (
  organization_id, sku, type, name, slug, description, status,
  attributes, unit, parent_id, attribute_family_id
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
RETURNING *;

-- name: GetProductByID :one
SELECT * FROM products
WHERE organization_id = $1 AND id = $2 AND deleted_at IS NULL;

-- name: GetProductIDByPublicID :one
SELECT id FROM products
WHERE organization_id = $1 AND public_id = $2 AND deleted_at IS NULL;

-- name: ListProductsAdmin :many
SELECT * FROM products
WHERE organization_id = $1 AND deleted_at IS NULL
ORDER BY name
LIMIT $2 OFFSET $3;

-- name: CountProductsAdmin :one
SELECT count(*) FROM products
WHERE organization_id = $1 AND deleted_at IS NULL;

-- SearchProductsAdmin: admin product search over the FTS vector (PRD §14),
-- ranked by relevance. Includes all statuses (admins manage drafts too).
-- name: SearchProductsAdmin :many
SELECT * FROM products
WHERE organization_id = $1 AND deleted_at IS NULL
  AND search_vector @@ websearch_to_tsquery('english', $2)
ORDER BY ts_rank(search_vector, websearch_to_tsquery('english', $2)) DESC, name
LIMIT $3 OFFSET $4;

-- name: CountSearchProductsAdmin :one
SELECT count(*) FROM products
WHERE organization_id = $1 AND deleted_at IS NULL
  AND search_vector @@ websearch_to_tsquery('english', $2);

-- name: UpdateProduct :one
UPDATE products
SET sku                 = $3,
    type                = $4,
    name                = $5,
    slug                = $6,
    description         = $7,
    status              = $8,
    attributes          = $9,
    unit                = $10,
    parent_id           = $11,
    attribute_family_id = $12
WHERE organization_id = $1 AND id = $2 AND deleted_at IS NULL
RETURNING *;

-- SetProductCost sets a product's unit cost (for margin). Kept separate from
-- Create/UpdateProduct so those stay free of cost — every existing caller that
-- doesn't set a cost relies on the column's DEFAULT 0. The param maps straight
-- to the column, so it generates a clean CostPrice (callers pass a valid decimal
-- string, never empty).
-- name: SetProductCost :one
UPDATE products SET cost_price = $3
WHERE organization_id = $1 AND id = $2 AND deleted_at IS NULL
RETURNING *;

-- name: SoftDeleteProduct :execrows
UPDATE products
SET deleted_at = now()
WHERE organization_id = $1 AND id = $2 AND deleted_at IS NULL;

-- SearchActiveProducts: full-text product search (PRD §14, Postgres FTS).
-- $2 is the raw user query in websearch syntax (e.g. `brass valve`, `"ball valve"`,
-- `valve -plastic`); results are ranked by relevance, then name as a tiebreak.
-- name: SearchActiveProducts :many
SELECT p.id, p.public_id, p.sku, p.name, p.slug, p.description,
       p.status, p.attributes, p.unit
FROM products p
WHERE p.organization_id = $1
  AND p.status = 'active' AND p.approval_status = 'approved' AND p.deleted_at IS NULL
  AND p.search_vector @@ websearch_to_tsquery('english', $2)
ORDER BY ts_rank(p.search_vector, websearch_to_tsquery('english', $2)) DESC, p.name
LIMIT $3 OFFSET $4;

-- ===== Faceted search (PRD §14 V1) =========================================
-- One filter set drives three queries (items / count / facet aggregation), all
-- sharing the same WHERE. Every filter is optional via the `$n::type IS NULL OR`
-- idiom: $2 keyword (FTS), $3 attribute JSONB (@>), $4 category-id array (subtree
-- resolved in Go). $5 sort: 'relevance' | 'newest' | else name.

-- name: SearchProductsFaceted :many
SELECT p.id, p.public_id, p.sku, p.name, p.slug, p.description, p.status, p.attributes, p.unit
FROM products p
WHERE p.organization_id = sqlc.arg('org') AND p.status = 'active' AND p.approval_status = 'approved' AND p.deleted_at IS NULL
  AND (sqlc.narg('q')::text IS NULL OR p.search_vector @@ websearch_to_tsquery('english', sqlc.narg('q')))
  AND (sqlc.narg('attrs')::jsonb IS NULL OR p.attributes @> sqlc.narg('attrs'))
  AND (sqlc.narg('cat_ids')::bigint[] IS NULL OR p.id IN (SELECT pc.product_id FROM product_categories pc WHERE pc.category_id = ANY(sqlc.narg('cat_ids'))))
ORDER BY
  (CASE WHEN sqlc.arg('sort')::text = 'newest' THEN p.created_at END) DESC NULLS LAST,
  (CASE WHEN sqlc.arg('sort')::text = 'relevance' THEN ts_rank(p.search_vector, websearch_to_tsquery('english', COALESCE(sqlc.narg('q'), ''))) END) DESC NULLS LAST,
  p.name
LIMIT sqlc.arg('lim') OFFSET sqlc.arg('off');

-- name: CountProductsFaceted :one
SELECT count(*) FROM products p
WHERE p.organization_id = sqlc.arg('org') AND p.status = 'active' AND p.approval_status = 'approved' AND p.deleted_at IS NULL
  AND (sqlc.narg('q')::text IS NULL OR p.search_vector @@ websearch_to_tsquery('english', sqlc.narg('q')))
  AND (sqlc.narg('attrs')::jsonb IS NULL OR p.attributes @> sqlc.narg('attrs'))
  AND (sqlc.narg('cat_ids')::bigint[] IS NULL OR p.id IN (SELECT pc.product_id FROM product_categories pc WHERE pc.category_id = ANY(sqlc.narg('cat_ids'))));

-- ProductFacets unnests the JSONB attributes of the filtered result set into
-- (attribute, value, count) for the storefront filter sidebar.
-- name: ProductFacets :many
SELECT kv.key::text AS attr, kv.value::text AS value, count(*)::bigint AS count
FROM products p, jsonb_each_text(p.attributes) AS kv(key, value)
WHERE p.organization_id = sqlc.arg('org') AND p.status = 'active' AND p.approval_status = 'approved' AND p.deleted_at IS NULL
  AND (sqlc.narg('q')::text IS NULL OR p.search_vector @@ websearch_to_tsquery('english', sqlc.narg('q')))
  AND (sqlc.narg('attrs')::jsonb IS NULL OR p.attributes @> sqlc.narg('attrs'))
  AND (sqlc.narg('cat_ids')::bigint[] IS NULL OR p.id IN (SELECT pc.product_id FROM product_categories pc WHERE pc.category_id = ANY(sqlc.narg('cat_ids'))))
GROUP BY kv.key, kv.value
ORDER BY kv.key, count DESC, kv.value;

-- FilterActiveProductsByAttributes: faceted filter over the JSONB attributes,
-- backed by idx_products_attrs_gin (Pack 1 §12.5). $2 is a JSONB object like
-- {"color":"red","voltage":"24"}.
-- name: FilterActiveProductsByAttributes :many
SELECT * FROM products
WHERE organization_id = $1
  AND status = 'active' AND approval_status = 'approved' AND deleted_at IS NULL
  AND attributes @> $2
ORDER BY name
LIMIT $3 OFFSET $4;

-- ===== Categories ==========================================================

-- name: CreateCategory :one
INSERT INTO categories (organization_id, parent_id, name, slug, sort_order)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetCategory :one
SELECT * FROM categories WHERE organization_id = $1 AND id = $2;

-- name: GetCategoryBySlug :one
SELECT * FROM categories WHERE organization_id = $1 AND slug = $2;

-- name: ListCategories :many
SELECT * FROM categories
WHERE organization_id = $1
ORDER BY sort_order, name;

-- CategoryDescendantIDs returns the category and all of its descendants
-- (subtree, Pack 1 §12.3). $1 root id, $2 organization_id.
-- name: CategoryDescendantIDs :many
WITH RECURSIVE subtree AS (
  SELECT c0.id FROM categories c0 WHERE c0.id = $1 AND c0.organization_id = $2
  UNION ALL
  SELECT c.id FROM categories c JOIN subtree s ON c.parent_id = s.id
)
SELECT subtree.id FROM subtree;

-- ListActiveProductsInCategory returns active products in a category's whole
-- subtree (storefront browse, §12.3). $1 org, $2 root category, $3 limit, $4 offset.
-- name: ListActiveProductsInCategory :many
WITH RECURSIVE subtree AS (
  SELECT c0.id FROM categories c0 WHERE c0.id = $2 AND c0.organization_id = $1
  UNION ALL
  SELECT c.id FROM categories c JOIN subtree s ON c.parent_id = s.id
)
SELECT DISTINCT p.id, p.public_id, p.sku, p.name, p.slug, p.description,
       p.status, p.attributes, p.unit
FROM products p
JOIN product_categories pc ON pc.product_id = p.id
WHERE pc.category_id IN (SELECT subtree.id FROM subtree)
  AND p.organization_id = $1
  AND p.status = 'active' AND p.approval_status = 'approved' AND p.deleted_at IS NULL
ORDER BY p.name
LIMIT $3 OFFSET $4;

-- name: AssignProductToCategory :exec
INSERT INTO product_categories (product_id, category_id)
VALUES ($1, $2)
ON CONFLICT DO NOTHING;

-- name: RemoveProductFromCategory :exec
DELETE FROM product_categories WHERE product_id = $1 AND category_id = $2;

-- name: ListProductCategoryIDs :many
SELECT category_id FROM product_categories WHERE product_id = $1;

-- ===== Attributes & families ==============================================

-- name: CreateAttribute :one
INSERT INTO attributes (
  organization_id, code, label, data_type, options, is_filterable, is_variant_axis
) VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: ListAttributes :many
SELECT * FROM attributes WHERE organization_id = $1 ORDER BY label;

-- name: CreateAttributeFamily :one
INSERT INTO attribute_families (organization_id, name)
VALUES ($1, $2)
RETURNING *;

-- name: ListAttributeFamilies :many
SELECT * FROM attribute_families WHERE organization_id = $1 ORDER BY name;

-- name: AssignAttributeToFamily :exec
INSERT INTO attribute_family_attributes (family_id, attribute_id, is_required, sort_order)
VALUES ($1, $2, $3, $4)
ON CONFLICT (family_id, attribute_id)
DO UPDATE SET is_required = EXCLUDED.is_required, sort_order = EXCLUDED.sort_order;

-- name: ListFamilyAttributes :many
SELECT a.id, a.code, a.label, a.data_type, a.options, a.is_filterable, a.is_variant_axis,
       fa.is_required, fa.sort_order
FROM attribute_family_attributes fa
JOIN attributes a ON a.id = fa.attribute_id
WHERE fa.family_id = $1
ORDER BY fa.sort_order, a.label;

-- ===== Catalog visibility (per-customer/group, migration 0005) =============

-- HiddenProductIDsForCustomer returns the product ids hidden from a buyer by a
-- visible=false rule matching the customer or their group, applied directly to
-- a product or to any category the product belongs to. With cust+grp both null
-- (anonymous) it returns nothing — the default catalog is fully visible.
-- name: HiddenProductIDsForCustomer :many
SELECT DISTINCT p.id
FROM products p
WHERE p.organization_id = $1
  AND (
    EXISTS (
      SELECT 1 FROM catalog_visibility v
      WHERE v.visible = false AND v.product_id = p.id
        AND (v.customer_id = sqlc.narg('cust') OR v.customer_group_id = sqlc.narg('grp'))
    )
    OR EXISTS (
      SELECT 1 FROM catalog_visibility v
      JOIN product_categories pc ON pc.category_id = v.category_id
      WHERE v.visible = false AND pc.product_id = p.id
        AND (v.customer_id = sqlc.narg('cust') OR v.customer_group_id = sqlc.narg('grp'))
    )
  );

-- name: CreateCatalogVisibility :one
INSERT INTO catalog_visibility (product_id, category_id, customer_id, customer_group_id, visible)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- ListCatalogVisibilityForProduct lists the rules attached to a product (org
-- scoped via the product join so admins can't read across tenants).
-- name: ListCatalogVisibilityForProduct :many
SELECT v.* FROM catalog_visibility v
JOIN products p ON p.id = v.product_id
WHERE v.product_id = $1 AND p.organization_id = $2
ORDER BY v.id;

-- name: DeleteCatalogVisibility :execrows
DELETE FROM catalog_visibility v
USING products p
WHERE v.id = $1 AND v.product_id = p.id AND p.organization_id = $2;
