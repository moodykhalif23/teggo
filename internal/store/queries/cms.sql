-- CMS queries — Pack 2 §2.

-- ===== Pages ===============================================================

-- name: CreatePage :one
INSERT INTO content_pages (website_id, locale, slug, title, status, blocks, seo, target_customer_group_id)
VALUES ($1, $2, $3, $4, 'draft', $5, $6, $7)
RETURNING *;

-- GetPageAdmin fetches any page (any status) by id, org-scoped via its website.
-- name: GetPageAdmin :one
SELECT p.* FROM content_pages p
JOIN websites w ON w.id = p.website_id
WHERE p.id = $1 AND w.organization_id = $2;

-- ListPagesAdmin lists all pages for the org's websites.
-- name: ListPagesAdmin :many
SELECT p.* FROM content_pages p
JOIN websites w ON w.id = p.website_id
WHERE w.organization_id = $1
ORDER BY p.updated_at DESC;

-- GetPublishedPage resolves a published page by website + locale + slug (the
-- storefront read path).
-- name: GetPublishedPage :one
SELECT * FROM content_pages
WHERE website_id = $1 AND locale = $2 AND slug = $3 AND status = 'published';

-- name: UpdatePage :one
UPDATE content_pages
SET title = $3, slug = $4, locale = $5, blocks = $6, seo = $7, target_customer_group_id = $8
WHERE id = $1 AND website_id = $2
RETURNING *;

-- name: SetPageStatus :one
UPDATE content_pages
SET status = $2, published_at = CASE WHEN $2 = 'published' THEN now() ELSE published_at END
WHERE id = $1
RETURNING *;

-- ===== Menus ===============================================================

-- name: CreateMenu :one
INSERT INTO menus (website_id, code, name) VALUES ($1, $2, $3)
ON CONFLICT (website_id, code) DO UPDATE SET name = EXCLUDED.name
RETURNING *;

-- name: GetMenuByCode :one
SELECT * FROM menus WHERE website_id = $1 AND code = $2;

-- name: ListMenusForWebsite :many
SELECT * FROM menus WHERE website_id = $1 ORDER BY code;

-- name: AddMenuItem :one
INSERT INTO menu_items (menu_id, parent_id, label, url, category_id, page_id, sort_order)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: ListMenuItems :many
SELECT * FROM menu_items WHERE menu_id = $1 ORDER BY sort_order, id;

-- ===== Media ===============================================================

-- name: CreateMediaAsset :one
INSERT INTO media_assets (organization_id, url, mime_type, width, height, alt, folder)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: ListMediaAssets :many
SELECT * FROM media_assets WHERE organization_id = $1 ORDER BY created_at DESC LIMIT $2 OFFSET $3;

-- ===== Redirects ===========================================================

-- name: GetRedirect :one
SELECT * FROM redirects WHERE website_id = $1 AND from_path = $2;

-- name: CreateRedirect :one
INSERT INTO redirects (website_id, from_path, to_path, status_code)
VALUES ($1, $2, $3, $4)
ON CONFLICT (website_id, from_path) DO UPDATE SET to_path = EXCLUDED.to_path, status_code = EXCLUDED.status_code
RETURNING *;

-- name: ListRedirects :many
SELECT * FROM redirects WHERE website_id = $1 ORDER BY from_path;
