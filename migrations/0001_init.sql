-- 0001_init.sql — extensions, shared trigger, foundation (orgs, websites, users, RBAC)
-- Consistent with Implementation Pack 1 §0 and §1.

CREATE EXTENSION IF NOT EXISTS citext;
CREATE EXTENSION IF NOT EXISTS pgcrypto;  -- gen_random_uuid() (also built-in on PG16)

CREATE OR REPLACE FUNCTION set_updated_at() RETURNS trigger AS $$
BEGIN
  NEW.updated_at = now();
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TABLE organizations (
  id            BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  name          text NOT NULL,
  is_active     boolean NOT NULL DEFAULT true,
  created_at    timestamptz NOT NULL DEFAULT now(),
  updated_at    timestamptz NOT NULL DEFAULT now()
);
CREATE TRIGGER trg_orgs_updated BEFORE UPDATE ON organizations
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE websites (
  id                BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  organization_id   BIGINT NOT NULL REFERENCES organizations(id),
  name              text NOT NULL,
  domain            text NOT NULL UNIQUE,
  default_currency  CHAR(3) NOT NULL,
  default_locale    text NOT NULL DEFAULT 'en',
  is_active         boolean NOT NULL DEFAULT true,
  created_at        timestamptz NOT NULL DEFAULT now(),
  updated_at        timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX idx_websites_org ON websites(organization_id);
CREATE TRIGGER trg_websites_updated BEFORE UPDATE ON websites
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE users (
  id              BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  organization_id BIGINT NOT NULL REFERENCES organizations(id),
  email           citext NOT NULL,
  password_hash   text NOT NULL,
  full_name       text NOT NULL,
  is_active       boolean NOT NULL DEFAULT true,
  last_login_at   timestamptz,
  created_at      timestamptz NOT NULL DEFAULT now(),
  updated_at      timestamptz NOT NULL DEFAULT now(),
  UNIQUE (organization_id, email)
);
CREATE TRIGGER trg_users_updated BEFORE UPDATE ON users
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE roles (
  id              BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  organization_id BIGINT NOT NULL REFERENCES organizations(id),
  name            text NOT NULL,
  description     text,
  UNIQUE (organization_id, name)
);

CREATE TABLE role_permissions (
  role_id    BIGINT NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
  permission text   NOT NULL,
  PRIMARY KEY (role_id, permission)
);

CREATE TABLE user_roles (
  user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  role_id BIGINT NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
  PRIMARY KEY (user_id, role_id)
);
