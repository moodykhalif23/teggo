// Package money does exact decimal arithmetic on the NUMERIC(15,4) values that
// cross the API as strings (see the sqlc money override). It uses math/big.Rat
// so there is never any binary-floating-point drift in totals.
package money

import (
	"fmt"
	"math/big"
)

const Scale = 4

// Parse reads a decimal string ("42.0000") into an exact rational.
func Parse(s string) (*big.Rat, error) {
	r, ok := new(big.Rat).SetString(s)
	if !ok {
		return nil, fmt.Errorf("money: invalid decimal %q", s)
	}
	return r, nil
}

// Format renders a rational as a fixed 4-dp decimal string.
func Format(r *big.Rat) string { return r.FloatString(Scale) }

// LineTotal returns quantity * unitPrice as a 4-dp string.
func LineTotal(quantity, unitPrice string) (string, error) {
	q, err := Parse(quantity)
	if err != nil {
		return "", err
	}
	p, err := Parse(unitPrice)
	if err != nil {
		return "", err
	}
	return Format(new(big.Rat).Mul(q, p)), nil
}

// Sub returns a - b as a 4-dp string.
func Sub(a, b string) (string, error) {
	ra, err := Parse(a)
	if err != nil {
		return "", err
	}
	rb, err := Parse(b)
	if err != nil {
		return "", err
	}
	return Format(new(big.Rat).Sub(ra, rb)), nil
}

// Cmp compares two decimal strings: -1 if a<b, 0 if equal, 1 if a>b.
func Cmp(a, b string) (int, error) {
	ra, err := Parse(a)
	if err != nil {
		return 0, err
	}
	rb, err := Parse(b)
	if err != nil {
		return 0, err
	}
	return ra.Cmp(rb), nil
}

// RowTotal returns quantity*unitPrice - discount as a 4-dp string.
func RowTotal(quantity, unitPrice, discount string) (string, error) {
	q, err := Parse(quantity)
	if err != nil {
		return "", err
	}
	p, err := Parse(unitPrice)
	if err != nil {
		return "", err
	}
	d, err := Parse(discount)
	if err != nil {
		return "", err
	}
	line := new(big.Rat).Mul(q, p)
	return Format(new(big.Rat).Sub(line, d)), nil
}

// Percent returns amount * (percent/100) as a 4-dp string, computed exactly on
// rationals and rounded only at the final formatting step. Used for marketplace
// commission (a vendor rate of "12.5" means 12.5%).
func Percent(amount, percent string) (string, error) {
	a, err := Parse(amount)
	if err != nil {
		return "", err
	}
	p, err := Parse(percent)
	if err != nil {
		return "", err
	}
	prod := new(big.Rat).Mul(a, p)
	return Format(new(big.Rat).Quo(prod, big.NewRat(100, 1))), nil
}

// Sum adds 4-dp decimal strings exactly.
func Sum(values ...string) (string, error) {
	acc := new(big.Rat)
	for _, v := range values {
		r, err := Parse(v)
		if err != nil {
			return "", err
		}
		acc.Add(acc, r)
	}
	return Format(acc), nil
}
