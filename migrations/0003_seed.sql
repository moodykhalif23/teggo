-- 0003_seed.sql — development seed data so the app is usable on first boot.
-- The admin password below is bcrypt('admin1234'). Change/remove for production.

INSERT INTO organizations (name) VALUES ('Demo Org');

INSERT INTO websites (organization_id, name, domain, default_currency, default_locale)
VALUES (1, 'Demo Store', 'demo.localhost', 'USD', 'en');

INSERT INTO users (organization_id, email, password_hash, full_name)
VALUES (1, 'admin@demo.test',
        '$2a$10$KJNNgCtq2yM0p08JTkuouefWrFkbgqASEEjSR5/DkDyn/5QK2R3wq',  -- bcrypt('admin1234')
        'Demo Admin');

INSERT INTO roles (organization_id, name, description) VALUES (1, 'admin', 'Full access');
INSERT INTO role_permissions (role_id, permission) VALUES
  (1, 'product.view'), (1, 'product.edit'),
  (1, 'order.view'), (1, 'order.edit'),
  (1, 'price_list.manage');
INSERT INTO user_roles (user_id, role_id) VALUES (1, 1);

INSERT INTO products (organization_id, sku, type, name, slug, description, status, attributes)
VALUES
  (1, 'VALVE-100', 'simple', 'Brass Ball Valve 1"', 'brass-ball-valve-1in',
   'Industrial brass ball valve.', 'active', '{"material":"brass","size":"1in"}'),
  (1, 'PIPE-200', 'simple', 'Steel Pipe 2m', 'steel-pipe-2m',
   'Galvanised steel pipe, 2 metre.', 'active', '{"material":"steel","length":"2m"}'),
  (1, 'FITTING-300', 'simple', 'Elbow Fitting 90deg', 'elbow-fitting-90',
   '90 degree elbow fitting.', 'draft', '{"angle":"90"}');
