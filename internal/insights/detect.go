package insights

import (
	"fmt"
	"sort"
	"strings"

	"b2bcommerce/internal/money"
)

// Severities, worst-first.
const (
	SevCritical = "critical"
	SevWarn     = "warn"
	SevInfo     = "info"
)

// Action is an actionable deep-link attached to an anomaly — the "do something
// about it" half of an insight. The admin UI renders it as a button into the
// relevant screen, which is what makes the briefing part of the workflow rather
// than a passive report.
type Action struct {
	Kind  string `json:"kind"`
	Label string `json:"label"`
	Href  string `json:"href"`
}

// Anomaly is one detected signal: what it is, how serious, what it means, and
// what to do about it.
type Anomaly struct {
	Key            string  `json:"key"`
	Severity       string  `json:"severity"`
	Title          string  `json:"title"`
	Detail         string  `json:"detail"`
	Metric         string  `json:"metric"`
	Recommendation string  `json:"recommendation"`
	Action         *Action `json:"action,omitempty"`
}

// Detect runs the deterministic rule set over a snapshot and returns the signals
// worth surfacing, ordered worst-first. The rules are intentionally explainable
// (no opaque scoring) so an operator can trust why each one fired.
func Detect(s Snapshot) []Anomaly {
	var out []Anomaly

	// --- revenue trend -------------------------------------------------------
	switch {
	case s.PrevRevenueNonZero() && s.RevenueDelta <= -25:
		out = append(out, Anomaly{
			Key: "revenue_drop", Severity: SevCritical,
			Title:          fmt.Sprintf("Revenue down %.1f%%", -s.RevenueDelta),
			Detail:         fmt.Sprintf("revenue fell from %s to %s versus the prior period", s.PrevRevenue, s.Revenue),
			Metric:         pct(s.RevenueDelta),
			Recommendation: "Identify the accounts that stopped or cut back and have a rep reach out this week",
			Action:         &Action{Kind: "at_risk", Label: "Review at-risk accounts", Href: "/account-health"},
		})
	case s.PrevRevenueNonZero() && s.RevenueDelta <= -10:
		out = append(out, Anomaly{
			Key: "revenue_softening", Severity: SevWarn,
			Title:          fmt.Sprintf("Revenue softening (%.1f%%)", s.RevenueDelta),
			Detail:         fmt.Sprintf("revenue eased from %s to %s versus the prior period", s.PrevRevenue, s.Revenue),
			Metric:         pct(s.RevenueDelta),
			Recommendation: "Watch the biggest accounts for slipping order frequency",
			Action:         &Action{Kind: "at_risk", Label: "Review at-risk accounts", Href: "/account-health"},
		})
	case s.PrevRevenueNonZero() && s.RevenueDelta >= 20:
		out = append(out, Anomaly{
			Key: "revenue_growth", Severity: SevInfo,
			Title:  fmt.Sprintf("Revenue up %.1f%%", s.RevenueDelta),
			Detail: fmt.Sprintf("revenue grew from %s to %s versus the prior period", s.PrevRevenue, s.Revenue),
			Metric: pct(s.RevenueDelta),
		})
	}

	// --- receivables ---------------------------------------------------------
	if isPositive(s.AR90Plus) {
		sev := SevWarn
		rec := "Prioritise collections on the oldest invoices and send a dunning reminder"
		concShare := share(s.AR90Plus, s.OpenAR)
		detail := fmt.Sprintf("%s of open receivables is more than 90 days past due", s.AR90Plus)
		if concShare >= 25 {
			sev = SevCritical
			detail = fmt.Sprintf("%s (%.0f%% of open AR) is more than 90 days past due", s.AR90Plus, concShare)
		}
		out = append(out, Anomaly{
			Key: "ar_90_plus", Severity: sev,
			Title:  fmt.Sprintf("%s past 90 days", s.AR90Plus),
			Detail: detail, Metric: s.AR90Plus,
			Recommendation: rec,
			Action:         &Action{Kind: "ar_aging", Label: "Open receivables aging", Href: "/ar-aging"},
		})
	}

	// --- gross margin --------------------------------------------------------
	// Only meaningful once products carry a cost; a drop in margin % vs the prior
	// period is the "profitability slipping" signal.
	if s.HasCost && s.PrevMarginPct > 0 {
		drop := s.PrevMarginPct - s.MarginPct
		if drop >= 5 {
			sev := SevWarn
			if drop >= 10 {
				sev = SevCritical
			}
			out = append(out, Anomaly{
				Key: "margin_erosion", Severity: sev,
				Title:          fmt.Sprintf("Gross margin down %.1f pts", drop),
				Detail:         fmt.Sprintf("margin fell from %.1f%% to %.1f%% versus the prior period", s.PrevMarginPct, s.MarginPct),
				Metric:         fmt.Sprintf("%.1f%%", s.MarginPct),
				Recommendation: "Review pricing and supplier costs on the highest-volume SKUs",
				Action:         &Action{Kind: "analytics", Label: "Open analytics", Href: "/analytics"},
			})
		}
	}

	// --- churn risk ----------------------------------------------------------
	if len(s.AtRisk) > 0 {
		names := make([]string, 0, 3)
		for i, a := range s.AtRisk {
			if i >= 3 {
				break
			}
			names = append(names, a.Name)
		}
		more := ""
		if len(s.AtRisk) > len(names) {
			more = fmt.Sprintf(" and %d more", len(s.AtRisk)-len(names))
		}
		out = append(out, Anomaly{
			Key: "at_risk_accounts", Severity: SevWarn,
			Title:          fmt.Sprintf("%d account(s) at churn risk", len(s.AtRisk)),
			Detail:         fmt.Sprintf("%s%s", strings.Join(names, ", "), more),
			Metric:         fmt.Sprintf("%d", len(s.AtRisk)),
			Recommendation: "Have a rep re-engage the flagged accounts before they lapse",
			Action:         &Action{Kind: "at_risk", Label: "Review at-risk accounts", Href: "/account-health"},
		})
	}

	// --- revenue concentration ----------------------------------------------
	if s.TopConcentration >= 40 && len(s.TopCustomers) > 0 {
		out = append(out, Anomaly{
			Key: "revenue_concentration", Severity: SevWarn,
			Title:          fmt.Sprintf("Revenue concentration: %.0f%% from one account", s.TopConcentration),
			Detail:         fmt.Sprintf("%s accounts for %.0f%% of this period's revenue", s.TopCustomers[0].Name, s.TopConcentration),
			Metric:         fmt.Sprintf("%.0f%%", s.TopConcentration),
			Recommendation: "Grow demand across more accounts to reduce single-customer dependency",
			Action:         &Action{Kind: "customers", Label: "View accounts", Href: "/customers"},
		})
	}

	// --- inventory -----------------------------------------------------------
	if s.LowStock > 0 {
		sev := SevInfo
		if s.LowStock >= 10 {
			sev = SevWarn
		}
		out = append(out, Anomaly{
			Key: "low_stock", Severity: sev,
			Title:          fmt.Sprintf("%d SKU(s) at or below reorder point", s.LowStock),
			Detail:         "low stock risks back-orders and lost sales",
			Metric:         fmt.Sprintf("%d", s.LowStock),
			Recommendation: "Replenish the low-stock SKUs to avoid stockouts",
			Action:         &Action{Kind: "inventory", Label: "Open low-stock report", Href: "/inventory"},
		})
	}

	// --- acquisition (positive) ---------------------------------------------
	if s.NewCustomers > 0 {
		out = append(out, Anomaly{
			Key: "new_customers", Severity: SevInfo,
			Title:  fmt.Sprintf("Won %d new account(s)", s.NewCustomers),
			Detail: "first orders from accounts that had never ordered before",
			Metric: fmt.Sprintf("%d", s.NewCustomers),
		})
	}

	sort.SliceStable(out, func(i, j int) bool {
		return severityRank(out[i].Severity) < severityRank(out[j].Severity)
	})
	return out
}

// PrevRevenueNonZero reports whether the prior period had any revenue (so a
// percentage change is meaningful rather than a grew-from-zero artifact).
func (s Snapshot) PrevRevenueNonZero() bool { return isPositive(s.PrevRevenue) }

func severityRank(sev string) int {
	switch sev {
	case SevCritical:
		return 0
	case SevWarn:
		return 1
	default:
		return 2
	}
}

// isPositive reports whether a money string parses to a value greater than zero.
func isPositive(v string) bool {
	r, err := money.Parse(v)
	if err != nil {
		return false
	}
	return r.Sign() > 0
}

func pct(f float64) string { return fmt.Sprintf("%+.1f%%", f) }
