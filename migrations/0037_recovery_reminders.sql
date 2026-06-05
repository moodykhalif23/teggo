-- Revenue-recovery reminders (pain point #3): track when we last nudged a buyer
-- about an expiring quote or an abandoned cart, so the scheduled sweeps remind
-- once per episode instead of every run. Nullable — no behaviour change until a
-- quote_followup / cart_recovery automation rule is configured.
ALTER TABLE quotes ADD COLUMN followup_at timestamptz;
ALTER TABLE carts ADD COLUMN reminded_at timestamptz;
