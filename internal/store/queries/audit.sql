-- Audit trail: append-only writes + a filterable, paginated viewer. The list /
-- count / export queries share one optional-filter shape (every filter is a
-- nullable arg; a NULL arg means "don't filter on this").

-- name: CreateAuditLog :one
INSERT INTO audit_log (
  organization_id, actor_user_id, actor_audience, action, entity_type, entity_id,
  method, path, status_code, ip, user_agent, request_id, summary, metadata
) VALUES (
  $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14
)
RETURNING *;

-- name: GetAuditLog :one
SELECT * FROM audit_log
WHERE id = $1 AND organization_id = $2;

-- name: ListAuditLog :many
SELECT * FROM audit_log
WHERE organization_id = sqlc.arg('organization_id')
  AND (sqlc.narg('actor')::bigint       IS NULL OR actor_user_id = sqlc.narg('actor'))
  AND (sqlc.narg('audience')::text      IS NULL OR actor_audience = sqlc.narg('audience'))
  AND (sqlc.narg('action')::text        IS NULL OR action = sqlc.narg('action'))
  AND (sqlc.narg('entity_type')::text   IS NULL OR entity_type = sqlc.narg('entity_type'))
  AND (sqlc.narg('entity_id')::bigint   IS NULL OR entity_id = sqlc.narg('entity_id'))
  AND (sqlc.narg('from_ts')::timestamptz IS NULL OR created_at >= sqlc.narg('from_ts'))
  AND (sqlc.narg('to_ts')::timestamptz   IS NULL OR created_at <  sqlc.narg('to_ts'))
ORDER BY created_at DESC, id DESC
LIMIT sqlc.arg('lim') OFFSET sqlc.arg('off');

-- name: CountAuditLog :one
SELECT count(*)::bigint AS total FROM audit_log
WHERE organization_id = sqlc.arg('organization_id')
  AND (sqlc.narg('actor')::bigint       IS NULL OR actor_user_id = sqlc.narg('actor'))
  AND (sqlc.narg('audience')::text      IS NULL OR actor_audience = sqlc.narg('audience'))
  AND (sqlc.narg('action')::text        IS NULL OR action = sqlc.narg('action'))
  AND (sqlc.narg('entity_type')::text   IS NULL OR entity_type = sqlc.narg('entity_type'))
  AND (sqlc.narg('entity_id')::bigint   IS NULL OR entity_id = sqlc.narg('entity_id'))
  AND (sqlc.narg('from_ts')::timestamptz IS NULL OR created_at >= sqlc.narg('from_ts'))
  AND (sqlc.narg('to_ts')::timestamptz   IS NULL OR created_at <  sqlc.narg('to_ts'));

-- ExportAuditLog is the list query without pagination, capped, for CSV export.
-- name: ExportAuditLog :many
SELECT * FROM audit_log
WHERE organization_id = sqlc.arg('organization_id')
  AND (sqlc.narg('actor')::bigint       IS NULL OR actor_user_id = sqlc.narg('actor'))
  AND (sqlc.narg('audience')::text      IS NULL OR actor_audience = sqlc.narg('audience'))
  AND (sqlc.narg('action')::text        IS NULL OR action = sqlc.narg('action'))
  AND (sqlc.narg('entity_type')::text   IS NULL OR entity_type = sqlc.narg('entity_type'))
  AND (sqlc.narg('entity_id')::bigint   IS NULL OR entity_id = sqlc.narg('entity_id'))
  AND (sqlc.narg('from_ts')::timestamptz IS NULL OR created_at >= sqlc.narg('from_ts'))
  AND (sqlc.narg('to_ts')::timestamptz   IS NULL OR created_at <  sqlc.narg('to_ts'))
ORDER BY created_at DESC, id DESC
LIMIT sqlc.arg('lim');
