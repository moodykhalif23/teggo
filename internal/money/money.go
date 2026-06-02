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
