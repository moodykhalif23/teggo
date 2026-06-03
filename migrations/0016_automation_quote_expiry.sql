-- Seed the quote-expiry automation rule (Pack 2 §3.6) for the demo org: on the
-- hourly schedule, run the expire_quotes action (which flips past-validity
-- sent/revised quotes to `expired` and emails the customer). No conditions —
-- the sweep itself selects the affected quotes.
INSERT INTO automation_rules (organization_id, name, trigger_event, conditions, actions, is_active)
VALUES (
  1,
  'Expire stale quotes',
  'schedule.hourly',
  '[]'::jsonb,
  '[{"key": "expire_quotes"}]'::jsonb,
  true
);
