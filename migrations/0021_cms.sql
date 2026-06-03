-- CMS — Pack 2 §2. Block-based content pages (JSONB block trees) served to the
-- Nuxt storefront, plus navigation menus, media assets, and redirects. New block
-- types are additive (no migration) — block payloads validate app-side.

CREATE TABLE content_pages (
  id              BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  public_id       UUID NOT NULL DEFAULT gen_random_uuid() UNIQUE,
  website_id      BIGINT NOT NULL REFERENCES websites(id),
  locale          text NOT NULL DEFAULT 'en',
  slug            text NOT NULL,
  title           text NOT NULL,
  status          text NOT NULL DEFAULT 'draft'
                    CHECK (status IN ('draft','published','archived')),
  blocks          JSONB NOT NULL DEFAULT '[]'::jsonb,
  seo             JSONB NOT NULL DEFAULT '{}'::jsonb,
  target_customer_group_id BIGINT REFERENCES customer_groups(id),  -- null = all
  published_at    timestamptz,
  created_at      timestamptz NOT NULL DEFAULT now(),
  updated_at      timestamptz NOT NULL DEFAULT now(),
  UNIQUE (website_id, locale, slug)
);
CREATE INDEX idx_content_pages_status ON content_pages(website_id, status);
CREATE TRIGGER trg_content_pages_updated BEFORE UPDATE ON content_pages
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE menus (
  id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  website_id  BIGINT NOT NULL REFERENCES websites(id),
  code        text NOT NULL,
  name        text NOT NULL,
  UNIQUE (website_id, code)
);

CREATE TABLE menu_items (
  id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  menu_id     BIGINT NOT NULL REFERENCES menus(id) ON DELETE CASCADE,
  parent_id   BIGINT REFERENCES menu_items(id),
  label       text NOT NULL,
  url         text,
  category_id BIGINT REFERENCES categories(id),
  page_id     BIGINT REFERENCES content_pages(id),
  sort_order  int NOT NULL DEFAULT 0
);
CREATE INDEX idx_menu_items_menu ON menu_items(menu_id);

CREATE TABLE media_assets (
  id              BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  organization_id BIGINT NOT NULL REFERENCES organizations(id),
  url             text NOT NULL,
  mime_type       text,
  width           int,
  height          int,
  alt             text,
  folder          text DEFAULT '/',
  created_at      timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE redirects (
  id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  website_id  BIGINT NOT NULL REFERENCES websites(id),
  from_path   text NOT NULL,
  to_path     text NOT NULL,
  status_code int NOT NULL DEFAULT 301 CHECK (status_code IN (301, 302)),
  UNIQUE (website_id, from_path)
);

-- CMS permissions for the demo admin role.
INSERT INTO role_permissions (role_id, permission)
SELECT r.id, p.permission
  FROM roles r
  CROSS JOIN (VALUES ('cms.view'), ('cms.manage')) AS p(permission)
 WHERE r.organization_id = 1 AND r.name = 'admin'
ON CONFLICT DO NOTHING;

-- Seed a published landing page + a main menu for the demo website (id 1).
INSERT INTO content_pages (website_id, locale, slug, title, status, blocks, seo, published_at)
VALUES (
  1, 'en', 'about', 'About Teggo', 'published',
  '[{"type":"hero","id":"b1","props":{"heading":"Welcome to Teggo","subheading":"Your B2B supply partner"}},
    {"type":"rich-text","id":"b2","props":{"html":"<p>Teggo supplies industrial products to businesses across the region.</p>"}}]'::jsonb,
  '{"title":"About Teggo","description":"Learn about the Teggo B2B platform."}'::jsonb,
  now()
);

INSERT INTO menus (website_id, code, name) VALUES (1, 'main', 'Main navigation');
INSERT INTO menu_items (menu_id, label, url, sort_order)
SELECT m.id, v.label, v.url, v.sort_order
  FROM menus m
  CROSS JOIN (VALUES ('Catalog', '/c/all', 1), ('About', '/page/about', 2)) AS v(label, url, sort_order)
 WHERE m.website_id = 1 AND m.code = 'main';
