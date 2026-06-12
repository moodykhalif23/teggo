-- Subscriptions / recurring orders (Roadmap Tier 2 #4).

-- name: CreateSubscription :one
INSERT INTO subscriptions (
  organization_id, website_id, customer_id, customer_user_id, name, currency,
  cadence, next_run_date, po_number, created_by
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
RETURNING *;

-- name: CreateSubscriptionItem :one
INSERT INTO subscription_items (subscription_id, product_id, quantity, unit)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: GetSubscription :one
SELECT * FROM subscriptions WHERE organization_id = $1 AND id = $2;

-- name: GetSubscriptionForCustomer :one
SELECT * FROM subscriptions WHERE customer_id = $1 AND id = $2;

-- name: ListSubscriptionsAdmin :many
SELECT * FROM subscriptions WHERE organization_id = $1
ORDER BY (status = 'active') DESC, next_run_date, id;

-- name: ListSubscriptionsForCustomer :many
SELECT * FROM subscriptions WHERE customer_id = $1
ORDER BY (status = 'active') DESC, next_run_date, id;

-- name: ListSubscriptionItems :many
SELECT si.id, si.subscription_id, si.product_id, si.quantity, si.unit, p.sku, p.name
FROM subscription_items si
JOIN products p ON p.id = si.product_id
WHERE si.subscription_id = $1
ORDER BY si.id;

-- name: ListSubscriptionRuns :many
SELECT * FROM subscription_runs WHERE subscription_id = $1
ORDER BY created_at DESC, id DESC
LIMIT 50;

-- ListDueSubscriptions feeds the daily materialization job (all orgs).
-- name: ListDueSubscriptions :many
SELECT * FROM subscriptions
WHERE status = 'active' AND next_run_date <= $1
ORDER BY id;

-- name: SetSubscriptionStatus :one
UPDATE subscriptions SET status = $3 WHERE organization_id = $1 AND id = $2
RETURNING *;

-- name: SetSubscriptionStatusForCustomer :one
UPDATE subscriptions SET status = $3 WHERE customer_id = $1 AND id = $2
RETURNING *;

-- SetSubscriptionNextRun moves the next run date (used by "skip" and the job's advance).
-- name: SetSubscriptionNextRun :exec
UPDATE subscriptions SET next_run_date = $2 WHERE id = $1;

-- AdvanceSubscription records a completed run: new next date + last_run_at stamp.
-- name: AdvanceSubscription :exec
UPDATE subscriptions SET next_run_date = $2, last_run_at = $3 WHERE id = $1;

-- name: CreateSubscriptionRun :one
INSERT INTO subscription_runs (subscription_id, order_id, run_date, status, note)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: UpdateSubscription :one
UPDATE subscriptions SET name = $3, cadence = $4, po_number = $5
WHERE organization_id = $1 AND id = $2
RETURNING *;

-- name: UpdateSubscriptionForCustomer :one
UPDATE subscriptions SET name = $3, cadence = $4, po_number = $5
WHERE customer_id = $1 AND id = $2
RETURNING *;

-- name: DeleteSubscriptionItems :exec
DELETE FROM subscription_items WHERE subscription_id = $1;

-- GetCustomerUserEmailByID resolves the buyer email for subscription notifications.
-- name: GetCustomerUserEmailByID :one
SELECT email FROM customer_users WHERE id = $1;
