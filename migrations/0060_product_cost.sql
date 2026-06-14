-- 0060_product_cost.sql — add a product cost price so the insights engine can
-- compute gross margin / profitability. Single cost in the product's own terms
-- (per-currency and effective-dated costing, and per-line cost snapshots on
-- order_items for exact historical margin, are future refinements — v1 margin is
-- "at current cost"). Defaults to 0, so existing rows and inserts are unaffected;
-- margin simply reads as 100% until a real cost is set.
ALTER TABLE products ADD COLUMN cost_price NUMERIC(15,4) NOT NULL DEFAULT 0;
