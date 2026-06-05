package crm_test

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

func newServer(t *testing.T) (http.Handler, *auth.Issuer, *pgxpool.Pool) {
	t.Helper()
	pool := testsupport.NewDB(t)
	st := store.New(pool)
	issuer := auth.NewIssuer(testSecret, time.Hour)
	return server.New(st, issuer), issuer, pool
}

func crmToken(t *testing.T, issuer *auth.Issuer) string {
	t.Helper()
	tok, err := issuer.Issue("1", 1, "admin", []string{"crm.view", "crm.manage"})
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

func stageID(t *testing.T, pool *pgxpool.Pool, code string) int64 {
	t.Helper()
	q := gen.New(pool)
	pl, err := q.GetDefaultPipeline(context.Background(), 1)
	if err != nil {
		t.Fatalf("default pipeline: %v", err)
	}
	stages, err := q.ListPipelineStages(context.Background(), pl.ID)
	if err != nil {
		t.Fatalf("stages: %v", err)
	}
	for _, s := range stages {
		if s.Code == code {
			return s.ID
		}
	}
	t.Fatalf("stage %q not found", code)
	return 0
}

// ---- lead conversion (idempotent) ----------------------------------------

func TestLeadConvertIsIdempotent(t *testing.T) {
	h, issuer, pool := newServer(t)
	tok := crmToken(t, issuer)

	cr := do(t, h, http.MethodPost, "/admin/leads", tok, map[string]any{
		"company_name": "Globex Inc", "contact_name": "Hank Scorpio", "email": "hank@globex.test",
	})
	if cr.Code != http.StatusCreated {
		t.Fatalf("create lead: %d (%s)", cr.Code, cr.Body.String())
	}
	var lead struct {
		ID     int64  `json:"id"`
		Status string `json:"status"`
	}
	decode(t, cr, &lead)

	conv := do(t, h, http.MethodPost, "/admin/leads/"+strconv.FormatInt(lead.ID, 10)+"/convert", tok, nil)
	if conv.Code != http.StatusOK {
		t.Fatalf("convert: %d (%s)", conv.Code, conv.Body.String())
	}
	var res struct {
		CustomerID    int64 `json:"customer_id"`
		ContactID     int64 `json:"contact_id"`
		OpportunityID int64 `json:"opportunity_id"`
	}
	decode(t, conv, &res)
	if res.CustomerID == 0 || res.ContactID == 0 || res.OpportunityID == 0 {
		t.Fatalf("convert should create customer+contact+opportunity, got %+v", res)
	}

	// Lead is now converted; an opportunity_stage_history row exists for the opp.
	var n int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM opportunity_stage_history WHERE opportunity_id=$1`, res.OpportunityID).Scan(&n); err != nil {
		t.Fatalf("history: %v", err)
	}
	if n != 1 {
		t.Errorf("initial stage history: want 1, got %d", n)
	}

	// Second conversion is rejected (idempotency).
	again := do(t, h, http.MethodPost, "/admin/leads/"+strconv.FormatInt(lead.ID, 10)+"/convert", tok, nil)
	if again.Code != http.StatusConflict {
		t.Errorf("double convert: want 409, got %d (%s)", again.Code, again.Body.String())
	}
}

// ---- opportunity stage moves + history -----------------------------------

func TestOpportunityStageMoveWritesHistory(t *testing.T) {
	h, issuer, pool := newServer(t)
	tok := crmToken(t, issuer)
	q := gen.New(pool)
	ctx := context.Background()

	cust, _ := q.CreateCustomer(ctx, gen.CreateCustomerParams{OrganizationID: 1, Name: "Acme", CreditLimit: "0"})

	cr := do(t, h, http.MethodPost, "/admin/opportunities", tok, map[string]any{
		"customer_id": cust.ID, "name": "Big deal", "amount": "1000.0000", "currency": "USD",
	})
	if cr.Code != http.StatusCreated {
		t.Fatalf("create opp: %d (%s)", cr.Code, cr.Body.String())
	}
	var opp struct {
		ID       int64   `json:"id"`
		StageID  int64   `json:"stage_id"`
		ClosedAt *string `json:"closed_at"`
	}
	decode(t, cr, &opp)

	// Move to "won" → closed_at stamped.
	won := stageID(t, pool, "won")
	mv := do(t, h, http.MethodPatch, "/admin/opportunities/"+strconv.FormatInt(opp.ID, 10)+"/stage", tok, map[string]any{"stage_id": won})
	if mv.Code != http.StatusOK {
		t.Fatalf("move stage: %d (%s)", mv.Code, mv.Body.String())
	}
	var moved struct {
		StageID  int64            `json:"stage_id"`
		ClosedAt *json.RawMessage `json:"closed_at"`
	}
	decode(t, mv, &moved)
	if moved.StageID != won {
		t.Errorf("stage not updated: want %d, got %d", won, moved.StageID)
	}

	// History: initial (create) + the move = 2 rows.
	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM opportunity_stage_history WHERE opportunity_id=$1`, opp.ID).Scan(&n); err != nil {
		t.Fatalf("history: %v", err)
	}
	if n != 2 {
		t.Errorf("stage history rows: want 2, got %d", n)
	}

	// closed_at must be set after moving to a won stage.
	var closed *time.Time
	if err := pool.QueryRow(ctx, `SELECT closed_at FROM opportunities WHERE id=$1`, opp.ID).Scan(&closed); err != nil {
		t.Fatalf("closed_at: %v", err)
	}
	if closed == nil {
		t.Error("closed_at should be set after moving to a won stage")
	}

	// A stage from another pipeline is rejected.
	bad := do(t, h, http.MethodPatch, "/admin/opportunities/"+strconv.FormatInt(opp.ID, 10)+"/stage", tok, map[string]any{"stage_id": 999999})
	if bad.Code != http.StatusBadRequest {
		t.Errorf("invalid stage: want 400, got %d", bad.Code)
	}
}

// ---- weighted forecast on the board --------------------------------------

func TestPipelineBoardWeightedForecast(t *testing.T) {
	h, issuer, pool := newServer(t)
	tok := crmToken(t, issuer)
	q := gen.New(pool)
	ctx := context.Background()

	pl, _ := q.GetDefaultPipeline(ctx, 1)
	cust, _ := q.CreateCustomer(ctx, gen.CreateCustomerParams{OrganizationID: 1, Name: "Acme", CreditLimit: "0"})
	qualified := stageID(t, pool, "qualified") // probability 25.00

	// One open opportunity of 1000 in the 25% stage → weighted 250.
	if _, err := q.CreateOpportunity(ctx, gen.CreateOpportunityParams{
		OrganizationID: 1, CustomerID: cust.ID, PipelineID: pl.ID, StageID: qualified,
		Name: "Deal", Amount: "1000.0000", Currency: "USD",
	}); err != nil {
		t.Fatalf("seed opp: %v", err)
	}

	rr := do(t, h, http.MethodGet, "/admin/pipelines/"+strconv.FormatInt(pl.ID, 10)+"/board", tok, nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("board: %d (%s)", rr.Code, rr.Body.String())
	}
	var board struct {
		Items []struct {
			Code           string `json:"code"`
			OpenCount      int64  `json:"open_count"`
			TotalAmount    string `json:"total_amount"`
			WeightedAmount string `json:"weighted_amount"`
		} `json:"items"`
	}
	decode(t, rr, &board)
	var found bool
	for _, s := range board.Items {
		if s.Code == "qualified" {
			found = true
			if s.OpenCount != 1 || s.TotalAmount != "1000.0000" || s.WeightedAmount != "250.0000" {
				t.Errorf("qualified stage: want count1/total1000/weighted250, got %d/%s/%s", s.OpenCount, s.TotalAmount, s.WeightedAmount)
			}
		}
	}
	if !found {
		t.Error("qualified stage missing from board")
	}
}

// ---- unified activity timeline -------------------------------------------

func TestCustomerTimelineAggregates(t *testing.T) {
	h, issuer, pool := newServer(t)
	tok := crmToken(t, issuer)
	q := gen.New(pool)
	ctx := context.Background()

	pl, _ := q.GetDefaultPipeline(ctx, 1)
	first := stageID(t, pool, "new")
	cust, _ := q.CreateCustomer(ctx, gen.CreateCustomerParams{OrganizationID: 1, Name: "Acme", CreditLimit: "0"})
	contact, _ := q.CreateContact(ctx, gen.CreateContactParams{OrganizationID: 1, CustomerID: &cust.ID, FullName: "Pat Buyer"})
	opp, _ := q.CreateOpportunity(ctx, gen.CreateOpportunityParams{
		OrganizationID: 1, CustomerID: cust.ID, PipelineID: pl.ID, StageID: first, Name: "Deal", Amount: "0", Currency: "USD",
	})

	// One activity each on the customer, its contact, and its opportunity.
	do(t, h, http.MethodPost, "/admin/activities", tok, map[string]any{"type": "note", "subject": "on customer", "customer_id": cust.ID})
	do(t, h, http.MethodPost, "/admin/activities", tok, map[string]any{"type": "call", "subject": "on contact", "contact_id": contact.ID})
	do(t, h, http.MethodPost, "/admin/activities", tok, map[string]any{"type": "meeting", "subject": "on opp", "opportunity_id": opp.ID})

	rr := do(t, h, http.MethodGet, "/admin/customers/"+strconv.FormatInt(cust.ID, 10)+"/timeline", tok, nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("timeline: %d (%s)", rr.Code, rr.Body.String())
	}
	var tl struct {
		Items []struct {
			Subject string `json:"subject"`
		} `json:"items"`
	}
	decode(t, rr, &tl)
	if len(tl.Items) != 3 {
		t.Fatalf("timeline should aggregate customer+contact+opportunity activities: want 3, got %d", len(tl.Items))
	}
}

// ---- auth / audience / tenant isolation ----------------------------------

func TestCrmAuthAndIsolation(t *testing.T) {
	h, issuer, pool := newServer(t)
	tok := crmToken(t, issuer)
	q := gen.New(pool)
	ctx := context.Background()

	// Storefront token cannot reach admin CRM.
	custTok, _ := issuer.IssueStorefront(0, 1, 12345)
	if rr := do(t, h, http.MethodGet, "/admin/leads", custTok, nil); rr.Code != http.StatusForbidden {
		t.Errorf("storefront token on /admin/leads: want 403, got %d", rr.Code)
	}
	// A token without crm.view is forbidden.
	noPerm, _ := issuer.Issue("1", 1, "admin", []string{"product.view"})
	if rr := do(t, h, http.MethodGet, "/admin/leads", noPerm, nil); rr.Code != http.StatusForbidden {
		t.Errorf("missing permission: want 403, got %d", rr.Code)
	}

	// A lead in another org must not appear in org-1's list.
	var org2 int64
	if err := pool.QueryRow(ctx, `INSERT INTO organizations (name) VALUES ('Org Two') RETURNING id`).Scan(&org2); err != nil {
		t.Fatalf("org2: %v", err)
	}
	if _, err := q.CreateLead(ctx, gen.CreateLeadParams{OrganizationID: org2, Source: "manual", CompanyName: strptr("Other Co")}); err != nil {
		t.Fatalf("org2 lead: %v", err)
	}
	rr := do(t, h, http.MethodGet, "/admin/leads", tok, nil)
	var resp struct {
		Items []struct {
			CompanyName *string `json:"company_name"`
		} `json:"items"`
	}
	decode(t, rr, &resp)
	for _, l := range resp.Items {
		if l.CompanyName != nil && *l.CompanyName == "Other Co" {
			t.Error("tenant isolation breach: org-2 lead in org-1 list")
		}
	}
}

func strptr(s string) *string { return &s }

// ---- public storefront lead capture --------------------------------------

func TestSubmitStorefrontLead(t *testing.T) {
	h, issuer, _ := newServer(t)
	tok := crmToken(t, issuer)

	// Anonymous submission (no token) succeeds and creates a storefront_form lead.
	rr := do(t, h, http.MethodPost, "/storefront/leads", "", map[string]any{
		"contact_name": "Jane Buyer", "email": "jane@prospect.test",
		"company_name": "Prospect Inc", "notes": "Need 500 widgets",
	})
	if rr.Code != http.StatusCreated {
		t.Fatalf("submit lead: want 201, got %d (%s)", rr.Code, rr.Body.String())
	}

	// It shows up in the admin lead list with source storefront_form.
	la := do(t, h, http.MethodGet, "/admin/leads", tok, nil)
	var resp struct {
		Items []struct {
			Source      string  `json:"source"`
			ContactName *string `json:"contact_name"`
		} `json:"items"`
	}
	decode(t, la, &resp)
	found := false
	for _, l := range resp.Items {
		if l.ContactName != nil && *l.ContactName == "Jane Buyer" && l.Source == "storefront_form" {
			found = true
		}
	}
	if !found {
		t.Errorf("submitted lead not found with source storefront_form: %+v", resp.Items)
	}
}

func TestSubmitStorefrontLeadValidation(t *testing.T) {
	h, _, _ := newServer(t)
	// Missing contact_name.
	if rr := do(t, h, http.MethodPost, "/storefront/leads", "", map[string]any{"email": "x@y.test"}); rr.Code != http.StatusBadRequest {
		t.Errorf("no contact_name: want 400, got %d", rr.Code)
	}
	// No email and no phone.
	if rr := do(t, h, http.MethodPost, "/storefront/leads", "", map[string]any{"contact_name": "X"}); rr.Code != http.StatusBadRequest {
		t.Errorf("no email/phone: want 400, got %d", rr.Code)
	}
}

// ---- account health / churn risk -----------------------------------------

func TestAccountHealth(t *testing.T) {
	h, issuer, pool := newServer(t)
	tok := crmToken(t, issuer)
	q := gen.New(pool)
	ctx := context.Background()

	// orders with backdated created_at (no created_at trigger on orders).
	seedAccount := func(name string, agos ...int) int64 {
		cust, err := q.CreateCustomer(ctx, gen.CreateCustomerParams{OrganizationID: 1, Name: name, CreditLimit: "0"})
		if err != nil {
			t.Fatalf("customer: %v", err)
		}
		for _, ago := range agos {
			o, err := q.CreateOrder(ctx, gen.CreateOrderParams{
				OrganizationID: 1, WebsiteID: 1, CustomerID: cust.ID, Currency: "USD",
				BillingAddress: []byte("{}"), ShippingAddress: []byte("{}"),
				Subtotal: "100", TaxTotal: "0", ShippingTotal: "0", GrandTotal: "100",
			})
			if err != nil {
				t.Fatalf("order: %v", err)
			}
			if _, err := pool.Exec(ctx, `UPDATE orders SET created_at = now() - make_interval(days => $1) WHERE id = $2`, ago, o.ID); err != nil {
				t.Fatalf("backdate: %v", err)
			}
		}
		return cust.ID
	}
	// Slipping: cadence ~30d but silent for 90d (overdue).
	slipping := seedAccount("Slipping Co", 150, 120, 90)
	// Healthy: ordered recently and regularly.
	healthy := seedAccount("Healthy Co", 40, 20, 5)

	rr := do(t, h, http.MethodGet, "/admin/accounts/health", tok, nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("health: %d (%s)", rr.Code, rr.Body.String())
	}
	var resp struct {
		Items []struct {
			CustomerID int64 `json:"customer_id"`
			AtRisk     bool  `json:"at_risk"`
		} `json:"items"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)
	risk := map[int64]bool{}
	for _, it := range resp.Items {
		risk[it.CustomerID] = it.AtRisk
	}
	if !risk[slipping] {
		t.Errorf("slipping account should be at_risk")
	}
	if risk[healthy] {
		t.Errorf("healthy account should not be at_risk")
	}

	// at_risk filter returns the slipping account but not the healthy one.
	fr := do(t, h, http.MethodGet, "/admin/accounts/health?at_risk=true", tok, nil)
	var fresp struct {
		Items []struct {
			CustomerID int64 `json:"customer_id"`
		} `json:"items"`
	}
	_ = json.Unmarshal(fr.Body.Bytes(), &fresp)
	seen := map[int64]bool{}
	for _, it := range fresp.Items {
		seen[it.CustomerID] = true
	}
	if !seen[slipping] || seen[healthy] {
		t.Errorf("at_risk filter: want slipping only (slipping=%v healthy=%v)", seen[slipping], seen[healthy])
	}
}
