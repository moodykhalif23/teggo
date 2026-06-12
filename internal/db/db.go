package db

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"b2bcommerce/internal/tenantctx"
)

// PoolConfig tunes the connection pool. Non-positive values fall back to safe
// defaults (20 max connections, 5m idle recycle).
type PoolConfig struct {
	MaxConns        int32
	MaxConnIdleTime time.Duration

	// ArmTenantRLS turns on the row-level-security net (SAAS.md #3): every
	// connection acquisition reads the request's org from context (set by the
	// auth middleware via tenantctx) and pins Postgres' app.org_id setting, so
	// the org_isolation policies scope even a query that forgot its WHERE
	// clause. Unauthenticated acquisitions clear the setting. Used by the API;
	// the worker runs unarmed (cross-org sweeps with explicit filters).
	// NOTE: requires session-pooling (incompatible with PgBouncer transaction
	// mode, which breaks session settings).
	ArmTenantRLS bool
}

// NewPool creates and verifies a pgx connection pool with default tuning.
func NewPool(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
	return NewPoolWithConfig(ctx, dsn, PoolConfig{})
}

// NewPoolWithConfig creates and verifies a pgx connection pool with the given
// tuning. The pool is configurable so production can raise connection limits
// beyond the conservative default.
func NewPoolWithConfig(ctx context.Context, dsn string, pc PoolConfig) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse dsn: %w", err)
	}
	if pc.MaxConns <= 0 {
		pc.MaxConns = 20
	}
	if pc.MaxConnIdleTime <= 0 {
		pc.MaxConnIdleTime = 5 * time.Minute
	}
	cfg.MaxConns = pc.MaxConns
	cfg.MaxConnIdleTime = pc.MaxConnIdleTime
	cfg.MaxConnLifetime = time.Hour
	if pc.ArmTenantRLS {
		armTenantRLS(cfg)
	}

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("create pool: %w", err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping: %w", err)
	}
	return pool, nil
}

// armTenantRLS wires the per-acquisition app.org_id pinning. Arming means two
// session changes: SET ROLE teggo_app (a NOLOGIN, NOBYPASSRLS role from
// migration 0054 — the connection user is usually a superuser, which Postgres
// exempts from RLS entirely) plus the app.org_id setting the policies key on.
// The last value set on each physical connection is tracked so the extra
// round-trip only happens when the org actually changes on that connection.
func armTenantRLS(cfg *pgxpool.Config) {
	var mu sync.Mutex
	last := make(map[*pgx.Conn]string) // conn → last app.org_id sent

	cfg.BeforeAcquire = func(ctx context.Context, conn *pgx.Conn) bool {
		want := ""
		if org, ok := tenantctx.OrgFrom(ctx); ok {
			want = strconv.FormatInt(org, 10)
		}
		mu.Lock()
		have, tracked := last[conn]
		mu.Unlock()
		if tracked && have == want {
			return true
		}
		// Multi-statement (no params → simple protocol). `want` is digits-only,
		// derived from an int64 — safe to inline.
		sql := "RESET ROLE; SET app.org_id = ''"
		if want != "" {
			sql = "SET ROLE teggo_app; SET app.org_id = '" + want + "'"
		}
		if _, err := conn.Exec(ctx, sql); err != nil {
			// A connection whose session can't be trusted must not serve the
			// request — discarding it makes the pool dial a fresh one.
			return false
		}
		mu.Lock()
		last[conn] = want
		mu.Unlock()
		return true
	}
	cfg.BeforeClose = func(conn *pgx.Conn) {
		mu.Lock()
		delete(last, conn)
		mu.Unlock()
	}
}
