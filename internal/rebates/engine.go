// Package rebates computes volume-incentive periods and tiered rebate amounts.
// Pure and DB-free: the caller derives the qualifying total from orders and the
// tiers from the store. Money math is exact (internal/money).
package rebates

import (
	"fmt"
	"time"

	"b2bcommerce/internal/money"
)

// PeriodWindow returns the [start, end) window (UTC) and a display key for the
// period of the given type containing ref.
func PeriodWindow(period string, ref time.Time) (start, end time.Time, key string) {
	ref = ref.UTC()
	y := ref.Year()
	switch period {
	case "monthly":
		start = time.Date(y, ref.Month(), 1, 0, 0, 0, 0, time.UTC)
		end = start.AddDate(0, 1, 0)
		key = start.Format("2006-01")
	case "annual":
		start = time.Date(y, time.January, 1, 0, 0, 0, 0, time.UTC)
		end = start.AddDate(1, 0, 0)
		key = start.Format("2006")
	case "quarterly":
		fallthrough
	default:
		q := (int(ref.Month()) - 1) / 3 // 0..3
		start = time.Date(y, time.Month(q*3+1), 1, 0, 0, 0, 0, time.UTC)
		end = start.AddDate(0, 3, 0)
		key = fmt.Sprintf("%d-Q%d", y, q+1)
	}
	return start, end, key
}

// Tier is a rebate threshold + rate (percent, e.g. "2.5" = 2.5%).
type Tier struct {
	MinAmount   string
	RatePercent string
}

// Applicable returns the highest tier whose MinAmount <= total (retroactive: the
// rate applies to the whole total). tiers may be unsorted; ok is false when none
// qualify.
func Applicable(total string, tiers []Tier) (rate, minAmount string, ok bool) {
	best := -1
	for i, t := range tiers {
		if cmp, err := money.Cmp(total, t.MinAmount); err != nil || cmp < 0 {
			continue // doesn't reach this tier
		}
		if best == -1 {
			best = i
			continue
		}
		if c, _ := money.Cmp(t.MinAmount, tiers[best].MinAmount); c > 0 {
			best = i
		}
	}
	if best == -1 {
		return "", "", false
	}
	return tiers[best].RatePercent, tiers[best].MinAmount, true
}

// Rebate computes total × ratePercent% as a 4-dp money string.
func Rebate(total, ratePercent string) (string, error) {
	return money.Percent(total, ratePercent)
}
