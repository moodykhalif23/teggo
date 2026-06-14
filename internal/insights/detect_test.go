package insights

import "testing"

func find(as []Anomaly, key string) *Anomaly {
	for i := range as {
		if as[i].Key == key {
			return &as[i]
		}
	}
	return nil
}

func TestDetectRevenueDropIsCritical(t *testing.T) {
	s := Snapshot{PrevRevenue: "1000.0000", Revenue: "700.0000", RevenueDelta: -30, OpenAR: "0", AR90Plus: "0"}
	as := Detect(s)
	a := find(as, "revenue_drop")
	if a == nil {
		t.Fatalf("expected revenue_drop anomaly, got %+v", as)
	}
	if a.Severity != SevCritical {
		t.Errorf("revenue_drop severity = %q, want critical", a.Severity)
	}
	if a.Action == nil || a.Action.Href == "" {
		t.Errorf("revenue_drop should carry an action deep-link")
	}
}

func TestDetectRevenueSofteningIsWarn(t *testing.T) {
	s := Snapshot{PrevRevenue: "1000.0000", Revenue: "850.0000", RevenueDelta: -15, OpenAR: "0", AR90Plus: "0"}
	if a := find(Detect(s), "revenue_softening"); a == nil || a.Severity != SevWarn {
		t.Fatalf("expected revenue_softening (warn), got %+v", a)
	}
}

func TestDetectGrewFromZeroIsNotADrop(t *testing.T) {
	// Prior period had no revenue → a percentage swing must not fire a drop/growth.
	s := Snapshot{PrevRevenue: "0", Revenue: "500.0000", RevenueDelta: 100, OpenAR: "0", AR90Plus: "0"}
	if a := find(Detect(s), "revenue_growth"); a != nil {
		t.Errorf("grew-from-zero should not fire revenue_growth, got %+v", a)
	}
	if a := find(Detect(s), "revenue_drop"); a != nil {
		t.Errorf("grew-from-zero should not fire revenue_drop, got %+v", a)
	}
}

func TestDetectAR90Concentration(t *testing.T) {
	// 60% of open AR is 90+ → critical, not just warn.
	s := Snapshot{Revenue: "0", PrevRevenue: "0", OpenAR: "1000.0000", AR90Plus: "600.0000"}
	a := find(Detect(s), "ar_90_plus")
	if a == nil || a.Severity != SevCritical {
		t.Fatalf("expected critical ar_90_plus, got %+v", a)
	}
}

func TestDetectAtRiskAndConcentration(t *testing.T) {
	s := Snapshot{
		Revenue: "1000.0000", PrevRevenue: "1000.0000", RevenueDelta: 0, OpenAR: "0", AR90Plus: "0",
		AtRisk:           []RiskAccount{{Name: "Acme", Reason: "slipping"}, {Name: "Beta", Reason: "overdue"}},
		TopCustomers:     []CustomerSpend{{Name: "Acme", Revenue: "600.0000"}},
		TopConcentration: 60,
	}
	as := Detect(s)
	if find(as, "at_risk_accounts") == nil {
		t.Errorf("expected at_risk_accounts anomaly")
	}
	if find(as, "revenue_concentration") == nil {
		t.Errorf("expected revenue_concentration anomaly")
	}
}

func TestDetectMarginErosion(t *testing.T) {
	// Margin fell 12 points with cost data present → critical.
	s := Snapshot{
		Revenue: "1000.0000", PrevRevenue: "1000.0000", RevenueDelta: 0, OpenAR: "0", AR90Plus: "0",
		HasCost: true, MarginPct: 30, PrevMarginPct: 42,
	}
	a := find(Detect(s), "margin_erosion")
	if a == nil || a.Severity != SevCritical {
		t.Fatalf("expected critical margin_erosion, got %+v", a)
	}

	// Same drop but NO cost data → not meaningful, no signal.
	s.HasCost = false
	if a := find(Detect(s), "margin_erosion"); a != nil {
		t.Errorf("no cost data should suppress margin_erosion, got %+v", a)
	}
}

func TestDetectHealthyIsQuiet(t *testing.T) {
	s := Snapshot{
		Revenue: "1010.0000", PrevRevenue: "1000.0000", RevenueDelta: 1, OrdersDelta: 1,
		OpenAR: "100.0000", AR90Plus: "0", TopConcentration: 10, LowStock: 0, NewCustomers: 0,
	}
	if as := Detect(s); len(as) != 0 {
		t.Errorf("healthy snapshot should produce no signals, got %+v", as)
	}
}

func TestDetectOrdersWorstFirst(t *testing.T) {
	// A critical signal must sort ahead of an informational one.
	s := Snapshot{
		PrevRevenue: "1000.0000", Revenue: "600.0000", RevenueDelta: -40, OpenAR: "0", AR90Plus: "0",
		NewCustomers: 3,
	}
	as := Detect(s)
	if len(as) < 2 {
		t.Fatalf("expected at least 2 signals, got %+v", as)
	}
	if as[0].Severity != SevCritical {
		t.Errorf("worst signal should be first; got %q", as[0].Severity)
	}
}

func TestPctChangeHelpers(t *testing.T) {
	if got := pctChange("110.0000", "100.0000"); got != 10 {
		t.Errorf("pctChange(110,100) = %v, want 10", got)
	}
	if got := pctChange("50.0000", "0"); got != 100 {
		t.Errorf("pctChange(50,0) = %v, want 100 (grew from zero)", got)
	}
	if got := share("250.0000", "1000.0000"); got != 25 {
		t.Errorf("share(250,1000) = %v, want 25", got)
	}
	if got := avgOrderValue("1000.0000", 4); got != "250.0000" {
		t.Errorf("avgOrderValue(1000,4) = %q, want 250.0000", got)
	}
}
