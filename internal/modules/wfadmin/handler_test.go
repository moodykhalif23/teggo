package wfadmin_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
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

func tokenWith(t *testing.T, issuer *auth.Issuer, perms ...string) string {
	t.Helper()
	tok, err := issuer.Issue("1", 1, "admin", perms)
	if err != nil {
		t.Fatalf("token: %v", err)
	}
	return tok
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

func TestListWorkflowsExposesSeededOrderDefault(t *testing.T) {
	h, issuer, _ := newServer(t)
	tok := tokenWith(t, issuer, "workflow.view")

	rr := do(t, h, http.MethodGet, "/admin/workflows", tok, nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("list: %d (%s)", rr.Code, rr.Body.String())
	}
	var resp struct {
		Items []struct {
			Code        string `json:"code"`
			States      []any  `json:"states"`
			Transitions []struct {
				Code   string          `json:"code"`
				From   string          `json:"from"`
				To     string          `json:"to"`
				Guards json.RawMessage `json:"guards"`
			} `json:"transitions"`
		} `json:"items"`
	}
	decode(t, rr, &resp)

	var order *struct {
		Code        string `json:"code"`
		States      []any  `json:"states"`
		Transitions []struct {
			Code   string          `json:"code"`
			From   string          `json:"from"`
			To     string          `json:"to"`
			Guards json.RawMessage `json:"guards"`
		} `json:"transitions"`
	}
	for i := range resp.Items {
		if resp.Items[i].Code == "order_default" {
			order = &resp.Items[i]
		}
	}
	if order == nil {
		t.Fatal("order_default workflow not exposed")
	}
	if len(order.States) != 8 {
		t.Errorf("order_default states: want 8, got %d", len(order.States))
	}
	// The confirm transition carries the amount_lte_limit guard as readable JSON.
	for _, tr := range order.Transitions {
		if tr.Code == "confirm" {
			if got := string(tr.Guards); got == "[]" || got == "" {
				t.Errorf("confirm guard should be present, got %q", got)
			}
		}
	}
}

func TestEditTransitionGuards(t *testing.T) {
	h, issuer, pool := newServer(t)
	tok := tokenWith(t, issuer, "workflow.view", "workflow.manage")
	ctx := context.Background()

	// Find the `process` transition id (confirmed→processing) of order_default.
	var tid int64
	if err := pool.QueryRow(ctx, `
		SELECT t.id FROM workflow_transitions t
		JOIN workflow_definitions d ON d.id=t.definition_id
		WHERE d.organization_id=1 AND d.code='order_default' AND t.code='process'`).Scan(&tid); err != nil {
		t.Fatalf("find transition: %v", err)
	}

	body := map[string]any{
		"guards":  []map[string]any{{"key": "has_permission", "params": map[string]any{"permission": "order.process"}}},
		"actions": []any{},
	}
	rr := do(t, h, http.MethodPatch, "/admin/workflow-transitions/"+strconv.FormatInt(tid, 10), tok, body)
	if rr.Code != http.StatusOK {
		t.Fatalf("update transition: %d (%s)", rr.Code, rr.Body.String())
	}

	// Persisted (read back the raw JSONB).
	var guards string
	_ = pool.QueryRow(ctx, `SELECT guards::text FROM workflow_transitions WHERE id=$1`, tid).Scan(&guards)
	if guards == "[]" || guards == "" {
		t.Errorf("guards not persisted, got %q", guards)
	}

	// Invalid (non-array) guards are rejected.
	bad := do(t, h, http.MethodPatch, "/admin/workflow-transitions/"+strconv.FormatInt(tid, 10), tok, map[string]any{"guards": map[string]any{"nope": true}})
	if bad.Code != http.StatusBadRequest {
		t.Errorf("non-array guards: want 400, got %d", bad.Code)
	}
}

func TestAutomationRuleCRUD(t *testing.T) {
	h, issuer, _ := newServer(t)
	view := tokenWith(t, issuer, "workflow.view")
	manage := tokenWith(t, issuer, "workflow.view", "workflow.manage")

	// Seeded rules (quote-expiry + order status) are listed.
	rr := do(t, h, http.MethodGet, "/admin/automation-rules", view, nil)
	var list struct {
		Items []struct {
			ID       int64 `json:"id"`
			IsActive bool  `json:"is_active"`
		} `json:"items"`
	}
	decode(t, rr, &list)
	if len(list.Items) < 2 {
		t.Fatalf("want >=2 seeded rules, got %d", len(list.Items))
	}

	// Create a rule.
	cr := do(t, h, http.MethodPost, "/admin/automation-rules", manage, map[string]any{
		"name": "Big deal alert", "trigger_event": "order.status_changed",
		"conditions": []map[string]any{{"field": "grand_total", "op": "gt", "value": 100000}},
		"actions":    []map[string]any{{"key": "email_customer", "params": map[string]any{"template": "order_status_update"}}},
	})
	if cr.Code != http.StatusCreated {
		t.Fatalf("create rule: %d (%s)", cr.Code, cr.Body.String())
	}
	var created struct {
		ID       int64 `json:"id"`
		IsActive bool  `json:"is_active"`
	}
	decode(t, cr, &created)
	if !created.IsActive {
		t.Error("new rule should default active")
	}

	// Toggle it off via PATCH.
	off := false
	up := do(t, h, http.MethodPatch, "/admin/automation-rules/"+strconv.FormatInt(created.ID, 10), manage, map[string]any{"is_active": off})
	if up.Code != http.StatusOK {
		t.Fatalf("update rule: %d (%s)", up.Code, up.Body.String())
	}
	var updated struct {
		IsActive bool `json:"is_active"`
	}
	decode(t, up, &updated)
	if updated.IsActive {
		t.Error("rule should be toggled inactive")
	}
}

func TestWorkflowAdminAuth(t *testing.T) {
	h, issuer, _ := newServer(t)
	// Missing workflow.view → 403.
	if rr := do(t, h, http.MethodGet, "/admin/workflows", tokenWith(t, issuer, "order.view"), nil); rr.Code != http.StatusForbidden {
		t.Errorf("no permission: want 403, got %d", rr.Code)
	}
	// Storefront token → 403 (wrong audience).
	cust, _ := issuer.IssueStorefront(0, 1, 1)
	if rr := do(t, h, http.MethodGet, "/admin/automation-rules", cust, nil); rr.Code != http.StatusForbidden {
		t.Errorf("storefront token: want 403, got %d", rr.Code)
	}
}
