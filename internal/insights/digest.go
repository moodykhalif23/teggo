package insights

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"b2bcommerce/internal/ai"
	"b2bcommerce/internal/store/gen"
)

// Engine is the query surface the digest generator needs: the analytics Querier
// plus digest persistence. *gen.Queries satisfies it.
type Engine interface {
	Querier
	CreateInsightDigest(ctx context.Context, arg gen.CreateInsightDigestParams) (gen.InsightDigest, error)
}

// GenerateDigest builds the snapshot, detects anomalies, narrates an executive
// briefing (falling back to the deterministic narrator if the configured one
// errors so a digest is always produced), persists it, and returns the stored
// row. trigger is "schedule" (the weekly job) or "manual" (an on-demand run).
func GenerateDigest(ctx context.Context, q Engine, narrator ai.Narrator, orgID int64, now time.Time, windowDays int, trigger string) (gen.InsightDigest, error) {
	snap, err := Build(ctx, q, orgID, now, windowDays)
	if err != nil {
		return gen.InsightDigest{}, err
	}
	anomalies := Detect(snap)
	narrative, source := narrate(ctx, narrator, snap, anomalies)

	kpis, err := json.Marshal(snap.KPIs())
	if err != nil {
		return gen.InsightDigest{}, err
	}
	anomaliesJSON, err := json.Marshal(anomalies)
	if err != nil {
		return gen.InsightDigest{}, err
	}
	if trigger != "manual" {
		trigger = "schedule"
	}
	return q.CreateInsightDigest(ctx, gen.CreateInsightDigestParams{
		OrganizationID: orgID,
		PeriodStart:    pgtype.Date{Time: snap.PeriodStart, Valid: true},
		PeriodEnd:      pgtype.Date{Time: snap.PeriodEnd, Valid: true},
		Source:         source,
		Trigger:        trigger,
		Narrative:      narrative,
		Kpis:           kpis,
		Anomalies:      anomaliesJSON,
	})
}

// narrate runs the configured narrator with the deterministic narrator as a hard
// fallback. source records which engine actually authored the text.
func narrate(ctx context.Context, narrator ai.Narrator, snap Snapshot, anomalies []Anomaly) (text, source string) {
	brief := snap.Brief(anomalies)
	if narrator != nil {
		if out, err := narrator.Narrate(ctx, brief); err == nil && out != "" {
			return out, narrator.Name()
		}
	}
	out, _ := ai.NewDeterministicNarrator().Narrate(ctx, brief)
	return out, "deterministic"
}

// KPIs is the flat metric map persisted on a digest and returned by the live
// metrics endpoint — one shape, used by both, so the dashboard and the stored
// briefing never diverge.
func (s Snapshot) KPIs() map[string]any {
	return map[string]any{
		"revenue":               s.Revenue,
		"orders":                s.Orders,
		"avg_order_value":       s.AOV,
		"prev_revenue":          s.PrevRevenue,
		"prev_orders":           s.PrevOrders,
		"revenue_delta_pct":     s.RevenueDelta,
		"orders_delta_pct":      s.OrdersDelta,
		"new_customers":         s.NewCustomers,
		"cost":                  s.Cost,
		"gross_margin":          s.GrossMargin,
		"margin_pct":            s.MarginPct,
		"prev_margin_pct":       s.PrevMarginPct,
		"margin_delta_pts":      s.MarginDeltaPts,
		"has_cost":              s.HasCost,
		"open_ar":               s.OpenAR,
		"ar_90_plus":            s.AR90Plus,
		"open_invoices":         s.OpenInvoices,
		"top_concentration_pct": s.TopConcentration,
		"low_stock":             s.LowStock,
		"top_customers":         s.TopCustomers,
		"at_risk":               s.AtRisk,
		"period_start":          s.PeriodStart.Format("2006-01-02"),
		"period_end":            s.PeriodEnd.Format("2006-01-02"),
		"window_days":           s.WindowDays,
	}
}

// PeriodLabel is a human label for the window (a calendar-aware "week of …" when
// the window is the default trading week).
func (s Snapshot) PeriodLabel() string {
	start := s.PeriodStart.Format("Jan 2")
	end := s.PeriodEnd.Format("Jan 2, 2006")
	if s.WindowDays == DefaultWindowDays {
		return fmt.Sprintf("week of %s – %s", start, end)
	}
	return fmt.Sprintf("%d days, %s – %s", s.WindowDays, start, end)
}

// Brief maps the snapshot + detected anomalies into the narrator's grounding
// facts. The headline lines are the only place numbers are formatted for prose;
// the narrator is told to use exactly these.
func (s Snapshot) Brief(anomalies []Anomaly) ai.InsightBrief {
	headlines := []string{
		fmt.Sprintf("Revenue %s (%s vs the prior period).", s.Revenue, pct(s.RevenueDelta)),
		fmt.Sprintf("Orders %d (%s); average order value %s.", s.Orders, pct(s.OrdersDelta), s.AOV),
	}
	if s.HasCost {
		headlines = append(headlines, fmt.Sprintf("Gross margin %.1f%% (%+.1f pts vs the prior period).", s.MarginPct, s.MarginDeltaPts))
	}
	if isPositive(s.OpenAR) {
		headlines = append(headlines, fmt.Sprintf("Open receivables %s across %d invoice(s).", s.OpenAR, s.OpenInvoices))
	}
	if s.NewCustomers > 0 {
		headlines = append(headlines, fmt.Sprintf("%d new account(s) placed a first order.", s.NewCustomers))
	}
	ba := make([]ai.BriefAnomaly, 0, len(anomalies))
	for _, a := range anomalies {
		ba = append(ba, ai.BriefAnomaly{
			Severity: a.Severity, Title: a.Title, Detail: a.Detail, Recommendation: a.Recommendation,
		})
	}
	return ai.InsightBrief{Period: s.PeriodLabel(), Anomalies: ba, Headlines: headlines}
}
