-- Product content translations (Roadmap Tier 3 #8). Per-locale name/description
-- overrides; the storefront falls back to the base product when none exists.

-- name: ListProductTranslations :many
SELECT product_id, locale, name, description FROM product_translations
WHERE product_id = $1 ORDER BY locale;

-- name: GetProductTranslation :one
SELECT name, description FROM product_translations
WHERE product_id = $1 AND lower(locale) = lower($2);

-- ListProductTranslationsForLocale batch-resolves a page of products for a locale.
-- name: ListProductTranslationsForLocale :many
SELECT product_id, name, description FROM product_translations
WHERE lower(locale) = lower($2) AND product_id = ANY($1::bigint[]);

-- name: UpsertProductTranslation :one
INSERT INTO product_translations (product_id, locale, name, description)
VALUES ($1, $2, $3, $4)
ON CONFLICT (product_id, locale) DO UPDATE SET name = EXCLUDED.name, description = EXCLUDED.description
RETURNING product_id, locale, name, description;

-- name: DeleteProductTranslation :execrows
DELETE FROM product_translations WHERE product_id = $1 AND lower(locale) = lower($2);

-- DistinctTranslationLocales lists configured locales across an org's products
-- (for the storefront locale selector).
-- name: DistinctTranslationLocales :many
SELECT DISTINCT pt.locale
FROM product_translations pt
JOIN products p ON p.id = pt.product_id
WHERE p.organization_id = $1
ORDER BY pt.locale;
