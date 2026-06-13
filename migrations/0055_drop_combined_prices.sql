-- 0055_drop_combined_prices.sql — retire the per-customer price cache.
-- Pricing now resolves live from price_lists + prices + assignments on every
-- read (ResolvePriceTier / ResolvePriceTiersForSlug / ListResolvedPricesFor
-- Customer), riding idx_prices_product + idx_pla_* — sub-millisecond per lookup.
-- The materialized cache was O(customers × products) rows (~84M at 1,400
-- dealers × 30k parts in the Toyoka simulation) and needed a multi-hour
-- recompute storm on any price change. Read-time resolution removes the cache,
-- the recompute job, and the price-change fan-out entirely; a price edit is now
-- live immediately. See TOYOKA_SIMULATION.md §6.

DROP TABLE IF EXISTS combined_prices;
