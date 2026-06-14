// Package insights is the executive-analytics engine: it computes
// period-over-period business metrics for an organization, detects anomalies
// with deterministic rules, and (via an ai.Narrator) turns the result into a
// written executive briefing. A weekly background job materialises one digest
// per active org and emails it; the same engine serves a live, on-demand metrics
// view for the dashboard.
//
// The engine answers "what changed, why it matters, and what to do" — the layer
// above reporting's "what are my numbers". All queries are live, date-windowed
// aggregates (no materialization), and every query filters by organization_id so
// the engine is safe to run both in the RLS-armed API and the plain worker pool.
package insights

import (
	"context"
	"fmt"
	"math/big"
	"time"

	"b2bcommerce/internal/money"
	"b2bcommerce/internal/store/gen"
)

// DefaultWindowDays is the digest period (a trading week).
const DefaultWindowDays = 7

// topCustomerLimit caps how many ranked accounts the snapshot carries.
const topCustomerLimit = 5

// Querier is the slice of the generated query API the engine needs. *gen.Queries
// satisfies it; narrowing to an interface keeps the engine's dependency explicit.
type Querier interface {
	RevenueWindow(ctx context.Context, arg gen.RevenueWindowParams) (gen.RevenueWindowRow, error)
	RevenueByCustomerWindow(ctx context.Context, arg gen.RevenueByCustomerWindowParams) ([]gen.RevenueByCustomerWindowRow, error)
	NewCustomerCountWindow(ctx context.Context, arg gen.NewCustomerCountWindowParams) (int64, error)
	MarginWindow(ctx context.Context, arg gen.MarginWindowParams) (gen.MarginWindowRow, error)
	ListOpenInvoicesForOrg(ctx context.Context, organizationID int64) ([]gen.ListOpenInvoicesForOrgRow, error)
	AccountHealth(ctx context.Context, organizationID int64) ([]gen.AccountHealthRow, error)
	CountLowStock(ctx context.Context, organizationID int64) (int64, error)
}

// RiskAccount is a churn-risk account surfaced from AccountHealth.
type RiskAccount struct {
	Name   string `json:"name"`
	Reason string `json:"reason"`
}

// CustomerSpend is one account's spend in the period.
type CustomerSpend struct {
	CustomerID int64  `json:"customer_id"`
	Name       string `json:"name"`
	Orders     int64  `json:"orders"`
	Revenue    string `json:"revenue"`
}

// Snapshot is the full computed picture for one org over the period. It is the
// input both to anomaly detection and to the KPI payload persisted on a digest.
type Snapshot struct {
	OrgID       int64     `json:"org_id"`
	PeriodStart time.Time `json:"period_start"`
	PeriodEnd   time.Time `json:"period_end"`
	WindowDays  int       `json:"window_days"`

	// Headline metrics for the period, with the immediately-preceding window for
	// comparison. Revenue/AOV are money strings; deltas are signed percentages.
	Revenue      string  `json:"revenue"`
	Orders       int64   `json:"orders"`
	AOV          string  `json:"avg_order_value"`
	PrevRevenue  string  `json:"prev_revenue"`
	PrevOrders   int64   `json:"prev_orders"`
	RevenueDelta float64 `json:"revenue_delta_pct"`
	OrdersDelta  float64 `json:"orders_delta_pct"`
	NewCustomers int64   `json:"new_customers"`

	// Gross margin (line-item revenue minus cost of goods at current cost).
	// HasCost is false when no product carries a cost yet, in which case margin
	// is not meaningful and the UI/narrator omits it.
	MarginRevenue  string  `json:"margin_revenue"`
	Cost           string  `json:"cost"`
	GrossMargin    string  `json:"gross_margin"`
	MarginPct      float64 `json:"margin_pct"`
	PrevMarginPct  float64 `json:"prev_margin_pct"`
	MarginDeltaPts float64 `json:"margin_delta_pts"`
	HasCost        bool    `json:"has_cost"`

	// Receivables.
	OpenAR       string `json:"open_ar"`
	AR90Plus     string `json:"ar_90_plus"`
	OpenInvoices int    `json:"open_invoices"`

	// Accounts.
	AtRisk           []RiskAccount   `json:"at_risk"`
	TopCustomers     []CustomerSpend `json:"top_customers"`
	TopConcentration float64         `json:"top_concentration_pct"`

	// Inventory.
	LowStock int64 `json:"low_stock"`
}

// Build computes the snapshot for org over [now-windowDays, now), comparing
// against the immediately-preceding window of equal length.
func Build(ctx context.Context, q Querier, orgID int64, now time.Time, windowDays int) (Snapshot, error) {
	if windowDays <= 0 {
		windowDays = DefaultWindowDays
	}
	end := now
	start := now.AddDate(0, 0, -windowDays)
	prevStart := start.AddDate(0, 0, -windowDays)

	s := Snapshot{OrgID: orgID, PeriodStart: start, PeriodEnd: end, WindowDays: windowDays}

	// Current + prior headline rollups.
	cur, err := q.RevenueWindow(ctx, gen.RevenueWindowParams{OrganizationID: orgID, FromTs: start, ToTs: end})
	if err != nil {
		return Snapshot{}, fmt.Errorf("revenue window: %w", err)
	}
	prev, err := q.RevenueWindow(ctx, gen.RevenueWindowParams{OrganizationID: orgID, FromTs: prevStart, ToTs: start})
	if err != nil {
		return Snapshot{}, fmt.Errorf("prior revenue window: %w", err)
	}
	s.Revenue, s.Orders = cur.Revenue, cur.OrderCount
	s.PrevRevenue, s.PrevOrders = prev.Revenue, prev.OrderCount
	s.AOV = avgOrderValue(cur.Revenue, cur.OrderCount)
	s.RevenueDelta = pctChange(cur.Revenue, prev.Revenue)
	s.OrdersDelta = pctChangeInt(cur.OrderCount, prev.OrderCount)

	// New-logo acquisition this period.
	if n, err := q.NewCustomerCountWindow(ctx, gen.NewCustomerCountWindowParams{OrganizationID: orgID, FromTs: start, ToTs: end}); err == nil {
		s.NewCustomers = n
	}

	// Gross margin (current vs prior), at current product cost.
	curM, err := q.MarginWindow(ctx, gen.MarginWindowParams{OrganizationID: orgID, FromTs: start, ToTs: end})
	if err != nil {
		return Snapshot{}, fmt.Errorf("margin window: %w", err)
	}
	prevM, err := q.MarginWindow(ctx, gen.MarginWindowParams{OrganizationID: orgID, FromTs: prevStart, ToTs: start})
	if err != nil {
		return Snapshot{}, fmt.Errorf("prior margin window: %w", err)
	}
	s.MarginRevenue = curM.Revenue
	s.Cost = curM.Cost
	s.GrossMargin, _ = money.Sub(curM.Revenue, curM.Cost)
	s.MarginPct = marginPct(curM.Revenue, curM.Cost)
	s.PrevMarginPct = marginPct(prevM.Revenue, prevM.Cost)
	s.HasCost = isPositive(curM.Cost)
	if s.HasCost {
		s.MarginDeltaPts = round1(s.MarginPct - s.PrevMarginPct)
	}

	// Receivables aging (open invoices bucketed by days past due).
	if err := s.fillReceivables(ctx, q, orgID, now); err != nil {
		return Snapshot{}, err
	}

	// Account health → churn-risk list (same signals as the at-risk tool).
	if err := s.fillAtRisk(ctx, q, orgID, now); err != nil {
		return Snapshot{}, err
	}

	// Customer revenue ranking → top accounts + concentration.
	if err := s.fillCustomerConcentration(ctx, q, orgID, start, end); err != nil {
		return Snapshot{}, err
	}

	// Inventory pressure.
	if n, err := q.CountLowStock(ctx, orgID); err == nil {
		s.LowStock = n
	}

	return s, nil
}

func (s *Snapshot) fillReceivables(ctx context.Context, q Querier, orgID int64, now time.Time) error {
	rows, err := q.ListOpenInvoicesForOrg(ctx, orgID)
	if err != nil {
		return fmt.Errorf("open invoices: %w", err)
	}
	open := "0"
	over90 := "0"
	for _, inv := range rows {
		open, _ = money.Sum(open, inv.GrandTotal)
		days := 0
		if inv.DueAt.Valid {
			days = int(now.Sub(inv.DueAt.Time).Hours() / 24)
		}
		if days > 90 {
			over90, _ = money.Sum(over90, inv.GrandTotal)
		}
	}
	s.OpenAR = open
	s.AR90Plus = over90
	s.OpenInvoices = len(rows)
	return nil
}

func (s *Snapshot) fillAtRisk(ctx context.Context, q Querier, orgID int64, now time.Time) error {
	rows, err := q.AccountHealth(ctx, orgID)
	if err != nil {
		return fmt.Errorf("account health: %w", err)
	}
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
			s.AtRisk = append(s.AtRisk, RiskAccount{Name: a.Name, Reason: reason})
		}
	}
	return nil
}

func (s *Snapshot) fillCustomerConcentration(ctx context.Context, q Querier, orgID int64, start, end time.Time) error {
	rows, err := q.RevenueByCustomerWindow(ctx, gen.RevenueByCustomerWindowParams{OrganizationID: orgID, FromTs: start, ToTs: end})
	if err != nil {
		return fmt.Errorf("revenue by customer: %w", err)
	}
	for i, r := range rows {
		if i >= topCustomerLimit {
			break
		}
		s.TopCustomers = append(s.TopCustomers, CustomerSpend{
			CustomerID: r.CustomerID, Name: r.Name, Orders: r.OrderCount, Revenue: r.Revenue,
		})
	}
	// Concentration = the single largest account's share of period revenue.
	if len(rows) > 0 {
		s.TopConcentration = share(rows[0].Revenue, s.Revenue)
	}
	return nil
}

// ---- money / percentage helpers ------------------------------------------

func avgOrderValue(revenue string, orders int64) string {
	if orders <= 0 {
		return "0"
	}
	rev, err := money.Parse(revenue)
	if err != nil {
		return "0"
	}
	return money.Format(new(big.Rat).Quo(rev, new(big.Rat).SetInt64(orders)))
}

// pctChange returns the signed percent change from prev to cur. A zero prior with
// a positive current reads as +100 (grew from nothing); zero/zero is 0.
func pctChange(cur, prev string) float64 {
	c, err1 := money.Parse(cur)
	p, err2 := money.Parse(prev)
	if err1 != nil || err2 != nil {
		return 0
	}
	cf, _ := c.Float64()
	pf, _ := p.Float64()
	return ratio(cf, pf)
}

func pctChangeInt(cur, prev int64) float64 { return ratio(float64(cur), float64(prev)) }

func ratio(cur, prev float64) float64 {
	if prev == 0 {
		if cur > 0 {
			return 100
		}
		return 0
	}
	return round1((cur - prev) / prev * 100)
}

// marginPct returns gross margin as a percentage of revenue (0 when revenue is 0).
func marginPct(revenue, cost string) float64 {
	r, err1 := money.Parse(revenue)
	c, err2 := money.Parse(cost)
	if err1 != nil || err2 != nil {
		return 0
	}
	rf, _ := r.Float64()
	cf, _ := c.Float64()
	if rf == 0 {
		return 0
	}
	return round1((rf - cf) / rf * 100)
}

// share returns part as a percentage of whole (0 when whole is 0).
func share(part, whole string) float64 {
	p, err1 := money.Parse(part)
	w, err2 := money.Parse(whole)
	if err1 != nil || err2 != nil {
		return 0
	}
	pf, _ := p.Float64()
	wf, _ := w.Float64()
	if wf == 0 {
		return 0
	}
	return round1(pf / wf * 100)
}

func round1(f float64) float64 { return float64(int64(f*10+sign(f)*0.5)) / 10 }

func sign(f float64) float64 {
	if f < 0 {
		return -1
	}
	return 1
}
