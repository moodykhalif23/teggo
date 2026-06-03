-- Permissions for the workflow/automation admin editor (Pack 2 §3) — granted to
-- the demo admin role.
INSERT INTO role_permissions (role_id, permission)
SELECT r.id, p.permission
  FROM roles r
  CROSS JOIN (VALUES ('workflow.view'), ('workflow.manage')) AS p(permission)
 WHERE r.organization_id = 1 AND r.name = 'admin'
ON CONFLICT DO NOTHING;
