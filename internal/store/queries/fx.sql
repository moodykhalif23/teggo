-- FX rates (Roadmap Tier 2 #5).

-- GetLatestFxRate returns the most recent base→quote rate for an org.
-- name: GetLatestFxRate :one
SELECT rate FROM fx_rates
WHERE organization_id = $1 AND base_currency = $2 AND quote_currency = $3
ORDER BY as_of DESC, id DESC
LIMIT 1;

-- name: CreateFxRate :one
INSERT INTO fx_rates (organization_id, base_currency, quote_currency, rate)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- ListLatestFxRates returns the current rate for each configured pair.
-- name: ListLatestFxRates :many
SELECT DISTINCT ON (base_currency, quote_currency)
  id, organization_id, base_currency, quote_currency, rate, as_of, created_at
FROM fx_rates
WHERE organization_id = $1
ORDER BY base_currency, quote_currency, as_of DESC, id DESC;

-- name: DeleteFxRate :execrows
DELETE FROM fx_rates WHERE organization_id = $1 AND id = $2;

-- ListQuoteCurrencies returns the display currencies available from a base
-- (for the storefront currency selector).
-- name: ListQuoteCurrencies :many
SELECT DISTINCT quote_currency FROM fx_rates
WHERE organization_id = $1 AND base_currency = $2
ORDER BY quote_currency;

-- SetOrderFxSnapshot locks the buyer's display currency + rate onto an order.
-- name: SetOrderFxSnapshot :exec
UPDATE orders SET display_currency = $2, fx_rate = $3, display_grand_total = $4 WHERE id = $1;
