-- Pricing engine queries — Implementation Pack 1 §4 + §12.1.
-- NUMERIC params arrive as strings (sqlc money override), so quantity
-- comparisons cast explicitly with ::numeric.

-- ===== Price lists =========================================================

-- name: CreatePriceList :one
INSERT INTO price_lists (organization_id, name, currency, is_default, is_active)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetPriceList :one
SELECT * FROM price_lists
WHERE organization_id = $1 AND id = $2 AND deleted_at IS NULL;

-- name: ListPriceLists :many
SELECT * FROM price_lists
WHERE organization_id = $1 AND deleted_at IS NULL
ORDER BY name;

-- name: UpdatePriceList :one
UPDATE price_lists
SET name = $3, currency = $4, is_default = $5, is_active = $6
WHERE organization_id = $1 AND id = $2 AND deleted_at IS NULL
RETURNING *;

-- ===== Prices (tiers) ======================================================

-- name: UpsertPrice :one
INSERT INTO prices (price_list_id, product_id, unit, min_quantity, value, valid_from, valid_to)
VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (price_list_id, product_id, unit, min_quantity)
DO UPDATE SET value = EXCLUDED.value, valid_from = EXCLUDED.valid_from, valid_to = EXCLUDED.valid_to
RETURNING *;

-- name: ListPricesForList :many
SELECT * FROM prices
WHERE price_list_id = $1
ORDER BY product_id, unit, min_quantity;

-- ===== Assignments =========================================================

-- name: CreatePriceListAssignment :one
INSERT INTO price_list_assignments (price_list_id, customer_id, customer_group_id, website_id, priority)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: ListAssignmentsForList :many
SELECT * FROM price_list_assignments WHERE price_list_id = $1 ORDER BY priority DESC, id;

-- name: DeleteAssignment :execrows
DELETE FROM price_list_assignments WHERE id = $1;

-- name: GetDefaultWebsite :one
SELECT id, default_currency FROM websites
WHERE organization_id = $1 AND is_active
ORDER BY id LIMIT 1;

-- ===== Resolution (§12.1, on-the-fly) ======================================

-- ResolvePrice returns the single unit price plus the source price list for a
-- (customer, product, quantity, currency, website, at). Priority: customer (3)
-- > group (2) > website default (1); higher priority wins within a level; ties
-- broken by the most specific qty tier <= requested.
-- params: $1 customer_id, $2 product_id, $3 quantity, $4 currency, $5 website_id, $6 at
-- name: ResolvePrice :one
WITH RECURSIVE ancestors AS (
  SELECT c0.id, c0.parent_id, 0 AS depth
    FROM customers c0 WHERE c0.id = $1
  UNION ALL
  SELECT p.id, p.parent_id, ancestors.depth + 1
    FROM customers p JOIN ancestors ON p.id = ancestors.parent_id
),
cust AS (
  SELECT customers.id, customers.customer_group_id FROM customers WHERE customers.id = $1
),
-- Precedence: own customer (4) > inherited from an ancestor (3) > group (2) >
-- website (1). Account-hierarchy inheritance (PRD §5.1): a child with no list of
-- its own falls back to the nearest ancestor's assignment.
candidate_lists AS (
  SELECT pla.price_list_id, 4 AS level, pla.priority
    FROM price_list_assignments pla WHERE pla.customer_id = $1
  UNION ALL
  SELECT pla.price_list_id, 3, pla.priority
    FROM price_list_assignments pla
    JOIN ancestors a ON a.id = pla.customer_id AND a.depth > 0
  UNION ALL
  SELECT pla.price_list_id, 2, pla.priority
    FROM price_list_assignments pla
    JOIN cust ON cust.customer_group_id = pla.customer_group_id
  UNION ALL
  SELECT pla.price_list_id, 1, pla.priority
    FROM price_list_assignments pla WHERE pla.website_id = $5
),
priced AS (
  SELECT pr.value, pr.min_quantity, pr.price_list_id, cl.level, cl.priority
    FROM prices pr
    JOIN candidate_lists cl ON cl.price_list_id = pr.price_list_id
    JOIN price_lists pl ON pl.id = pr.price_list_id
   WHERE pr.product_id = $2
     AND pl.currency   = $4
     AND pl.is_active
     AND pr.min_quantity <= $3::numeric
     AND ($6 BETWEEN COALESCE(pr.valid_from, '-infinity') AND COALESCE(pr.valid_to, 'infinity'))
)
SELECT priced.value, priced.price_list_id
  FROM priced
 ORDER BY priced.level DESC, priced.priority DESC, priced.min_quantity DESC
 LIMIT 1;

-- ===== Read-time resolution (replaces the combined_prices cache) ===========
-- These resolve the winning price live from prices + assignments on every read.
-- The recursive ancestor walk is shallow and the per-product lookups ride
-- idx_prices_product + idx_pla_* indexes, so a single resolve is sub-millisecond
-- — the same path the catalog proved fast at scale. No per-customer fan-out, no
-- materialized rows, no recompute storm: a price change is live immediately.

-- ResolvePriceTier is the storefront/cart unit-price read: the winning unit
-- price, its source list, and the matched volume tier for a given quantity.
-- Replaces GetCombinedPrice. Precedence: customer (4) > ancestor (3) > group (2)
-- > website (1); within a level, higher priority then the most specific tier.
-- params: $1 customer_id, $2 product_id, $3 unit, $4 quantity, $5 currency, $6 website_id, $7 at
-- name: ResolvePriceTier :one
WITH RECURSIVE ancestors AS (
  SELECT c0.id, c0.parent_id, 0 AS depth FROM customers c0 WHERE c0.id = $1
  UNION ALL
  SELECT p.id, p.parent_id, ancestors.depth + 1
    FROM customers p JOIN ancestors ON p.id = ancestors.parent_id
),
cust AS (SELECT customers.id, customers.customer_group_id FROM customers WHERE customers.id = $1),
candidate_lists AS (
  SELECT pla.price_list_id, 4 AS level, pla.priority FROM price_list_assignments pla WHERE pla.customer_id = $1
  UNION ALL
  SELECT pla.price_list_id, 3, pla.priority FROM price_list_assignments pla
    JOIN ancestors a ON a.id = pla.customer_id AND a.depth > 0
  UNION ALL
  SELECT pla.price_list_id, 2, pla.priority FROM price_list_assignments pla
    JOIN cust ON cust.customer_group_id = pla.customer_group_id
  UNION ALL
  SELECT pla.price_list_id, 1, pla.priority FROM price_list_assignments pla WHERE pla.website_id = $6
),
priced AS (
  SELECT pr.value, pr.min_quantity, pr.price_list_id, cl.level, cl.priority
    FROM prices pr
    JOIN candidate_lists cl ON cl.price_list_id = pr.price_list_id
    JOIN price_lists pl ON pl.id = pr.price_list_id
   WHERE pr.product_id = $2
     AND pr.unit = $3
     AND pl.currency = $5
     AND pl.is_active
     AND pr.min_quantity <= $4::numeric
     AND ($7 BETWEEN COALESCE(pr.valid_from, '-infinity') AND COALESCE(pr.valid_to, 'infinity'))
)
SELECT priced.value, priced.price_list_id AS source_price_list_id, priced.min_quantity
  FROM priced
 ORDER BY priced.level DESC, priced.priority DESC, priced.min_quantity DESC
 LIMIT 1;

-- ResolvePriceTiersForSlug returns every volume tier of the WINNING list for a
-- customer on a product (by slug), so the storefront can show contract pricing
-- ("buy 100+ at X"). Replaces ListCustomerPriceTiersForSlug.
-- params: $1 customer_id, $2 slug, $3 organization_id, $4 currency, $5 website_id, $6 at
-- name: ResolvePriceTiersForSlug :many
WITH RECURSIVE ancestors AS (
  SELECT c0.id, c0.parent_id, 0 AS depth FROM customers c0 WHERE c0.id = $1
  UNION ALL
  SELECT p.id, p.parent_id, ancestors.depth + 1
    FROM customers p JOIN ancestors ON p.id = ancestors.parent_id
),
cust AS (SELECT customers.id, customers.customer_group_id FROM customers WHERE customers.id = $1),
prod AS (SELECT pp.id FROM products pp WHERE pp.slug = $2 AND pp.organization_id = $3 AND pp.deleted_at IS NULL),
candidate_lists AS (
  SELECT pla.price_list_id, 4 AS level, pla.priority FROM price_list_assignments pla WHERE pla.customer_id = $1
  UNION ALL
  SELECT pla.price_list_id, 3, pla.priority FROM price_list_assignments pla
    JOIN ancestors a ON a.id = pla.customer_id AND a.depth > 0
  UNION ALL
  SELECT pla.price_list_id, 2, pla.priority FROM price_list_assignments pla
    JOIN cust ON cust.customer_group_id = pla.customer_group_id
  UNION ALL
  SELECT pla.price_list_id, 1, pla.priority FROM price_list_assignments pla WHERE pla.website_id = $5
),
valid_prices AS (
  SELECT pr.unit, pr.min_quantity, pr.value, pr.price_list_id, cl.level, cl.priority
    FROM prices pr
    JOIN candidate_lists cl ON cl.price_list_id = pr.price_list_id
    JOIN price_lists pl ON pl.id = pr.price_list_id
    JOIN prod ON prod.id = pr.product_id
   WHERE pl.currency = $4
     AND pl.is_active
     AND ($6 BETWEEN COALESCE(pr.valid_from, '-infinity') AND COALESCE(pr.valid_to, 'infinity'))
),
winner AS (
  SELECT DISTINCT ON (unit) unit, price_list_id
    FROM valid_prices ORDER BY unit, level DESC, priority DESC
)
SELECT vp.unit, vp.min_quantity, vp.value
  FROM valid_prices vp JOIN winner w ON w.unit = vp.unit AND w.price_list_id = vp.price_list_id
 ORDER BY vp.unit, vp.min_quantity;

-- ListResolvedPricesForCustomer resolves the winning price per product for a
-- customer over a KEYSET page of products (id > $6, limit $7). Replaces the
-- admin ListCombinedPricesForCustomer read; paginated so it never resolves the
-- whole catalog at once.
-- params: $1 customer_id, $2 website_id, $3 currency, $4 at, $5 organization_id, $6 after_product_id, $7 limit
-- name: ListResolvedPricesForCustomer :many
WITH RECURSIVE ancestors AS (
  SELECT c0.id, c0.parent_id, 0 AS depth FROM customers c0 WHERE c0.id = $1
  UNION ALL
  SELECT p.id, p.parent_id, ancestors.depth + 1
    FROM customers p JOIN ancestors ON p.id = ancestors.parent_id
),
cust AS (SELECT customers.id, customers.customer_group_id FROM customers WHERE customers.id = $1),
page AS (
  SELECT pp.id FROM products pp
   WHERE pp.organization_id = $5 AND pp.deleted_at IS NULL AND pp.id > $6
   ORDER BY pp.id LIMIT $7
),
candidate_lists AS (
  SELECT pla.price_list_id, 4 AS level, pla.priority FROM price_list_assignments pla WHERE pla.customer_id = $1
  UNION ALL
  SELECT pla.price_list_id, 3, pla.priority FROM price_list_assignments pla
    JOIN ancestors a ON a.id = pla.customer_id AND a.depth > 0
  UNION ALL
  SELECT pla.price_list_id, 2, pla.priority FROM price_list_assignments pla
    JOIN cust ON cust.customer_group_id = pla.customer_group_id
  UNION ALL
  SELECT pla.price_list_id, 1, pla.priority FROM price_list_assignments pla WHERE pla.website_id = $2
),
valid_prices AS (
  SELECT pr.product_id, pr.unit, pr.min_quantity, pr.value, pr.price_list_id, cl.level, cl.priority
    FROM prices pr
    JOIN candidate_lists cl ON cl.price_list_id = pr.price_list_id
    JOIN price_lists pl ON pl.id = pr.price_list_id
    JOIN page ON page.id = pr.product_id
   WHERE pl.currency = $3
     AND pl.is_active
     AND ($4 BETWEEN COALESCE(pr.valid_from, '-infinity') AND COALESCE(pr.valid_to, 'infinity'))
),
winners AS (
  SELECT DISTINCT ON (product_id) product_id, price_list_id
    FROM valid_prices ORDER BY product_id, level DESC, priority DESC
)
SELECT vp.product_id, vp.unit, vp.min_quantity, vp.value, vp.price_list_id AS source_price_list_id
  FROM valid_prices vp JOIN winners w ON w.product_id = vp.product_id AND w.price_list_id = vp.price_list_id
 ORDER BY vp.product_id, vp.unit, vp.min_quantity;
