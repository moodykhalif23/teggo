package field_test

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
	"b2bcommerce/internal/store/gen"
	"b2bcommerce/internal/testsupport"
)

const (
	testSecret = "field-test-secret"
	deviceA    = "11111111-1111-1111-1111-111111111111"
	deviceB    = "22222222-2222-2222-2222-222222222222"
)

func newServer(t *testing.T) (http.Handler, *auth.Issuer, *pgxpool.Pool) {
	t.Helper()
	pool := testsupport.NewDB(t)
	st := store.New(pool)
	issuer := auth.NewIssuer(testSecret, time.Hour)
	return server.New(st, issuer), issuer, pool
}

// repToken issues an admin token for user id 1 (the rep) with the perms the
// field flow + seeding need.
func repToken(t *testing.T, issuer *auth.Issuer) string {
	t.Helper()
	tok, _ := issuer.Issue("1", 1, "admin", []string{"field.sync", "customer.view", "customer.manage", "product.view", "product.manage"})
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

type pullResp struct {
	Cursor  int64 `json:"cursor"`
	Changes []struct {
		EntityType string          `json:"entity_type"`
		EntityID   int64           `json:"entity_id"`
		Op         string          `json:"op"`
		Payload    json.RawMessage `json:"payload"`
	} `json:"changes"`
}

func pull(t *testing.T, h http.Handler, tok, device string, since int64) pullResp {
	t.Helper()
	rr := do(t, h, http.MethodGet, "/field/sync/pull?device="+device+"&since="+strconv.FormatInt(since, 10), tok, nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("pull: %d (%s)", rr.Code, rr.Body.String())
	}
	var pr pullResp
	_ = json.Unmarshal(rr.Body.Bytes(), &pr)
	return pr
}

type pushResp struct {
	Cursor  int64 `json:"cursor"`
	Results []struct {
		ClientChangeID string          `json:"client_change_id"`
		Status         string          `json:"status"`
		ServerEntityID *int64          `json:"server_entity_id"`
		ServerRecord   json.RawMessage `json:"server_record"`
		Replayed       bool            `json:"replayed"`
		Detail         string          `json:"detail"`
	} `json:"results"`
}

func push(t *testing.T, h http.Handler, tok, device string, changes []map[string]any) pushResp {
	t.Helper()
	rr := do(t, h, http.MethodPost, "/field/sync/push", tok, map[string]any{"device_uuid": device, "changes": changes})
	if rr.Code != http.StatusOK {
		t.Fatalf("push: %d (%s)", rr.Code, rr.Body.String())
	}
	var pr pushResp
	_ = json.Unmarshal(rr.Body.Bytes(), &pr)
	return pr
}

func TestFieldSyncReadSideScoping(t *testing.T) {
	h, issuer, _ := newServer(t)
	tok := repToken(t, issuer)

	// Admin creates a customer assigned to rep 1 (rep-scoped) and a product
	// (global) — both should appear in rep 1's pull via change_log.
	if rr := do(t, h, http.MethodPost, "/admin/customers", tok, map[string]any{"name": "Field Cust", "assigned_sales_rep_id": 1}); rr.Code != http.StatusCreated {
		t.Fatalf("create customer: %d (%s)", rr.Code, rr.Body.String())
	}
	if rr := do(t, h, http.MethodPost, "/admin/products", tok, map[string]any{"sku": "FLD-1", "type": "simple", "name": "Field Widget", "slug": "fld-1", "status": "active", "unit": "each", "attributes": map[string]any{}}); rr.Code != http.StatusCreated {
		t.Fatalf("create product: %d (%s)", rr.Code, rr.Body.String())
	}

	pr := pull(t, h, tok, deviceA, 0)
	var sawCustomer, sawProduct bool
	for _, c := range pr.Changes {
		if c.EntityType == "customer" {
			sawCustomer = true
		}
		if c.EntityType == "product" {
			sawProduct = true
		}
	}
	if !sawCustomer || !sawProduct {
		t.Fatalf("pull missing seeded changes: customer=%v product=%v (%d changes)", sawCustomer, sawProduct, len(pr.Changes))
	}
	if pr.Cursor == 0 {
		t.Fatal("cursor should advance past 0")
	}

	// Pulling again from the high-water returns nothing new.
	pr2 := pull(t, h, tok, deviceA, pr.Cursor)
	if len(pr2.Changes) != 0 {
		t.Errorf("incremental pull should be empty, got %d", len(pr2.Changes))
	}
}

func TestFieldSyncPushIdempotentAndConflict(t *testing.T) {
	h, issuer, pool := newServer(t)
	tok := repToken(t, issuer)

	// Create an activity offline.
	create := push(t, h, tok, deviceA, []map[string]any{
		{"client_change_id": "aaaaaaaa-0000-0000-0000-000000000001", "entity_type": "activity", "op": "upsert",
			"payload": map[string]any{"type": "note", "subject": "Site visit"}},
	})
	if len(create.Results) != 1 || create.Results[0].Status != "applied" || create.Results[0].ServerEntityID == nil {
		t.Fatalf("activity create: %+v", create.Results)
	}
	actID := *create.Results[0].ServerEntityID

	// Replaying the same client_change_id applies nothing new, returns same id.
	replay := push(t, h, tok, deviceA, []map[string]any{
		{"client_change_id": "aaaaaaaa-0000-0000-0000-000000000001", "entity_type": "activity", "op": "upsert",
			"payload": map[string]any{"type": "note", "subject": "Site visit CHANGED"}},
	})
	if replay.Results[0].Status != "applied" || !replay.Results[0].Replayed || *replay.Results[0].ServerEntityID != actID {
		t.Fatalf("replay not idempotent: %+v", replay.Results[0])
	}
	// The subject was NOT overwritten by the replay.
	act, _ := gen.New(pool).GetActivity(context.Background(), gen.GetActivityParams{OrganizationID: 1, ID: actID})
	if act.Subject != "Site visit" {
		t.Errorf("replay mutated the record: subject=%q", act.Subject)
	}

	// A stale edit (base_updated_at in the past) conflicts and returns the server record.
	conflict := push(t, h, tok, deviceA, []map[string]any{
		{"client_change_id": "bbbbbbbb-0000-0000-0000-000000000001", "entity_type": "activity", "op": "upsert",
			"base_updated_at": "2000-01-01T00:00:00Z",
			"payload":         map[string]any{"id": actID, "type": "note", "subject": "Stale edit"}},
	})
	if conflict.Results[0].Status != "conflict" || len(conflict.Results[0].ServerRecord) == 0 {
		t.Fatalf("expected conflict with server record, got %+v", conflict.Results[0])
	}

	// Read-only entity push is refused.
	ro := push(t, h, tok, deviceA, []map[string]any{
		{"client_change_id": "cccccccc-0000-0000-0000-000000000001", "entity_type": "product", "op": "upsert",
			"payload": map[string]any{"sku": "X"}},
	})
	if ro.Results[0].Status != "rejected" {
		t.Errorf("read-only push: want rejected, got %q", ro.Results[0].Status)
	}
}

func TestFieldSyncOrderCreateAndMultiDevice(t *testing.T) {
	h, issuer, pool := newServer(t)
	tok := repToken(t, issuer)
	ctx := context.Background()
	q := gen.New(pool)

	cust, _ := q.CreateCustomer(ctx, gen.CreateCustomerParams{OrganizationID: 1, Name: "Buyer", CreditLimit: "0"})
	if _, err := q.CreateProduct(ctx, gen.CreateProductParams{OrganizationID: 1, Sku: "ORD-SKU", Type: "simple", Name: "Orderable", Slug: "ord-sku", Status: "active", Attributes: []byte("{}"), Unit: "each"}); err != nil {
		t.Fatalf("product: %v", err)
	}

	// Rep creates an order offline on device A.
	res := push(t, h, tok, deviceA, []map[string]any{
		{"client_change_id": "dddddddd-0000-0000-0000-000000000001", "entity_type": "order", "op": "upsert",
			"payload": map[string]any{"customer_id": cust.ID, "currency": "USD",
				"lines": []map[string]any{{"sku": "ORD-SKU", "quantity": "5", "unit_price": "8.0000"}}}},
	})
	if res.Results[0].Status != "applied" || res.Results[0].ServerEntityID == nil {
		t.Fatalf("order create: %+v", res.Results[0])
	}
	orderID := *res.Results[0].ServerEntityID
	o, err := q.GetOrderByID(ctx, gen.GetOrderByIDParams{OrganizationID: 1, ID: orderID})
	if err != nil || o.GrandTotal != "40.0000" {
		t.Fatalf("order total: %+v err=%v", o, err)
	}

	// Device B (same rep) pulls and sees the order change.
	pr := pull(t, h, tok, deviceB, 0)
	var sawOrder bool
	for _, c := range pr.Changes {
		if c.EntityType == "order" && c.EntityID == orderID {
			sawOrder = true
		}
	}
	if !sawOrder {
		t.Errorf("device B did not pull the order change")
	}
}

func TestFieldSyncAuth(t *testing.T) {
	h, issuer, _ := newServer(t)
	// storefront token → 403 (wrong audience / no perm).
	cust, _ := issuer.IssueStorefront(0, 1, 1)
	if rr := do(t, h, http.MethodGet, "/field/sync/pull?device="+deviceA, cust, nil); rr.Code != http.StatusForbidden {
		t.Errorf("storefront token: want 403, got %d", rr.Code)
	}
	// admin without field.sync → 403.
	noPerm, _ := issuer.Issue("1", 1, "admin", []string{"order.view"})
	if rr := do(t, h, http.MethodGet, "/field/sync/pull?device="+deviceA, noPerm, nil); rr.Code != http.StatusForbidden {
		t.Errorf("missing field.sync: want 403, got %d", rr.Code)
	}
}
