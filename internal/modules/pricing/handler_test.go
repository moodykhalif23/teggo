package pricing_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"b2bcommerce/internal/auth"
	"b2bcommerce/internal/server"
	"b2bcommerce/internal/store"
	"b2bcommerce/internal/store/gen"
	"b2bcommerce/internal/testsupport"
)

const testSecret = "test-secret-please-change"

func newServer(t *testing.T) (http.Handler, *auth.Issuer, *pgxpool.Pool) {
	t.Helper()
	pool := testsupport.NewDB(t)
	st := store.New(pool)
	issuer := auth.NewIssuer(testSecret, time.Hour)
	h := server.New(st, issuer)
	return h, issuer, pool
}

// resolveTier is the read-time price resolution helper the tests assert against
// (replaces the old combined_prices cache reads).
func resolveTier(ctx context.Context, q *gen.Queries, cust, prod int64, qty string) (gen.ResolvePriceTierRow, error) {
	return q.ResolvePriceTier(ctx, gen.ResolvePriceTierParams{
		ID: cust, ProductID: prod, Unit: "each", Column4: qty, Currency: "USD",
		WebsiteID: ptr(int64(1)), ValidFrom: nowTs(),
	})
}

func token(t *testing.T, issuer *auth.Issuer, perms ...string) string {
	t.Helper()
	tok, err := issuer.Issue("1", 1, "admin", perms)
	if err != nil {
		t.Fatalf("issue token: %v", err)
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

// ---- seed helpers (query layer) ------------------------------------------

func mkProduct(t *testing.T, q *gen.Queries, org int64, sku, slug string) int64 {
	t.Helper()
	p, err := q.CreateProduct(context.Background(), gen.CreateProductParams{
		OrganizationID: org, Sku: sku, Type: "simple", Name: sku, Slug: slug,
		Status: "active", Attributes: []byte("{}"), Unit: "each",
	})
	if err != nil {
		t.Fatalf("mkProduct %s: %v", sku, err)
	}
	return p.ID
}

func mkCustomer(t *testing.T, q *gen.Queries, org int64, name string, group *int64) int64 {
	t.Helper()
	c, err := q.CreateCustomer(context.Background(), gen.CreateCustomerParams{
		OrganizationID: org, Name: name, CreditLimit: "0", CustomerGroupID: group,
	})
	if err != nil {
		t.Fatalf("mkCustomer %s: %v", name, err)
	}
	return c.ID
}

func mkList(t *testing.T, q *gen.Queries, org int64, name, currency string) gen.PriceList {
	t.Helper()
	pl, err := q.CreatePriceList(context.Background(), gen.CreatePriceListParams{
		OrganizationID: org, Name: name, Currency: currency, IsActive: true,
	})
	if err != nil {
		t.Fatalf("mkList %s: %v", name, err)
	}
	return pl
}

func addPrice(t *testing.T, q *gen.Queries, listID, productID int64, minQty, value string, validTo *time.Time) {
	t.Helper()
	vt := pgtype.Timestamptz{}
	if validTo != nil {
		vt = pgtype.Timestamptz{Time: *validTo, Valid: true}
	}
	if _, err := q.UpsertPrice(context.Background(), gen.UpsertPriceParams{
		PriceListID: listID, ProductID: productID, Unit: "each",
		MinQuantity: minQty, Value: value, ValidTo: vt,
	}); err != nil {
		t.Fatalf("addPrice: %v", err)
	}
}

func nowTs() pgtype.Timestamptz { return pgtype.Timestamptz{Time: time.Now(), Valid: true} }

func ptr[T any](v T) *T { return &v }

// ---- ResolvePrice (§12.1): the core -------------------------------------

func TestResolvePrice_PriorityTiersCurrencyTime(t *testing.T) {
	pool := testsupport.NewDB(t)
	q := gen.New(pool)
	ctx := context.Background()
	const website = int64(1) // seeded demo website (org 1, USD)

	prod := mkProduct(t, q, 1, "P-1", "p-1")

	// Website-default list: tiers 1->100, 10->90.
	wsList := mkList(t, q, 1, "Website Default", "USD")
	addPrice(t, q, wsList.ID, prod, "1", "100.0000", nil)
	addPrice(t, q, wsList.ID, prod, "10", "90.0000", nil)
	if _, err := q.CreatePriceListAssignment(ctx, gen.CreatePriceListAssignmentParams{
		PriceListID: wsList.ID, WebsiteID: ptr(website), Priority: 0,
	}); err != nil {
		t.Fatalf("assign website: %v", err)
	}

	// Customer with its own list at 80 (higher level beats website).
	custList := mkList(t, q, 1, "Acme Contract", "USD")
	addPrice(t, q, custList.ID, prod, "1", "80.0000", nil)
	acme := mkCustomer(t, q, 1, "Acme", nil)
	if _, err := q.CreatePriceListAssignment(ctx, gen.CreatePriceListAssignmentParams{
		PriceListID: custList.ID, CustomerID: ptr(acme), Priority: 0,
	}); err != nil {
		t.Fatalf("assign customer: %v", err)
	}

	resolve := func(cust int64, qty, currency string) (gen.ResolvePriceRow, error) {
		return q.ResolvePrice(ctx, gen.ResolvePriceParams{
			ID: cust, ProductID: prod, Column3: qty, Currency: currency,
			WebsiteID: ptr(website), ValidFrom: nowTs(),
		})
	}

	// Customer level wins regardless of website tiers.
	if row, err := resolve(acme, "1", "USD"); err != nil || row.Value != "80.0000" {
		t.Fatalf("customer price: want 80.0000, got %q err=%v", row.Value, err)
	}

	// A customer with no own assignment falls back to the website default + tiers.
	other := mkCustomer(t, q, 1, "Other", nil)
	if row, err := resolve(other, "1", "USD"); err != nil || row.Value != "100.0000" {
		t.Fatalf("website tier@1: want 100.0000, got %q err=%v", row.Value, err)
	}
	if row, err := resolve(other, "12", "USD"); err != nil || row.Value != "90.0000" {
		t.Fatalf("website tier@12: want 90.0000, got %q err=%v", row.Value, err)
	}

	// Wrong currency resolves to nothing (price-on-request path).
	if _, err := resolve(acme, "1", "EUR"); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("currency mismatch: want ErrNoRows, got %v", err)
	}

	// Time-bounded price in the past is ignored.
	expProd := mkProduct(t, q, 1, "EXP-1", "exp-1")
	past := time.Now().Add(-48 * time.Hour)
	addPrice(t, q, wsList.ID, expProd, "1", "5.0000", &past)
	if _, err := q.ResolvePrice(ctx, gen.ResolvePriceParams{
		ID: other, ProductID: expProd, Column3: "1", Currency: "USD",
		WebsiteID: ptr(website), ValidFrom: nowTs(),
	}); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("expired price: want ErrNoRows, got %v", err)
	}
}

func TestResolvePrice_GroupLevel(t *testing.T) {
	pool := testsupport.NewDB(t)
	q := gen.New(pool)
	ctx := context.Background()

	grp, err := q.CreateCustomerGroup(ctx, gen.CreateCustomerGroupParams{OrganizationID: 1, Name: "Dealers"})
	if err != nil {
		t.Fatalf("group: %v", err)
	}
	prod := mkProduct(t, q, 1, "G-1", "g-1")
	cust := mkCustomer(t, q, 1, "Dealer A", ptr(grp.ID))

	groupList := mkList(t, q, 1, "Dealer Pricing", "USD")
	addPrice(t, q, groupList.ID, prod, "1", "70.0000", nil)
	if _, err := q.CreatePriceListAssignment(ctx, gen.CreatePriceListAssignmentParams{
		PriceListID: groupList.ID, CustomerGroupID: ptr(grp.ID), Priority: 0,
	}); err != nil {
		t.Fatalf("assign group: %v", err)
	}

	row, err := q.ResolvePrice(ctx, gen.ResolvePriceParams{
		ID: cust, ProductID: prod, Column3: "1", Currency: "USD", WebsiteID: ptr(int64(1)), ValidFrom: nowTs(),
	})
	if err != nil || row.Value != "70.0000" {
		t.Fatalf("group price: want 70.0000, got %q err=%v", row.Value, err)
	}
}

// ---- Read-time resolution: winning list + tiers + website fallback --------

func TestResolvePriceTier_WinningListAndTiers(t *testing.T) {
	pool := testsupport.NewDB(t)
	q := gen.New(pool)
	ctx := context.Background()
	const website = int64(1)

	prodA := mkProduct(t, q, 1, "A-1", "a-1")
	prodB := mkProduct(t, q, 1, "B-1", "b-1")

	// Website list prices both A and B.
	wsList := mkList(t, q, 1, "WS", "USD")
	addPrice(t, q, wsList.ID, prodA, "1", "100.0000", nil)
	addPrice(t, q, wsList.ID, prodB, "1", "200.0000", nil)
	if _, err := q.CreatePriceListAssignment(ctx, gen.CreatePriceListAssignmentParams{PriceListID: wsList.ID, WebsiteID: ptr(website)}); err != nil {
		t.Fatal(err)
	}

	// Customer list overrides only A, with two tiers.
	cust := mkCustomer(t, q, 1, "Acme", nil)
	custList := mkList(t, q, 1, "Acme", "USD")
	addPrice(t, q, custList.ID, prodA, "1", "80.0000", nil)
	addPrice(t, q, custList.ID, prodA, "10", "75.0000", nil)
	if _, err := q.CreatePriceListAssignment(ctx, gen.CreatePriceListAssignmentParams{PriceListID: custList.ID, CustomerID: ptr(cust)}); err != nil {
		t.Fatal(err)
	}

	// Resolved live: A → customer list (tiered); B → website fallback. No cache,
	// no recompute — the prices above are in effect immediately.
	if r, err := resolveTier(ctx, q, cust, prodA, "1"); err != nil || r.Value != "80.0000" {
		t.Errorf("A@1: want 80.0000, got %q err=%v", r.Value, err)
	}
	if r, err := resolveTier(ctx, q, cust, prodA, "10"); err != nil || r.Value != "75.0000" {
		t.Errorf("A@10 (tier): want 75.0000, got %q err=%v", r.Value, err)
	}
	if r, err := resolveTier(ctx, q, cust, prodB, "1"); err != nil || r.Value != "200.0000" {
		t.Errorf("B@1 (website fallback): want 200.0000, got %q err=%v", r.Value, err)
	}

	// The tiers-for-slug read returns the winning list's full tier ladder for A.
	tiers, err := q.ResolvePriceTiersForSlug(ctx, gen.ResolvePriceTiersForSlugParams{
		ID: cust, Slug: "a-1", OrganizationID: 1, Currency: "USD", WebsiteID: ptr(website), ValidFrom: nowTs(),
	})
	if err != nil {
		t.Fatalf("tiers: %v", err)
	}
	if len(tiers) != 2 || tiers[0].Value != "80.0000" || tiers[1].Value != "75.0000" {
		t.Errorf("A tiers: want [80,75], got %+v", tiers)
	}

	// A price change is live on the very next read — no fan-out, no staleness.
	addPrice(t, q, custList.ID, prodA, "1", "60.0000", nil)
	if r, err := resolveTier(ctx, q, cust, prodA, "1"); err != nil || r.Value != "60.0000" {
		t.Errorf("A@1 after edit: want 60.0000 immediately, got %q err=%v", r.Value, err)
	}
}

// ---- HTTP: auth gate + live read-time resolution -------------------------

func TestPricing_AuthGateAndLiveResolution(t *testing.T) {
	h, issuer, pool := newServer(t)
	q := gen.New(pool)
	ctx := context.Background()

	// 401 / 403 gates on create.
	if rr := do(t, h, http.MethodPost, "/admin/price-lists", "", nil); rr.Code != http.StatusUnauthorized {
		t.Fatalf("no token: want 401, got %d", rr.Code)
	}
	viewOnly := token(t, issuer, "price_list.view")
	if rr := do(t, h, http.MethodPost, "/admin/price-lists", viewOnly, map[string]any{"name": "X", "currency": "USD"}); rr.Code != http.StatusForbidden {
		t.Fatalf("view-only: want 403, got %d", rr.Code)
	}

	tok := token(t, issuer, "price_list.view", "price_list.manage")

	// Create list, add a price, assign to a customer — each mutation auto-enqueues
	// recompute (synchronously in tests).
	listResp := do(t, h, http.MethodPost, "/admin/price-lists", tok, map[string]any{"name": "Acme", "currency": "USD"})
	if listResp.Code != http.StatusCreated {
		t.Fatalf("create list: %d (%s)", listResp.Code, listResp.Body.String())
	}
	var list gen.PriceList
	_ = json.Unmarshal(listResp.Body.Bytes(), &list)

	prod := mkProduct(t, q, 1, "WIDGET", "widget")
	cust := mkCustomer(t, q, 1, "Acme Inc", nil)

	if rr := do(t, h, http.MethodPost, "/admin/price-list-assignments", tok, map[string]any{
		"price_list_id": list.ID, "customer_id": cust,
	}); rr.Code != http.StatusCreated {
		t.Fatalf("assign: %d (%s)", rr.Code, rr.Body.String())
	}
	if rr := do(t, h, http.MethodPost, "/admin/price-lists/"+strconv.FormatInt(list.ID, 10)+"/prices", tok, map[string]any{
		"product_id": prod, "min_quantity": "1", "value": "42.0000",
	}); rr.Code != http.StatusCreated {
		t.Fatalf("upsert price: %d (%s)", rr.Code, rr.Body.String())
	}

	// No recompute, no cache: read-time resolution reflects the price immediately.
	if got, err := resolveTier(ctx, q, cust, prod, "1"); err != nil || got.Value != "42.0000" {
		t.Fatalf("live resolve: want 42.0000, got %q err=%v", got.Value, err)
	}

	// resolve endpoint reflects the price too.
	rr := do(t, h, http.MethodGet, "/admin/pricing/resolve?customer_id="+strconv.FormatInt(cust, 10)+"&product_id="+strconv.FormatInt(prod, 10)+"&quantity=1&currency=USD", tok, nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("resolve: %d (%s)", rr.Code, rr.Body.String())
	}
	var res struct {
		PriceOnRequest bool   `json:"price_on_request"`
		Value          string `json:"value"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &res)
	if res.PriceOnRequest || res.Value != "42.0000" {
		t.Errorf("resolve result: want value 42.0000, got %+v", res)
	}

	// The resolved-prices admin page returns this customer's live prices.
	pr := do(t, h, http.MethodGet, "/admin/customers/"+strconv.FormatInt(cust, 10)+"/resolved-prices?currency=USD", tok, nil)
	if pr.Code != http.StatusOK {
		t.Fatalf("resolved-prices: %d (%s)", pr.Code, pr.Body.String())
	}
	var page struct {
		Items []struct {
			ProductID int64  `json:"product_id"`
			Value     string `json:"value"`
		} `json:"items"`
	}
	_ = json.Unmarshal(pr.Body.Bytes(), &page)
	if len(page.Items) != 1 || page.Items[0].Value != "42.0000" {
		t.Errorf("resolved-prices page: want [42.0000], got %+v", page.Items)
	}
}

func TestTenantIsolation_PriceLists(t *testing.T) {
	h, issuer, pool := newServer(t)
	tok := token(t, issuer, "price_list.view", "price_list.manage")
	ctx := context.Background()

	var org2 int64
	if err := pool.QueryRow(ctx, `INSERT INTO organizations (name) VALUES ('Org Two') RETURNING id`).Scan(&org2); err != nil {
		t.Fatalf("org2: %v", err)
	}
	if _, err := gen.New(pool).CreatePriceList(ctx, gen.CreatePriceListParams{
		OrganizationID: org2, Name: "Org2 List", Currency: "USD", IsActive: true,
	}); err != nil {
		t.Fatalf("org2 list: %v", err)
	}

	rr := do(t, h, http.MethodGet, "/admin/price-lists", tok, nil)
	var resp struct {
		Items []gen.PriceList `json:"items"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)
	for _, pl := range resp.Items {
		if pl.Name == "Org2 List" {
			t.Error("tenant isolation breach: org1 sees org2 price list")
		}
	}
}

// ---- account-hierarchy price inheritance (PRD §5.1) ----------------------

func TestResolvePrice_InheritsFromAncestor(t *testing.T) {
	pool := testsupport.NewDB(t)
	q := gen.New(pool)
	ctx := context.Background()
	const website = int64(1)

	prod := mkProduct(t, q, 1, "HP-1", "hp-1")

	parent := mkCustomer(t, q, 1, "Parent Co", nil)
	child := mkCustomer(t, q, 1, "Child Co", nil)
	if _, err := pool.Exec(ctx, `UPDATE customers SET parent_id=$1 WHERE id=$2`, parent, child); err != nil {
		t.Fatalf("set parent: %v", err)
	}

	// A contract list assigned to the PARENT only.
	parentList := mkList(t, q, 1, "Parent Contract", "USD")
	addPrice(t, q, parentList.ID, prod, "1", "42.0000", nil)
	if _, err := q.CreatePriceListAssignment(ctx, gen.CreatePriceListAssignmentParams{PriceListID: parentList.ID, CustomerID: ptr(parent)}); err != nil {
		t.Fatalf("assign parent: %v", err)
	}

	resolve := func(cust int64) (gen.ResolvePriceRow, error) {
		return q.ResolvePrice(ctx, gen.ResolvePriceParams{ID: cust, ProductID: prod, Column3: "1", Currency: "USD", WebsiteID: ptr(website), ValidFrom: nowTs()})
	}

	// The child has no list of its own → inherits the parent's contract price.
	if row, err := resolve(child); err != nil || row.Value != "42.0000" {
		t.Fatalf("child inherits ancestor price: want 42.0000, got %q err=%v", row.Value, err)
	}

	// A child's OWN assignment still beats the inherited one.
	childList := mkList(t, q, 1, "Child Contract", "USD")
	addPrice(t, q, childList.ID, prod, "1", "30.0000", nil)
	if _, err := q.CreatePriceListAssignment(ctx, gen.CreatePriceListAssignmentParams{PriceListID: childList.ID, CustomerID: ptr(child)}); err != nil {
		t.Fatalf("assign child: %v", err)
	}
	if row, err := resolve(child); err != nil || row.Value != "30.0000" {
		t.Fatalf("own price beats inherited: want 30.0000, got %q err=%v", row.Value, err)
	}

	// Inheritance also flows through the read-time tier resolver: a fresh
	// customer whose only price source is its parent's list (42).
	niece := mkCustomer(t, q, 1, "Niece Co", nil)
	if _, err := pool.Exec(ctx, `UPDATE customers SET parent_id=$1 WHERE id=$2`, parent, niece); err != nil {
		t.Fatalf("set niece parent: %v", err)
	}
	cp, err := resolveTier(ctx, q, niece, prod, "1")
	if err != nil || cp.Value != "42.0000" {
		t.Fatalf("niece inherits parent price via read-time resolution: want 42.0000, got %q err=%v", cp.Value, err)
	}
}
