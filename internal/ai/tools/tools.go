// Package tools is the concrete capability catalog for the AI assistant. Every
// tool wraps existing, already-tested queries and runs under the caller's scope
// (org + customer/vendor + permissions) supplied in ai.ToolContext, so the
// assistant can read/act only on what the caller is entitled to. Tools are
// deliberately small and auditable.
package tools

import (
	"context"
	"fmt"
	"strings"
	"time"

	"b2bcommerce/internal/ai"
	"b2bcommerce/internal/money"
	"b2bcommerce/internal/store/gen"
)

// All returns the full catalog. The registry/agent filter by audience+permission
// at call time, so it is safe to register everything once.
func All() []ai.Tool {
	return []ai.Tool{
		// Buyer (storefront)
		orderStatusTool{}, listOrdersTool{}, outstandingInvoicesTool{}, reorderDueTool{}, budgetStatusTool{},
		// Seller (admin)
		arAgingTool{}, atRiskAccountsTool{}, orderLookupTool{},
		productSearchTool{}, customerLookupTool{}, inventoryStatusTool{},
	}
}

// ===== buyer tools =========================================================

type orderStatusTool struct{}

func (orderStatusTool) Name() string { return "order_status" }
func (orderStatusTool) Description() string {
	return "Check the status of one of your orders (give the order id)."
}
func (orderStatusTool) Audience() string   { return "storefront" }
func (orderStatusTool) Permission() string { return "" }
func (orderStatusTool) Params() []ai.ParamSpec {
	return []ai.ParamSpec{{Name: "order_id", Type: "string", Description: "Order public id or its short prefix", Required: true}}
}
func (orderStatusTool) Match(msg string) (map[string]any, bool) {
	tok := token(msg, 6)
	if tok != "" && containsAny(msg, "order", "status", "track", "where", "delivery", "shipped") {
		return map[string]any{"order_id": tok}, true
	}
	return nil, false
}
func (orderStatusTool) Run(ctx context.Context, tc ai.ToolContext, args map[string]any) (ai.ToolResult, error) {
	want := strings.ToLower(asString(args["order_id"]))
	orders, err := tc.Q.ListOrdersForCustomer(ctx, tc.CustomerID)
	if err != nil {
		return ai.ToolResult{}, err
	}
	for _, o := range orders {
		if strings.HasPrefix(strings.ToLower(o.PublicID.String()), want) {
			return ai.ToolResult{
				Summary: fmt.Sprintf("Order %s… is %s — total %s %s.", o.PublicID.String()[:8], human(o.Status), o.GrandTotal, o.Currency),
				Data:    map[string]any{"public_id": o.PublicID.String(), "status": o.Status, "grand_total": o.GrandTotal, "currency": o.Currency},
			}, nil
		}
	}
	return ai.ToolResult{Summary: "I couldn't find an order matching that id on your account."}, nil
}

type listOrdersTool struct{}

func (listOrdersTool) Name() string           { return "list_orders" }
func (listOrdersTool) Description() string    { return "List your most recent orders." }
func (listOrdersTool) Audience() string       { return "storefront" }
func (listOrdersTool) Permission() string     { return "" }
func (listOrdersTool) Params() []ai.ParamSpec { return nil }
func (listOrdersTool) Match(msg string) (map[string]any, bool) {
	if token(msg, 6) != "" {
		return nil, false // a specific id → order_status handles it
	}
	if containsAny(msg, "my orders", "recent orders", "list orders", "order history", "orders") {
		return map[string]any{}, true
	}
	return nil, false
}
func (listOrdersTool) Run(ctx context.Context, tc ai.ToolContext, _ map[string]any) (ai.ToolResult, error) {
	orders, err := tc.Q.ListOrdersForCustomer(ctx, tc.CustomerID)
	if err != nil {
		return ai.ToolResult{}, err
	}
	if len(orders) == 0 {
		return ai.ToolResult{Summary: "You don't have any orders yet."}, nil
	}
	n := len(orders)
	if n > 5 {
		n = 5
	}
	var b strings.Builder
	fmt.Fprintf(&b, "You have %d orders. Most recent:", len(orders))
	items := make([]map[string]any, 0, n)
	for _, o := range orders[:n] {
		fmt.Fprintf(&b, "\n• %s… — %s (%s %s)", o.PublicID.String()[:8], human(o.Status), o.GrandTotal, o.Currency)
		items = append(items, map[string]any{"public_id": o.PublicID.String(), "status": o.Status, "grand_total": o.GrandTotal, "currency": o.Currency})
	}
	return ai.ToolResult{Summary: b.String(), Data: map[string]any{"count": len(orders), "orders": items}}, nil
}

type outstandingInvoicesTool struct{}

func (outstandingInvoicesTool) Name() string { return "outstanding_invoices" }
func (outstandingInvoicesTool) Description() string {
	return "See what your company currently owes (unpaid invoices)."
}
func (outstandingInvoicesTool) Audience() string       { return "storefront" }
func (outstandingInvoicesTool) Permission() string     { return "" }
func (outstandingInvoicesTool) Params() []ai.ParamSpec { return nil }
func (outstandingInvoicesTool) Match(msg string) (map[string]any, bool) {
	if containsAny(msg, "owe", "outstanding", "invoice", "balance", "unpaid", "what do i owe", "bill") {
		return map[string]any{}, true
	}
	return nil, false
}
func (outstandingInvoicesTool) Run(ctx context.Context, tc ai.ToolContext, _ map[string]any) (ai.ToolResult, error) {
	invs, err := tc.Q.ListInvoicesForCustomer(ctx, tc.CustomerID)
	if err != nil {
		return ai.ToolResult{}, err
	}
	total := "0"
	currency := ""
	count := 0
	for _, inv := range invs {
		if inv.Status == "paid" || inv.Status == "void" || inv.Status == "cancelled" {
			continue
		}
		count++
		currency = inv.Currency
		total, _ = money.Sum(total, inv.GrandTotal)
	}
	if count == 0 {
		return ai.ToolResult{Summary: "You have no outstanding invoices — your account is settled. 🎉"}, nil
	}
	return ai.ToolResult{
		Summary: fmt.Sprintf("You have %d unpaid invoice(s) totalling %s %s.", count, total, currency),
		Data:    map[string]any{"count": count, "total": total, "currency": currency},
	}, nil
}

type reorderDueTool struct{}

func (reorderDueTool) Name() string { return "reorder_due" }
func (reorderDueTool) Description() string {
	return "Find items it may be time to reorder based on your buying cadence."
}
func (reorderDueTool) Audience() string       { return "storefront" }
func (reorderDueTool) Permission() string     { return "" }
func (reorderDueTool) Params() []ai.ParamSpec { return nil }
func (reorderDueTool) Match(msg string) (map[string]any, bool) {
	if containsAny(msg, "reorder", "re-order", "replenish", "restock", "run low", "running low", "buy again", "order again", "time to order") {
		return map[string]any{}, true
	}
	return nil, false
}
func (reorderDueTool) Run(ctx context.Context, tc ai.ToolContext, _ map[string]any) (ai.ToolResult, error) {
	rows, err := tc.Q.ReorderCadence(ctx, tc.CustomerID)
	if err != nil {
		return ai.ToolResult{}, err
	}
	now := time.Now()
	due := make([]map[string]any, 0)
	var names []string
	for _, r := range rows {
		if r.OrderCount < 2 {
			continue
		}
		span := r.LastOrdered.Sub(r.FirstOrdered)
		avg := span / time.Duration(r.OrderCount-1)
		if avg <= 0 {
			continue
		}
		if now.Sub(r.LastOrdered) >= avg {
			due = append(due, map[string]any{"sku": r.Sku, "name": r.Name, "slug": r.Slug})
			names = append(names, r.Name)
		}
	}
	if len(due) == 0 {
		return ai.ToolResult{Summary: "Nothing looks due for reorder right now based on your cadence."}, nil
	}
	return ai.ToolResult{
		Summary: fmt.Sprintf("It may be time to reorder %d item(s): %s.", len(due), strings.Join(names, ", ")),
		Data:    map[string]any{"items": due},
	}, nil
}

type budgetStatusTool struct{}

func (budgetStatusTool) Name() string { return "budget_status" }
func (budgetStatusTool) Description() string {
	return "Check your procurement budgets and how much is left this period."
}
func (budgetStatusTool) Audience() string       { return "storefront" }
func (budgetStatusTool) Permission() string     { return "" }
func (budgetStatusTool) Params() []ai.ParamSpec { return nil }
func (budgetStatusTool) Match(msg string) (map[string]any, bool) {
	if containsAny(msg, "budget", "cost center", "cost centre", "spend limit", "spending cap", "how much can i spend") {
		return map[string]any{}, true
	}
	return nil, false
}
func (budgetStatusTool) Run(ctx context.Context, tc ai.ToolContext, _ map[string]any) (ai.ToolResult, error) {
	budgets, err := tc.Q.ListBudgetsForCustomer(ctx, tc.CustomerID)
	if err != nil {
		return ai.ToolResult{}, err
	}
	if len(budgets) == 0 {
		return ai.ToolResult{Summary: "Your company has no procurement budgets configured."}, nil
	}
	now := time.Now()
	var b strings.Builder
	b.WriteString("Your budgets this period:")
	out := make([]map[string]any, 0, len(budgets))
	for _, bd := range budgets {
		cc := bd.CostCenter
		spent, err := tc.Q.SpendForCustomerPeriod(ctx, gen.SpendForCustomerPeriodParams{
			CustomerID: tc.CustomerID, CostCenter: &cc, CreatedAt: periodStart(bd.Period, now),
		})
		if err != nil {
			spent = "0"
		}
		remaining, _ := money.Sub(bd.Amount, spent)
		label := cc
		if label == "" {
			label = "company-wide"
		}
		fmt.Fprintf(&b, "\n• %s (%s): %s of %s %s spent, %s left", label, bd.Period, spent, bd.Amount, bd.Currency, remaining)
		out = append(out, map[string]any{"cost_center": cc, "period": bd.Period, "amount": bd.Amount, "spent": spent, "remaining": remaining, "currency": bd.Currency})
	}
	return ai.ToolResult{Summary: b.String(), Data: map[string]any{"budgets": out}}, nil
}

// ===== seller (admin) tools ================================================

type arAgingTool struct{}

func (arAgingTool) Name() string { return "ar_aging" }
func (arAgingTool) Description() string {
	return "Summarise accounts-receivable aging (open invoices by age bucket)."
}
func (arAgingTool) Audience() string       { return "admin" }
func (arAgingTool) Permission() string     { return "invoice.view" }
func (arAgingTool) Params() []ai.ParamSpec { return nil }
func (arAgingTool) Match(msg string) (map[string]any, bool) {
	if containsAny(msg, "aging", "ageing", "receivable", "overdue", "past due", "who owes", "ar ") {
		return map[string]any{}, true
	}
	return nil, false
}
func (arAgingTool) Run(ctx context.Context, tc ai.ToolContext, _ map[string]any) (ai.ToolResult, error) {
	rows, err := tc.Q.ListOpenInvoicesForOrg(ctx, tc.OrgID)
	if err != nil {
		return ai.ToolResult{}, err
	}
	buckets := map[string]string{"current": "0", "1-30": "0", "31-60": "0", "61-90": "0", "90+": "0"}
	now := time.Now()
	for _, inv := range rows {
		days := 0
		if inv.DueAt.Valid {
			days = int(now.Sub(inv.DueAt.Time).Hours() / 24)
		}
		b := bucketFor(days)
		buckets[b], _ = money.Sum(buckets[b], inv.GrandTotal)
	}
	total, _ := money.Sum(buckets["current"], buckets["1-30"], buckets["31-60"], buckets["61-90"], buckets["90+"])
	return ai.ToolResult{
		Summary: fmt.Sprintf("Open receivables total %s across %d invoices. Past 90 days: %s; 61-90: %s; 31-60: %s; 1-30: %s; current: %s.",
			total, len(rows), buckets["90+"], buckets["61-90"], buckets["31-60"], buckets["1-30"], buckets["current"]),
		Data: map[string]any{"buckets": buckets, "open_total": total, "open_count": len(rows)},
	}, nil
}

type atRiskAccountsTool struct{}

func (atRiskAccountsTool) Name() string { return "at_risk_accounts" }
func (atRiskAccountsTool) Description() string {
	return "List accounts at churn risk (overdue to reorder or ordering less than before)."
}
func (atRiskAccountsTool) Audience() string       { return "admin" }
func (atRiskAccountsTool) Permission() string     { return "crm.view" }
func (atRiskAccountsTool) Params() []ai.ParamSpec { return nil }
func (atRiskAccountsTool) Match(msg string) (map[string]any, bool) {
	if containsAny(msg, "at risk", "at-risk", "churn", "slipping", "account health", "losing", "declining") {
		return map[string]any{}, true
	}
	return nil, false
}
func (atRiskAccountsTool) Run(ctx context.Context, tc ai.ToolContext, _ map[string]any) (ai.ToolResult, error) {
	rows, err := tc.Q.AccountHealth(ctx, tc.OrgID)
	if err != nil {
		return ai.ToolResult{}, err
	}
	type risk struct {
		Name   string `json:"name"`
		Reason string `json:"reason"`
	}
	var risks []risk
	now := time.Now()
	for _, a := range rows {
		reason := ""
		// Overdue to reorder: well past the account's usual cadence.
		if a.OrderCount >= 2 {
			span := a.LastOrdered.Sub(a.FirstOrdered)
			avg := span / time.Duration(a.OrderCount-1)
			if avg > 0 && now.Sub(a.LastOrdered) > 2*avg {
				reason = "overdue to reorder (well past its usual cadence)"
			}
		}
		if reason == "" && a.PriorCount > 0 && a.RecentCount < a.PriorCount {
			reason = "ordering less than the prior quarter"
		}
		if reason != "" {
			risks = append(risks, risk{Name: a.Name, Reason: reason})
		}
	}
	if len(risks) == 0 {
		return ai.ToolResult{Summary: "No accounts are flagged at risk right now."}, nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d account(s) at risk:", len(risks))
	data := make([]map[string]any, 0, len(risks))
	for _, r := range risks {
		fmt.Fprintf(&b, "\n• %s — %s", r.Name, r.Reason)
		data = append(data, map[string]any{"name": r.Name, "reason": r.Reason})
	}
	return ai.ToolResult{Summary: b.String(), Data: map[string]any{"accounts": data}}, nil
}

type orderLookupTool struct{}

func (orderLookupTool) Name() string { return "order_lookup" }
func (orderLookupTool) Description() string {
	return "Look up any order by its id (status, customer, total)."
}
func (orderLookupTool) Audience() string   { return "admin" }
func (orderLookupTool) Permission() string { return "order.view" }
func (orderLookupTool) Params() []ai.ParamSpec {
	return []ai.ParamSpec{{Name: "order_id", Type: "string", Description: "Order public id or short prefix", Required: true}}
}
func (orderLookupTool) Match(msg string) (map[string]any, bool) {
	tok := token(msg, 6)
	if tok != "" && containsAny(msg, "order", "lookup", "look up", "find", "status") {
		return map[string]any{"order_id": tok}, true
	}
	return nil, false
}
func (orderLookupTool) Run(ctx context.Context, tc ai.ToolContext, args map[string]any) (ai.ToolResult, error) {
	want := strings.ToLower(asString(args["order_id"]))
	rows, err := tc.Q.ListOrdersAdmin(ctx, gen.ListOrdersAdminParams{OrganizationID: tc.OrgID, Limit: 500, Offset: 0})
	if err != nil {
		return ai.ToolResult{}, err
	}
	for _, o := range rows {
		if strings.HasPrefix(strings.ToLower(o.PublicID.String()), want) {
			return ai.ToolResult{
				Summary: fmt.Sprintf("Order %s… is %s — customer #%d, total %s %s.", o.PublicID.String()[:8], human(o.Status), o.CustomerID, o.GrandTotal, o.Currency),
				Data:    map[string]any{"public_id": o.PublicID.String(), "status": o.Status, "customer_id": o.CustomerID, "grand_total": o.GrandTotal, "currency": o.Currency},
			}, nil
		}
	}
	return ai.ToolResult{Summary: "No order matched that id."}, nil
}

type productSearchTool struct{}

func (productSearchTool) Name() string { return "product_search" }
func (productSearchTool) Description() string {
	return "Search the product catalog by name or SKU."
}
func (productSearchTool) Audience() string   { return "admin" }
func (productSearchTool) Permission() string { return "product.view" }
func (productSearchTool) Params() []ai.ParamSpec {
	return []ai.ParamSpec{{Name: "query", Type: "string", Description: "Product name or SKU to search for (optional)", Required: false}}
}
func (productSearchTool) Match(msg string) (map[string]any, bool) {
	if containsAny(msg, "product", "sku", "catalog", "catalogue") {
		return map[string]any{"query": searchTerm(msg, "product", "products", "sku", "skus", "catalog", "catalogue")}, true
	}
	return nil, false
}
func (productSearchTool) Run(ctx context.Context, tc ai.ToolContext, args map[string]any) (ai.ToolResult, error) {
	q := strings.TrimSpace(asString(args["query"]))
	var rows []gen.Product
	var err error
	if q != "" {
		rows, err = tc.Q.SearchProductsAdmin(ctx, gen.SearchProductsAdminParams{OrganizationID: tc.OrgID, WebsearchToTsquery: q, Limit: 8, Offset: 0})
	} else {
		rows, err = tc.Q.ListProductsAdmin(ctx, gen.ListProductsAdminParams{OrganizationID: tc.OrgID, Limit: 8, Offset: 0})
	}
	if err != nil {
		return ai.ToolResult{}, err
	}
	if len(rows) == 0 {
		return ai.ToolResult{Summary: fmt.Sprintf("No products matched %q.", q)}, nil
	}
	var b strings.Builder
	if q != "" {
		fmt.Fprintf(&b, "Products matching %q:", q)
	} else {
		b.WriteString("Products in the catalog:")
	}
	items := make([]map[string]any, 0, len(rows))
	for _, p := range rows {
		fmt.Fprintf(&b, "\n• %s — %s (%s)", p.Sku, p.Name, p.Status)
		items = append(items, map[string]any{"sku": p.Sku, "name": p.Name, "status": p.Status, "slug": p.Slug})
	}
	return ai.ToolResult{Summary: b.String(), Data: map[string]any{"products": items, "count": len(rows)}}, nil
}

type customerLookupTool struct{}

func (customerLookupTool) Name() string { return "customer_lookup" }
func (customerLookupTool) Description() string {
	return "Find customer accounts by name (payment terms, credit limit, status)."
}
func (customerLookupTool) Audience() string   { return "admin" }
func (customerLookupTool) Permission() string { return "customer.view" }
func (customerLookupTool) Params() []ai.ParamSpec {
	return []ai.ParamSpec{{Name: "name", Type: "string", Description: "Account name to search for (optional)", Required: false}}
}
func (customerLookupTool) Match(msg string) (map[string]any, bool) {
	if containsAny(msg, "customer", "account", "buyer", "client") {
		return map[string]any{"name": searchTerm(msg, "customer", "customers", "account", "accounts", "buyer", "buyers", "client", "clients")}, true
	}
	return nil, false
}
func (customerLookupTool) Run(ctx context.Context, tc ai.ToolContext, args map[string]any) (ai.ToolResult, error) {
	want := strings.ToLower(strings.TrimSpace(asString(args["name"])))
	rows, err := tc.Q.ListCustomers(ctx, gen.ListCustomersParams{OrganizationID: tc.OrgID, Limit: 500, Offset: 0})
	if err != nil {
		return ai.ToolResult{}, err
	}
	matches := make([]gen.Customer, 0)
	for _, c := range rows {
		if want == "" || strings.Contains(strings.ToLower(c.Name), want) {
			matches = append(matches, c)
		}
	}
	if len(matches) == 0 {
		return ai.ToolResult{Summary: fmt.Sprintf("No accounts matched %q.", want)}, nil
	}
	if len(matches) > 8 {
		matches = matches[:8]
	}
	var b strings.Builder
	if want != "" {
		fmt.Fprintf(&b, "Accounts matching %q:", want)
	} else {
		b.WriteString("Accounts:")
	}
	items := make([]map[string]any, 0, len(matches))
	for _, c := range matches {
		status := "active"
		if !c.IsActive {
			status = "inactive"
		}
		fmt.Fprintf(&b, "\n• %s — net %d days, credit limit %s (%s)", c.Name, c.PaymentTermsDays, c.CreditLimit, status)
		items = append(items, map[string]any{"name": c.Name, "payment_terms_days": c.PaymentTermsDays, "credit_limit": c.CreditLimit, "is_active": c.IsActive})
	}
	return ai.ToolResult{Summary: b.String(), Data: map[string]any{"accounts": items, "count": len(items)}}, nil
}

type inventoryStatusTool struct{}

func (inventoryStatusTool) Name() string { return "inventory_status" }
func (inventoryStatusTool) Description() string {
	return "Check stock on hand and available for a product."
}
func (inventoryStatusTool) Audience() string   { return "admin" }
func (inventoryStatusTool) Permission() string { return "inventory.view" }
func (inventoryStatusTool) Params() []ai.ParamSpec {
	return []ai.ParamSpec{{Name: "product", Type: "string", Description: "Product name or SKU", Required: true}}
}
func (inventoryStatusTool) Match(msg string) (map[string]any, bool) {
	if containsAny(msg, "stock", "inventory", "on hand", "on-hand", "in stock", "how much stock") {
		return map[string]any{"product": searchTerm(msg, "stock", "inventory", "on", "hand", "in")}, true
	}
	return nil, false
}
func (inventoryStatusTool) Run(ctx context.Context, tc ai.ToolContext, args map[string]any) (ai.ToolResult, error) {
	q := strings.TrimSpace(asString(args["product"]))
	if q == "" {
		return ai.ToolResult{Summary: "Which product? Give me a name or SKU to check stock for."}, nil
	}
	prods, err := tc.Q.SearchProductsAdmin(ctx, gen.SearchProductsAdminParams{OrganizationID: tc.OrgID, WebsearchToTsquery: q, Limit: 1, Offset: 0})
	if err != nil {
		return ai.ToolResult{}, err
	}
	if len(prods) == 0 {
		return ai.ToolResult{Summary: fmt.Sprintf("No product matched %q.", q)}, nil
	}
	p := prods[0]
	levels, err := tc.Q.ListInventoryLevelsForProduct(ctx, p.ID)
	if err != nil {
		return ai.ToolResult{}, err
	}
	if len(levels) == 0 {
		return ai.ToolResult{Summary: fmt.Sprintf("%s (%s) has no stock records yet.", p.Name, p.Sku)}, nil
	}
	onHand, available := "0", "0"
	for _, l := range levels {
		onHand, _ = money.Sum(onHand, l.QuantityOnHand)
		available, _ = money.Sum(available, l.Available)
	}
	return ai.ToolResult{
		Summary: fmt.Sprintf("%s (%s): %s available, %s on hand across %d location(s).", p.Name, p.Sku, available, onHand, len(levels)),
		Data:    map[string]any{"sku": p.Sku, "name": p.Name, "available": available, "on_hand": onHand, "locations": len(levels)},
	}, nil
}

// ===== helpers =============================================================

func containsAny(msg string, terms ...string) bool {
	m := strings.ToLower(msg)
	for _, t := range terms {
		if strings.Contains(m, t) {
			return true
		}
	}
	return false
}

// token returns the first id-like token (hex/dash, >= minLen) in the message.
func token(msg string, minLen int) string {
	for _, raw := range strings.FieldsFunc(msg, func(r rune) bool {
		return r == ' ' || r == ',' || r == '#' || r == '\n' || r == '\t' || r == '"' || r == '\''
	}) {
		tok := strings.TrimRight(raw, ".?!")
		if len(tok) >= minLen && isHexDash(tok) {
			return tok
		}
	}
	return ""
}

func isHexDash(s string) bool {
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9', r >= 'a' && r <= 'f', r >= 'A' && r <= 'F', r == '-':
		default:
			return false
		}
	}
	return len(s) > 0
}

func asString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", v)
}

// searchTerm strips common command/stop words (plus tool-specific ones) from the
// message and returns the remaining text as a search query.
func searchTerm(msg string, extra ...string) string {
	skip := map[string]bool{
		"find": true, "search": true, "show": true, "me": true, "the": true, "a": true, "an": true,
		"for": true, "of": true, "any": true, "all": true, "list": true, "lookup": true, "look": true,
		"up": true, "what": true, "whats": true, "what's": true, "is": true, "are": true, "do": true,
		"we": true, "i": true, "have": true, "has": true, "named": true, "called": true, "named?": true,
		"named:": true, "much": true, "many": true, "how": true,
	}
	for _, s := range extra {
		skip[s] = true
	}
	var out []string
	for _, w := range strings.Fields(strings.ToLower(msg)) {
		w = strings.Trim(w, ".,?!\"'")
		if w == "" || skip[w] {
			continue
		}
		out = append(out, w)
	}
	return strings.Join(out, " ")
}

func human(status string) string { return strings.ReplaceAll(status, "_", " ") }

func bucketFor(days int) string {
	switch {
	case days <= 0:
		return "current"
	case days <= 30:
		return "1-30"
	case days <= 60:
		return "31-60"
	case days <= 90:
		return "61-90"
	default:
		return "90+"
	}
}

func periodStart(period string, now time.Time) time.Time {
	y, m, _ := now.Date()
	loc := now.Location()
	switch period {
	case "annual":
		return time.Date(y, 1, 1, 0, 0, 0, 0, loc)
	case "quarterly":
		qm := time.Month((int(m)-1)/3*3 + 1)
		return time.Date(y, qm, 1, 0, 0, 0, 0, loc)
	default:
		return time.Date(y, m, 1, 0, 0, 0, 0, loc)
	}
}
