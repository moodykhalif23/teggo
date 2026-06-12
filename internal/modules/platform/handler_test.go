package platform_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"b2bcommerce/internal/auth"
	"b2bcommerce/internal/server"
	"b2bcommerce/internal/store"
	"b2bcommerce/internal/testsupport"
)

const testSecret = "test-secret-please-change"

func newServer(t *testing.T) (http.Handler, *auth.Issuer, *pgxpool.Pool) {
	t.Helper()
	pool := testsupport.NewDB(t)
	return server.New(store.New(pool), auth.NewIssuer(testSecret, time.Hour)), auth.NewIssuer(testSecret, time.Hour), pool
}

// reqSeq gives every request a distinct client IP so the per-IP login/signup
// rate limiter (10/min) never throttles the test flows themselves.
var reqSeq atomic.Int64

func do(t *testing.T, h http.Handler, method, path, tok string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(method, path, &buf)
	n := reqSeq.Add(1)
	req.Header.Set("X-Forwarded-For", fmt.Sprintf("10.9.%d.%d", (n/250)%250, n%250+1))
	if tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	return rr
}

func login(t *testing.T, h http.Handler, email, password string) (string, int) {
	t.Helper()
	rr := do(t, h, http.MethodPost, "/admin/auth/login", "", map[string]any{"email": email, "password": password})
	var res struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &res)
	return res.Token, rr.Code
}

// TestSignupProvisionGoldenPath walks the whole tenant lifecycle: signup →
// pending blocks login → verify → org-aware login (no org_id) → the new admin
// works inside their org with the full template permission set → tenant
// isolation against org 1 → operator suspension shuts the tenant off.
func TestSignupProvisionGoldenPath(t *testing.T) {
	h, _, pool := newServer(t)
	ctx := context.Background()

	// Signup.
	rr := do(t, h, http.MethodPost, "/signup", "", map[string]any{
		"organization": "Acme Industrial",
		"full_name":    "Ada Admin",
		"email":        "ada@acme.test",
		"password":     "pw-123456",
		"subdomain":    "acme",
		"currency":     "KES",
	})
	if rr.Code != http.StatusCreated {
		t.Fatalf("signup: %d (%s)", rr.Code, rr.Body.String())
	}

	// The org is pending: login must be blocked with a specific code.
	if _, code := login(t, h, "ada@acme.test", "pw-123456"); code != http.StatusForbidden {
		t.Fatalf("pending login: want 403, got %d", code)
	}

	// Fetch the verification token the way the email would carry it.
	var token string
	var orgID int64
	if err := pool.QueryRow(ctx,
		`SELECT token::text, organization_id FROM signup_verifications ORDER BY id DESC LIMIT 1`,
	).Scan(&token, &orgID); err != nil {
		t.Fatalf("read verification: %v", err)
	}

	// Verify; second use of the same token must fail (single-use).
	if rr := do(t, h, http.MethodPost, "/signup/verify", "", map[string]any{"token": token}); rr.Code != http.StatusOK {
		t.Fatalf("verify: %d (%s)", rr.Code, rr.Body.String())
	}
	if rr := do(t, h, http.MethodPost, "/signup/verify", "", map[string]any{"token": token}); rr.Code != http.StatusBadRequest {
		t.Fatalf("verify reuse: want 400, got %d", rr.Code)
	}

	// Org-aware login: no org_id in the body — the email resolves it.
	tok, code := login(t, h, "ada@acme.test", "pw-123456")
	if code != http.StatusOK || tok == "" {
		t.Fatalf("login after verify: %d", code)
	}

	// The template gave the tenant admin real permissions: create a product.
	pr := do(t, h, http.MethodPost, "/admin/products", tok, map[string]any{
		"sku": "ACME-1", "type": "simple", "name": "Acme Widget", "slug": "acme-widget", "status": "active",
	})
	if pr.Code != http.StatusCreated {
		t.Fatalf("tenant create product: %d (%s)", pr.Code, pr.Body.String())
	}

	// Tenant isolation: the new org sees only its own catalog (org 1 seeds 3).
	lr := do(t, h, http.MethodGet, "/admin/products", tok, nil)
	var list struct {
		Items []struct {
			Sku string `json:"sku"`
		} `json:"items"`
	}
	_ = json.Unmarshal(lr.Body.Bytes(), &list)
	if len(list.Items) != 1 || list.Items[0].Sku != "ACME-1" {
		t.Fatalf("tenant product list leaked: %s", lr.Body.String())
	}

	// platform.* is operator-only: the tenant admin must not hold it.
	if rr := do(t, h, http.MethodGet, "/admin/platform/organizations", tok, nil); rr.Code != http.StatusForbidden {
		t.Fatalf("tenant reaching platform routes: want 403, got %d", rr.Code)
	}

	// The platform operator (org 1) lists both orgs and suspends the tenant.
	opTok := operatorToken(t, h)
	or := do(t, h, http.MethodGet, "/admin/platform/organizations", opTok, nil)
	var orgs struct {
		Items []struct {
			ID     int64  `json:"id"`
			Status string `json:"status"`
		} `json:"items"`
	}
	_ = json.Unmarshal(or.Body.Bytes(), &orgs)
	if len(orgs.Items) < 2 {
		t.Fatalf("operator org list: %s", or.Body.String())
	}
	sr := do(t, h, http.MethodPost, "/admin/platform/organizations/"+strconv.FormatInt(orgID, 10)+"/status",
		opTok, map[string]any{"status": "suspended"})
	if sr.Code != http.StatusOK {
		t.Fatalf("suspend: %d (%s)", sr.Code, sr.Body.String())
	}

	// Suspension bites immediately: the live token is blocked by the org gate
	// and a fresh login is refused.
	if rr := do(t, h, http.MethodGet, "/admin/products", tok, nil); rr.Code != http.StatusForbidden {
		t.Fatalf("suspended org with live token: want 403, got %d", rr.Code)
	}
	if _, code := login(t, h, "ada@acme.test", "pw-123456"); code != http.StatusForbidden {
		t.Fatalf("suspended login: want 403, got %d", code)
	}

	// Reactivate → access returns.
	if rr := do(t, h, http.MethodPost, "/admin/platform/organizations/"+strconv.FormatInt(orgID, 10)+"/status",
		opTok, map[string]any{"status": "active"}); rr.Code != http.StatusOK {
		t.Fatalf("reactivate: %d", rr.Code)
	}
	if rr := do(t, h, http.MethodGet, "/admin/products", tok, nil); rr.Code != http.StatusOK {
		t.Fatalf("reactivated org: want 200, got %d", rr.Code)
	}
}

// operatorToken logs in as the seeded demo admin (org 1 = the platform owner),
// whose role migration 0051 granted platform.view/platform.manage.
func operatorToken(t *testing.T, h http.Handler) string {
	t.Helper()
	tok, code := login(t, h, "admin@demo.test", "admin1234")
	if code != http.StatusOK {
		t.Fatalf("operator login: %d", code)
	}
	return tok
}

func TestSignupValidationAndCollisions(t *testing.T) {
	h, _, _ := newServer(t)

	base := map[string]any{
		"organization": "Beta Corp", "full_name": "Bea", "email": "bea@beta.test",
		"password": "pw-123456", "subdomain": "beta",
	}
	mutate := func(k string, v any) map[string]any {
		m := make(map[string]any, len(base))
		for kk, vv := range base {
			m[kk] = vv
		}
		m[k] = v
		return m
	}

	for name, body := range map[string]map[string]any{
		"short password":    mutate("password", "short"),
		"bad subdomain":     mutate("subdomain", "Bad_Sub!"),
		"reserved":          mutate("subdomain", "admin"),
		"missing email":     mutate("email", ""),
		"long org":          mutate("organization", string(bytes.Repeat([]byte("x"), 200))),
		"4-letter currency": mutate("currency", "KESH"),
	} {
		if rr := do(t, h, http.MethodPost, "/signup", "", body); rr.Code != http.StatusBadRequest {
			t.Errorf("%s: want 400, got %d (%s)", name, rr.Code, rr.Body.String())
		}
	}

	// First good signup wins the subdomain; the second collides.
	if rr := do(t, h, http.MethodPost, "/signup", "", base); rr.Code != http.StatusCreated {
		t.Fatalf("signup: %d (%s)", rr.Code, rr.Body.String())
	}
	dup := mutate("email", "other@beta.test")
	if rr := do(t, h, http.MethodPost, "/signup", "", dup); rr.Code != http.StatusConflict {
		t.Errorf("duplicate subdomain: want 409, got %d (%s)", rr.Code, rr.Body.String())
	}

	// Garbage verification tokens are rejected, not 500s.
	for _, tok := range []string{"", "not-a-uuid", "7f000000-0000-0000-0000-00000000dead"} {
		if rr := do(t, h, http.MethodPost, "/signup/verify", "", map[string]any{"token": tok}); rr.Code != http.StatusBadRequest {
			t.Errorf("verify %q: want 400, got %d", tok, rr.Code)
		}
	}
}

// TestOperatorGuards: tenants can't touch platform routes (covered above), the
// platform org itself can't be suspended, and bad statuses are rejected.
func TestOperatorGuards(t *testing.T) {
	h, _, _ := newServer(t)
	opTok := operatorToken(t, h)

	if rr := do(t, h, http.MethodPost, "/admin/platform/organizations/1/status", opTok,
		map[string]any{"status": "suspended"}); rr.Code != http.StatusBadRequest {
		t.Errorf("suspend platform org: want 400, got %d", rr.Code)
	}
	if rr := do(t, h, http.MethodPost, "/admin/platform/organizations/2/status", opTok,
		map[string]any{"status": "pending"}); rr.Code != http.StatusBadRequest {
		t.Errorf("set pending: want 400, got %d", rr.Code)
	}
	if rr := do(t, h, http.MethodPost, "/admin/platform/organizations/99999/status", opTok,
		map[string]any{"status": "suspended"}); rr.Code != http.StatusNotFound {
		t.Errorf("missing org: want 404, got %d", rr.Code)
	}
}
