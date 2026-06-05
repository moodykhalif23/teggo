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

-- CustomersAffectedByPriceList returns every customer whose resolved price may
-- change when this price list changes (direct, via group, or via any website
-- assignment of the list). Used to fan out recompute jobs.
-- name: CustomersAffectedByPriceList :many
SELECT DISTINCT c.id AS customer_id
FROM customers c
WHERE c.deleted_at IS NULL
  AND c.organization_id = (SELECT pl.organization_id FROM price_lists pl WHERE pl.id = $1)
  AND (
    EXISTS (SELECT 1 FROM price_list_assignments a WHERE a.price_list_id = $1 AND a.customer_id = c.id)
    OR EXISTS (SELECT 1 FROM price_list_assignments a WHERE a.price_list_id = $1 AND a.customer_group_id = c.customer_group_id)
    OR EXISTS (SELECT 1 FROM price_list_assignments a WHERE a.price_list_id = $1 AND a.website_id IS NOT NULL)
  );

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

-- ===== combined_prices (precomputed read path) =============================

-- GetCombinedPrice is the O(1) storefront read: the resolved tier for a qty.
-- params: $1 customer_id, $2 product_id, $3 unit, $4 quantity, $5 currency
-- name: GetCombinedPrice :one
SELECT cp.value, cp.source_price_list_id, cp.min_quantity
FROM combined_prices cp
WHERE cp.customer_id = $1 AND cp.product_id = $2 AND cp.unit = $3
  AND cp.currency = $5 AND cp.min_quantity <= $4::numeric
ORDER BY cp.min_quantity DESC
LIMIT 1;

-- name: ListCombinedPricesForCustomer :many
SELECT * FROM combined_prices WHERE customer_id = $1 AND currency = $2
ORDER BY product_id, min_quantity;

-- ListCustomerPriceTiersForSlug returns every volume tier (min_quantity break)
-- resolved for a customer on a product, so the storefront can show the buyer
-- their contract pricing ("buy 100+ at X").
-- name: ListCustomerPriceTiersForSlug :many
SELECT cp.unit, cp.min_quantity, cp.value
FROM combined_prices cp
JOIN products p ON p.id = cp.product_id
WHERE cp.customer_id = $1 AND p.slug = $2 AND p.organization_id = $3 AND cp.currency = $4
ORDER BY cp.unit, cp.min_quantity;

-- name: DeleteCombinedPricesForCustomerCurrency :exec
DELETE FROM combined_prices WHERE customer_id = $1 AND currency = $2;

-- RecomputeCombinedPricesForCustomer rebuilds the cache for one customer in one
-- currency: for each product it picks the winning candidate list (highest
-- level, then priority) that has a valid price, and flattens that list's tiers.
-- Run after DeleteCombinedPricesForCustomerCurrency inside one tx.
-- params: $1 customer_id, $2 website_id, $3 currency, $4 at
-- name: RecomputeCombinedPricesForCustomer :exec
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
-- Same precedence as ResolvePrice: customer (4) > ancestor (3) > group (2) > website (1).
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
    FROM price_list_assignments pla WHERE pla.website_id = $2
),
valid_prices AS (
  SELECT pr.product_id, pr.unit, pr.min_quantity, pr.value,
         pr.price_list_id, cl.level, cl.priority
    FROM prices pr
    JOIN candidate_lists cl ON cl.price_list_id = pr.price_list_id
    JOIN price_lists pl ON pl.id = pr.price_list_id
   WHERE pl.currency = $3
     AND pl.is_active
     AND ($4 BETWEEN COALESCE(pr.valid_from, '-infinity') AND COALESCE(pr.valid_to, 'infinity'))
),
winners AS (
  SELECT DISTINCT ON (valid_prices.product_id)
         valid_prices.product_id, valid_prices.price_list_id
    FROM valid_prices
   ORDER BY valid_prices.product_id, valid_prices.level DESC, valid_prices.priority DESC
)
INSERT INTO combined_prices (customer_id, product_id, unit, min_quantity, currency, value, source_price_list_id, computed_at)
SELECT $1, vp.product_id, vp.unit, vp.min_quantity, $3, vp.value, vp.price_list_id, $4
  FROM valid_prices vp
  JOIN winners w ON w.product_id = vp.product_id AND w.price_list_id = vp.price_list_id
ON CONFLICT (customer_id, product_id, unit, min_quantity, currency)
DO UPDATE SET value = EXCLUDED.value, source_price_list_id = EXCLUDED.source_price_list_id, computed_at = EXCLUDED.computed_at;
