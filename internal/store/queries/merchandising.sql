-- Search merchandising (Roadmap Tier 2 #6).

-- ===== Synonyms ============================================================

-- name: ListSearchSynonyms :many
SELECT * FROM search_synonyms WHERE organization_id = $1 ORDER BY term;

-- name: UpsertSearchSynonym :one
INSERT INTO search_synonyms (organization_id, term, synonyms)
VALUES ($1, $2, $3)
ON CONFLICT (organization_id, term) DO UPDATE SET synonyms = EXCLUDED.synonyms
RETURNING *;

-- name: DeleteSearchSynonym :execrows
DELETE FROM search_synonyms WHERE organization_id = $1 AND id = $2;

-- ===== Redirects ===========================================================

-- name: ListSearchRedirects :many
SELECT * FROM search_redirects WHERE organization_id = $1 ORDER BY query;

-- name: GetSearchRedirect :one
SELECT target FROM search_redirects WHERE organization_id = $1 AND lower(query) = lower($2);

-- name: UpsertSearchRedirect :one
INSERT INTO search_redirects (organization_id, query, target)
VALUES ($1, $2, $3)
ON CONFLICT (organization_id, query) DO UPDATE SET target = EXCLUDED.target
RETURNING *;

-- name: DeleteSearchRedirect :execrows
DELETE FROM search_redirects WHERE organization_id = $1 AND id = $2;

-- ===== Merchandising rules =================================================

-- name: ListMerchandisingRules :many
SELECT mr.*, p.sku, p.name
FROM merchandising_rules mr
JOIN products p ON p.id = mr.product_id
WHERE mr.organization_id = $1
ORDER BY mr.scope_type, mr.scope_value, mr.action, mr.position, mr.id;

-- ListMerchandisingRulesForScope feeds the storefront search reorder.
-- name: ListMerchandisingRulesForScope :many
SELECT product_id, action, position
FROM merchandising_rules
WHERE organization_id = $1 AND scope_type = $2 AND lower(scope_value) = lower($3)
ORDER BY position, id;

-- name: CreateMerchandisingRule :one
INSERT INTO merchandising_rules (organization_id, scope_type, scope_value, product_id, action, position)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: DeleteMerchandisingRule :execrows
DELETE FROM merchandising_rules WHERE organization_id = $1 AND id = $2;
