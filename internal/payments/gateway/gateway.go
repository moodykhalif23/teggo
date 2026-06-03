package gateway

import (
	"context"
	"errors"
	"strings"
)

type ChargeRequest struct {
	Amount    string // e.g. "100.0000"
	Currency  string // ISO-4217, 3 letters
	Token     string // gateway card token / nonce
	Reference string // our idempotency reference (e.g. invoice public_id)
}

// ChargeResult is the outcome of a charge.
type ChargeResult struct {
	GatewayReference string // processor's charge/intent id
	Captured         bool
}

// ErrDeclined is returned when the processor declines the charge.
var ErrDeclined = errors.New("payment declined")

// Gateway is the payment-processor abstraction.
type Gateway interface {
	Provider() string
	CreateCharge(ctx context.Context, r ChargeRequest) (ChargeResult, error)
}

type Mock struct{}

func (Mock) Provider() string { return "mock" }

func (Mock) CreateCharge(_ context.Context, r ChargeRequest) (ChargeResult, error) {
	if strings.Contains(strings.ToLower(r.Token), "decline") ||
		strings.Contains(strings.ToLower(r.Reference), "decline") {
		return ChargeResult{}, ErrDeclined
	}
	ref := "mock_ch_" + r.Reference
	if ref == "mock_ch_" {
		ref = "mock_ch_unref"
	}
	return ChargeResult{GatewayReference: ref, Captured: true}, nil
}
