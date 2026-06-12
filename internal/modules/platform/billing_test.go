package platform_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// signupVerifiedOrg provisions a tenant via the public flow (lands on the free
// plan) and returns its admin token + org id.
func signupVerifiedOrg(t *testing.T, h http.Handler, pool *pgxpool.Pool, sub string) (string, int64) {
	t.Helper()
	rr := do(t, h, http.MethodPost, "/signup", "", map[string]any{
		"organization": "Tenant " + sub, "full_name": "Ten Ant", "email": sub + "@tenant.test",
		"password": "pw-123456", "subdomain": sub,
	})
	if rr.Code != http.StatusCreated {
		t.Fatalf("signup: %d (%s)", rr.Code, rr.Body.String())
	}
	var token string
	var orgID int64
	if err := pool.QueryRow(context.Background(),
		`SELECT token::text, organization_id FROM signup_verifications ORDER BY id DESC LIMIT 1`,
	).Scan(&token, &orgID); err != nil {
		t.Fatalf("read verification: %v", err)
	}
	if rr := do(t, h, http.MethodPost, "/signup/verify", "", map[string]any{"token": token}); rr.Code != http.StatusOK {
		t.Fatalf("verify: %d", rr.Code)
	}
	tok, code := login(t, h, sub+"@tenant.test", "pw-123456")
	if code != http.StatusOK {
		t.Fatalf("login: %d", code)
	}
	return tok, orgID
}

func jsonCode(body []byte, want string) bool {
	var e struct {
		Code string `json:"code"`
	}
	_ = json.Unmarshal(body, &e)
	return e.Code == want
}

// TestPlanFeatureGate: a free-plan tenant is blocked from premium modules with
// a clear code; the scale-plan platform org is not; an operator upgrade flips
// access without re-login.
func TestPlanFeatureGate(t *testing.T) {
	h, _, pool := newServer(t)
	tenantTok, orgID := signupVerifiedOrg(t, h, pool, "gateco")

	// Free plan: premium modules are 403 feature_not_in_plan...
	for _, path := range []string{"/admin/subscriptions", "/admin/rebates", "/admin/fx-rates", "/admin/search-synonyms"} {
		rr := do(t, h, http.MethodGet, path, tenantTok, nil)
		if rr.Code != http.StatusForbidden || !jsonCode(rr.Body.Bytes(), "feature_not_in_plan") {
			t.Errorf("%s on free plan: want 403 feature_not_in_plan, got %d (%s)", path, rr.Code, rr.Body.String())
		}
	}
	// ...while core commerce works.
	if rr := do(t, h, http.MethodGet, "/admin/products", tenantTok, nil); rr.Code != http.StatusOK {
		t.Fatalf("core route on free plan: %d", rr.Code)
	}

	// Org 1 (seeded onto scale by 0053) passes the same gates.
	opTok := operatorToken(t, h)
	if rr := do(t, h, http.MethodGet, "/admin/rebates", opTok, nil); rr.Code != http.StatusOK {
		t.Fatalf("scale org rebates: %d (%s)", rr.Code, rr.Body.String())
	}

	// Operator upgrades the tenant to scale → access opens immediately.
	up := do(t, h, http.MethodPost, "/admin/platform/organizations/"+strconv.FormatInt(orgID, 10)+"/plan",
		opTok, map[string]any{"plan_code": "scale"})
	if up.Code != http.StatusOK {
		t.Fatalf("assign plan: %d (%s)", up.Code, up.Body.String())
	}
	if rr := do(t, h, http.MethodGet, "/admin/subscriptions", tenantTok, nil); rr.Code != http.StatusOK {
		t.Fatalf("subscriptions after upgrade: %d (%s)", rr.Code, rr.Body.String())
	}
}

// TestPlanQuotaEnforced: shrink the free plan's order cap to 1 — the second
// order is refused with quota_exceeded, and /admin/billing shows plan + usage.
func TestPlanQuotaEnforced(t *testing.T) {
	h, _, pool := newServer(t)
	tenantTok, _ := signupVerifiedOrg(t, h, pool, "quotaco")
	opTok := operatorToken(t, h)

	// Operator tightens the free plan: 1 order/month.
	if rr := do(t, h, http.MethodPut, "/admin/platform/plans/free", opTok,
		map[string]any{"limits": map[string]any{"orders": 1}}); rr.Code != http.StatusOK {
		t.Fatalf("update plan: %d (%s)", rr.Code, rr.Body.String())
	}

	// Order-on-behalf needs a product + customer inside the tenant org.
	mkBody := func(sku string) map[string]any {
		pr := do(t, h, http.MethodPost, "/admin/products", tenantTok, map[string]any{
			"sku": sku, "type": "simple", "name": sku, "slug": sku, "status": "active",
		})
		if pr.Code != http.StatusCreated {
			t.Fatalf("product: %d (%s)", pr.Code, pr.Body.String())
		}
		var p struct {
			ID int64 `json:"id"`
		}
		_ = json.Unmarshal(pr.Body.Bytes(), &p)
		cr := do(t, h, http.MethodPost, "/admin/customers", tenantTok, map[string]any{"name": "Buyer " + sku})
		if cr.Code >= 300 {
			t.Fatalf("customer: %d (%s)", cr.Code, cr.Body.String())
		}
		var c struct {
			ID int64 `json:"id"`
		}
		_ = json.Unmarshal(cr.Body.Bytes(), &c)
		return map[string]any{
			"customer_id": c.ID,
			"items":       []map[string]any{{"product_id": p.ID, "quantity": "1", "unit_price": "10"}},
		}
	}

	// First order: allowed (and metered on success).
	if rr := do(t, h, http.MethodPost, "/admin/orders", tenantTok, mkBody("q-1")); rr.Code != http.StatusOK {
		t.Fatalf("first order: %d (%s)", rr.Code, rr.Body.String())
	}
	// Second order: the cap bites with a clear code.
	rr := do(t, h, http.MethodPost, "/admin/orders", tenantTok, mkBody("q-2"))
	if rr.Code != http.StatusForbidden || !jsonCode(rr.Body.Bytes(), "quota_exceeded") {
		t.Fatalf("second order: want 403 quota_exceeded, got %d (%s)", rr.Code, rr.Body.String())
	}

	// The tenant's billing screen reflects plan, limits and consumption.
	br := do(t, h, http.MethodGet, "/admin/billing", tenantTok, nil)
	var view struct {
		Plan struct {
			Code string `json:"code"`
		} `json:"plan"`
		Limits map[string]int64 `json:"limits"`
		Usage  map[string]int64 `json:"usage"`
	}
	_ = json.Unmarshal(br.Body.Bytes(), &view)
	if view.Plan.Code != "free" || view.Limits["orders"] != 1 || view.Usage["orders"] != 1 {
		t.Fatalf("billing view wrong: %s", br.Body.String())
	}

	// Operator plan list shows the three tiers.
	pl := do(t, h, http.MethodGet, "/admin/platform/plans", opTok, nil)
	var plans struct {
		Items []struct {
			Code string `json:"code"`
		} `json:"items"`
	}
	_ = json.Unmarshal(pl.Body.Bytes(), &plans)
	if len(plans.Items) != 3 {
		t.Fatalf("plans: %s", pl.Body.String())
	}
}
