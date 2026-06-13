// Package simulation_test is a real-world load + capability simulation
// ("Toyoka", an automaker selling cars + spare parts to 1000+ global customers).
// It seeds at scale (raw SQL — seeding is allowed to be fast) and then drives
// OPERATIONS through the full armed HTTP stack (auth → RLS → billing gate → every
// module) via httptest, measuring latency distributions and recording where the
// platform shines, strains, or fails.
//
// Gated behind SIM=1 so it never runs in the normal suite. Run against the live
// Postgres (isolated, auto-dropped clone — your b2b data is untouched):
//
//	TEST_DATABASE_URL='postgres://b2b:b2b@localhost:5432/b2b?sslmode=disable' \
//	SIM=1 go test ./internal/simulation -run Toyoka -v -timeout 30m
//
// Tunables (env): SIM_PARTS, SIM_CUSTOMERS, SIM_ORDERS, SIM_SEARCHES, SIM_CONCURRENCY.
package simulation_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"os"
	"sort"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"b2bcommerce/internal/auth"
	appdb "b2bcommerce/internal/db"
	"b2bcommerce/internal/server"
	"b2bcommerce/internal/store"
	"b2bcommerce/internal/store/gen"
	"b2bcommerce/internal/testsupport"
)

const secret = "sim-secret-please-change"

func envInt(k string, def int) int {
	if v := os.Getenv(k); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

// ---- latency stats ---------------------------------------------------------

type stat struct {
	name string
	ds   []time.Duration
	errs int
	mu   sync.Mutex
}

func (s *stat) add(d time.Duration, ok bool) {
	s.mu.Lock()
	s.ds = append(s.ds, d)
	if !ok {
		s.errs++
	}
	s.mu.Unlock()
}

func (s *stat) line(wall time.Duration) string {
	if len(s.ds) == 0 {
		return fmt.Sprintf("%-28s no samples", s.name)
	}
	cp := append([]time.Duration(nil), s.ds...)
	sort.Slice(cp, func(i, j int) bool { return cp[i] < cp[j] })
	p := func(q float64) time.Duration { return cp[int(float64(len(cp)-1)*q)] }
	var sum time.Duration
	for _, d := range cp {
		sum += d
	}
	tput := float64(len(cp)) / wall.Seconds()
	return fmt.Sprintf("%-28s n=%-5d errs=%-4d p50=%-8v p95=%-8v max=%-8v mean=%-8v  %.0f ops/s",
		s.name, len(cp), s.errs, p(0.50).Round(time.Microsecond), p(0.95).Round(time.Microsecond),
		cp[len(cp)-1].Round(time.Microsecond), (sum / time.Duration(len(cp))).Round(time.Microsecond), tput)
}

// report accumulates the human-readable findings log.
type report struct{ b bytes.Buffer }

func (r *report) logf(f string, a ...any) { fmt.Fprintf(&r.b, f+"\n", a...) }

func TestToyokaSimulation(t *testing.T) {
	if os.Getenv("SIM") != "1" {
		t.Skip("set SIM=1 (and TEST_DATABASE_URL) to run the Toyoka load simulation")
	}
	nParts := envInt("SIM_PARTS", 5000)
	nCust := envInt("SIM_CUSTOMERS", 1200)
	nOrders := envInt("SIM_ORDERS", 800)
	nSearch := envInt("SIM_SEARCHES", 500)
	conc := envInt("SIM_CONCURRENCY", 32)
	rng := rand.New(rand.NewSource(42))

	ctx := context.Background()
	seedPool, dsn := testsupport.NewDBWithDSN(t)
	armed, err := appdb.NewPoolWithConfig(ctx, dsn, appdb.PoolConfig{MaxConns: 40, ArmTenantRLS: true})
	if err != nil {
		t.Fatalf("armed pool: %v", err)
	}
	defer armed.Close()
	issuer := auth.NewIssuer(secret, time.Hour)
	h := server.New(store.New(armed), issuer)

	rep := &report{}
	rep.logf("# Toyoka simulation — %s", time.Now().Format(time.RFC3339))
	rep.logf("config: parts=%d customers=%d orders=%d searches=%d concurrency=%d", nParts, nCust, nOrders, nSearch, conc)

	// ===================== SEED (raw SQL, measured) =========================
	seedStart := time.Now()
	orgID, adminUserID, partIDs, custIDs := seedToyoka(t, ctx, seedPool, nParts, nCust, rng, rep)
	rep.logf("seed: org=%d admin_user=%d parts=%d customers=%d in %v",
		orgID, adminUserID, len(partIDs), len(custIDs), time.Since(seedStart).Round(time.Millisecond))

	// Broad admin token for the Toyoka operator running the battery.
	perms := []string{
		"product.view", "product.manage", "category.view", "customer.view", "customer.manage",
		"order.view", "order.manage", "quote.view", "quote.manage", "rfq.view",
		"invoice.view", "invoice.manage", "shipment.view", "shipment.manage", "return.view",
		"price_list.view", "promotion.view", "settings.view", "inventory.view", "report.view",
		"rebate.view", "rebate.manage", "subscription.view", "fx.view", "merchandising.view",
		"crm.view", "workflow.view", "cms.view", "tenant.view", "vendor.view",
	}
	tok, _ := issuer.Issue(strconv.FormatInt(adminUserID, 10), orgID, "admin", perms)

	do := func(method, path string, body any) (*httptest.ResponseRecorder, time.Duration) {
		var buf bytes.Buffer
		if body != nil {
			_ = json.NewEncoder(&buf).Encode(body)
		}
		req := httptest.NewRequest(method, path, &buf)
		req.Header.Set("Authorization", "Bearer "+tok)
		rr := httptest.NewRecorder()
		start := time.Now()
		h.ServeHTTP(rr, req)
		return rr, time.Since(start)
	}

	// ===================== A. Catalog faceted search ========================
	{
		terms := []string{"brake", "filter", "pump", "sensor", "gasket", "bearing", "valve", "clutch", "radiator", "alternator", ""}
		s := &stat{name: "catalog.search"}
		start := time.Now()
		for i := 0; i < nSearch; i++ {
			q := terms[rng.Intn(len(terms))]
			rr, d := do(http.MethodGet, "/storefront/catalog?q="+q+"&page_size=24", nil)
			s.add(d, rr.Code == http.StatusOK)
		}
		rep.logf("[A] %s", s.line(time.Since(start)))
	}

	// ===================== B. Order-on-behalf (sequential) ==================
	orderPub := make([]string, 0, nOrders)
	{
		s := &stat{name: "order.create.seq"}
		start := time.Now()
		for i := 0; i < nOrders; i++ {
			body := randomOrder(rng, custIDs, partIDs, 5+rng.Intn(35)) // 5–40 lines
			rr, d := do(http.MethodPost, "/admin/orders", body)
			ok := rr.Code == http.StatusOK
			s.add(d, ok)
			if ok {
				if pid := extractPublicID(rr.Body.Bytes()); pid != "" {
					orderPub = append(orderPub, pid)
				}
			} else if s.errs <= 2 {
				rep.logf("    order.create error (%d): %s", rr.Code, truncate(rr.Body.String(), 160))
			}
		}
		rep.logf("[B] %s", s.line(time.Since(start)))
	}

	// ===================== B2. Mega parts orders (200+ lines) ===============
	{
		s := &stat{name: "order.create.mega200"}
		start := time.Now()
		for i := 0; i < 10; i++ {
			body := randomOrder(rng, custIDs, partIDs, 200)
			rr, d := do(http.MethodPost, "/admin/orders", body)
			s.add(d, rr.Code == http.StatusOK)
			if rr.Code != http.StatusOK && s.errs <= 2 {
				rep.logf("    mega order error (%d): %s", rr.Code, truncate(rr.Body.String(), 160))
			}
		}
		rep.logf("[B2] %s", s.line(time.Since(start)))
	}

	// ===================== C. Concurrent order burst ========================
	{
		s := &stat{name: "order.create.concurrent"}
		burst := conc * 8
		start := time.Now()
		var wg sync.WaitGroup
		sem := make(chan struct{}, conc)
		for i := 0; i < burst; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()
				// each goroutine needs its own rng draw guarded — use a local source
				body := randomOrder(rand.New(rand.NewSource(rng.Int63())), custIDs, partIDs, 5+rand.Intn(20))
				rr, d := do(http.MethodPost, "/admin/orders", body)
				s.add(d, rr.Code == http.StatusOK)
			}()
		}
		wg.Wait()
		rep.logf("[C] %s (concurrency=%d)", s.line(time.Since(start)), conc)
	}

	// ===================== D. Quote create + send ===========================
	{
		sc := &stat{name: "quote.create"}
		ss := &stat{name: "quote.send"}
		start := time.Now()
		for i := 0; i < 100; i++ {
			body := randomOrder(rng, custIDs, partIDs, 3+rng.Intn(10)) // same line shape
			rr, d := do(http.MethodPost, "/admin/quotes", quoteBody(body))
			sc.add(d, rr.Code == http.StatusOK)
			if rr.Code == http.StatusOK {
				id := extractID(rr.Body.Bytes())
				if id > 0 {
					rr2, d2 := do(http.MethodPost, fmt.Sprintf("/admin/quotes/%d/send", id), nil)
					ss.add(d2, rr2.Code == http.StatusOK)
				}
			}
		}
		rep.logf("[D] %s", sc.line(time.Since(start)))
		rep.logf("[D] %s", ss.line(time.Since(start)))
	}

	// ===================== E. Invoice issuance ==============================
	{
		s := &stat{name: "invoice.issue"}
		start := time.Now()
		n := 0
		for _, pub := range orderPub {
			if n >= 200 {
				break
			}
			// Need the order's numeric id; fetch by listing is heavy, so resolve via DB.
			var oid int64
			if e := seedPool.QueryRow(ctx, `SELECT id FROM orders WHERE public_id=$1`, pub).Scan(&oid); e != nil {
				continue
			}
			rr, d := do(http.MethodPost, fmt.Sprintf("/admin/orders/%d/invoices", oid), nil)
			s.add(d, rr.Code == http.StatusOK || rr.Code == http.StatusCreated)
			n++
		}
		rep.logf("[E] %s", s.line(time.Since(start)))
	}

	// ===================== F. Reporting at scale ============================
	{
		for _, ep := range []string{"/admin/reports/summary?days=90", "/admin/reports/sales?days=90", "/admin/reports/top-products?days=90&limit=20"} {
			s := &stat{name: "report " + ep}
			start := time.Now()
			for i := 0; i < 20; i++ {
				rr, d := do(http.MethodGet, ep, nil)
				s.add(d, rr.Code == http.StatusOK)
			}
			rep.logf("[F] %s", s.line(time.Since(start)))
		}
	}

	// ===================== G. Low-stock report ==============================
	{
		s := &stat{name: "inventory.low-stock"}
		start := time.Now()
		for i := 0; i < 20; i++ {
			rr, d := do(http.MethodGet, "/admin/inventory/low-stock", nil)
			s.add(d, rr.Code == http.StatusOK)
			if i == 0 {
				rep.logf("    low-stock first page bytes=%d code=%d", rr.Body.Len(), rr.Code)
			}
		}
		rep.logf("[G] %s", s.line(time.Since(start)))
	}

	// ===================== H. Combined-price recompute ======================
	{
		// Read-time price resolution — the NEW hot path that replaced the
		// combined_prices cache. One resolve per (customer, product, qty); this
		// is what cart-add / quote / subscription pay on every line. No cache, no
		// recompute, no fan-out: a price edit is live on the next read.
		q := store.New(seedPool).Queries()
		var wsID int64
		_ = seedPool.QueryRow(ctx, `SELECT id FROM websites WHERE organization_id=$1 ORDER BY id LIMIT 1`, orgID).Scan(&wsID)
		s := &stat{name: "pricing.resolve/line"}
		n := envInt("SIM_RESOLVES", 2000)
		start := time.Now()
		for i := 0; i < n; i++ {
			cid := custIDs[rng.Intn(len(custIDs))]
			pid := partIDs[rng.Intn(len(partIDs))]
			t0 := time.Now()
			_, err := q.ResolvePriceTier(ctx, gen.ResolvePriceTierParams{
				ID: cid, ProductID: pid, Unit: "each", Column4: "5", Currency: "USD",
				WebsiteID: &wsID, ValidFrom: pgtype.Timestamptz{Time: time.Now(), Valid: true},
			})
			s.add(time.Since(t0), err == nil || errors.Is(err, pgx.ErrNoRows))
		}
		rep.logf("[H] %s", s.line(time.Since(start)))
		rep.logf("    (read-time resolution — no combined_prices cache, no recompute storm)")
	}

	// ===================== I. Rebate program → report → settle ==============
	{
		cr, _ := do(http.MethodPost, "/admin/rebates", map[string]any{
			"name": "Global Volume Rebate", "period": "annual", "currency": "USD",
		})
		if cr.Code == http.StatusCreated {
			pid := extractID(cr.Body.Bytes())
			do(http.MethodPost, fmt.Sprintf("/admin/rebates/%d/tiers", pid), map[string]any{"min_amount": "100000", "rate_percent": "3"})
			do(http.MethodPost, fmt.Sprintf("/admin/rebates/%d/tiers", pid), map[string]any{"min_amount": "1000000", "rate_percent": "6"})
			rr, d := do(http.MethodGet, fmt.Sprintf("/admin/rebates/%d/report", pid), nil)
			rep.logf("[I] rebate.report code=%d latency=%v bytes=%d", rr.Code, d.Round(time.Millisecond), rr.Body.Len())
			rrs, ds := do(http.MethodPost, fmt.Sprintf("/admin/rebates/%d/settle", pid), nil)
			rep.logf("[I] rebate.settle code=%d latency=%v %s", rrs.Code, ds.Round(time.Millisecond), truncate(rrs.Body.String(), 120))
		} else {
			rep.logf("[I] rebate.create FAILED code=%d %s", cr.Code, truncate(cr.Body.String(), 160))
		}
	}

	// ===================== J. Cross-tenant isolation @ scale ================
	{
		// A foreign org with full perms must see none of Toyoka's catalog/orders.
		ftok, _ := issuer.Issue("999999", 424242, "admin", perms)
		fdo := func(path string) int {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			req.Header.Set("Authorization", "Bearer "+ftok)
			rr := httptest.NewRecorder()
			h.ServeHTTP(rr, req)
			n := 0
			var env struct {
				Items []json.RawMessage `json:"items"`
			}
			_ = json.Unmarshal(rr.Body.Bytes(), &env)
			n = len(env.Items)
			return n
		}
		leakProducts := fdo("/storefront/catalog?page_size=24")
		leakOrders := fdo("/admin/orders")
		rep.logf("[J] foreign-org leak check: catalog items=%d orders items=%d (want 0/0)", leakProducts, leakOrders)
		if leakProducts != 0 || leakOrders != 0 {
			t.Errorf("ISOLATION LEAK: foreign org saw products=%d orders=%d", leakProducts, leakOrders)
		}
	}

	// ===================== totals ===========================================
	var totalOrders, totalItems, totalInvoices int64
	_ = seedPool.QueryRow(ctx, `SELECT count(*) FROM orders WHERE organization_id=$1`, orgID).Scan(&totalOrders)
	_ = seedPool.QueryRow(ctx, `SELECT count(*) FROM order_items oi JOIN orders o ON o.id=oi.order_id WHERE o.organization_id=$1`, orgID).Scan(&totalItems)
	_ = seedPool.QueryRow(ctx, `SELECT count(*) FROM invoices i JOIN orders o ON o.id=i.order_id WHERE o.organization_id=$1`, orgID).Scan(&totalInvoices)
	rep.logf("totals: orders=%d order_items=%d invoices=%d", totalOrders, totalItems, totalInvoices)

	// Dump the findings to stdout AND a file the report step can read.
	out := rep.b.String()
	t.Log("\n" + out)
	_ = os.WriteFile("toyoka_sim_result.txt", []byte(out), 0o644)
}

// ---- seeding (raw SQL for speed) -------------------------------------------

func seedToyoka(t *testing.T, ctx context.Context, pool *pgxpool.Pool, nParts, nCust int, rng *rand.Rand, rep *report) (orgID, adminUserID int64, partIDs, custIDs []int64) {
	t.Helper()
	mustExec := func(sql string, args ...any) {
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("seed exec failed: %v\nSQL: %s", err, truncate(sql, 200))
		}
	}

	if err := pool.QueryRow(ctx,
		`INSERT INTO organizations (name, status) VALUES ('Toyoka Motors', 'active') RETURNING id`).Scan(&orgID); err != nil {
		t.Fatalf("create org: %v", err)
	}
	// Plan: scale (unlimited) so the billing gate never blocks the battery.
	mustExec(`INSERT INTO org_subscriptions (organization_id, plan_id)
	          SELECT $1, id FROM plans WHERE code='scale'`, orgID)

	// Websites per region (USD global + regional currencies).
	regions := []struct{ name, domain, cur, loc string }{
		{"Toyoka USA", "us.toyoka.test", "USD", "en"},
		{"Toyoka Europe", "eu.toyoka.test", "EUR", "en"},
		{"Toyoka Japan", "jp.toyoka.test", "JPY", "ja"},
		{"Toyoka Africa", "ke.toyoka.test", "KES", "en"},
	}
	var defaultWebsiteID int64
	for i, r := range regions {
		var wid int64
		if err := pool.QueryRow(ctx,
			`INSERT INTO websites (organization_id, name, domain, default_currency, default_locale)
			 VALUES ($1,$2,$3,$4,$5) RETURNING id`, orgID, r.name, r.domain, r.cur, r.loc).Scan(&wid); err != nil {
			t.Fatalf("website: %v", err)
		}
		if i == 0 {
			defaultWebsiteID = wid
		}
	}

	// FX rates vs USD base.
	for _, fx := range []struct {
		q string
		r string
	}{{"EUR", "0.92"}, {"GBP", "0.79"}, {"JPY", "157.0"}, {"KES", "129.0"}} {
		mustExec(`INSERT INTO fx_rates (organization_id, base_currency, quote_currency, rate, as_of) VALUES ($1,'USD',$2,$3, now())`,
			orgID, fx.q, fx.r)
	}

	// Admin user (placed_by_sales_rep_id references users(id)).
	hash, _ := auth.HashPassword("toyoka-admin-123")
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (organization_id, email, password_hash, full_name)
		 VALUES ($1,'ops@toyoka.test',$2,'Toyoka Ops') RETURNING id`, orgID, hash).Scan(&adminUserID); err != nil {
		t.Fatalf("admin user: %v", err)
	}

	// Customer groups (dealer tiers).
	groupIDs := map[string]int64{}
	for _, g := range []string{"OEM Dealer", "Distributor", "Fleet", "Independent"} {
		var gid int64
		if err := pool.QueryRow(ctx,
			`INSERT INTO customer_groups (organization_id, name) VALUES ($1,$2) RETURNING id`, orgID, g).Scan(&gid); err != nil {
			t.Fatalf("group: %v", err)
		}
		groupIDs[g] = gid
	}

	// 12 vehicle models + N spare parts, in one server-side bulk insert each.
	models := []string{"Corolla", "Camry", "RAV4", "Hilux", "Land Cruiser", "Prius", "Yaris", "Tacoma", "Tundra", "Highlander", "Supra", "bZ4X"}
	for i, m := range models {
		mustExec(`INSERT INTO products (organization_id, sku, type, name, slug, description, status, attributes, unit)
		          VALUES ($1,$2,'simple',$3,$4,$5,'active',$6,'each')`,
			orgID, fmt.Sprintf("TYK-CAR-%03d", i+1), "Toyoka "+m,
			"toyoka-"+slugify(m), "Toyoka "+m+" vehicle", carAttrs(m))
	}
	// Parts: bulk via generate_series. search_vector is a STORED generated column
	// so it's populated automatically (and GIN-indexed).
	cats := []string{"brake", "filter", "pump", "sensor", "gasket", "bearing", "valve", "clutch", "radiator", "alternator", "spark plug", "belt"}
	mustExec(`
		INSERT INTO products (organization_id, sku, type, name, slug, description, status, attributes, unit)
		SELECT $1,
		       'TYK-PRT-' || lpad(g::text, 7, '0'),
		       'simple',
		       initcap(($2::text[])[ (g % array_length($2::text[],1)) + 1 ]) || ' Assembly ' || g::text,
		       'tyk-prt-' || g::text,
		       'Genuine Toyoka spare part ' || g::text || ' — fits multiple models',
		       'active',
		       jsonb_build_object('category', ($2::text[])[ (g % array_length($2::text[],1)) + 1 ], 'oem', true),
		       'each'
		FROM generate_series(1, $3) g`, orgID, cats, nParts)

	// Approve everything so faceted search (approval_status='approved') returns it.
	mustExec(`UPDATE products SET approval_status='approved' WHERE organization_id=$1`, orgID)

	// Capture part ids.
	rows, err := pool.Query(ctx, `SELECT id FROM products WHERE organization_id=$1 AND sku LIKE 'TYK-PRT-%' ORDER BY id`, orgID)
	if err != nil {
		t.Fatalf("load part ids: %v", err)
	}
	for rows.Next() {
		var id int64
		_ = rows.Scan(&id)
		partIDs = append(partIDs, id)
	}
	rows.Close()

	// Pricing: a global USD list assigned at website level; prices for every product.
	var globalListID int64
	if err := pool.QueryRow(ctx,
		`INSERT INTO price_lists (organization_id, name, currency, is_default, is_active)
		 VALUES ($1,'Global Wholesale USD','USD', true, true) RETURNING id`, orgID).Scan(&globalListID); err != nil {
		t.Fatalf("price list: %v", err)
	}
	mustExec(`INSERT INTO price_list_assignments (price_list_id, website_id, priority) VALUES ($1,$2,1)`, globalListID, defaultWebsiteID)
	// One base price per product + a 100+ volume tier.
	mustExec(`INSERT INTO prices (price_list_id, product_id, unit, min_quantity, value)
	          SELECT $1, id, 'each', 1, round((random()*900+50)::numeric, 2) FROM products WHERE organization_id=$2`, globalListID, orgID)
	mustExec(`INSERT INTO prices (price_list_id, product_id, unit, min_quantity, value)
	          SELECT $1, id, 'each', 100, round((random()*800+40)::numeric, 2) FROM products WHERE organization_id=$2`, globalListID, orgID)

	// Inventory: 4 warehouses, levels for every part, ~15% below reorder threshold.
	whIDs := []int64{}
	for _, wn := range []string{"Nagoya DC", "Brussels DC", "Dubai DC", "Nairobi DC"} {
		var wid int64
		if err := pool.QueryRow(ctx,
			`INSERT INTO warehouses (organization_id, name, is_active) VALUES ($1,$2,true) RETURNING id`, orgID, wn).Scan(&wid); err != nil {
			t.Fatalf("warehouse: %v", err)
		}
		whIDs = append(whIDs, wid)
	}
	for _, wid := range whIDs {
		mustExec(`INSERT INTO inventory_levels (product_id, warehouse_id, quantity_on_hand, quantity_reserved, reorder_threshold)
		          SELECT id, $1,
		                 (random()*500)::int,
		                 0,
		                 50
		          FROM products WHERE organization_id=$2 AND sku LIKE 'TYK-PRT-%'
		          ON CONFLICT (product_id, warehouse_id) DO NOTHING`, wid, orgID)
	}

	// Customers: bulk via generate_series across groups + regions, each with a
	// default billing + shipping address. customer_users share one bcrypt hash.
	grpArr := []int64{groupIDs["OEM Dealer"], groupIDs["Distributor"], groupIDs["Fleet"], groupIDs["Independent"]}
	mustExec(`
		INSERT INTO customers (organization_id, customer_group_id, name, payment_terms_days, credit_limit)
		SELECT $1,
		       ($2::bigint[])[ (g % 4) + 1 ],
		       'Dealer ' || lpad(g::text, 5, '0'),
		       (ARRAY[15,30,45,60])[ (g % 4) + 1 ],
		       round((random()*900000+100000)::numeric, 2)
		FROM generate_series(1, $3) g`, orgID, grpArr, nCust)

	rows2, err := pool.Query(ctx, `SELECT id FROM customers WHERE organization_id=$1 ORDER BY id`, orgID)
	if err != nil {
		t.Fatalf("load cust ids: %v", err)
	}
	for rows2.Next() {
		var id int64
		_ = rows2.Scan(&id)
		custIDs = append(custIDs, id)
	}
	rows2.Close()

	countries := []string{"US", "DE", "JP", "KE", "GB", "FR", "ZA", "AE"}
	carr := "{" + joinCountries(countries) + "}"
	for _, ty := range []string{"billing", "shipping"} {
		mustExec(`
			INSERT INTO customer_addresses (customer_id, type, is_default, line1, city, region, postal_code, country)
			SELECT c.id, $2, true, '1 Industrial Way', 'Port City', 'Region', '00100',
			       ($3::text[])[ (c.id % array_length($3::text[],1)) + 1 ]
			FROM customers c WHERE c.organization_id=$1`, orgID, ty, carr)
	}
	mustExec(`
		INSERT INTO customer_users (customer_id, email, password_hash, full_name, role)
		SELECT c.id, 'buyer' || c.id || '@dealer.test', $2, 'Buyer ' || c.id, 'admin'
		FROM customers c WHERE c.organization_id=$1`, orgID, hash)

	return orgID, adminUserID, partIDs, custIDs
}

// ---- request builders ------------------------------------------------------

func randomOrder(rng *rand.Rand, custIDs, partIDs []int64, lines int) map[string]any {
	items := make([]map[string]any, 0, lines)
	seen := map[int64]bool{}
	for len(items) < lines {
		pid := partIDs[rng.Intn(len(partIDs))]
		if seen[pid] {
			continue
		}
		seen[pid] = true
		items = append(items, map[string]any{
			"product_id": pid,
			"quantity":   strconv.Itoa(1 + rng.Intn(20)),
			"unit_price": fmt.Sprintf("%d.00", 50+rng.Intn(900)),
		})
	}
	return map[string]any{
		"customer_id": custIDs[rng.Intn(len(custIDs))],
		"currency":    "USD",
		"items":       items,
	}
}

func quoteBody(order map[string]any) map[string]any {
	return map[string]any{"customer_id": order["customer_id"], "currency": "USD", "items": order["items"]}
}

// ---- helpers ---------------------------------------------------------------

func extractPublicID(b []byte) string {
	var m map[string]any
	if json.Unmarshal(b, &m) != nil {
		return ""
	}
	if v, ok := m["public_id"].(string); ok {
		return v
	}
	return ""
}

func extractID(b []byte) int64 {
	var m map[string]any
	if json.Unmarshal(b, &m) != nil {
		return 0
	}
	if v, ok := m["id"].(float64); ok {
		return int64(v)
	}
	return 0
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func slugify(s string) string {
	out := []rune{}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			out = append(out, r)
		case r >= 'A' && r <= 'Z':
			out = append(out, r+32)
		case r == ' ':
			out = append(out, '-')
		}
	}
	return string(out)
}

func carAttrs(model string) []byte {
	b, _ := json.Marshal(map[string]any{"category": "vehicle", "model": model, "oem": true})
	return b
}

func joinCountries(cs []string) string {
	out := ""
	for i, c := range cs {
		if i > 0 {
			out += ","
		}
		out += c
	}
	return out
}
