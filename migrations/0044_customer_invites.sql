-- 0044_customer_invites.sql — shareable signup links for buyer onboarding.
-- An admin mints an invite for a customer (company) and shares the link; staff
-- open it on the storefront and self-register as customer_users with the
-- invite's role/spending limit. Links are multi-use (a whole team can join from
-- one link) until they expire or are revoked.

CREATE TABLE customer_invites (
  id              BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  token           UUID NOT NULL DEFAULT gen_random_uuid() UNIQUE,
  customer_id     BIGINT NOT NULL REFERENCES customers(id),
  role            text NOT NULL DEFAULT 'buyer'
                    CHECK (role IN ('buyer','approver','admin')),
  spending_limit  NUMERIC(15,4),                -- null = unlimited (mirrors customer_users)
  expires_at      timestamptz NOT NULL,
  use_count       INT NOT NULL DEFAULT 0,
  revoked_at      timestamptz,
  created_by      BIGINT REFERENCES users(id),  -- the admin who minted it
  created_at      timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX customer_invites_customer_idx ON customer_invites (customer_id);
