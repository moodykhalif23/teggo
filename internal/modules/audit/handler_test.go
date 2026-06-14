package audit_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
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
	st := store.New(pool)
	issuer := auth.NewIssuer(testSecret, time.Hour)
	return server.New(st, issuer), issuer, pool
}

func do(t *testing.T, h http.Handler, method, path, tok string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(method, path, &buf)
	if tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	return rr
}

func decode(t *testing.T, rr *httptest.ResponseRecorder, v any) {
	t.Helper()
	if err := json.Unmarshal(rr.Body.Bytes(), v); err != nil {
		t.Fatalf("decode: %v (body=%s)", err, rr.Body.String())
	}
}

func adminToken(t *testing.T, issuer *auth.Issuer, perms ...string) string {
	t.Helper()
	tok, _ := issuer.Issue("1", 1, "admin", perms)
	return tok
}

type auditItem struct {
	Action        string         `json:"action"`
	EntityType    string         `json:"entity_type"`
	EntityID      *int64         `json:"entity_id"`
	ActorUserID   *int64         `json:"actor_user_id"`
	ActorAudience string         `json:"actor_audience"`
	StatusCode    int            `json:"status_code"`
	Method        string         `json:"method"`
	Path          string         `json:"path"`
	Metadata      map[string]any `json:"metadata"`
}

type auditList struct {
	Total int64       `json:"total"`
	Items []auditItem `json:"items"`
}

func find(items []auditItem, action string) *auditItem {
	for i := range items {
		if items[i].Action == action {
			return &items[i]
		}
	}
	return nil
}

// TestAuditRecordsMutation proves the middleware captures a real staff mutation
// (who/action/entity/result) automatically, and that handler enrichment attaches
// the before/after snapshot.
func TestAuditRecordsMutation(t *testing.T) {
	h, issuer, _ := newServer(t)
	tok := adminToken(t, issuer, "customer.manage", "audit.view")

	rr := do(t, h, http.MethodPost, "/admin/customers", tok, map[string]any{"name": "Audited Co", "credit_limit": "0"})
	if rr.Code != http.StatusCreated {
		t.Fatalf("create customer: status %d (%s)", rr.Code, rr.Body.String())
	}

	var list auditList
	decode(t, do(t, h, http.MethodGet, "/admin/audit", tok, nil), &list)

	a := find(list.Items, "customers.create")
	if a == nil {
		t.Fatalf("expected a customers.create audit entry, got %+v", list.Items)
	}
	if a.EntityType != "customers" {
		t.Errorf("entity_type = %q, want customers", a.EntityType)
	}
	if a.ActorUserID == nil || *a.ActorUserID != 1 {
		t.Errorf("actor_user_id = %v, want 1", a.ActorUserID)
	}
	if a.ActorAudience != "admin" {
		t.Errorf("actor_audience = %q, want admin", a.ActorAudience)
	}
	if a.StatusCode != http.StatusCreated || a.Method != http.MethodPost {
		t.Errorf("result = %s/%d, want POST/201", a.Method, a.StatusCode)
	}
	// Enrichment: the customer handler attached a before/after change snapshot.
	change, _ := a.Metadata["change"].(map[string]any)
	if change == nil || change["after"] == nil {
		t.Errorf("expected metadata.change.after from enrichment, got %v", a.Metadata)
	}
}

func TestAuditFilterByAction(t *testing.T) {
	h, issuer, _ := newServer(t)
	tok := adminToken(t, issuer, "customer.manage", "audit.view")
	_ = do(t, h, http.MethodPost, "/admin/customers", tok, map[string]any{"name": "Filtered Co", "credit_limit": "0"})

	var match auditList
	decode(t, do(t, h, http.MethodGet, "/admin/audit?action=customers.create", tok, nil), &match)
	if len(match.Items) == 0 {
		t.Fatalf("action filter should return the create entry")
	}
	for _, it := range match.Items {
		if it.Action != "customers.create" {
			t.Errorf("action filter leaked %q", it.Action)
		}
	}

	var none auditList
	decode(t, do(t, h, http.MethodGet, "/admin/audit?action=does.not.exist", tok, nil), &none)
	if len(none.Items) != 0 {
		t.Errorf("unknown action filter should be empty, got %d", len(none.Items))
	}
}

func TestAuditExportCSV(t *testing.T) {
	h, issuer, _ := newServer(t)
	tok := adminToken(t, issuer, "customer.manage", "audit.view")
	_ = do(t, h, http.MethodPost, "/admin/customers", tok, map[string]any{"name": "Export Co", "credit_limit": "0"})

	rr := do(t, h, http.MethodGet, "/admin/audit/export", tok, nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("export: status %d", rr.Code)
	}
	if ct := rr.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/csv") {
		t.Errorf("export content-type = %q, want text/csv", ct)
	}
	body := rr.Body.String()
	if !strings.Contains(body, "customers.create") || !strings.Contains(body, "timestamp,actor_user_id") {
		t.Errorf("CSV missing header or row; got:\n%s", body)
	}
}

// TestAuditCapturesFailedLogin proves login attempts are audited even though the
// login route is public (the middleware can't see it) — recorded via the
// explicit Record path.
func TestAuditCapturesFailedLogin(t *testing.T) {
	h, issuer, _ := newServer(t)

	rr := do(t, h, http.MethodPost, "/admin/auth/login", "", map[string]any{"email": "attacker@evil.test", "password": "nope"})
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("failed login: want 401, got %d", rr.Code)
	}

	tok := adminToken(t, issuer, "audit.view")
	var list auditList
	decode(t, do(t, h, http.MethodGet, "/admin/audit", tok, nil), &list)
	a := find(list.Items, "auth.login_failed")
	if a == nil {
		t.Fatalf("expected an auth.login_failed audit entry, got %+v", list.Items)
	}
	if a.StatusCode != http.StatusUnauthorized || a.ActorAudience != "admin" {
		t.Errorf("failed-login entry = %d/%s, want 401/admin", a.StatusCode, a.ActorAudience)
	}
}

func TestAuditAuth(t *testing.T) {
	h, issuer, _ := newServer(t)

	// Storefront token is the wrong audience.
	cust, _ := issuer.IssueStorefront(0, 1, 1)
	if rr := do(t, h, http.MethodGet, "/admin/audit", cust, nil); rr.Code != http.StatusForbidden {
		t.Errorf("storefront token: want 403, got %d", rr.Code)
	}
	// Admin without audit.view cannot read the trail.
	noPerm := adminToken(t, issuer, "customer.manage")
	if rr := do(t, h, http.MethodGet, "/admin/audit", noPerm, nil); rr.Code != http.StatusForbidden {
		t.Errorf("missing audit.view: want 403, got %d", rr.Code)
	}
	// With audit.view it's allowed.
	ok := adminToken(t, issuer, "audit.view")
	if rr := do(t, h, http.MethodGet, "/admin/audit", ok, nil); rr.Code != http.StatusOK {
		t.Errorf("audit.view: want 200, got %d", rr.Code)
	}
}
