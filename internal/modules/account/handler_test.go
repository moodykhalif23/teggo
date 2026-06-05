package account_test

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

const testSecret = "test-secret-please-change"
const custPassword = "buyer-pass-123"

func newServer(t *testing.T) (http.Handler, *pgxpool.Pool) {
	t.Helper()
	pool := testsupport.NewDB(t)
	st := store.New(pool)
	issuer := auth.NewIssuer(testSecret, time.Hour)
	return server.New(st, issuer), pool
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

// seedCustomer creates a customer company + one buyer login.
func seedCustomer(t *testing.T, pool *pgxpool.Pool, name, email string) int64 {
	t.Helper()
	q := gen.New(pool)
	ctx := context.Background()
	cust, err := q.CreateCustomer(ctx, gen.CreateCustomerParams{OrganizationID: 1, Name: name, CreditLimit: "0"})
	if err != nil {
		t.Fatalf("customer: %v", err)
	}
	hash, _ := auth.HashPassword(custPassword)
	if _, err := q.CreateCustomerUser(ctx, gen.CreateCustomerUserParams{
		CustomerID: cust.ID, Email: email, PasswordHash: hash, FullName: name + " Buyer", Role: "buyer",
	}); err != nil {
		t.Fatalf("customer user: %v", err)
	}
	return cust.ID
}

// addUser adds another customer-user to an existing company with a given role.
func addUser(t *testing.T, pool *pgxpool.Pool, customerID int64, email, fullName, role string) int64 {
	t.Helper()
	q := gen.New(pool)
	hash, _ := auth.HashPassword(custPassword)
	u, err := q.CreateCustomerUser(context.Background(), gen.CreateCustomerUserParams{
		CustomerID: customerID, Email: email, PasswordHash: hash, FullName: fullName, Role: role,
	})
	if err != nil {
		t.Fatalf("add user %s: %v", email, err)
	}
	return u.ID
}

// seedHeldOrder creates an order in on_hold status (awaiting approval) for a
// company, placed by the given customer-user.
func seedHeldOrder(t *testing.T, pool *pgxpool.Pool, customerID int64, placedBy *int64) gen.Order {
	return seedHeldOrderAmount(t, pool, customerID, placedBy, "1000")
}

func seedHeldOrderAmount(t *testing.T, pool *pgxpool.Pool, customerID int64, placedBy *int64, amount string) gen.Order {
	t.Helper()
	q := gen.New(pool)
	ctx := context.Background()
	o, err := q.CreateOrder(ctx, gen.CreateOrderParams{
		OrganizationID: 1, WebsiteID: 1, CustomerID: customerID, CustomerUserID: placedBy, Currency: "USD",
		BillingAddress: []byte("{}"), ShippingAddress: []byte("{}"),
		Subtotal: amount, TaxTotal: "0", ShippingTotal: "0", GrandTotal: amount,
	})
	if err != nil {
		t.Fatalf("create order: %v", err)
	}
	if _, err := q.SetOrderStatus(ctx, gen.SetOrderStatusParams{ID: o.ID, Status: "on_hold"}); err != nil {
		t.Fatalf("hold order: %v", err)
	}
	o.Status = "on_hold"
	return o
}

func login(t *testing.T, h http.Handler, email string) string {
	t.Helper()
	rr := do(t, h, http.MethodPost, "/storefront/auth/login", "", map[string]any{"email": email, "password": custPassword, "org_id": 1})
	if rr.Code != http.StatusOK {
		t.Fatalf("login %s: want 200, got %d (%s)", email, rr.Code, rr.Body.String())
	}
	var resp struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)
	return resp.Token
}

func TestAccountAddressesCreateListAndIsolation(t *testing.T) {
	h, pool := newServer(t)
	seedCustomer(t, pool, "acme", "buyer@acme.test")
	bID := seedCustomer(t, pool, "beta", "buyer@beta.test")
	_ = bID
	tokA := login(t, h, "buyer@acme.test")
	tokB := login(t, h, "buyer@beta.test")

	// A creates a shipping address.
	cr := do(t, h, http.MethodPost, "/storefront/account/addresses", tokA, map[string]any{
		"type": "shipping", "is_default": true, "line1": "1 Market St", "city": "SF", "country": "US",
	})
	if cr.Code != http.StatusCreated {
		t.Fatalf("create address: want 201, got %d (%s)", cr.Code, cr.Body.String())
	}

	// A sees exactly their one address.
	la := do(t, h, http.MethodGet, "/storefront/account/addresses", tokA, nil)
	var ra struct {
		Items []gen.CustomerAddress `json:"items"`
	}
	_ = json.Unmarshal(la.Body.Bytes(), &ra)
	if len(ra.Items) != 1 || ra.Items[0].City != "SF" {
		t.Fatalf("A addresses: want 1 (SF), got %+v", ra.Items)
	}

	// B's address list is independent and empty — no cross-company leakage.
	lb := do(t, h, http.MethodGet, "/storefront/account/addresses", tokB, nil)
	var rb struct {
		Items []gen.CustomerAddress `json:"items"`
	}
	_ = json.Unmarshal(lb.Body.Bytes(), &rb)
	if len(rb.Items) != 0 {
		t.Errorf("B addresses: want 0, got %d", len(rb.Items))
	}
}

func TestAccountAddressValidation(t *testing.T) {
	h, pool := newServer(t)
	seedCustomer(t, pool, "acme", "buyer@acme.test")
	tok := login(t, h, "buyer@acme.test")

	// Missing line1/city/country.
	if rr := do(t, h, http.MethodPost, "/storefront/account/addresses", tok, map[string]any{"type": "shipping"}); rr.Code != http.StatusBadRequest {
		t.Errorf("invalid address: want 400, got %d", rr.Code)
	}
	// Bad type.
	if rr := do(t, h, http.MethodPost, "/storefront/account/addresses", tok, map[string]any{
		"type": "warehouse", "line1": "x", "city": "y", "country": "US",
	}); rr.Code != http.StatusBadRequest {
		t.Errorf("bad type: want 400, got %d", rr.Code)
	}
}

func TestCompanyProfileAndUserManagement(t *testing.T) {
	h, pool := newServer(t)
	custID := seedCustomer(t, pool, "acme", "buyer@acme.test") // role buyer
	addUser(t, pool, custID, "admin@acme.test", "Acme Admin", "admin")
	adminTok := login(t, h, "admin@acme.test")

	// Company profile shows the company and the caller's own user.
	comp := do(t, h, http.MethodGet, "/storefront/account/company", adminTok, nil)
	if comp.Code != http.StatusOK {
		t.Fatalf("company: want 200, got %d (%s)", comp.Code, comp.Body.String())
	}
	var cp struct {
		Company struct {
			Name string `json:"name"`
		} `json:"company"`
		Me struct {
			ID   int64  `json:"id"`
			Role string `json:"role"`
		} `json:"me"`
	}
	_ = json.Unmarshal(comp.Body.Bytes(), &cp)
	if cp.Company.Name != "acme" || cp.Me.Role != "admin" {
		t.Fatalf("company profile: got %+v", cp)
	}

	// Admin lists users (buyer + admin = 2).
	lu := do(t, h, http.MethodGet, "/storefront/account/users", adminTok, nil)
	var users struct {
		Items []gen.ListCustomerUsersRow `json:"items"`
	}
	_ = json.Unmarshal(lu.Body.Bytes(), &users)
	if len(users.Items) != 2 {
		t.Fatalf("list users: want 2, got %d", len(users.Items))
	}

	// Admin invites a new user.
	cr := do(t, h, http.MethodPost, "/storefront/account/users", adminTok, map[string]any{
		"email": "buyer2@acme.test", "password": "pw-123456", "full_name": "Buyer Two", "role": "approver",
	})
	if cr.Code != http.StatusCreated {
		t.Fatalf("create user: want 201, got %d (%s)", cr.Code, cr.Body.String())
	}
	// Duplicate email is rejected.
	if dup := do(t, h, http.MethodPost, "/storefront/account/users", adminTok, map[string]any{
		"email": "buyer2@acme.test", "password": "pw-123456", "full_name": "Dup",
	}); dup.Code != http.StatusConflict {
		t.Errorf("duplicate email: want 409, got %d", dup.Code)
	}

	// Find the buyer user and update role + spending limit.
	var buyerID int64
	for _, u := range users.Items {
		if u.Email == "buyer@acme.test" {
			buyerID = u.ID
		}
	}
	limit := "500.00"
	up := do(t, h, http.MethodPatch, "/storefront/account/users/"+strconv.FormatInt(buyerID, 10), adminTok, map[string]any{
		"role": "approver", "spending_limit": limit,
	})
	if up.Code != http.StatusOK {
		t.Fatalf("update user: want 200, got %d (%s)", up.Code, up.Body.String())
	}
	var updated struct {
		Role string `json:"role"`
	}
	_ = json.Unmarshal(up.Body.Bytes(), &updated)
	if updated.Role != "approver" {
		t.Errorf("update role: want approver, got %s", updated.Role)
	}

	// Lockout guard: admin cannot strip their own admin role.
	self := do(t, h, http.MethodPatch, "/storefront/account/users/"+strconv.FormatInt(cp.Me.ID, 10), adminTok, map[string]any{"role": "buyer"})
	if self.Code != http.StatusBadRequest {
		t.Errorf("self-demote: want 400, got %d (%s)", self.Code, self.Body.String())
	}
}

func TestNonAdminCannotManageUsers(t *testing.T) {
	h, pool := newServer(t)
	seedCustomer(t, pool, "acme", "buyer@acme.test") // role buyer
	tok := login(t, h, "buyer@acme.test")

	if rr := do(t, h, http.MethodGet, "/storefront/account/users", tok, nil); rr.Code != http.StatusForbidden {
		t.Errorf("buyer list users: want 403, got %d", rr.Code)
	}
	if rr := do(t, h, http.MethodPost, "/storefront/account/users", tok, map[string]any{
		"email": "x@acme.test", "password": "pw-123456", "full_name": "X",
	}); rr.Code != http.StatusForbidden {
		t.Errorf("buyer create user: want 403, got %d", rr.Code)
	}
}

func TestOrderApprovalFlow(t *testing.T) {
	h, pool := newServer(t)
	custID := seedCustomer(t, pool, "acme", "buyer@acme.test")
	buyerID := addUser(t, pool, custID, "junior@acme.test", "Junior Buyer", "buyer")
	addUser(t, pool, custID, "approver@acme.test", "The Approver", "approver")
	approverTok := login(t, h, "approver@acme.test")

	// Two held orders placed by the junior buyer.
	o1 := seedHeldOrder(t, pool, custID, &buyerID)
	o2 := seedHeldOrder(t, pool, custID, &buyerID)

	// Approver sees both pending approvals.
	la := do(t, h, http.MethodGet, "/storefront/account/approvals", approverTok, nil)
	if la.Code != http.StatusOK {
		t.Fatalf("list approvals: want 200, got %d (%s)", la.Code, la.Body.String())
	}
	var list struct {
		Items []struct {
			PublicID string `json:"public_id"`
		} `json:"items"`
	}
	_ = json.Unmarshal(la.Body.Bytes(), &list)
	if len(list.Items) != 2 {
		t.Fatalf("approvals: want 2, got %d", len(list.Items))
	}

	// Approve o1 -> pending.
	ap := do(t, h, http.MethodPost, "/storefront/account/approvals/"+o1.PublicID.String()+"/approve", approverTok, nil)
	if ap.Code != http.StatusOK {
		t.Fatalf("approve: want 200, got %d (%s)", ap.Code, ap.Body.String())
	}
	var ar struct {
		Status string `json:"status"`
	}
	_ = json.Unmarshal(ap.Body.Bytes(), &ar)
	if ar.Status != "pending" {
		t.Errorf("approved status: want pending, got %s", ar.Status)
	}

	// Reject o2 -> cancelled.
	rj := do(t, h, http.MethodPost, "/storefront/account/approvals/"+o2.PublicID.String()+"/reject", approverTok, nil)
	if rj.Code != http.StatusOK {
		t.Fatalf("reject: want 200, got %d (%s)", rj.Code, rj.Body.String())
	}

	// Approving again is a conflict (no longer on_hold).
	if again := do(t, h, http.MethodPost, "/storefront/account/approvals/"+o1.PublicID.String()+"/approve", approverTok, nil); again.Code != http.StatusConflict {
		t.Errorf("re-approve: want 409, got %d", again.Code)
	}

	// No approvals remain.
	la2 := do(t, h, http.MethodGet, "/storefront/account/approvals", approverTok, nil)
	_ = json.Unmarshal(la2.Body.Bytes(), &list)
	if len(list.Items) != 0 {
		t.Errorf("after decisions: want 0 approvals, got %d", len(list.Items))
	}
}

func TestApprovalAuthorizationAndSeparationOfDuties(t *testing.T) {
	h, pool := newServer(t)
	custID := seedCustomer(t, pool, "acme", "buyer@acme.test") // role buyer
	approverID := addUser(t, pool, custID, "approver@acme.test", "The Approver", "approver")
	buyerTok := login(t, h, "buyer@acme.test")
	approverTok := login(t, h, "approver@acme.test")

	// A plain buyer cannot view or act on approvals.
	if rr := do(t, h, http.MethodGet, "/storefront/account/approvals", buyerTok, nil); rr.Code != http.StatusForbidden {
		t.Errorf("buyer list approvals: want 403, got %d", rr.Code)
	}

	// Separation of duties: the approver cannot approve an order they placed.
	own := seedHeldOrder(t, pool, custID, &approverID)
	if rr := do(t, h, http.MethodPost, "/storefront/account/approvals/"+own.PublicID.String()+"/approve", approverTok, nil); rr.Code != http.StatusForbidden {
		t.Errorf("approve own order: want 403, got %d (%s)", rr.Code, rr.Body.String())
	}

	// Cross-company isolation: approver from another company gets 404.
	otherID := seedCustomer(t, pool, "beta", "buyer@beta.test")
	addUser(t, pool, otherID, "approver@beta.test", "Beta Approver", "approver")
	betaTok := login(t, h, "approver@beta.test")
	o := seedHeldOrder(t, pool, custID, nil)
	if rr := do(t, h, http.MethodPost, "/storefront/account/approvals/"+o.PublicID.String()+"/approve", betaTok, nil); rr.Code != http.StatusNotFound {
		t.Errorf("foreign approve: want 404, got %d", rr.Code)
	}
}

func TestApprovalRoutingTierRequiresHigherRole(t *testing.T) {
	h, pool := newServer(t)
	custID := seedCustomer(t, pool, "acme", "buyer@acme.test")
	buyerID := addUser(t, pool, custID, "junior@acme.test", "Junior", "buyer")
	addUser(t, pool, custID, "approver@acme.test", "Approver", "approver")
	addUser(t, pool, custID, "admin@acme.test", "Admin", "admin")

	// Org tier: orders >= 500 require an admin to release.
	max := (*string)(nil)
	if _, err := gen.New(pool).CreateApprovalRoutingRule(context.Background(), gen.CreateApprovalRoutingRuleParams{
		OrganizationID: 1, MinAmount: "500", MaxAmount: max, RequiredRole: "admin", SortOrder: 0,
	}); err != nil {
		t.Fatalf("routing rule: %v", err)
	}

	approverTok := login(t, h, "approver@acme.test")
	adminTok := login(t, h, "admin@acme.test")

	// A 1000 order is in the admin tier — an approver may NOT release it.
	big := seedHeldOrderAmount(t, pool, custID, &buyerID, "1000")
	if rr := do(t, h, http.MethodPost, "/storefront/account/approvals/"+big.PublicID.String()+"/approve", approverTok, nil); rr.Code != http.StatusForbidden {
		t.Errorf("approver on admin-tier order: want 403, got %d (%s)", rr.Code, rr.Body.String())
	}
	// But an admin can.
	if rr := do(t, h, http.MethodPost, "/storefront/account/approvals/"+big.PublicID.String()+"/approve", adminTok, nil); rr.Code != http.StatusOK {
		t.Errorf("admin on admin-tier order: want 200, got %d (%s)", rr.Code, rr.Body.String())
	}

	// A 100 order is below the tier — an approver may release it (fallback).
	small := seedHeldOrderAmount(t, pool, custID, &buyerID, "100")
	if rr := do(t, h, http.MethodPost, "/storefront/account/approvals/"+small.PublicID.String()+"/approve", approverTok, nil); rr.Code != http.StatusOK {
		t.Errorf("approver on sub-tier order: want 200, got %d (%s)", rr.Code, rr.Body.String())
	}
}

func TestAccountRequiresStorefrontToken(t *testing.T) {
	h, _ := newServer(t)
	if rr := do(t, h, http.MethodGet, "/storefront/account/addresses", "", nil); rr.Code != http.StatusUnauthorized {
		t.Errorf("no token: want 401, got %d", rr.Code)
	}
}

// ---- reorder suggestions (replenishment) ---------------------------------

func TestReorderSuggestions(t *testing.T) {
	h, pool := newServer(t)
	custID := seedCustomer(t, pool, "acme", "buyer@acme.test")
	q := gen.New(pool)
	ctx := context.Background()

	prod, err := q.CreateProduct(ctx, gen.CreateProductParams{
		OrganizationID: 1, Sku: "REORD-1", Type: "simple", Name: "Reorder Widget", Slug: "reord-1",
		Status: "active", Attributes: []byte("{}"), Unit: "each",
	})
	if err != nil {
		t.Fatalf("product: %v", err)
	}
	// Three orders at -100, -70, -45 days: span 55d / 2 intervals = ~27d avg,
	// last was 45d ago -> due (overdue ~18d).
	for _, ago := range []int{100, 70, 45} {
		o, err := q.CreateOrder(ctx, gen.CreateOrderParams{
			OrganizationID: 1, WebsiteID: 1, CustomerID: custID, Currency: "USD",
			BillingAddress: []byte("{}"), ShippingAddress: []byte("{}"),
			Subtotal: "10", TaxTotal: "0", ShippingTotal: "0", GrandTotal: "10",
		})
		if err != nil {
			t.Fatalf("order: %v", err)
		}
		if _, err := q.AddOrderItem(ctx, gen.AddOrderItemParams{
			OrderID: o.ID, ProductID: prod.ID, Sku: prod.Sku, Name: prod.Name,
			Quantity: "1", Unit: "each", UnitPrice: "10", TaxAmount: "0", RowTotal: "10",
		}); err != nil {
			t.Fatalf("order item: %v", err)
		}
		if _, err := pool.Exec(ctx, `UPDATE orders SET created_at = now() - make_interval(days => $1) WHERE id = $2`, ago, o.ID); err != nil {
			t.Fatalf("backdate: %v", err)
		}
	}

	tok := login(t, h, "buyer@acme.test")
	rr := do(t, h, http.MethodGet, "/storefront/account/reorder-suggestions", tok, nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("suggestions: %d (%s)", rr.Code, rr.Body.String())
	}
	var resp struct {
		Items []struct {
			Slug            string `json:"slug"`
			AvgIntervalDays int    `json:"avg_interval_days"`
			DaysSince       int    `json:"days_since"`
			DaysOverdue     int    `json:"days_overdue"`
		} `json:"items"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)
	if len(resp.Items) != 1 || resp.Items[0].Slug != "reord-1" {
		t.Fatalf("want 1 suggestion for reord-1, got %+v", resp.Items)
	}
	s := resp.Items[0]
	if s.AvgIntervalDays < 25 || s.AvgIntervalDays > 30 {
		t.Errorf("avg interval: want ~27, got %d", s.AvgIntervalDays)
	}
	if s.DaysSince < 44 || s.DaysSince > 46 || s.DaysOverdue <= 0 {
		t.Errorf("due metrics: want ~45 days since and overdue>0, got since=%d overdue=%d", s.DaysSince, s.DaysOverdue)
	}
}

func TestReorderSuggestionsNeedsTwoOrders(t *testing.T) {
	h, pool := newServer(t)
	custID := seedCustomer(t, pool, "beta", "buyer@beta.test")
	q := gen.New(pool)
	ctx := context.Background()
	prod, _ := q.CreateProduct(ctx, gen.CreateProductParams{OrganizationID: 1, Sku: "ONCE-1", Type: "simple", Name: "Once", Slug: "once-1", Status: "active", Attributes: []byte("{}"), Unit: "each"})
	o, _ := q.CreateOrder(ctx, gen.CreateOrderParams{OrganizationID: 1, WebsiteID: 1, CustomerID: custID, Currency: "USD", BillingAddress: []byte("{}"), ShippingAddress: []byte("{}"), Subtotal: "10", TaxTotal: "0", ShippingTotal: "0", GrandTotal: "10"})
	_, _ = q.AddOrderItem(ctx, gen.AddOrderItemParams{OrderID: o.ID, ProductID: prod.ID, Sku: prod.Sku, Name: prod.Name, Quantity: "1", Unit: "each", UnitPrice: "10", TaxAmount: "0", RowTotal: "10"})

	tok := login(t, h, "buyer@beta.test")
	rr := do(t, h, http.MethodGet, "/storefront/account/reorder-suggestions", tok, nil)
	var resp struct {
		Items []any `json:"items"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)
	if len(resp.Items) != 0 {
		t.Errorf("single-order product should not suggest reorder, got %d", len(resp.Items))
	}
}
