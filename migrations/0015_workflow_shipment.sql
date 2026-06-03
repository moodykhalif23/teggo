-- Seed the default SHIPMENT workflow (Pack 2 §3) for the demo org, mirroring the
-- previously-hardcoded shipment state machine. No new tables — just config rows
-- in the workflow_* tables from 0014.

INSERT INTO workflow_definitions (organization_id, code, entity_type, name)
VALUES (1, 'shipment_default', 'shipment', 'Default shipment lifecycle');

INSERT INTO workflow_states (definition_id, code, label, is_initial, is_final, sort_order)
SELECT d.id, s.code, s.label, s.is_initial, s.is_final, s.sort_order
  FROM workflow_definitions d
  CROSS JOIN (VALUES
    ('pending',   'Pending',   true,  false, 1),
    ('shipped',   'Shipped',   false, false, 2),
    ('delivered', 'Delivered', false, false, 3),
    ('returned',  'Returned',  false, true,  4)
  ) AS s(code, label, is_initial, is_final, sort_order)
 WHERE d.organization_id = 1 AND d.code = 'shipment_default';

INSERT INTO workflow_transitions (definition_id, code, label, from_state_id, to_state_id, sort_order)
SELECT d.id, t.code, t.label, fs.id, ts.id, t.sort_order
  FROM workflow_definitions d
  CROSS JOIN (VALUES
    ('ship',             'Ship',     'pending',   'shipped',   1),
    ('deliver',          'Deliver',  'shipped',   'delivered', 2),
    ('return_pending',   'Return',   'pending',   'returned',  3),
    ('return_shipped',   'Return',   'shipped',   'returned',  4),
    ('return_delivered', 'Return',   'delivered', 'returned',  5)
  ) AS t(code, label, from_code, to_code, sort_order)
  JOIN workflow_states fs ON fs.definition_id = d.id AND fs.code = t.from_code
  JOIN workflow_states ts ON ts.definition_id = d.id AND ts.code = t.to_code
 WHERE d.organization_id = 1 AND d.code = 'shipment_default';
