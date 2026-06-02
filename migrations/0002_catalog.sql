-- 0002_catalog.sql — minimal products (subset of Implementation Pack 1 §3) so the
-- example catalog endpoint has something to read. Extend with the full PIM schema later.

CREATE TABLE products (
  id              BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  public_id       UUID NOT NULL DEFAULT gen_random_uuid() UNIQUE,
  organization_id BIGINT NOT NULL REFERENCES organizations(id),
  sku             text NOT NULL,
  type            text NOT NULL DEFAULT 'simple'
                    CHECK (type IN ('simple','configurable','kit','digital')),
  name            text NOT NULL,
  slug            text NOT NULL,
  description     text,
  status          text NOT NULL DEFAULT 'draft'
                    CHECK (status IN ('draft','active','disabled')),
  attributes      JSONB NOT NULL DEFAULT '{}'::jsonb,
  unit            text NOT NULL DEFAULT 'each',
  created_at      timestamptz NOT NULL DEFAULT now(),
  updated_at      timestamptz NOT NULL DEFAULT now(),
  deleted_at      timestamptz,
  UNIQUE (organization_id, sku)
);
CREATE INDEX idx_products_org ON products(organization_id);
CREATE INDEX idx_products_attrs_gin ON products USING GIN (attributes);
CREATE UNIQUE INDEX uq_products_slug ON products(organization_id, slug) WHERE deleted_at IS NULL;
CREATE TRIGGER trg_products_updated BEFORE UPDATE ON products
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();
