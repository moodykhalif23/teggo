package otc_test

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
	"b2bcommerce/internal/pdf"
	"b2bcommerce/internal/queue/jobs"
	"b2bcommerce/internal/server"
	"b2bcommerce/internal/store"
	"b2bcommerce/internal/store/gen"
	"b2bcommerce/internal/testsupport"
)

const (
	testSecret   = "test-secret-please-change"
	custPassword = "buyer-pass-123"
)

// syncPDF runs the invoice-PDF job inline so tests can assert pdf_url.
type syncPDF struct{ pool *pgxpool.Pool }

func (s syncPDF) EnqueueInvoicePDF(ctx context.Context, invoiceID int64) error {
	return jobs.GenerateInvoicePDF(ctx, s.pool, pdf.Stub{}, invoiceID)
}

func newServer(t *testing.T) (http.Handler, *auth.Issuer, *pgxpool.Pool) {
	t.Helper()
	pool := testsupport.NewDB(t)
	st := store.New(pool)
	issuer := auth.NewIssuer(testSecret, time.Hour)
	h := server.New(st, issuer, server.WithInvoicePDF(syncPDF{pool: pool}))
	return h, issuer, pool
}

func adminToken(t *testing.T, issuer *auth.Issuer) string {
	t.Helper()
	tok, _ := issuer.Issue("1", 1, "admin", []string{
		"order.view", "order.manage", "shipment.view", "shipment.manage",
		"invoice.view", "invoice.manage", "payment.view", "payment.manage",
	})
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

// seedOrder makes a customer (with terms/credit), an order, and one order line
// (qty 2 @ 10 = 20). Returns ids.
func seedOrder(t *testing.T, pool *pgxpool.Pool, terms int32, creditLimit string) (customerID, orderID, orderItemID int64) {
	t.Helper()
	q := gen.New(pool)
	ctx := context.Background()
	cust, err := q.CreateCustomer(ctx, gen.CreateCustomerParams{
		OrganizationID: 1, Name: "Acme", CreditLimit: creditLimit, PaymentTermsDays: terms,
	})
	if err != nil {
		t.Fatalf("customer: %v", err)
	}
	suffix := strconv.FormatInt(cust.ID, 10)
	prod, err := q.CreateProduct(ctx, gen.CreateProductParams{OrganizationID: 1, Sku: "WIDGET-" + suffix, Type: "simple", Name: "Widget", Slug: "widget-" + suffix, Status: "active", Attributes: []byte("{}"), Unit: "each"})
	if err != nil {
		t.Fatalf("product: %v", err)
	}
	order, err := q.CreateOrder(ctx, gen.CreateOrderParams{
		OrganizationID: 1, WebsiteID: 1, CustomerID: cust.ID, Currency: "USD",
		BillingAddress: []byte("{}"), ShippingAddress: []byte("{}"),
		Subtotal: "20.0000", TaxTotal: "0", ShippingTotal: "0", GrandTotal: "20.0000",
	})
	if err != nil {
		t.Fatalf("order: %v", err)
	}
	oi, err := q.AddOrderItem(ctx, gen.AddOrderItemParams{
		OrderID: order.ID, ProductID: prod.ID, Sku: "WIDGET", Name: "Widget",
		Quantity: "2", Unit: "each", UnitPrice: "10.0000", TaxAmount: "0", RowTotal: "20.0000",
	})
	if err != nil {
		t.Fatalf("order item: %v", err)
	}
	return cust.ID, order.ID, oi.ID
}

// ---- Shipments -----------------------------------------------------------

func TestShipmentsQuantityCapAndStatus(t *testing.T) {
	h, issuer, pool := newServer(t)
	tok := adminToken(t, issuer)
	_, orderID, oiID := seedOrder(t, pool, 0, "0")
	oid := strconv.FormatInt(orderID, 10)

	// Partial ship 1 of 2 -> ok.
	s1 := do(t, h, http.MethodPost, "/admin/orders/"+oid+"/shipments", tok, map[string]any{
		"items": []map[string]any{{"order_item_id": oiID, "quantity": "1"}},
	})
	if s1.Code != http.StatusCreated {
		t.Fatalf("first shipment: %d (%s)", s1.Code, s1.Body.String())
	}
	var sh struct {
		ID     int64  `json:"id"`
		Status string `json:"status"`
	}
	_ = json.Unmarshal(s1.Body.Bytes(), &sh)

	// Ship the remaining 1 -> ok.
	if s2 := do(t, h, http.MethodPost, "/admin/orders/"+oid+"/shipments", tok, map[string]any{
		"items": []map[string]any{{"order_item_id": oiID, "quantity": "1"}},
	}); s2.Code != http.StatusCreated {
		t.Fatalf("second shipment: %d (%s)", s2.Code, s2.Body.String())
	}

	// Over-ship (nothing left) -> 422.
	if s3 := do(t, h, http.MethodPost, "/admin/orders/"+oid+"/shipments", tok, map[string]any{
		"items": []map[string]any{{"order_item_id": oiID, "quantity": "1"}},
	}); s3.Code != http.StatusUnprocessableEntity {
		t.Fatalf("over-ship: want 422, got %d (%s)", s3.Code, s3.Body.String())
	}

	// Status transition: pending -> shipped ok; pending again invalid.
	sid := strconv.FormatInt(sh.ID, 10)
	if rr := do(t, h, http.MethodPatch, "/admin/shipments/"+sid+"/status", tok, map[string]any{"status": "shipped"}); rr.Code != http.StatusOK {
		t.Fatalf("ship: %d (%s)", rr.Code, rr.Body.String())
	}
	if rr := do(t, h, http.MethodPatch, "/admin/shipments/"+sid+"/status", tok, map[string]any{"status": "pending"}); rr.Code != http.StatusConflict {
		t.Errorf("invalid shipment transition: want 409, got %d", rr.Code)
	}
}

// ---- Invoices ------------------------------------------------------------

func issueInvoice(t *testing.T, h http.Handler, tok string, orderID int64) (id int64, publicID, status, pdf string) {
	t.Helper()
	rr := do(t, h, http.MethodPost, "/admin/orders/"+strconv.FormatInt(orderID, 10)+"/invoices", tok, nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("issue invoice: %d (%s)", rr.Code, rr.Body.String())
	}
	var inv struct {
		ID         int64   `json:"id"`
		PublicID   string  `json:"public_id"`
		Status     string  `json:"status"`
		GrandTotal string  `json:"grand_total"`
		PdfURL     *string `json:"pdf_url"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &inv)
	if inv.GrandTotal != "20.0000" {
		t.Fatalf("invoice grand_total: want 20.0000, got %s", inv.GrandTotal)
	}
	pdfURL := ""
	if inv.PdfURL != nil {
		pdfURL = *inv.PdfURL
	}
	return inv.ID, inv.PublicID, inv.Status, pdfURL
}

func TestInvoiceIssueFreezesAndGeneratesPDF(t *testing.T) {
	h, issuer, pool := newServer(t)
	tok := adminToken(t, issuer)
	_, orderID, _ := seedOrder(t, pool, 30, "100000")

	id, _, status, _ := issueInvoice(t, h, tok, orderID)
	if status != "issued" {
		t.Fatalf("issued status: want issued, got %s", status)
	}
	// Re-fetch: the sync PDF job set pdf_url.
	rr := do(t, h, http.MethodGet, "/admin/invoices/"+strconv.FormatInt(id, 10), tok, nil)
	var got struct {
		PdfURL *string `json:"pdf_url"`
		Items  []any   `json:"items"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &got)
	if got.PdfURL == nil || *got.PdfURL == "" {
		t.Fatalf("pdf_url should be set after async generation, got %v", got.PdfURL)
	}
	if len(got.Items) != 1 {
		t.Errorf("invoice items frozen from order: want 1, got %d", len(got.Items))
	}

	// The capability URL serves a real PDF, unauthenticated, to anyone with the
	// (unguessable) link.
	dl := do(t, h, http.MethodGet, *got.PdfURL, "", nil)
	if dl.Code != http.StatusOK {
		t.Fatalf("download PDF: want 200, got %d (%s)", dl.Code, dl.Body.String())
	}
	if ct := dl.Header().Get("Content-Type"); ct != "application/pdf" {
		t.Errorf("PDF content-type: want application/pdf, got %q", ct)
	}
	if body := dl.Body.Bytes(); len(body) < 4 || string(body[:4]) != "%PDF" {
		t.Errorf("downloaded body is not a PDF (prefix %q)", body[:min(4, len(body))])
	}

	// An unknown invoice id is a 404.
	if nf := do(t, h, http.MethodGet, "/files/invoices/00000000-0000-0000-0000-000000000000.pdf", "", nil); nf.Code != http.StatusNotFound {
		t.Errorf("unknown PDF: want 404, got %d", nf.Code)
	}
}

// ---- Payments ------------------------------------------------------------

func TestPaymentCapturedFlipsInvoicePaid(t *testing.T) {
	h, issuer, pool := newServer(t)
	tok := adminToken(t, issuer)
	custID, orderID, _ := seedOrder(t, pool, 30, "100000")
	invID, _, _, _ := issueInvoice(t, h, tok, orderID)

	// Partial capture (5) leaves it issued.
	pay := func(amount string) *httptest.ResponseRecorder {
		return do(t, h, http.MethodPost, "/admin/payments", tok, map[string]any{
			"invoice_id": invID, "customer_id": custID, "method": "card", "amount": amount, "currency": "USD",
		})
	}
	if rr := pay("5.0000"); rr.Code != http.StatusCreated {
		t.Fatalf("partial payment: %d (%s)", rr.Code, rr.Body.String())
	}
	if s := invoiceStatus(t, h, tok, invID); s != "issued" {
		t.Fatalf("after partial: want issued, got %s", s)
	}
	// Remaining 15 covers it -> paid.
	if rr := pay("15.0000"); rr.Code != http.StatusCreated {
		t.Fatalf("settling payment: %d (%s)", rr.Code, rr.Body.String())
	}
	if s := invoiceStatus(t, h, tok, invID); s != "paid" {
		t.Errorf("after full capture: want paid, got %s", s)
	}
}

func TestInvoiceMethodCreditGate(t *testing.T) {
	h, issuer, pool := newServer(t)
	tok := adminToken(t, issuer)

	// No terms -> invoice method rejected.
	noTermsCust, _, _ := seedOrder(t, pool, 0, "100000")
	if rr := do(t, h, http.MethodPost, "/admin/payments", tok, map[string]any{
		"customer_id": noTermsCust, "method": "invoice", "amount": "10.0000", "currency": "USD",
	}); rr.Code != http.StatusUnprocessableEntity {
		t.Fatalf("no-terms invoice method: want 422, got %d (%s)", rr.Code, rr.Body.String())
	}

	// Terms but credit exceeded: open invoices (20) > credit limit (5).
	custID, orderID, _ := seedOrder(t, pool, 30, "5")
	issueInvoice(t, h, tok, orderID) // open total now 20 > 5
	if rr := do(t, h, http.MethodPost, "/admin/payments", tok, map[string]any{
		"customer_id": custID, "method": "invoice", "amount": "20.0000", "currency": "USD",
	}); rr.Code != http.StatusUnprocessableEntity {
		t.Fatalf("credit exceeded: want 422, got %d (%s)", rr.Code, rr.Body.String())
	}
}

func TestPaymentRefund(t *testing.T) {
	h, issuer, pool := newServer(t)
	tok := adminToken(t, issuer)
	custID, _, _ := seedOrder(t, pool, 0, "0")

	rr := do(t, h, http.MethodPost, "/admin/payments", tok, map[string]any{
		"customer_id": custID, "method": "card", "amount": "10.0000", "currency": "USD",
	})
	var pay struct {
		ID int64 `json:"id"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &pay)
	pid := strconv.FormatInt(pay.ID, 10)

	if rf := do(t, h, http.MethodPost, "/admin/payments/"+pid+"/refund", tok, nil); rf.Code != http.StatusOK {
		t.Fatalf("refund: %d (%s)", rf.Code, rf.Body.String())
	}
	// Second refund rejected (already refunded).
	if rf := do(t, h, http.MethodPost, "/admin/payments/"+pid+"/refund", tok, nil); rf.Code != http.StatusConflict {
		t.Errorf("double refund: want 409, got %d", rf.Code)
	}
}

// ---- Storefront invoice views + auth -------------------------------------

func TestStorefrontInvoiceViewsAndIsolation(t *testing.T) {
	h, issuer, pool := newServer(t)
	tok := adminToken(t, issuer)
	q := gen.New(pool)
	ctx := context.Background()

	custID, orderID, _ := seedOrder(t, pool, 30, "100000")
	_, pubID, _, _ := issueInvoice(t, h, tok, orderID)

	// A customer-user that belongs to the invoice's customer.
	hash, _ := auth.HashPassword(custPassword)
	if _, err := q.CreateCustomerUser(ctx, gen.CreateCustomerUserParams{CustomerID: custID, Email: "buyer@acme.test", PasswordHash: hash, FullName: "Buyer", Role: "buyer"}); err != nil {
		t.Fatalf("customer user: %v", err)
	}
	custTok, _ := issuer.IssueStorefront(0, 1, custID)

	// Lists and reads own invoice.
	if rr := do(t, h, http.MethodGet, "/storefront/invoices", custTok, nil); rr.Code != http.StatusOK {
		t.Fatalf("list my invoices: %d", rr.Code)
	}
	if rr := do(t, h, http.MethodGet, "/storefront/invoices/"+pubID, custTok, nil); rr.Code != http.StatusOK {
		t.Fatalf("get my invoice: %d (%s)", rr.Code, rr.Body.String())
	}

	// Another customer cannot see it.
	otherTok, _ := issuer.IssueStorefront(0, 1, custID+99999)
	if rr := do(t, h, http.MethodGet, "/storefront/invoices/"+pubID, otherTok, nil); rr.Code != http.StatusNotFound {
		t.Errorf("cross-customer invoice: want 404, got %d", rr.Code)
	}

	// Storefront token cannot reach admin invoice routes.
	if rr := do(t, h, http.MethodGet, "/admin/orders/"+strconv.FormatInt(orderID, 10)+"/invoices", custTok, nil); rr.Code != http.StatusForbidden {
		t.Errorf("audience gate: want 403, got %d", rr.Code)
	}
}

func invoiceStatus(t *testing.T, h http.Handler, tok string, invID int64) string {
	t.Helper()
	rr := do(t, h, http.MethodGet, "/admin/invoices/"+strconv.FormatInt(invID, 10), tok, nil)
	var got struct {
		Status string `json:"status"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &got)
	return got.Status
}

func TestAdminListInvoices(t *testing.T) {
	h, issuer, pool := newServer(t)
	tok := adminToken(t, issuer)
	ctx := context.Background()

	_, orderID, _ := seedOrder(t, pool, 30, "100000")
	invID, pubID, _, _ := issueInvoice(t, h, tok, orderID)

	// Org-wide admin list includes our invoice.
	rr := do(t, h, http.MethodGet, "/admin/invoices", tok, nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("admin invoices: want 200, got %d (%s)", rr.Code, rr.Body.String())
	}
	var resp struct {
		Items []struct {
			ID       int64  `json:"id"`
			PublicID string `json:"public_id"`
		} `json:"items"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)
	found := false
	for _, i := range resp.Items {
		if i.ID == invID {
			found = true
		}
	}
	if !found {
		t.Errorf("expected issued invoice %d in admin list", invID)
	}

	// Tenant isolation: an invoice in another org must not appear.
	q := gen.New(pool)
	var org2 int64
	if err := pool.QueryRow(ctx, `INSERT INTO organizations (name) VALUES ('Org Two') RETURNING id`).Scan(&org2); err != nil {
		t.Fatalf("org2: %v", err)
	}
	c2, _ := q.CreateCustomer(ctx, gen.CreateCustomerParams{OrganizationID: org2, Name: "Other", CreditLimit: "0"})
	o2, _ := q.CreateOrder(ctx, gen.CreateOrderParams{
		OrganizationID: org2, WebsiteID: 1, CustomerID: c2.ID, Currency: "USD",
		BillingAddress: []byte("{}"), ShippingAddress: []byte("{}"), Subtotal: "1", TaxTotal: "0", ShippingTotal: "0", GrandTotal: "1",
	})
	inv2, _ := q.CreateInvoice(ctx, gen.CreateInvoiceParams{
		OrderID: o2.ID, CustomerID: c2.ID, Currency: "USD", Subtotal: "1", TaxTotal: "0", GrandTotal: "1",
	})
	rr2 := do(t, h, http.MethodGet, "/admin/invoices", tok, nil)
	_ = json.Unmarshal(rr2.Body.Bytes(), &resp)
	for _, i := range resp.Items {
		if i.PublicID == inv2.PublicID.String() {
			t.Error("tenant isolation breach: org1 admin list returned org2 invoice")
		}
	}
	_ = pubID
}
