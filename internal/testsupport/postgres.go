// Package testsupport provides shared helpers for integration tests that need a
// real PostgreSQL instance. The platform's core logic (recursive CTEs for the
// customer/category hierarchies, JSONB facet filters, the pricing-resolution
// query) only behaves correctly against real Postgres, so tests run against an
// actual server rather than mocks.
//
// By default a throwaway Postgres 16 container is started via testcontainers and
// reused for the whole test binary. Set TEST_DATABASE_URL to point at an existing
// server instead (e.g. in CI). Each call to NewDB returns an isolated, freshly
// migrated database that is dropped when the test finishes.
package testsupport

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	appdb "b2bcommerce/internal/db"
	"b2bcommerce/migrations"
)

const templateDB = "oro_template"

var (
	serverOnce sync.Once
	adminDSN   string // DSN to the server's default database, used for CREATE/DROP DATABASE
	templDSN   string // DSN to the migrated template database
	serverErr  error
	dbCounter  atomic.Int64
)

func ensureServer(t *testing.T) {
	t.Helper()
	serverOnce.Do(func() {
		ctx := context.Background()

		if envDSN := getenv("TEST_DATABASE_URL"); envDSN != "" {
			adminDSN = envDSN
		} else {
			pg, err := tcpostgres.Run(ctx,
				"postgres:16-alpine",
				tcpostgres.WithDatabase("postgres"),
				tcpostgres.WithUsername("oro"),
				tcpostgres.WithPassword("oro"),
				testcontainers.WithWaitStrategy(
					wait.ForLog("database system is ready to accept connections").
						WithOccurrence(2).
						WithStartupTimeout(60*time.Second),
				),
			)
			if err != nil {
				serverErr = fmt.Errorf("start postgres container: %w", err)
				return
			}
			dsn, err := pg.ConnectionString(ctx, "sslmode=disable")
			if err != nil {
				serverErr = fmt.Errorf("container dsn: %w", err)
				return
			}
			adminDSN = dsn
			// The container lives for the whole test binary; testcontainers' Ryuk
			// reaper removes it after the run.
		}

		// Build a migrated template database once; every test DB is cloned from it.
		templDSN = swapDBName(adminDSN, templateDB)
		if err := createDatabase(ctx, adminDSN, templateDB); err != nil {
			serverErr = fmt.Errorf("create template db: %w", err)
			return
		}
		if err := migrateAll(ctx, templDSN); err != nil {
			serverErr = fmt.Errorf("migrate template db: %w", err)
			return
		}
	})
	if serverErr != nil {
		t.Fatalf("testsupport: %v", serverErr)
	}
}

// NewDB returns a pool to an isolated, freshly migrated database. The database
// is cloned from the migrated template (fast) and dropped at test end.
func NewDB(t *testing.T) *pgxpool.Pool {
	pool, _ := NewDBWithDSN(t)
	return pool
}

// NewDBWithDSN is NewDB exposing the database's DSN, for tests that need an
// extra pool with different tuning (e.g. the RLS-armed pool in the isolation
// suite).
func NewDBWithDSN(t *testing.T) (*pgxpool.Pool, string) {
	t.Helper()
	ensureServer(t)

	ctx := context.Background()
	name := fmt.Sprintf("oro_test_%d_%d", dbCounter.Add(1), time.Now().UnixNano())

	if err := cloneDatabase(ctx, adminDSN, name, templateDB); err != nil {
		t.Fatalf("clone test db: %v", err)
	}

	dsn := swapDBName(adminDSN, name)
	pool, err := appdb.NewPool(ctx, dsn)
	if err != nil {
		t.Fatalf("connect test db: %v", err)
	}

	t.Cleanup(func() {
		pool.Close()
		// Best-effort drop; the server is torn down with the container anyway.
		_ = dropDatabase(context.Background(), adminDSN, name)
	})
	return pool, dsn
}

// migrateAll applies the embedded app migrations and river's own tables,
// mirroring cmd/migrate exactly so tests see the production schema.
func migrateAll(ctx context.Context, dsn string) error {
	pool, err := appdb.NewPool(ctx, dsn)
	if err != nil {
		return err
	}
	defer pool.Close()

	if err := appdb.Migrate(ctx, pool, migrations.FS); err != nil {
		return fmt.Errorf("app migrate: %w", err)
	}
	rm, err := rivermigrate.New(riverpgxv5.New(pool), nil)
	if err != nil {
		return fmt.Errorf("river migrator: %w", err)
	}
	if _, err := rm.Migrate(ctx, rivermigrate.DirectionUp, nil); err != nil {
		return fmt.Errorf("river migrate: %w", err)
	}
	return nil
}

func createDatabase(ctx context.Context, adminDSN, name string) error {
	return execAdmin(ctx, adminDSN, fmt.Sprintf("CREATE DATABASE %q", name))
}

func cloneDatabase(ctx context.Context, adminDSN, name, template string) error {
	return execAdmin(ctx, adminDSN, fmt.Sprintf("CREATE DATABASE %q TEMPLATE %q", name, template))
}

func dropDatabase(ctx context.Context, adminDSN, name string) error {
	return execAdmin(ctx, adminDSN, fmt.Sprintf("DROP DATABASE IF EXISTS %q WITH (FORCE)", name))
}

// execAdmin runs a single statement on a short-lived connection to the server's
// default database (CREATE/DROP DATABASE cannot run inside a pool transaction).
func execAdmin(ctx context.Context, adminDSN, sql string) error {
	conn, err := pgx.Connect(ctx, adminDSN)
	if err != nil {
		return err
	}
	defer conn.Close(ctx)
	_, err = conn.Exec(ctx, sql)
	return err
}
