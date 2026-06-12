package rebates

import (
	"testing"
	"time"
)

func TestPeriodWindow(t *testing.T) {
	ref := time.Date(2026, 5, 17, 9, 0, 0, 0, time.UTC) // mid-Q2, May
	cases := []struct {
		period, key, start, end string
	}{
		{"quarterly", "2026-Q2", "2026-04-01", "2026-07-01"},
		{"monthly", "2026-05", "2026-05-01", "2026-06-01"},
		{"annual", "2026", "2026-01-01", "2027-01-01"},
	}
	for _, c := range cases {
		s, e, k := PeriodWindow(c.period, ref)
		if k != c.key || s.Format("2006-01-02") != c.start || e.Format("2006-01-02") != c.end {
			t.Errorf("%s: want key=%s [%s,%s), got key=%s [%s,%s)", c.period, c.key, c.start, c.end, k, s.Format("2006-01-02"), e.Format("2006-01-02"))
		}
	}
}

func TestApplicable(t *testing.T) {
	tiers := []Tier{
		{MinAmount: "50000", RatePercent: "5"},
		{MinAmount: "10000", RatePercent: "2"},
		{MinAmount: "25000", RatePercent: "3.5"},
	}
	cases := []struct {
		total, wantRate string
		wantOK          bool
	}{
		{"5000", "", false},      // below all tiers
		{"10000", "2", true},     // exactly the first tier
		{"30000", "3.5", true},   // between 25k and 50k
		{"100000", "5", true},    // top tier
	}
	for _, c := range cases {
		rate, _, ok := Applicable(c.total, tiers)
		if ok != c.wantOK || rate != c.wantRate {
			t.Errorf("total %s: want rate=%q ok=%v, got rate=%q ok=%v", c.total, c.wantRate, c.wantOK, rate, ok)
		}
	}
}

func TestRebate(t *testing.T) {
	got, err := Rebate("30000.0000", "3.5")
	if err != nil || got != "1050.0000" {
		t.Errorf("rebate: want 1050.0000, got %s (err %v)", got, err)
	}
}
