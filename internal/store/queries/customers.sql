-- Customers & accounts queries — Implementation Pack 1 §2 + §12.2.
-- Every query is organization-scoped (tenant isolation enforced at the query layer).

-- name: CreateCustomerGroup :one
INSERT INTO customer_groups (organization_id, name)
VALUES ($1, $2)
RETURNING id, organization_id, name;

-- name: ListCustomerGroups :many
SELECT id, organization_id, name
FROM customer_groups
WHERE organization_id = $1
ORDER BY name;

-- name: CreateCustomer :one
INSERT INTO customers (
  organization_id, parent_id, customer_group_id, name, tax_id,
  payment_terms_days, credit_limit, assigned_sales_rep_id
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: GetCustomer :one
SELECT * FROM customers
WHERE organization_id = $1 AND id = $2 AND deleted_at IS NULL;

-- name: GetCustomerByPublicID :one
SELECT * FROM customers
WHERE organization_id = $1 AND public_id = $2 AND deleted_at IS NULL;

-- name: ListCustomers :many
SELECT * FROM customers
WHERE organization_id = $1 AND deleted_at IS NULL
ORDER BY name
LIMIT $2 OFFSET $3;

-- name: CountCustomers :one
SELECT count(*) FROM customers
WHERE organization_id = $1 AND deleted_at IS NULL;

-- name: UpdateCustomer :one
UPDATE customers
SET name               = $3,
    tax_id             = $4,
    payment_terms_days = $5,
    credit_limit       = $6,
    customer_group_id  = $7,
    parent_id          = $8,
    assigned_sales_rep_id = $9,
    is_active          = $10
WHERE organization_id = $1 AND id = $2 AND deleted_at IS NULL
RETURNING *;

-- name: SoftDeleteCustomer :execrows
UPDATE customers
SET deleted_at = now()
WHERE organization_id = $1 AND id = $2 AND deleted_at IS NULL;

-- CustomerAncestors returns all ancestors of a customer, nearest first
-- (cycle-safe recursive CTE — Pack 1 §12.2). Used to inherit price list /
-- settings down the account tree.
-- name: CustomerAncestors :many
WITH RECURSIVE chain AS (
  SELECT c0.id, c0.parent_id, 0 AS depth
    FROM customers c0
   WHERE c0.id = $1 AND c0.organization_id = $2
  UNION ALL
  SELECT c.id, c.parent_id, chain.depth + 1
    FROM customers c
    JOIN chain ON c.id = chain.parent_id
   WHERE c.organization_id = $2
)
SELECT chain.id, chain.depth FROM chain WHERE chain.depth > 0 ORDER BY chain.depth;

-- name: CreateCustomerUser :one
INSERT INTO customer_users (
  customer_id, email, password_hash, full_name, role, spending_limit
) VALUES ($1, $2, $3, $4, $5, $6)
RETURNING id, customer_id, email, full_name, role, spending_limit, is_active, created_at, updated_at;

-- name: ListCustomerUsers :many
SELECT id, customer_id, email, full_name, role, spending_limit, is_active, created_at, updated_at
FROM customer_users
WHERE customer_id = $1
ORDER BY full_name;

-- name: CreateCustomerAddress :one
INSERT INTO customer_addresses (
  customer_id, type, is_default, line1, line2, city, region, postal_code, country
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING *;

-- name: ListCustomerAddresses :many
SELECT * FROM customer_addresses
WHERE customer_id = $1
ORDER BY type, is_default DESC;
