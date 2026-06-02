// Package store provides data access. The methods here are hand-written with
// pgx so the server compiles and runs immediately. As you add sqlc queries
// (internal/store/queries/*.sql) and run `make generate`, migrate these to the
// generated, type-safe methods in internal/store/gen.
package store

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"b2bcommerce/internal/store/gen"
)

type Store struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

// Queries returns the sqlc-generated, type-safe query layer bound to the pool.
// New modules use this; the hand-written methods below are being migrated onto it.
func (s *Store) Queries() *gen.Queries { return gen.New(s.pool) }

// Pool exposes the underlying pool for transactions and the rare hand-written query.
func (s *Store) Pool() *pgxpool.Pool { return s.pool }

// Ping verifies database connectivity (used by readiness checks).
func (s *Store) Ping(ctx context.Context) error {
	return s.pool.Ping(ctx)
}

// User is the minimal projection used by the auth flow.
type User struct {
	ID           int64
	OrgID        int64
	Email        string
	PasswordHash string
	FullName     string
}

// GetUserByEmail loads an active user within an organization.
func (s *Store) GetUserByEmail(ctx context.Context, orgID int64, email string) (*User, error) {
	var u User
	err := s.pool.QueryRow(ctx,
		`SELECT id, organization_id, email, password_hash, full_name
		   FROM users
		  WHERE organization_id = $1 AND email = $2 AND is_active = true`,
		orgID, email,
	).Scan(&u.ID, &u.OrgID, &u.Email, &u.PasswordHash, &u.FullName)
	if err != nil {
		return nil, err
	}
	return &u, nil
}

// GetUserPermissions returns the distinct permission strings for a user.
func (s *Store) GetUserPermissions(ctx context.Context, userID int64) ([]string, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT DISTINCT rp.permission
		   FROM user_roles ur
		   JOIN role_permissions rp ON rp.role_id = ur.role_id
		  WHERE ur.user_id = $1`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var perms []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, err
		}
		perms = append(perms, p)
	}
	return perms, rows.Err()
}

// Product is the storefront-facing projection.
type Product struct {
	PublicID    string         `json:"public_id"`
	SKU         string         `json:"sku"`
	Name        string         `json:"name"`
	Slug        string         `json:"slug"`
	Description *string        `json:"description,omitempty"`
	Status      string         `json:"status"`
	Attributes  map[string]any `json:"attributes"`
	Unit        string         `json:"unit"`
}

// ListActiveProducts returns a page of active products for an organization.
func (s *Store) ListActiveProducts(ctx context.Context, orgID int64, limit, offset int32) ([]Product, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT public_id, sku, name, slug, description, status, attributes, unit
		   FROM products
		  WHERE organization_id = $1 AND status = 'active' AND deleted_at IS NULL
		  ORDER BY name LIMIT $2 OFFSET $3`,
		orgID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Product
	for rows.Next() {
		var p Product
		if err := rows.Scan(&p.PublicID, &p.SKU, &p.Name, &p.Slug,
			&p.Description, &p.Status, &p.Attributes, &p.Unit); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
