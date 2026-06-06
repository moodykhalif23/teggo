package tools_test

import (
	"context"
	"strings"
	"testing"

	"b2bcommerce/internal/ai"
	"b2bcommerce/internal/ai/tools"
	"b2bcommerce/internal/store/gen"
	"b2bcommerce/internal/testsupport"
)

// TestBuyerToolsAgainstRealQueries seeds a customer + order and exercises the
// buyer tools end to end through the deterministic agent, proving the catalog
// wires correctly to the existing queries under the caller's scope.
func TestBuyerToolsAgainstRealQueries(t *testing.T) {
	pool := testsupport.NewDB(t)
	q := gen.New(pool)
	ctx := context.Background()

	cust, err := q.CreateCustomer(ctx, gen.CreateCustomerParams{OrganizationID: 1, Name: "Buyer Co", CreditLimit: "0"})
	if err != nil {
		t.Fatalf("customer: %v", err)
	}
	order, err := q.CreateOrder(ctx, gen.CreateOrderParams{
		OrganizationID: 1, WebsiteID: 1, CustomerID: cust.ID, Currency: "USD",
		BillingAddress: []byte("{}"), ShippingAddress: []byte("{}"),
		Subtotal: "500", TaxTotal: "0", ShippingTotal: "0", GrandTotal: "500",
	})
	if err != nil {
		t.Fatalf("order: %v", err)
	}

	reg := ai.NewRegistry(tools.All()...)
	agent := ai.NewAgent(ai.NewDeterministicProvider(), reg)
	tc := ai.ToolContext{OrgID: 1, Audience: "storefront", CustomerID: cust.ID, Q: q}

	// list_orders
	r, err := agent.Handle(ctx, tc, "show me my recent orders", nil)
	if err != nil {
		t.Fatalf("list_orders: %v", err)
	}
	if r.Tool != "list_orders" || !strings.Contains(r.Text, "1 orders") {
		t.Errorf("list_orders: tool=%q text=%q", r.Tool, r.Text)
	}

	// order_status by public_id prefix
	prefix := order.PublicID.String()[:8]
	r, err = agent.Handle(ctx, tc, "what is the status of order "+prefix, nil)
	if err != nil {
		t.Fatalf("order_status: %v", err)
	}
	if r.Tool != "order_status" || r.Data["status"] != "pending" {
		t.Errorf("order_status: tool=%q data=%v text=%q", r.Tool, r.Data, r.Text)
	}

	// outstanding_invoices (none yet)
	r, _ = agent.Handle(ctx, tc, "what do I owe?", nil)
	if r.Tool != "outstanding_invoices" || !strings.Contains(r.Text, "no outstanding") {
		t.Errorf("outstanding_invoices: tool=%q text=%q", r.Tool, r.Text)
	}
}

// TestAdminToolsScopedAndGated proves admin tools require their permission and
// run against the org scope.
func TestAdminToolsScopedAndGated(t *testing.T) {
	pool := testsupport.NewDB(t)
	q := gen.New(pool)
	ctx := context.Background()

	reg := ai.NewRegistry(tools.All()...)
	agent := ai.NewAgent(ai.NewDeterministicProvider(), reg)

	// Admin WITHOUT invoice.view cannot reach ar_aging → falls back to help.
	noPerm := ai.ToolContext{OrgID: 1, Audience: "admin", Q: q}
	r, _ := agent.Handle(ctx, noPerm, "show me the receivables aging", nil)
	if r.Tool == "ar_aging" {
		t.Fatal("ar_aging reachable without invoice.view")
	}

	// With the permission, it runs (empty org → zero totals).
	withPerm := ai.ToolContext{OrgID: 1, Audience: "admin", Permissions: []string{"invoice.view"}, Q: q}
	r, err := agent.Handle(ctx, withPerm, "show me the receivables aging", nil)
	if err != nil {
		t.Fatalf("ar_aging: %v", err)
	}
	if r.Tool != "ar_aging" || !strings.Contains(r.Text, "Open receivables") {
		t.Errorf("ar_aging: tool=%q text=%q", r.Tool, r.Text)
	}

	// at_risk_accounts needs crm.view.
	crm := ai.ToolContext{OrgID: 1, Audience: "admin", Permissions: []string{"crm.view"}, Q: q}
	r, _ = agent.Handle(ctx, crm, "which accounts are at risk of churn?", nil)
	if r.Tool != "at_risk_accounts" {
		t.Errorf("at_risk_accounts: tool=%q text=%q", r.Tool, r.Text)
	}

	// A buyer can never reach an admin tool even with matching words.
	buyer := ai.ToolContext{OrgID: 1, Audience: "storefront", CustomerID: 1, Q: q}
	r, _ = agent.Handle(ctx, buyer, "show me the receivables aging report", nil)
	if r.Tool == "ar_aging" {
		t.Fatal("buyer reached an admin tool")
	}
}
