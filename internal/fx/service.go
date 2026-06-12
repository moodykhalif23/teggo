// Package fx converts money between currencies using org-scoped exchange rates.
// Rates are a time series in fx_rates (latest as_of wins). All arithmetic is exact
// (via internal/money) — never floats.
package fx

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"b2bcommerce/internal/money"
	"b2bcommerce/internal/store/gen"
)

type Service struct {
	q *gen.Queries
}

func NewService(q *gen.Queries) *Service { return &Service{q: q} }

// Rate returns the latest base→quote rate for an org. base == quote yields "1".
// ok is false when no rate is configured for the pair.
func (s *Service) Rate(ctx context.Context, org int64, base, quote string) (rate string, ok bool, err error) {
	if base == quote {
		return "1", true, nil
	}
	r, e := s.q.GetLatestFxRate(ctx, gen.GetLatestFxRateParams{OrganizationID: org, BaseCurrency: base, QuoteCurrency: quote})
	if errors.Is(e, pgx.ErrNoRows) {
		return "", false, nil
	}
	if e != nil {
		return "", false, e
	}
	return r, true, nil
}

// Convert multiplies a money amount by a rate, returning a 4-dp money string.
func Convert(amount, rate string) (string, error) {
	return money.LineTotal(amount, rate)
}
