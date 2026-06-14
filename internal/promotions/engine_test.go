package promotions

import (
	"testing"
	"time"
)

func ptrStr(s string) *string { return &s }
func ptrI32(n int32) *int32   { return &n }

func TestEvaluate(t *testing.T) {
	now := time.Date(2026, 6, 11, 12, 0, 0, 0, time.UTC)
	past := now.Add(-48 * time.Hour)
	future := now.Add(48 * time.Hour)

	cases := []struct {
		name      string
		subtotal  string
		code      string
		cands     []Candidate
		wantDisc  string
		wantPromo int64 // 0 = none
	}{
		{
			name:     "automatic percent",
			subtotal: "100.0000",
			cands:    []Candidate{{ID: 1, Name: "10% off", DiscountType: "percent", DiscountValue: "10"}},
			wantDisc: "10.0000", wantPromo: 1,
		},
		{
			name:     "fixed amount",
			subtotal: "100.0000",
			cands:    []Candidate{{ID: 2, DiscountType: "amount", DiscountValue: "15"}},
			wantDisc: "15.0000", wantPromo: 2,
		},
		{
			name:     "coupon required, not entered → no discount",
			subtotal: "100.0000",
			cands:    []Candidate{{ID: 3, Code: ptrStr("SAVE20"), DiscountType: "percent", DiscountValue: "20"}},
			wantDisc: "0", wantPromo: 0,
		},
		{
			name:     "coupon required, entered (case-insensitive)",
			subtotal: "100.0000", code: "save20",
			cands:    []Candidate{{ID: 4, Code: ptrStr("SAVE20"), DiscountType: "percent", DiscountValue: "20"}},
			wantDisc: "20.0000", wantPromo: 4,
		},
		{
			name:     "below minimum subtotal → excluded",
			subtotal: "40.0000",
			cands:    []Candidate{{ID: 5, MinSubtotal: ptrStr("50"), DiscountType: "amount", DiscountValue: "10"}},
			wantDisc: "0", wantPromo: 0,
		},
		{
			name:     "expired (ends before now) → excluded",
			subtotal: "100.0000",
			cands:    []Candidate{{ID: 6, EndsAt: &past, DiscountType: "percent", DiscountValue: "10"}},
			wantDisc: "0", wantPromo: 0,
		},
		{
			name:     "not started yet → excluded",
			subtotal: "100.0000",
			cands:    []Candidate{{ID: 7, StartsAt: &future, DiscountType: "percent", DiscountValue: "10"}},
			wantDisc: "0", wantPromo: 0,
		},
		{
			name:     "redemption cap reached → excluded",
			subtotal: "100.0000",
			cands:    []Candidate{{ID: 8, MaxRedemptions: ptrI32(5), TimesRedeemed: 5, DiscountType: "amount", DiscountValue: "10"}},
			wantDisc: "0", wantPromo: 0,
		},
		{
			name:     "amount larger than subtotal is clamped",
			subtotal: "8.0000",
			cands:    []Candidate{{ID: 9, DiscountType: "amount", DiscountValue: "100"}},
			wantDisc: "8.0000", wantPromo: 9,
		},
		{
			name:     "best of several wins (largest discount)",
			subtotal: "100.0000",
			cands: []Candidate{
				{ID: 10, DiscountType: "percent", DiscountValue: "10"}, // 10
				{ID: 11, DiscountType: "amount", DiscountValue: "25"},  // 25 ← best
				{ID: 12, DiscountType: "percent", DiscountValue: "5"},  // 5
			},
			wantDisc: "25.0000", wantPromo: 11,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Evaluate(tc.subtotal, tc.code, now, tc.cands)
			if got.Discount != tc.wantDisc {
				t.Errorf("discount: want %s, got %s", tc.wantDisc, got.Discount)
			}
			var gotID int64
			if got.Promotion != nil {
				gotID = got.Promotion.ID
			}
			if gotID != tc.wantPromo {
				t.Errorf("promotion: want id %d, got %d", tc.wantPromo, gotID)
			}
		})
	}
}
