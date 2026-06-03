-- Automation engine queries — Pack 2 §3.

-- ListAutomationRulesByEvent returns active rules for a trigger event, across
-- orgs (the scheduler doesn't know orgs; each rule carries its own).
-- name: ListAutomationRulesByEvent :many
SELECT * FROM automation_rules
WHERE trigger_event = $1 AND is_active
ORDER BY id;

-- name: CreateAutomationRule :one
INSERT INTO automation_rules (organization_id, name, trigger_event, conditions, actions, is_active)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: RecordAutomationExecution :exec
INSERT INTO automation_executions (rule_id, event_payload, status, result)
VALUES ($1, $2, $3, $4);

-- name: CountAutomationExecutions :one
SELECT count(*) FROM automation_executions WHERE rule_id = $1;

-- ListExpirableQuotes returns open quotes (sent/revised) whose validity has
-- passed — the quote-expiry sweep's working set.
-- name: ListExpirableQuotes :many
SELECT * FROM quotes
WHERE status IN ('sent', 'revised')
  AND valid_until IS NOT NULL
  AND valid_until < $1
ORDER BY id;
