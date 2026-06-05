-- RFQ -> Quote -> Order queries — Implementation Pack 1 §6.
-- Status transitions are validated in the app (state machines); these queries
-- are the primitives. Money columns are decimal strings (sqlc money override).

-- ===== RFQ =================================================================

-- name: CreateRFQ :one
INSERT INTO rfqs (organization_id, website_id, customer_id, customer_user_id, notes)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: AddRFQItem :one
INSERT INTO rfq_items (rfq_id, product_id, quantity, unit, target_price, notes)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: GetRFQByID :one
SELECT * FROM rfqs WHERE organization_id = $1 AND id = $2;

-- name: GetRFQByPublicID :one
SELECT * FROM rfqs WHERE organization_id = $1 AND public_id = $2;

-- name: ListRFQItems :many
SELECT ri.id, ri.product_id, p.sku, p.name, ri.quantity, ri.unit, ri.target_price, ri.notes
FROM rfq_items ri
JOIN products p ON p.id = ri.product_id
WHERE ri.rfq_id = $1
ORDER BY ri.id;

-- name: ListRFQsForCustomer :many
SELECT * FROM rfqs WHERE customer_id = $1 ORDER BY created_at DESC;

-- name: ListRFQsAdmin :many
SELECT * FROM rfqs WHERE organization_id = $1 ORDER BY created_at DESC LIMIT $2 OFFSET $3;

-- name: SetRFQStatus :one
UPDATE rfqs SET status = $2 WHERE id = $1 RETURNING *;

-- ===== Quote ===============================================================

-- name: CreateQuote :one
INSERT INTO quotes (organization_id, website_id, customer_id, rfq_id, sales_rep_user_id, currency, valid_until)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: AddQuoteItem :one
INSERT INTO quote_items (quote_id, product_id, quantity, unit, unit_price, discount, row_total)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: DeleteQuoteItems :exec
DELETE FROM quote_items WHERE quote_id = $1;

-- name: GetQuoteByID :one
SELECT * FROM quotes WHERE organization_id = $1 AND id = $2;

-- name: GetQuoteByPublicID :one
SELECT * FROM quotes WHERE public_id = $1;

-- name: ListQuoteItems :many
SELECT qi.id, qi.product_id, p.sku, p.name, qi.quantity, qi.unit, qi.unit_price, qi.discount, qi.row_total
FROM quote_items qi
JOIN products p ON p.id = qi.product_id
WHERE qi.quote_id = $1
ORDER BY qi.id;

-- name: ListQuotesAdmin :many
SELECT * FROM quotes WHERE organization_id = $1 ORDER BY created_at DESC LIMIT $2 OFFSET $3;

-- name: ListQuotesForCustomer :many
SELECT * FROM quotes WHERE customer_id = $1 ORDER BY created_at DESC;

-- name: SetQuoteSubtotal :exec
UPDATE quotes SET subtotal = $2 WHERE id = $1;

-- name: SetQuoteStatus :one
UPDATE quotes SET status = $2 WHERE id = $1 RETURNING *;

-- SendQuote moves the quote to 'sent' and bumps the version; the caller writes
-- the matching quote_revisions snapshot in the same transaction.
-- name: SendQuote :one
UPDATE quotes SET status = 'sent', version = version + 1, valid_until = COALESCE($2, valid_until)
WHERE id = $1
RETURNING *;

-- name: CreateQuoteRevision :exec
INSERT INTO quote_revisions (quote_id, version, snapshot, created_by)
VALUES ($1, $2, $3, $4);

-- name: ListQuoteRevisions :many
SELECT id, version, created_by, created_at FROM quote_revisions
WHERE quote_id = $1 ORDER BY version;

-- ===== Order ===============================================================

-- name: CreateOrder :one
INSERT INTO orders (
  organization_id, website_id, customer_id, customer_user_id, quote_id,
  placed_by_sales_rep_id, currency, po_number, requested_delivery_date,
  billing_address, shipping_address, subtotal, tax_total, shipping_total, grand_total
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
RETURNING *;

-- name: AddOrderItem :one
INSERT INTO order_items (order_id, product_id, sku, name, quantity, unit, unit_price, tax_amount, row_total)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING *;

-- name: GetOrderByID :one
SELECT * FROM orders WHERE organization_id = $1 AND id = $2;

-- name: GetOrderByPublicID :one
SELECT * FROM orders WHERE public_id = $1;

-- name: ListOrderItems :many
SELECT * FROM order_items WHERE order_id = $1 ORDER BY id;

-- name: ListOrdersAdmin :many
SELECT * FROM orders WHERE organization_id = $1 ORDER BY created_at DESC LIMIT $2 OFFSET $3;

-- name: ListOrdersForCustomer :many
SELECT * FROM orders WHERE customer_id = $1 ORDER BY created_at DESC;

-- name: ListOrdersForCustomerByStatus :many
SELECT * FROM orders WHERE customer_id = $1 AND status = $2 ORDER BY created_at DESC;

-- name: SetOrderStatus :one
UPDATE orders SET status = $2 WHERE id = $1 RETURNING *;

-- name: AddOrderStatusHistory :exec
INSERT INTO order_status_history (order_id, from_status, to_status, changed_by, note)
VALUES ($1, $2, $3, $4, $5);

-- name: ListOrderStatusHistory :many
SELECT from_status, to_status, changed_by, note, created_at
FROM order_status_history WHERE order_id = $1 ORDER BY created_at, id;

-- ===== Snapshot helpers ====================================================

-- GetCustomerDefaultAddress returns the default (or first) address of a type
-- for snapshotting onto an order.
-- name: GetCustomerDefaultAddress :one
SELECT line1, line2, city, region, postal_code, country
FROM customer_addresses
WHERE customer_id = $1 AND type = $2
ORDER BY is_default DESC, id
LIMIT 1;
