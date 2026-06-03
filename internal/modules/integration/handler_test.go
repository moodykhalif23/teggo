package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"b2bcommerce/internal/auth"
	"b2bcommerce/internal/server"
	"b2bcommerce/internal/store"
	"b2bcommerce/internal/store/gen"
	"b2bcommerce/internal/testsupport"
)

const testSecret = "integration-test-secret"

func newServer(t *testing.T) (http.Handler, *auth.Issuer, *pgxpool.Pool) {
	t.Helper()
	pool := testsupport.NewDB(t)
	st := store.New(pool)
	issuer := auth.NewIssuer(testSecret, time.Hour)
	h := server.New(st, issuer, server.WithIntegration("/store", "TEGGO", time.Hour))
	return h, issuer, pool
}

func adminToken(t *testing.T, issuer *auth.Issuer) string {
	t.Helper()
	tok, _ := issuer.Issue("1", 1, "admin", []string{"integration.view", "integration.manage"})
	return tok
}

func doJSON(t *testing.T, h http.Handler, method, path, tok string, body any) *httptest.ResponseRecorder {
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

func doRaw(t *testing.T, h http.Handler, method, path, contentType, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	return rr
}

// seedCustomerProduct makes a customer + a product with a known SKU in org 1.
func seedCustomerProduct(t *testing.T, pool *pgxpool.Pool, sku string) (custID, prodID int64) {
	t.Helper()
	q := gen.New(pool)
	ctx := context.Background()
	c, err := q.CreateCustomer(ctx, gen.CreateCustomerParams{OrganizationID: 1, Name: "Buyer Co", CreditLimit: "0"})
	if err != nil {
		t.Fatalf("customer: %v", err)
	}
	p, err := q.CreateProduct(ctx, gen.CreateProductParams{
		OrganizationID: 1, Sku: sku, Type: "simple", Name: "Integration Widget",
		Slug: strings.ToLower(sku), Status: "active", Attributes: []byte("{}"), Unit: "each",
	})
	if err != nil {
		t.Fatalf("product: %v", err)
	}
	return c.ID, p.ID
}

func createPartner(t *testing.T, h http.Handler, tok string, body map[string]any) int64 {
	t.Helper()
	rr := doJSON(t, h, http.MethodPost, "/admin/trading-partners", tok, body)
	if rr.Code != http.StatusCreated {
		t.Fatalf("create partner: %d (%s)", rr.Code, rr.Body.String())
	}
	var p struct {
		ID int64 `json:"id"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &p)
	return p.ID
}

func setupXML(identity, secret, cookie, returnURL string) string {
	return `<?xml version="1.0"?><cXML><Header><Sender><Credential domain="NetworkId">` +
		`<Identity>` + identity + `</Identity><SharedSecret>` + secret + `</SharedSecret>` +
		`</Credential></Sender></Header><Request><PunchOutSetupRequest operation="create">` +
		`<BuyerCookie>` + cookie + `</BuyerCookie><BrowserFormPost><URL>` + returnURL +
		`</URL></BrowserFormPost></PunchOutSetupRequest></Request></cXML>`
}

func TestPunchoutFlow(t *testing.T) {
	h, issuer, pool := newServer(t)
	tok := adminToken(t, issuer)
	custID, prodID := seedCustomerProduct(t, pool, "PO-WIDGET")

	createPartner(t, h, tok, map[string]any{
		"name": "Acme Procurement", "protocol": "cxml", "identity": "ACME-PROC",
		"shared_secret": "s3cr3t", "customer_id": custID,
	})

	// Bad credentials → 401.
	bad := doRaw(t, h, http.MethodPost, "/punchout/setup", "application/xml", setupXML("ACME-PROC", "wrong", "ck", "https://buyer/return"))
	if bad.Code != http.StatusUnauthorized {
		t.Fatalf("bad creds: want 401, got %d (%s)", bad.Code, bad.Body.String())
	}

	// Valid setup → 200 with a StartPage URL.
	rr := doRaw(t, h, http.MethodPost, "/punchout/setup", "application/xml", setupXML("ACME-PROC", "s3cr3t", "cookie-42", "https://buyer/return"))
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), "/punchout/start/") {
		t.Fatalf("setup: %d (%s)", rr.Code, rr.Body.String())
	}
	startURL := between(rr.Body.String(), "<URL>", "</URL>")
	publicID := startURL[strings.LastIndex(startURL, "/")+1:]

	// Start (JSON mode) issues a storefront token + cart.
	sr := doJSON(t, h, http.MethodGet, "/punchout/start/"+publicID+"?format=json", "", nil)
	if sr.Code != http.StatusOK {
		t.Fatalf("start: %d (%s)", sr.Code, sr.Body.String())
	}
	var started struct {
		Token  string `json:"token"`
		CartID int64  `json:"cart_id"`
	}
	_ = json.Unmarshal(sr.Body.Bytes(), &started)
	if started.Token == "" || started.CartID == 0 {
		t.Fatalf("start did not return token+cart: %s", sr.Body.String())
	}

	// Buyer adds a line to the cart, then transfers.
	if _, err := gen.New(pool).UpsertCartItem(context.Background(), gen.UpsertCartItemParams{
		CartID: started.CartID, ProductID: prodID, Quantity: "3", Unit: "each", UnitPrice: "20.0000",
	}); err != nil {
		t.Fatalf("cart item: %v", err)
	}

	tr := doRaw(t, h, http.MethodPost, "/punchout/transfer/"+publicID, "", "")
	if tr.Code != http.StatusOK {
		t.Fatalf("transfer: %d (%s)", tr.Code, tr.Body.String())
	}
	body := tr.Body.String()
	for _, want := range []string{"https://buyer/return", "cxml-base64"} {
		if !strings.Contains(body, want) {
			t.Errorf("transfer form missing %q", want)
		}
	}
	_ = custID

	// Second transfer is rejected (already returned).
	if again := doRaw(t, h, http.MethodPost, "/punchout/transfer/"+publicID, "", ""); again.Code != http.StatusConflict {
		t.Errorf("double transfer: want 409, got %d", again.Code)
	}
}

func TestPunchoutExpired(t *testing.T) {
	h, issuer, pool := newServer(t)
	tok := adminToken(t, issuer)
	custID, _ := seedCustomerProduct(t, pool, "EXP-1")
	pid := createPartner(t, h, tok, map[string]any{
		"name": "P", "protocol": "cxml", "identity": "EXP-PROC", "shared_secret": "x", "customer_id": custID,
	})
	// A session that expired an hour ago.
	sess, err := gen.New(pool).CreatePunchoutSession(context.Background(), gen.CreatePunchoutSessionParams{
		TradingPartnerID: pid, CustomerID: custID, BuyerCookie: "c", Operation: "create",
		ReturnUrl: "https://buyer/return", ExpiresAt: time.Now().Add(-time.Hour),
	})
	if err != nil {
		t.Fatalf("session: %v", err)
	}
	if rr := doRaw(t, h, http.MethodPost, "/punchout/transfer/"+sess.PublicID.String(), "", ""); rr.Code != http.StatusForbidden {
		t.Errorf("expired transfer: want 403, got %d", rr.Code)
	}
}

func po850(poNumber, sku, qty, price string) string {
	return "ISA*00*          *00*          *ZZ*ACME           *ZZ*TEGGO          *240101*1200*U*00401*000000001*0*P*>~" +
		"GS*PO*ACME*TEGGO*20240101*1200*1*X*004010~ST*850*0001~" +
		"BEG*00*SA*" + poNumber + "**20240101~CUR*BY*USD~" +
		"PO1*1*" + qty + "*EA*" + price + "**VP*" + sku + "~CTT*1~SE*6*0001~GE*1*1~IEA*1*000000001~"
}

func TestEDIInbound850CreatesOrder(t *testing.T) {
	h, issuer, pool := newServer(t)
	tok := adminToken(t, issuer)
	custID, _ := seedCustomerProduct(t, pool, "EDI-SKU-1")
	pid := createPartner(t, h, tok, map[string]any{
		"name": "EDI Partner", "protocol": "edi_x12", "transport": "as2",
		"identity": "ACME-EDI", "customer_id": custID,
	})

	// Inbound 850 → order + 855.
	rr := doRaw(t, h, http.MethodPost, "/edi/inbound/"+strconv.FormatInt(pid, 10), "application/edi-x12", po850("PO-1001", "EDI-SKU-1", "4", "15.0000"))
	if rr.Code != http.StatusOK {
		t.Fatalf("inbound 850: %d (%s)", rr.Code, rr.Body.String())
	}
	var res struct {
		OrderID int64  `json:"order_id"`
		PO      string `json:"po_number"`
		Lines   int    `json:"lines"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &res)
	if res.OrderID == 0 || res.PO != "PO-1001" || res.Lines != 1 {
		t.Fatalf("unexpected result: %+v", res)
	}
	// The order exists with the PO number and one line.
	o, err := gen.New(pool).GetOrderByID(context.Background(), gen.GetOrderByIDParams{OrganizationID: 1, ID: res.OrderID})
	if err != nil || o.PoNumber == nil || *o.PoNumber != "PO-1001" {
		t.Fatalf("order/PO mismatch: %+v err=%v", o, err)
	}

	// Document log shows the inbound 850 (processed) and outbound 855 (sent).
	var docs struct {
		Items []struct {
			Direction string `json:"direction"`
			DocType   string `json:"doc_type"`
			Status    string `json:"status"`
		} `json:"items"`
	}
	_ = json.Unmarshal(doJSON(t, h, http.MethodGet, "/admin/edi/documents", tok, nil).Body.Bytes(), &docs)
	var saw850, saw855 bool
	for _, d := range docs.Items {
		if d.DocType == "850" && d.Direction == "inbound" && d.Status == "processed" {
			saw850 = true
		}
		if d.DocType == "855" && d.Direction == "outbound" {
			saw855 = true
		}
	}
	if !saw850 || !saw855 {
		t.Errorf("doc log: 850-processed=%v 855-sent=%v", saw850, saw855)
	}

	// Duplicate PO is idempotent-rejected.
	if dup := doRaw(t, h, http.MethodPost, "/edi/inbound/"+strconv.FormatInt(pid, 10), "", po850("PO-1001", "EDI-SKU-1", "4", "15.0000")); dup.Code != http.StatusConflict {
		t.Errorf("duplicate 850: want 409, got %d", dup.Code)
	}

	// Unknown SKU is rejected (never partially applied).
	if bad := doRaw(t, h, http.MethodPost, "/edi/inbound/"+strconv.FormatInt(pid, 10), "", po850("PO-2002", "NO-SUCH-SKU", "1", "1")); bad.Code != http.StatusUnprocessableEntity {
		t.Errorf("unknown sku: want 422, got %d", bad.Code)
	}

	// Outbound 856 from the created order.
	asn := doJSON(t, h, http.MethodPost, "/admin/edi/856", tok, map[string]any{"order_id": res.OrderID, "trading_partner_id": pid})
	if asn.Code != http.StatusCreated {
		t.Fatalf("856: %d (%s)", asn.Code, asn.Body.String())
	}
	if !strings.Contains(asn.Body.String(), "BSN*00*SHP-") {
		t.Errorf("856 payload missing BSN: %s", asn.Body.String())
	}

	// Outbound 810 from an invoice on that order.
	inv, err := gen.New(pool).CreateInvoice(context.Background(), gen.CreateInvoiceParams{
		OrderID: res.OrderID, CustomerID: custID, Currency: "USD",
		Subtotal: "60.0000", TaxTotal: "0", GrandTotal: "60.0000",
		IssuedAt: pgtype.Timestamptz{Time: time.Now(), Valid: true},
	})
	if err != nil {
		t.Fatalf("invoice: %v", err)
	}
	inv810 := doJSON(t, h, http.MethodPost, "/admin/edi/810", tok, map[string]any{"invoice_id": inv.ID, "trading_partner_id": pid})
	if inv810.Code != http.StatusCreated || !strings.Contains(inv810.Body.String(), "ST*810") {
		t.Fatalf("810: %d (%s)", inv810.Code, inv810.Body.String())
	}
}

func TestTradingPartnerAuth(t *testing.T) {
	h, issuer, _ := newServer(t)
	// storefront token cannot manage partners.
	cust, _ := issuer.IssueStorefront(0, 1, 1)
	if rr := doJSON(t, h, http.MethodGet, "/admin/trading-partners", cust, nil); rr.Code != http.StatusForbidden {
		t.Errorf("storefront token: want 403, got %d", rr.Code)
	}
}

// between returns the substring between the first occurrence of a and the next b.
func between(s, a, b string) string {
	i := strings.Index(s, a)
	if i < 0 {
		return ""
	}
	s = s[i+len(a):]
	j := strings.Index(s, b)
	if j < 0 {
		return ""
	}
	return s[:j]
}
