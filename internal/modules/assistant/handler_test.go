package assistant_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"b2bcommerce/internal/auth"
	"b2bcommerce/internal/server"
	"b2bcommerce/internal/store"
	"b2bcommerce/internal/store/gen"
	"b2bcommerce/internal/testsupport"
)

const secret = "test-secret-please-change"
const pw = "buyer-pass-123"

func newServer(t *testing.T) (http.Handler, *pgxpool.Pool, *auth.Issuer) {
	t.Helper()
	pool := testsupport.NewDB(t)
	return server.New(store.New(pool), auth.NewIssuer(secret, time.Hour)), pool, auth.NewIssuer(secret, time.Hour)
}

func post(t *testing.T, h http.Handler, path, tok string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	_ = json.NewEncoder(&buf).Encode(body)
	r := httptest.NewRequest(http.MethodPost, path, &buf)
	if tok != "" {
		r.Header.Set("Authorization", "Bearer "+tok)
	}
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, r)
	return rr
}

type reply struct {
	Text string         `json:"text"`
	Tool string         `json:"tool"`
	Data map[string]any `json:"data"`
}

func ask(t *testing.T, h http.Handler, path, tok, msg string) reply {
	t.Helper()
	rr := post(t, h, path, tok, map[string]any{"message": msg})
	if rr.Code != http.StatusOK {
		t.Fatalf("ask %q: want 200, got %d (%s)", msg, rr.Code, rr.Body.String())
	}
	var rp reply
	_ = json.Unmarshal(rr.Body.Bytes(), &rp)
	return rp
}

// seedBuyer creates a customer + buyer login and returns (customerID, storefront token).
func seedBuyer(t *testing.T, h http.Handler, pool *pgxpool.Pool, email string) (int64, string) {
	t.Helper()
	q := gen.New(pool)
	ctx := context.Background()
	cust, err := q.CreateCustomer(ctx, gen.CreateCustomerParams{OrganizationID: 1, Name: "Buyer Co", CreditLimit: "0"})
	if err != nil {
		t.Fatalf("customer: %v", err)
	}
	hash, _ := auth.HashPassword(pw)
	if _, err := q.CreateCustomerUser(ctx, gen.CreateCustomerUserParams{
		CustomerID: cust.ID, Email: email, PasswordHash: hash, FullName: "Buyer", Role: "buyer",
	}); err != nil {
		t.Fatalf("customer user: %v", err)
	}
	rr := post(t, h, "/storefront/auth/login", "", map[string]any{"email": email, "password": pw, "org_id": 1})
	if rr.Code != http.StatusOK {
		t.Fatalf("login: %d (%s)", rr.Code, rr.Body.String())
	}
	var lr struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &lr)
	return cust.ID, lr.Token
}

func TestStorefrontAssistant(t *testing.T) {
	h, pool, _ := newServer(t)
	custID, tok := seedBuyer(t, h, pool, "buyer@acme.test")

	// Seed an order so list_orders/order_status have data.
	if _, err := gen.New(pool).CreateOrder(context.Background(), gen.CreateOrderParams{
		OrganizationID: 1, WebsiteID: 1, CustomerID: custID, Currency: "USD",
		BillingAddress: []byte("{}"), ShippingAddress: []byte("{}"),
		Subtotal: "500", TaxTotal: "0", ShippingTotal: "0", GrandTotal: "500",
	}); err != nil {
		t.Fatalf("order: %v", err)
	}

	if rp := ask(t, h, "/storefront/assistant", tok, "show my recent orders"); rp.Tool != "list_orders" {
		t.Errorf("list_orders: got tool=%q text=%q", rp.Tool, rp.Text)
	}
	if rp := ask(t, h, "/storefront/assistant", tok, "what do I owe?"); rp.Tool != "outstanding_invoices" {
		t.Errorf("owe: got tool=%q text=%q", rp.Tool, rp.Text)
	}
	if rp := ask(t, h, "/storefront/assistant", tok, "help"); rp.Text == "" {
		t.Errorf("help returned empty text")
	}
}

func TestAdminAssistantAndGating(t *testing.T) {
	h, pool, issuer := newServer(t)
	_, buyerTok := seedBuyer(t, h, pool, "b@acme.test")
	adminTok, _ := issuer.Issue("1", 1, "admin", []string{"invoice.view", "crm.view"})

	// Admin tool runs with permission.
	if rp := ask(t, h, "/admin/assistant", adminTok, "show me the receivables aging"); rp.Tool != "ar_aging" {
		t.Errorf("ar_aging: got tool=%q text=%q", rp.Tool, rp.Text)
	}

	// Audience gating: buyer token rejected on admin endpoint, admin on storefront.
	if rr := post(t, h, "/admin/assistant", buyerTok, map[string]any{"message": "aging"}); rr.Code != http.StatusForbidden {
		t.Errorf("buyer on admin: want 403, got %d", rr.Code)
	}
	if rr := post(t, h, "/storefront/assistant", adminTok, map[string]any{"message": "my orders"}); rr.Code != http.StatusForbidden {
		t.Errorf("admin on storefront: want 403, got %d", rr.Code)
	}

	// Admin WITHOUT the permission can't trigger the gated tool.
	weakTok, _ := issuer.Issue("1", 1, "admin", []string{})
	if rp := ask(t, h, "/admin/assistant", weakTok, "show me the receivables aging"); rp.Tool == "ar_aging" {
		t.Error("ar_aging reachable without invoice.view")
	}
}
