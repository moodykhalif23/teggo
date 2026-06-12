-- Platform billing & metering queries (SAAS.md #2).

-- name: ListPlans :many
SELECT * FROM plans WHERE is_active ORDER BY position, id;

-- name: GetPlanByCode :one
SELECT * FROM plans WHERE code = $1;

-- name: UpdatePlan :one
UPDATE plans SET name = $2, price = $3, currency = $4, features = $5, limits = $6
WHERE code = $1
RETURNING *;

-- GetOrgEntitlements joins the org's plan subscription onto its plan.
-- name: GetOrgEntitlements :one
SELECT s.status, s.current_period_start, p.code, p.name, p.price, p.currency, p.features, p.limits
FROM org_subscriptions s
JOIN plans p ON p.id = s.plan_id
WHERE s.organization_id = $1;

-- name: SetOrgPlan :one
INSERT INTO org_subscriptions (organization_id, plan_id, status, current_period_start)
VALUES ($1, $2, 'active', now())
ON CONFLICT (organization_id)
DO UPDATE SET plan_id = EXCLUDED.plan_id, status = 'active',
              current_period_start = now(), updated_at = now()
RETURNING *;

-- name: IncrementUsage :one
INSERT INTO usage_counters (organization_id, metric, period_key, value)
VALUES ($1, $2, $3, $4)
ON CONFLICT (organization_id, metric, period_key)
DO UPDATE SET value = usage_counters.value + EXCLUDED.value, updated_at = now()
RETURNING value;

-- name: GetUsageValue :one
SELECT value FROM usage_counters
WHERE organization_id = $1 AND metric = $2 AND period_key = $3;

-- ListUsageForPeriods returns an org's counters for the metering screen
-- (the current month plus lifetime gauges).
-- name: ListUsageForPeriods :many
SELECT metric, period_key, value FROM usage_counters
WHERE organization_id = $1 AND period_key = ANY(sqlc.arg(period_keys)::text[])
ORDER BY metric;
