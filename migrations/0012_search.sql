-- Full-text product search — PRD §14 (Postgres FTS, tsvector/GIN).
-- search_vector is a STORED generated column weighted name/sku (A) > description
-- (B). The 2-arg to_tsvector(regconfig, text) form is IMMUTABLE (required for a
-- generated column); the unqualified 1-arg form is only STABLE and would fail.
ALTER TABLE products
  ADD COLUMN search_vector tsvector GENERATED ALWAYS AS (
    setweight(to_tsvector('english', coalesce(name, '')), 'A') ||
    setweight(to_tsvector('english', coalesce(sku, '')), 'A') ||
    setweight(to_tsvector('english', coalesce(description, '')), 'B')
  ) STORED;

CREATE INDEX idx_products_search_gin ON products USING GIN (search_vector);
