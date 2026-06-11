-- Promotions & coupons (Roadmap Tier 1 #1). CRUD for the admin surface plus the
-- active-list read the cart/checkout evaluate against, and redemption tracking.

-- name: CreatePromotion :one
INSERT INTO promotions (
  organization_id, name, description, code, discount_type, discount_value,
  min_subtotal, starts_at, ends_at, max_redemptions, priority, is_active
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
RETURNING *;

-- name: UpdatePromotion :one
UPDATE promotions
SET name            = $3,
    description     = $4,
    code            = $5,
    discount_type   = $6,
    discount_value  = $7,
    min_subtotal    = $8,
    starts_at       = $9,
    ends_at         = $10,
    max_redemptions = $11,
    priority        = $12,
    is_active       = $13
WHERE organization_id = $1 AND id = $2
RETURNING *;

-- name: GetPromotion :one
SELECT * FROM promotions WHERE organization_id = $1 AND id = $2;

-- name: ListPromotions :many
SELECT * FROM promotions
WHERE organization_id = $1
ORDER BY is_active DESC, priority DESC, created_at DESC;

-- name: DeletePromotion :execrows
DELETE FROM promotions WHERE organization_id = $1 AND id = $2;

-- ListActivePromotions feeds the engine; schedule/code/cap filtering is applied
-- in Go (internal/promotions). Ordered by priority so ties resolve deterministically.
-- name: ListActivePromotions :many
SELECT * FROM promotions
WHERE organization_id = $1 AND is_active
ORDER BY priority DESC, id;

-- name: IncrementPromotionRedeemed :exec
UPDATE promotions SET times_redeemed = times_redeemed + 1 WHERE id = $1;

-- name: CreatePromotionRedemption :one
INSERT INTO promotion_redemptions (promotion_id, order_id, customer_id, amount)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: SetCartCoupon :exec
UPDATE carts SET coupon_code = $2, updated_at = now() WHERE id = $1;
