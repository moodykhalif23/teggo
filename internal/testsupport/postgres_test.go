package testsupport_test

import (
	"context"
	"testing"

	"b2bcommerce/internal/testsupport"
)

// TestMigrationsApply is the end-to-end migration-compatibility gate: it proves
// the full embedded migration set (plus river's tables) applies cleanly against
// a real Postgres 16, and that the foundational schema + seed are present. Every
// new migration must keep this green.
func TestMigrationsApply(t *testing.T) {
	pool := testsupport.NewDB(t)
	ctx := context.Background()

	// Seeded foundation rows exist (migrations/0003_seed.sql).
	var orgs int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM organizations`).Scan(&orgs); err != nil {
		t.Fatalf("count organizations: %v", err)
	}
	if orgs == 0 {
		t.Fatal("expected at least one seeded organization")
	}

	// A representative set of tables from each migration must exist.
	for _, table := range []string{
		"organizations", "websites", "users", "roles", "role_permissions",
		"user_roles", "products", "schema_migrations",
	} {
		var exists bool
		err := pool.QueryRow(ctx,
			`SELECT to_regclass($1) IS NOT NULL`, "public."+table).Scan(&exists)
		if err != nil {
			t.Fatalf("check table %s: %v", table, err)
		}
		if !exists {
			t.Errorf("expected table %q to exist", table)
		}
	}

	// River's own tables were migrated too (the worker depends on them).
	var riverJobExists bool
	if err := pool.QueryRow(ctx,
		`SELECT to_regclass('public.river_job') IS NOT NULL`).Scan(&riverJobExists); err != nil {
		t.Fatalf("check river_job: %v", err)
	}
	if !riverJobExists {
		t.Error("expected river_job table to exist")
	}
}

// TestIsolation proves each NewDB call is an independent database: a row written
// in one is invisible to the next.
func TestIsolation(t *testing.T) {
	ctx := context.Background()

	a := testsupport.NewDB(t)
	if _, err := a.Exec(ctx,
		`INSERT INTO organizations (name) VALUES ('iso-test')`); err != nil {
		t.Fatalf("insert into db a: %v", err)
	}

	b := testsupport.NewDB(t)
	var count int
	if err := b.QueryRow(ctx,
		`SELECT count(*) FROM organizations WHERE name = 'iso-test'`).Scan(&count); err != nil {
		t.Fatalf("query db b: %v", err)
	}
	if count != 0 {
		t.Errorf("expected isolation: db b should not see db a's row, got %d", count)
	}
}
