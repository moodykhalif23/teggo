// Package promotions evaluates cart-level discounts. It is a pure, DB-free
// engine (mirroring internal/pricing): the caller loads candidate promotions
// from the store, maps them to Candidate values, and asks Evaluate for the best
// applicable discount given a subtotal, an optionally entered coupon code, and
// the current time. Money is handled exactly via internal/money (never floats).
package promotions

import (
	"strings"
	"time"

	"b2bcommerce/internal/money"
)

// Candidate is a promotion considered for a cart, loaded from the store.
type Candidate struct {
	ID             int64
	Name           string
	Code           *string // nil = automatic (no code required)
	DiscountType   string  // "percent" | "amount"
	DiscountValue  string  // decimal string; e.g. "10" (=10%) or "5.00" (=$5)
	MinSubtotal    *string // qualify only at/above this subtotal (nil = none)
	StartsAt       *time.Time
	EndsAt         *time.Time
	MaxRedemptions *int32 // global cap (nil = unlimited)
	TimesRedeemed  int32
	Priority       int32
}

// Result is the chosen promotion plus the computed discount (4-dp, clamped to
// the subtotal). When nothing applies, Promotion is nil and Discount is "0".
type Result struct {
	Promotion *Candidate
	Discount  string
	Label     string
}

// Evaluate picks the best applicable promotion for `subtotal` at `now`, given an
// optionally entered coupon `code`. Automatic promotions (nil Code) are always
// considered; coded promotions only when the entered code matches. "Best" =
// largest discount, ties broken by higher priority. The discount never exceeds
// the subtotal. v1 applies a single promotion (no stacking).
func Evaluate(subtotal, code string, now time.Time, candidates []Candidate) Result {
	none := Result{Discount: "0"}
	if _, err := money.Parse(subtotal); err != nil {
		return none
	}
	entered := strings.ToLower(strings.TrimSpace(code))

	var best *Candidate
	var bestDisc string
	for i := range candidates {
		c := &candidates[i]
		if !c.eligible(subtotal, entered, now) {
			continue
		}
		disc := c.discountFor(subtotal)
		if cmp, _ := money.Cmp(disc, subtotal); cmp > 0 {
			disc = subtotal // never discount below zero
		}
		if cmp, _ := money.Cmp(disc, "0"); cmp <= 0 {
			continue // a zero discount isn't worth applying
		}
		if best == nil {
			best, bestDisc = c, disc
			continue
		}
		cmp, _ := money.Cmp(disc, bestDisc)
		if cmp > 0 || (cmp == 0 && c.Priority > best.Priority) {
			best, bestDisc = c, disc
		}
	}
	if best == nil {
		return none
	}
	return Result{Promotion: best, Discount: bestDisc, Label: best.Name}
}

func (c *Candidate) eligible(subtotal, enteredCode string, now time.Time) bool {
	// Coupon code gate.
	if c.Code != nil {
		if enteredCode == "" || enteredCode != strings.ToLower(strings.TrimSpace(*c.Code)) {
			return false
		}
	}
	// Schedule window.
	if c.StartsAt != nil && now.Before(*c.StartsAt) {
		return false
	}
	if c.EndsAt != nil && now.After(*c.EndsAt) {
		return false
	}
	// Global redemption cap.
	if c.MaxRedemptions != nil && c.TimesRedeemed >= *c.MaxRedemptions {
		return false
	}
	// Minimum subtotal threshold.
	if c.MinSubtotal != nil {
		if cmp, err := money.Cmp(subtotal, *c.MinSubtotal); err != nil || cmp < 0 {
			return false
		}
	}
	return true
}

// discountFor computes the raw discount this promotion yields for a subtotal.
func (c *Candidate) discountFor(subtotal string) string {
	switch c.DiscountType {
	case "percent":
		d, err := money.Percent(subtotal, c.DiscountValue)
		if err != nil {
			return "0"
		}
		return d
	case "amount":
		// Normalise the configured amount to a 4-dp string.
		d, err := money.Sum(c.DiscountValue)
		if err != nil {
			return "0"
		}
		return d
	default:
		return "0"
	}
}
