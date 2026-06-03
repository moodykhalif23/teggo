-- Punchout + EDI integration (Pack 3 §3).

-- ===== Product lookup (EDI 850 / punchout mapping) =========================

-- name: GetProductBySKU :one
SELECT * FROM products
WHERE organization_id = $1 AND sku = $2 AND deleted_at IS NULL;

-- ===== Trading partners ====================================================

-- name: CreateTradingPartner :one
INSERT INTO trading_partners (organization_id, customer_id, name, protocol, transport, identity, shared_secret, config, is_active)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING *;

-- name: ListTradingPartners :many
SELECT * FROM trading_partners WHERE organization_id = $1 ORDER BY name;

-- name: GetTradingPartner :one
SELECT * FROM trading_partners WHERE organization_id = $1 AND id = $2;

-- GetTradingPartnerByID resolves a partner without org context (inbound EDI
-- arrives on a partner-scoped endpoint; the org is derived from the partner).
-- name: GetTradingPartnerByID :one
SELECT * FROM trading_partners WHERE id = $1;

-- name: UpdateTradingPartner :one
UPDATE trading_partners
   SET customer_id = $3, name = $4, protocol = $5, transport = $6,
       identity = $7, shared_secret = $8, config = $9, is_active = $10
 WHERE organization_id = $1 AND id = $2
RETURNING *;

-- GetCxmlPartnerByIdentity resolves the punchout partner from the cXML sender
-- identity (used at setup time, before any org context exists).
-- name: GetCxmlPartnerByIdentity :one
SELECT * FROM trading_partners
WHERE identity = $1 AND protocol IN ('cxml', 'oci') AND is_active = true
LIMIT 1;

-- ===== Punchout sessions ===================================================

-- name: CreatePunchoutSession :one
INSERT INTO punchout_sessions (trading_partner_id, customer_id, buyer_cookie, operation, return_url, expires_at)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: GetPunchoutSessionByPublicID :one
SELECT * FROM punchout_sessions WHERE public_id = $1;

-- name: SetPunchoutCart :exec
UPDATE punchout_sessions SET cart_id = $2 WHERE id = $1;

-- name: SetPunchoutStatus :exec
UPDATE punchout_sessions SET status = $2 WHERE id = $1;

-- ===== EDI documents =======================================================

-- name: CreateEDIDocument :one
INSERT INTO edi_documents (organization_id, trading_partner_id, direction, doc_type, status, control_number, raw_payload)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: SetEDIResult :one
UPDATE edi_documents
   SET status = $2, parsed = $3, related_entity_type = $4, related_entity_id = $5,
       error = $6, processed_at = now()
 WHERE id = $1
RETURNING *;

-- name: GetEDIDocument :one
SELECT * FROM edi_documents WHERE organization_id = $1 AND id = $2;

-- name: ListEDIDocuments :many
SELECT id, organization_id, trading_partner_id, direction, doc_type, status,
       control_number, related_entity_type, related_entity_id, error, created_at, processed_at
FROM edi_documents
WHERE organization_id = $1
ORDER BY created_at DESC
LIMIT 200;
