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
	}
}

// ===== buyer tools =========================================================

type orderStatusTool struct{}

func (orderStatusTool) Name() string         { return "order_status" }
func (orderStatusTool) Description() string   { return "Check the status of one of your orders (give the order id)." }
func (orderStatusTool) Audience() string      { return "storefront" }
func (orderStatusTool) Permission() string    { return "" }
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

func (listOrdersTool) Name() string          { return "list_orders" }
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

func (outstandingInvoicesTool) Name() string          { return "outstanding_invoices" }
func (outstandingInvoicesTool) Description() string    { return "See what your company currently owes (unpaid invoices)." }
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

func (reorderDueTool) Name() string          { return "reorder_due" }
func (reorderDueTool) Description() string    { return "Find items it may be time to reorder based on your buying cadence." }
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

func (budgetStatusTool) Name() string          { return "budget_status" }
func (budgetStatusTool) Description() string    { return "Check your procurement budgets and how much is left this period." }
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

func (arAgingTool) Name() string          { return "ar_aging" }
func (arAgingTool) Description() string    { return "Summarise accounts-receivable aging (open invoices by age bucket)." }
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

func (atRiskAccountsTool) Name() string          { return "at_risk_accounts" }
func (atRiskAccountsTool) Description() string    { return "List accounts at churn risk (overdue to reorder or ordering less than before)." }
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

func (orderLookupTool) Name() string         { return "order_lookup" }
func (orderLookupTool) Description() string   { return "Look up any order by its id (status, customer, total)." }
func (orderLookupTool) Audience() string      { return "admin" }
func (orderLookupTool) Permission() string    { return "order.view" }
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
