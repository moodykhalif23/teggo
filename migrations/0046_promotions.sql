-- Promotions & coupons (Roadmap Tier 1 #1). Cart-level discounts: a percent or
-- fixed amount, with an optional coupon code, optional minimum subtotal to
-- qualify, a schedule window, and a global redemption cap. A NULL code makes the
-- promotion automatic (applies whenever it qualifies); a non-NULL code must be
-- entered on the cart. v1 applies the single best-value promotion (not stacked).
CREATE TABLE promotions (
  id               BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  public_id        UUID NOT NULL DEFAULT gen_random_uuid() UNIQUE,
  organization_id  BIGINT NOT NULL REFERENCES organizations(id),
  name             text NOT NULL,
  description      text,
  code             text,                       -- coupon code (NULL = automatic)
  discount_type    text NOT NULL CHECK (discount_type IN ('percent','amount')),
  discount_value   NUMERIC(15,4) NOT NULL CHECK (discount_value >= 0),
  min_subtotal     NUMERIC(15,4),              -- threshold to qualify (NULL = none)
  starts_at        timestamptz,
  ends_at          timestamptz,
  max_redemptions  int,                        -- global cap (NULL = unlimited)
  times_redeemed   int NOT NULL DEFAULT 0,
  priority         int NOT NULL DEFAULT 0,     -- higher wins ties
  is_active        boolean NOT NULL DEFAULT true,
  created_at       timestamptz NOT NULL DEFAULT now(),
  updated_at       timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX idx_promotions_org ON promotions(organization_id, is_active);
-- A coupon code is unique per org, case-insensitively, when set.
CREATE UNIQUE INDEX uq_promotions_code ON promotions(organization_id, lower(code)) WHERE code IS NOT NULL;

CREATE TRIGGER trg_promotions_updated BEFORE UPDATE ON promotions
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- One row per time a promotion is applied to a placed order (usage + reporting).
CREATE TABLE promotion_redemptions (
  id            BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  promotion_id  BIGINT NOT NULL REFERENCES promotions(id) ON DELETE CASCADE,
  order_id      BIGINT REFERENCES orders(id) ON DELETE SET NULL,
  customer_id   BIGINT REFERENCES customers(id),
  amount        NUMERIC(15,4) NOT NULL,
  created_at    timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX idx_promotion_redemptions_promo ON promotion_redemptions(promotion_id);

-- The cart carries an applied coupon code; the discount is resolved at render
-- and locked onto the order at checkout.
ALTER TABLE carts ADD COLUMN IF NOT EXISTS coupon_code text;

-- Orders persist the discount applied and which promotion produced it.
ALTER TABLE orders
  ADD COLUMN IF NOT EXISTS discount_total NUMERIC(15,4) NOT NULL DEFAULT 0,
  ADD COLUMN IF NOT EXISTS promotion_id   BIGINT REFERENCES promotions(id),
  ADD COLUMN IF NOT EXISTS promotion_code text;

-- Promotion permissions for the demo admin role.
INSERT INTO role_permissions (role_id, permission)
SELECT r.id, p.permission
  FROM roles r
  CROSS JOIN (VALUES ('promotion.view'), ('promotion.manage')) AS p(permission)
 WHERE r.organization_id = 1 AND r.name = 'admin'
ON CONFLICT DO NOTHING;
