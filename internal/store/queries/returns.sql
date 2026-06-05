-- Returns / RMA + credit notes (migration 0038).

-- name: CreateReturn :one
INSERT INTO returns (order_id, customer_id, reason)
VALUES ($1, $2, $3)
RETURNING *;

-- name: AddReturnItem :one
INSERT INTO return_items (return_id, order_item_id, product_id, quantity, reason)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- GetReturn loads a return scoped to its org (via the order).
-- name: GetReturn :one
SELECT r.* FROM returns r
JOIN orders o ON o.id = r.order_id
WHERE r.id = $1 AND o.organization_id = $2;

-- GetReturnByPublicID loads a return by public id within the customer (storefront).
-- name: GetReturnByPublicID :one
SELECT * FROM returns WHERE public_id = $1;

-- name: ListReturnItems :many
SELECT ri.id, ri.order_item_id, ri.product_id, ri.quantity, ri.reason,
       oi.sku, oi.name, oi.unit_price
FROM return_items ri
JOIN order_items oi ON oi.id = ri.order_item_id
WHERE ri.return_id = $1
ORDER BY ri.id;

-- name: ListReturnsForOrder :many
SELECT * FROM returns WHERE order_id = $1 ORDER BY created_at DESC;

-- name: ListReturnsAdmin :many
SELECT r.* FROM returns r
JOIN orders o ON o.id = r.order_id
WHERE o.organization_id = $1
ORDER BY r.created_at DESC
LIMIT $2 OFFSET $3;

-- name: ListReturnsForCustomer :many
SELECT * FROM returns WHERE customer_id = $1 ORDER BY created_at DESC;

-- name: SetReturnStatus :one
UPDATE returns SET status = $2 WHERE id = $1 RETURNING *;

-- SumReturnedForOrderItem totals quantity already returned for an order line
-- across non-rejected returns (the returnable cap).
-- name: SumReturnedForOrderItem :one
SELECT COALESCE(SUM(ri.quantity), 0)::numeric(15,4)
FROM return_items ri
JOIN returns r ON r.id = ri.return_id
WHERE ri.order_item_id = $1 AND r.status <> 'rejected';

-- name: CreateCreditNote :one
INSERT INTO credit_notes (return_id, invoice_id, customer_id, amount, currency)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: ListCreditNotesForReturn :many
SELECT * FROM credit_notes WHERE return_id = $1 ORDER BY id;

-- name: ListCreditNotesAdmin :many
SELECT cn.* FROM credit_notes cn
JOIN customers c ON c.id = cn.customer_id
WHERE c.organization_id = $1
ORDER BY cn.created_at DESC
LIMIT $2 OFFSET $3;
