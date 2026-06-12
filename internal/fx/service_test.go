package fx_test

import (
	"testing"

	"b2bcommerce/internal/fx"
)

func TestConvert(t *testing.T) {
	cases := []struct {
		amount, rate, want string
	}{
		{"100.0000", "1.30000000", "130.0000"},
		{"100.0000", "0.00750000", "0.7500"},
		{"0", "1.30000000", "0.0000"},
		{"19.9900", "1.10000000", "21.9890"},
	}
	for _, c := range cases {
		got, err := fx.Convert(c.amount, c.rate)
		if err != nil {
			t.Fatalf("convert %s × %s: %v", c.amount, c.rate, err)
		}
		if got != c.want {
			t.Errorf("convert %s × %s: want %s, got %s", c.amount, c.rate, c.want, got)
		}
	}
}
