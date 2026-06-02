package sales_test

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
	testSecret   = "test-secret-please-change"
	custPassword = "buyer-pass-123"
)

type fixture struct {
	h          http.Handler
	issuer     *auth.Issuer
	pool       *pgxpool.Pool
	adminTok   string
	custTok    string
	customerID int64
	p1ID       int64
	p1Public   string
	p2ID       int64
	p2Public   string
}

func setup(t *testing.T) fixture {
	t.Helper()
	pool := testsupport.NewDB(t)
	st := store.New(pool)
	issuer := auth.NewIssuer(testSecret, time.Hour)
	h := server.New(st, issuer)
	q := gen.New(pool)
	ctx := context.Background()

	adminTok, _ := issuer.Issue("1", 1, "admin", []string{
		"rfq.view", "rfq.manage", "quote.view", "quote.manage", "order.view", "order.manage",
	})

	cust, err := q.CreateCustomer(ctx, gen.CreateCustomerParams{OrganizationID: 1, Name: "Acme", CreditLimit: "0"})
	if err != nil {
		t.Fatalf("customer: %v", err)
	}
	hash, _ := auth.HashPassword(custPassword)
	if _, err := q.CreateCustomerUser(ctx, gen.CreateCustomerUserParams{
		CustomerID: cust.ID, Email: "buyer@acme.test", PasswordHash: hash, FullName: "Acme Buyer", Role: "buyer",
	}); err != nil {
		t.Fatalf("customer user: %v", err)
	}
	for _, typ := range []string{"billing", "shipping"} {
		if _, err := q.CreateCustomerAddress(ctx, gen.CreateCustomerAddressParams{
			CustomerID: cust.ID, Type: typ, IsDefault: true, Line1: "1 Main", City: "Nairobi", Country: "KE",
		}); err != nil {
			t.Fatalf("address: %v", err)
		}
	}
	p1, _ := q.CreateProduct(ctx, gen.CreateProductParams{OrganizationID: 1, Sku: "P1", Type: "simple", Name: "Product One", Slug: "p1", Status: "active", Attributes: []byte("{}"), Unit: "each"})
	p2, _ := q.CreateProduct(ctx, gen.CreateProductParams{OrganizationID: 1, Sku: "P2", Type: "simple", Name: "Product Two", Slug: "p2", Status: "active", Attributes: []byte("{}"), Unit: "each"})

	f := fixture{h: h, issuer: issuer, pool: pool, adminTok: adminTok, customerID: cust.ID,
		p1ID: p1.ID, p1Public: p1.PublicID.String(), p2ID: p2.ID, p2Public: p2.PublicID.String()}
	f.custTok = f.login(t, "buyer@acme.test")
	return f
}

func (f fixture) login(t *testing.T, email string) string {
	t.Helper()
	rr := f.do(t, http.MethodPost, "/storefront/auth/login", "", map[string]any{"email": email, "password": custPassword, "org_id": 1})
	if rr.Code != http.StatusOK {
		t.Fatalf("login: %d (%s)", rr.Code, rr.Body.String())
	}
	var resp struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)
	return resp.Token
}

func (f fixture) do(t *testing.T, method, path, tok string, body any) *httptest.ResponseRecorder {
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
	f.h.ServeHTTP(rr, req)
	return rr
}

func decode(t *testing.T, rr *httptest.ResponseRecorder, v any) {
	t.Helper()
	if err := json.Unmarshal(rr.Body.Bytes(), v); err != nil {
		t.Fatalf("decode: %v (body=%s)", err, rr.Body.String())
	}
}

// ---- the full negotiation flow -------------------------------------------

func TestRFQToQuoteToOrder(t *testing.T) {
	f := setup(t)

	// 1) Buyer creates an RFQ (draft) with two lines.
	rr := f.do(t, http.MethodPost, "/storefront/rfqs", f.custTok, map[string]any{
		"notes": "need a quote",
		"items": []map[string]any{
			{"product_public_id": f.p1Public, "quantity": "5"},
			{"product_public_id": f.p2Public, "quantity": "2"},
		},
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("create RFQ: %d (%s)", rr.Code, rr.Body.String())
	}
	var rfq struct {
		PublicID string `json:"public_id"`
		Status   string `json:"status"`
		Items    []any  `json:"items"`
	}
	decode(t, rr, &rfq)
	if rfq.Status != "draft" || len(rfq.Items) != 2 {
		t.Fatalf("RFQ: want draft with 2 items, got %s/%d", rfq.Status, len(rfq.Items))
	}

	// 2) Submit it.
	sub := f.do(t, http.MethodPost, "/storefront/rfqs/"+rfq.PublicID+"/submit", f.custTok, nil)
	if sub.Code != http.StatusOK {
		t.Fatalf("submit: %d (%s)", sub.Code, sub.Body.String())
	}
	decode(t, sub, &rfq)
	if rfq.Status != "submitted" {
		t.Fatalf("after submit: want submitted, got %s", rfq.Status)
	}
	// Re-submit rejected.
	if again := f.do(t, http.MethodPost, "/storefront/rfqs/"+rfq.PublicID+"/submit", f.custTok, nil); again.Code != http.StatusConflict {
		t.Errorf("re-submit: want 409, got %d", again.Code)
	}

	// The rep needs the RFQ's numeric id (from the admin list).
	rfqID := f.adminRFQID(t, rfq.PublicID)

	// 3) Rep creates a quote from the RFQ (items copied 1:1).
	qr := f.do(t, http.MethodPost, "/admin/rfqs/"+strconv.FormatInt(rfqID, 10)+"/quote", f.adminTok, nil)
	if qr.Code != http.StatusOK {
		t.Fatalf("quote from RFQ: %d (%s)", qr.Code, qr.Body.String())
	}
	var quote struct {
		ID       int64  `json:"id"`
		PublicID string `json:"public_id"`
		Status   string `json:"status"`
		Version  int    `json:"version"`
		Items    []any  `json:"items"`
	}
	decode(t, qr, &quote)
	if quote.Status != "draft" || len(quote.Items) != 2 {
		t.Fatalf("quote: want draft with 2 items, got %s/%d", quote.Status, len(quote.Items))
	}

	// 4) Rep edits line prices.
	qid := strconv.FormatInt(quote.ID, 10)
	ed := f.do(t, http.MethodPut, "/admin/quotes/"+qid, f.adminTok, map[string]any{
		"items": []map[string]any{
			{"product_id": f.p1ID, "quantity": "5", "unit_price": "10.0000"},
			{"product_id": f.p2ID, "quantity": "2", "unit_price": "25.0000", "discount": "5.0000"},
		},
	})
	if ed.Code != http.StatusOK {
		t.Fatalf("edit quote: %d (%s)", ed.Code, ed.Body.String())
	}
	var edited struct {
		Subtotal string `json:"subtotal"`
	}
	decode(t, ed, &edited)
	// 5*10 + (2*25 - 5) = 50 + 45 = 95.
	if edited.Subtotal != "95.0000" {
		t.Fatalf("edited subtotal: want 95.0000, got %s", edited.Subtotal)
	}

	// 5) Send the quote -> sent, version bumps, revision recorded, RFQ -> quoted.
	snd := f.do(t, http.MethodPost, "/admin/quotes/"+qid+"/send", f.adminTok, map[string]any{})
	if snd.Code != http.StatusOK {
		t.Fatalf("send quote: %d (%s)", snd.Code, snd.Body.String())
	}
	decode(t, snd, &quote)
	if quote.Status != "sent" || quote.Version != 2 {
		t.Fatalf("sent quote: want sent v2, got %s v%d", quote.Status, quote.Version)
	}
	f.assertRFQStatus(t, rfqID, "quoted")
	f.assertRevisionCount(t, quote.ID, 1)

	// 6) Buyer accepts -> order created in one tx, immutable snapshots.
	acc := f.do(t, http.MethodPost, "/storefront/quotes/"+quote.PublicID+"/accept", f.custTok, nil)
	if acc.Code != http.StatusOK {
		t.Fatalf("accept: %d (%s)", acc.Code, acc.Body.String())
	}
	var order struct {
		ID         int64  `json:"id"`
		PublicID   string `json:"public_id"`
		Status     string `json:"status"`
		Subtotal   string `json:"subtotal"`
		GrandTotal string `json:"grand_total"`
		Items      []struct {
			Sku       string `json:"sku"`
			Name      string `json:"name"`
			UnitPrice string `json:"unit_price"`
			RowTotal  string `json:"row_total"`
		} `json:"items"`
	}
	decode(t, acc, &order)
	if order.Status != "pending" || order.GrandTotal != "95.0000" || len(order.Items) != 2 {
		t.Fatalf("order: want pending/95.0000/2 items, got %s/%s/%d", order.Status, order.GrandTotal, len(order.Items))
	}
	// Snapshot check: line carries the product's sku/name, not a live join.
	foundP1 := false
	for _, it := range order.Items {
		if it.Sku == "P1" && it.Name == "Product One" && it.UnitPrice == "10.0000" {
			foundP1 = true
		}
	}
	if !foundP1 {
		t.Errorf("order item snapshot missing/incorrect: %+v", order.Items)
	}

	// Quote is now accepted (immutable) and the RFQ accepted.
	f.assertRFQStatus(t, rfqID, "accepted")
	gq := f.do(t, http.MethodGet, "/storefront/quotes/"+quote.PublicID, f.custTok, nil)
	var got struct {
		Status string `json:"status"`
	}
	decode(t, gq, &got)
	if got.Status != "accepted" {
		t.Errorf("quote status after accept: want accepted, got %s", got.Status)
	}
	// Second accept rejected.
	if again := f.do(t, http.MethodPost, "/storefront/quotes/"+quote.PublicID+"/accept", f.custTok, nil); again.Code != http.StatusConflict {
		t.Errorf("double accept: want 409, got %d", again.Code)
	}
}

// ---- expiry --------------------------------------------------------------

func TestQuoteExpiryBlocksAccept(t *testing.T) {
	f := setup(t)
	// Seller-initiated quote with a past validity, then sent.
	past := time.Now().Add(-24 * time.Hour).UTC().Format(time.RFC3339)
	cr := f.do(t, http.MethodPost, "/admin/quotes", f.adminTok, map[string]any{
		"customer_id": f.customerID, "valid_until": past,
		"items": []map[string]any{{"product_id": f.p1ID, "quantity": "1", "unit_price": "9.0000"}},
	})
	if cr.Code != http.StatusOK {
		t.Fatalf("create quote: %d (%s)", cr.Code, cr.Body.String())
	}
	var quote struct {
		ID       int64  `json:"id"`
		PublicID string `json:"public_id"`
	}
	decode(t, cr, &quote)
	f.do(t, http.MethodPost, "/admin/quotes/"+strconv.FormatInt(quote.ID, 10)+"/send", f.adminTok, map[string]any{})

	acc := f.do(t, http.MethodPost, "/storefront/quotes/"+quote.PublicID+"/accept", f.custTok, nil)
	if acc.Code != http.StatusConflict {
		t.Fatalf("expired accept: want 409, got %d (%s)", acc.Code, acc.Body.String())
	}
}

// ---- order status state machine ------------------------------------------

func TestOrderStatusTransitions(t *testing.T) {
	f := setup(t)
	cr := f.do(t, http.MethodPost, "/admin/orders", f.adminTok, map[string]any{
		"customer_id": f.customerID,
		"items":       []map[string]any{{"product_id": f.p1ID, "quantity": "1", "unit_price": "10.0000"}},
	})
	if cr.Code != http.StatusOK {
		t.Fatalf("on-behalf order: %d (%s)", cr.Code, cr.Body.String())
	}
	var order struct {
		ID     int64  `json:"id"`
		Status string `json:"status"`
	}
	decode(t, cr, &order)
	if order.Status != "pending" {
		t.Fatalf("new order: want pending, got %s", order.Status)
	}
	oid := strconv.FormatInt(order.ID, 10)

	// Valid: pending -> confirmed.
	if rr := f.do(t, http.MethodPatch, "/admin/orders/"+oid+"/status", f.adminTok, map[string]any{"status": "confirmed"}); rr.Code != http.StatusOK {
		t.Fatalf("pending->confirmed: %d (%s)", rr.Code, rr.Body.String())
	}
	// Invalid: confirmed -> delivered (must go through processing/shipped).
	if rr := f.do(t, http.MethodPatch, "/admin/orders/"+oid+"/status", f.adminTok, map[string]any{"status": "delivered"}); rr.Code != http.StatusConflict {
		t.Fatalf("confirmed->delivered: want 409, got %d", rr.Code)
	}

	// History rows were written (created + the confirmed transition).
	var n int
	if err := f.pool.QueryRow(context.Background(), `SELECT count(*) FROM order_status_history WHERE order_id = $1`, order.ID).Scan(&n); err != nil {
		t.Fatalf("history count: %v", err)
	}
	if n != 2 {
		t.Errorf("status history: want 2 rows, got %d", n)
	}
}

// ---- auth / audience / isolation -----------------------------------------

func TestSalesAuthAndAudience(t *testing.T) {
	f := setup(t)
	// Storefront token cannot reach admin routes.
	if rr := f.do(t, http.MethodGet, "/admin/rfqs", f.custTok, nil); rr.Code != http.StatusForbidden {
		t.Errorf("storefront token on /admin/rfqs: want 403, got %d", rr.Code)
	}
	// Admin token cannot reach storefront routes.
	if rr := f.do(t, http.MethodGet, "/storefront/rfqs", f.adminTok, nil); rr.Code != http.StatusForbidden {
		t.Errorf("admin token on /storefront/rfqs: want 403, got %d", rr.Code)
	}
}

func TestQuoteCustomerIsolation(t *testing.T) {
	f := setup(t)
	q := gen.New(f.pool)
	ctx := context.Background()

	// A second customer + login.
	other, _ := q.CreateCustomer(ctx, gen.CreateCustomerParams{OrganizationID: 1, Name: "Beta", CreditLimit: "0"})
	hash, _ := auth.HashPassword(custPassword)
	_, _ = q.CreateCustomerUser(ctx, gen.CreateCustomerUserParams{CustomerID: other.ID, Email: "buyer@beta.test", PasswordHash: hash, FullName: "Beta", Role: "buyer"})
	otherTok := f.login(t, "buyer@beta.test")

	// A quote for Acme, sent.
	cr := f.do(t, http.MethodPost, "/admin/quotes", f.adminTok, map[string]any{
		"customer_id": f.customerID,
		"items":       []map[string]any{{"product_id": f.p1ID, "quantity": "1", "unit_price": "9.0000"}},
	})
	var quote struct {
		ID       int64  `json:"id"`
		PublicID string `json:"public_id"`
	}
	decode(t, cr, &quote)
	f.do(t, http.MethodPost, "/admin/quotes/"+strconv.FormatInt(quote.ID, 10)+"/send", f.adminTok, map[string]any{})

	// Beta must not see or accept Acme's quote.
	if rr := f.do(t, http.MethodGet, "/storefront/quotes/"+quote.PublicID, otherTok, nil); rr.Code != http.StatusNotFound {
		t.Errorf("cross-customer get: want 404, got %d", rr.Code)
	}
	if rr := f.do(t, http.MethodPost, "/storefront/quotes/"+quote.PublicID+"/accept", otherTok, nil); rr.Code != http.StatusNotFound {
		t.Errorf("cross-customer accept: want 404, got %d", rr.Code)
	}
}

// ---- helpers -------------------------------------------------------------

func (f fixture) adminRFQID(t *testing.T, publicID string) int64 {
	t.Helper()
	rr := f.do(t, http.MethodGet, "/admin/rfqs", f.adminTok, nil)
	var resp struct {
		Items []struct {
			ID       int64  `json:"id"`
			PublicID string `json:"public_id"`
		} `json:"items"`
	}
	decode(t, rr, &resp)
	for _, it := range resp.Items {
		if it.PublicID == publicID {
			return it.ID
		}
	}
	t.Fatalf("RFQ %s not found in admin list", publicID)
	return 0
}

func (f fixture) assertRFQStatus(t *testing.T, rfqID int64, want string) {
	t.Helper()
	var got string
	if err := f.pool.QueryRow(context.Background(), `SELECT status FROM rfqs WHERE id = $1`, rfqID).Scan(&got); err != nil {
		t.Fatalf("rfq status: %v", err)
	}
	if got != want {
		t.Errorf("RFQ status: want %s, got %s", want, got)
	}
}

func (f fixture) assertRevisionCount(t *testing.T, quoteID int64, want int) {
	t.Helper()
	var n int
	if err := f.pool.QueryRow(context.Background(), `SELECT count(*) FROM quote_revisions WHERE quote_id = $1`, quoteID).Scan(&n); err != nil {
		t.Fatalf("revision count: %v", err)
	}
	if n != want {
		t.Errorf("revisions: want %d, got %d", want, n)
	}
}
