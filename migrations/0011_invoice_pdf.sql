-- Invoice PDF storage — Implementation Pack 1 §7 / PRD §16.
-- The invoice PDF is rendered asynchronously (Gotenberg, headless Chromium) by a
-- river job and stored here as bytes. The worker and API are separate processes
-- that share only the database, so the document lives in Postgres rather than a
-- shared filesystem; the API streams it at a capability URL keyed by the
-- invoice's public_id (see invoices.pdf_url). One row per invoice, replaced on
-- regeneration.

CREATE TABLE invoice_documents (
  invoice_id    BIGINT PRIMARY KEY REFERENCES invoices(id) ON DELETE CASCADE,
  content_type  text NOT NULL DEFAULT 'application/pdf',
  bytes         bytea NOT NULL,
  generated_at  timestamptz NOT NULL DEFAULT now()
);
