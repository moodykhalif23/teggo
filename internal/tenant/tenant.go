// Package tenant implements self-serve tenant provisioning (SAAS.md #1): one
// transactional Provision turns a signup into a fully-usable organization —
// org row ('pending' until email verification), the role set seeded from the
// canonical permission template (org 1's admin role, which every permission
// migration appends to), a default website on the tenant's subdomain, the
// first admin user, and a single-use verification token.
package tenant

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"b2bcommerce/internal/auth"
	"b2bcommerce/internal/store/gen"
)

// Organization lifecycle statuses. 'pending' (pre-verification) and 'suspended'
// block every sign-in; 'trial' and 'active' behave identically today (billing —
// SAAS.md #2 — will differentiate them).
const (
	StatusPending   = "pending"
	StatusTrial     = "trial"
	StatusActive    = "active"
	StatusSuspended = "suspended"
)

// ErrDomainTaken means the requested subdomain is already serving a website.
var ErrDomainTaken = errors.New("domain already in use")

// ValidationError is an input problem safe to echo back to the caller.
type ValidationError struct{ msg string }

func (e ValidationError) Error() string { return e.msg }

func invalid(format string, args ...any) error {
	return ValidationError{msg: fmt.Sprintf(format, args...)}
}

// Input is one signup submission.
type Input struct {
	OrgName    string
	FullName   string
	Email      string
	Password   string
	Subdomain  string
	BaseDomain string // deployment-wide suffix for tenant storefronts (e.g. teggo.local)
	Currency   string // ISO 4217; defaults to USD
	Locale     string // defaults to en
	VerifyTTL  time.Duration
}

// Result reports what Provision created.
type Result struct {
	OrgID     int64
	UserID    int64
	WebsiteID int64
	Domain    string
	Token     string // verification token to embed in the email link
	ExpiresAt time.Time
}

var subdomainRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{1,61}[a-z0-9])?$`)

// reservedSubdomains are platform-owned names a tenant may not claim.
var reservedSubdomains = map[string]bool{
	"www": true, "api": true, "app": true, "admin": true, "platform": true,
	"mail": true, "smtp": true, "imap": true, "pop": true, "ftp": true,
	"ns1": true, "ns2": true, "status": true, "docs": true, "cdn": true,
}

// Validate normalizes and checks a signup submission in place.
func Validate(in *Input) error {
	in.OrgName = strings.TrimSpace(in.OrgName)
	in.FullName = strings.TrimSpace(in.FullName)
	in.Email = strings.ToLower(strings.TrimSpace(in.Email))
	in.Subdomain = strings.ToLower(strings.TrimSpace(in.Subdomain))
	in.Currency = strings.ToUpper(strings.TrimSpace(in.Currency))

	switch {
	case in.OrgName == "" || len(in.OrgName) > 120:
		return invalid("organization name is required (max 120 characters)")
	case in.FullName == "" || len(in.FullName) > 120:
		return invalid("your name is required (max 120 characters)")
	case !strings.Contains(in.Email, "@") || strings.ContainsAny(in.Email, " \t"):
		return invalid("a valid email address is required")
	case len(in.Password) < 8:
		return invalid("password must be at least 8 characters")
	case len(in.Subdomain) < 3 || len(in.Subdomain) > 63 || !subdomainRe.MatchString(in.Subdomain):
		return invalid("subdomain must be 3-63 characters: lowercase letters, digits and hyphens")
	case reservedSubdomains[in.Subdomain]:
		return invalid("subdomain %q is reserved", in.Subdomain)
	}
	if in.Currency == "" {
		in.Currency = "USD"
	}
	if len(in.Currency) != 3 {
		return invalid("currency must be a 3-letter ISO code")
	}
	if in.Locale = strings.TrimSpace(in.Locale); in.Locale == "" {
		in.Locale = "en"
	}
	if in.BaseDomain == "" {
		in.BaseDomain = "teggo.local"
	}
	if in.VerifyTTL <= 0 {
		in.VerifyTTL = 48 * time.Hour
	}
	return nil
}

// Provision creates the whole tenant in one transaction. The returned token is
// NOT persisted in clear anywhere else — the caller emails it to the signer-up.
func Provision(ctx context.Context, pool *pgxpool.Pool, in Input) (Result, error) {
	if err := Validate(&in); err != nil {
		return Result{}, err
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return Result{}, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op after commit

	q := gen.New(tx)

	org, err := q.CreateOrganization(ctx, gen.CreateOrganizationParams{Name: in.OrgName, Status: StatusPending})
	if err != nil {
		return Result{}, fmt.Errorf("create org: %w", err)
	}

	// Role set: admin = the full template, staff = view+edit, viewer = view-only.
	adminRole, err := q.CreateRole(ctx, gen.CreateRoleParams{OrganizationID: org.ID, Name: "admin", Description: ptr("Full access")})
	if err != nil {
		return Result{}, fmt.Errorf("create admin role: %w", err)
	}
	seeded, err := q.SeedRolePermissionsFromTemplate(ctx, gen.SeedRolePermissionsFromTemplateParams{RoleID: adminRole.ID, Pattern: ""})
	if err != nil {
		return Result{}, fmt.Errorf("seed admin permissions: %w", err)
	}
	if seeded == 0 {
		// A powerless tenant admin is worse than a failed signup: the template
		// (org 1's admin role) must exist in any migrated database.
		return Result{}, errors.New("permission template is empty — is the platform org seeded?")
	}
	staffRole, err := q.CreateRole(ctx, gen.CreateRoleParams{OrganizationID: org.ID, Name: "staff", Description: ptr("View and edit, no management")})
	if err != nil {
		return Result{}, fmt.Errorf("create staff role: %w", err)
	}
	for _, pat := range []string{"%.view", "%.edit"} {
		if _, err := q.SeedRolePermissionsFromTemplate(ctx, gen.SeedRolePermissionsFromTemplateParams{RoleID: staffRole.ID, Pattern: pat}); err != nil {
			return Result{}, fmt.Errorf("seed staff permissions: %w", err)
		}
	}
	viewerRole, err := q.CreateRole(ctx, gen.CreateRoleParams{OrganizationID: org.ID, Name: "viewer", Description: ptr("Read-only")})
	if err != nil {
		return Result{}, fmt.Errorf("create viewer role: %w", err)
	}
	if _, err := q.SeedRolePermissionsFromTemplate(ctx, gen.SeedRolePermissionsFromTemplateParams{RoleID: viewerRole.ID, Pattern: "%.view"}); err != nil {
		return Result{}, fmt.Errorf("seed viewer permissions: %w", err)
	}

	domain := in.Subdomain + "." + in.BaseDomain
	ws, err := q.CreateWebsite(ctx, gen.CreateWebsiteParams{
		OrganizationID:  org.ID,
		Name:            in.OrgName,
		Domain:          domain,
		DefaultCurrency: in.Currency,
		DefaultLocale:   in.Locale,
	})
	if err != nil {
		if isUniqueViolation(err) {
			return Result{}, ErrDomainTaken
		}
		return Result{}, fmt.Errorf("create website: %w", err)
	}

	hash, err := auth.HashPassword(in.Password)
	if err != nil {
		return Result{}, err
	}
	user, err := q.CreateUser(ctx, gen.CreateUserParams{
		OrganizationID: org.ID,
		Email:          in.Email,
		PasswordHash:   hash,
		FullName:       in.FullName,
	})
	if err != nil {
		return Result{}, fmt.Errorf("create admin user: %w", err)
	}
	if err := q.AssignUserRole(ctx, gen.AssignUserRoleParams{UserID: user.ID, RoleID: adminRole.ID}); err != nil {
		return Result{}, fmt.Errorf("assign admin role: %w", err)
	}

	// New tenants start on the free plan (SAAS.md #2) — operators upgrade them.
	// A database without seeded plans provisions unmetered rather than failing.
	if plan, err := q.GetPlanByCode(ctx, "free"); err == nil {
		if _, err := q.SetOrgPlan(ctx, gen.SetOrgPlanParams{OrganizationID: org.ID, PlanID: plan.ID}); err != nil {
			return Result{}, fmt.Errorf("assign free plan: %w", err)
		}
	}

	ver, err := q.CreateSignupVerification(ctx, gen.CreateSignupVerificationParams{
		OrganizationID: org.ID,
		UserID:         user.ID,
		ExpiresAt:      time.Now().Add(in.VerifyTTL),
	})
	if err != nil {
		return Result{}, fmt.Errorf("create verification: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return Result{}, err
	}
	return Result{
		OrgID:     org.ID,
		UserID:    user.ID,
		WebsiteID: ws.ID,
		Domain:    domain,
		Token:     ver.Token.String(),
		ExpiresAt: ver.ExpiresAt,
	}, nil
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func ptr(s string) *string { return &s }
