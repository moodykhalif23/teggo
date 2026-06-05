-- Cart & shopping list queries — Implementation Pack 1 §5.

-- ===== Carts ===============================================================

-- name: GetActiveCart :one
SELECT * FROM carts
WHERE customer_id = $1 AND website_id = $2 AND status = 'active';

-- name: CreateCart :one
INSERT INTO carts (customer_id, customer_user_id, website_id, currency, status)
VALUES ($1, $2, $3, $4, 'active')
RETURNING *;

-- name: GetCartByID :one
SELECT * FROM carts WHERE id = $1;

-- name: MarkCartConverted :exec
UPDATE carts SET status = 'converted' WHERE id = $1;

-- ===== Cart items ==========================================================

-- UpsertCartItem snapshots unit_price at add-time; re-adding the same product
-- sets the quantity and refreshes the price.
-- name: UpsertCartItem :one
INSERT INTO cart_items (cart_id, product_id, quantity, unit, unit_price)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (cart_id, product_id, unit)
DO UPDATE SET quantity = EXCLUDED.quantity, unit_price = EXCLUDED.unit_price
RETURNING *;

-- name: ListCartItems :many
SELECT ci.id, ci.product_id, p.sku, p.name, ci.quantity, ci.unit, ci.unit_price
FROM cart_items ci
JOIN products p ON p.id = ci.product_id
WHERE ci.cart_id = $1
ORDER BY p.name;

-- name: GetCartItem :one
SELECT * FROM cart_items WHERE id = $1 AND cart_id = $2;

-- name: UpdateCartItemQuantity :one
UPDATE cart_items SET quantity = $3
WHERE id = $1 AND cart_id = $2
RETURNING *;

-- name: UpdateCartItemPrice :exec
UPDATE cart_items SET unit_price = $3 WHERE id = $1 AND cart_id = $2;

-- name: DeleteCartItem :execrows
DELETE FROM cart_items WHERE id = $1 AND cart_id = $2;

-- ===== Shopping lists ======================================================

-- name: CreateShoppingList :one
INSERT INTO shopping_lists (customer_id, customer_user_id, name, is_default)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: ListShoppingLists :many
SELECT * FROM shopping_lists WHERE customer_id = $1 ORDER BY is_default DESC, name;

-- name: GetShoppingList :one
SELECT * FROM shopping_lists WHERE id = $1 AND customer_id = $2;

-- name: UpsertShoppingListItem :one
INSERT INTO shopping_list_items (shopping_list_id, product_id, quantity, unit)
VALUES ($1, $2, $3, $4)
ON CONFLICT (shopping_list_id, product_id, unit)
DO UPDATE SET quantity = EXCLUDED.quantity
RETURNING *;

-- name: ListShoppingListItems :many
SELECT sli.id, sli.product_id, p.sku, p.name, sli.quantity, sli.unit
FROM shopping_list_items sli
JOIN products p ON p.id = sli.product_id
WHERE sli.shopping_list_id = $1
ORDER BY p.name;

-- name: UpdateShoppingListItem :one
UPDATE shopping_list_items SET quantity = $3
WHERE id = $1 AND shopping_list_id = $2
RETURNING *;

-- name: DeleteShoppingListItem :execrows
DELETE FROM shopping_list_items WHERE id = $1 AND shopping_list_id = $2;

-- name: RenameShoppingList :one
UPDATE shopping_lists SET name = $3, updated_at = now()
WHERE id = $1 AND customer_id = $2
RETURNING *;

-- name: DeleteShoppingList :execrows
DELETE FROM shopping_lists WHERE id = $1 AND customer_id = $2;
