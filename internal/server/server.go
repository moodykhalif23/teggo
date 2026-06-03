package server

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"

	"b2bcommerce/internal/auth"
	"b2bcommerce/internal/inventory"
	authmod "b2bcommerce/internal/modules/auth"
	"b2bcommerce/internal/modules/cart"
	"b2bcommerce/internal/modules/catalog"
	"b2bcommerce/internal/modules/customers"
	"b2bcommerce/internal/modules/health"
	"b2bcommerce/internal/modules/otc"
	"b2bcommerce/internal/modules/pricing"
	"b2bcommerce/internal/modules/sales"
	"b2bcommerce/internal/payments/gateway"
	mw "b2bcommerce/internal/server/middleware"
	"b2bcommerce/internal/store"
)

// options holds optional dependencies wired in by the caller.
type options struct {
	recompute pricing.Enqueuer
	pdf       otc.PDFEnqueuer
	notifier  otc.Notifier
	gateway   gateway.Gateway
}

// Option configures optional server dependencies.
type Option func(*options)

func WithRecompute(e pricing.Enqueuer) Option {
	return func(o *options) { o.recompute = e }
}

// WithInvoicePDF wires the invoice-PDF enqueuer into the order-to-cash module.
func WithInvoicePDF(e otc.PDFEnqueuer) Option {
	return func(o *options) { o.pdf = e }
}

// WithNotifier wires the transactional-email enqueuer into the sales + OTC
// modules (order confirmation, quote sent, invoice issued).
func WithNotifier(n otc.Notifier) Option {
	return func(o *options) { o.notifier = n }
}

// WithPaymentGateway sets the card processor for storefront card payments.
// Defaults to the deterministic Mock gateway when unset.
func WithPaymentGateway(g gateway.Gateway) Option {
	return func(o *options) { o.gateway = g }
}

// New builds the fully-wired HTTP handler.
func New(st *store.Store, issuer *auth.Issuer, opts ...Option) http.Handler {
	var o options
	for _, fn := range opts {
		fn(&o)
	}
	if o.gateway == nil {
		o.gateway = gateway.Mock{} // deterministic card path by default
	}

	r := chi.NewRouter()

	r.Use(chimw.RequestID)
	r.Use(chimw.RealIP)
	r.Use(chimw.Logger)
	r.Use(chimw.Recoverer)
	r.Use(chimw.Timeout(30 * time.Second))

	authMW := mw.Authenticator(issuer)

	// Modules mount their own routes. Add new modules here as they land.
	health.New(st).Routes(r)
	authmod.New(st, issuer).Routes(r)
	catalog.New(st.Queries()).Routes(r, authMW)
	customers.New(st.Queries()).Routes(r, authMW)
	pricing.New(st.Queries(), o.recompute).Routes(r, authMW)
	cart.New(st.Queries()).Routes(r, authMW)
	sales.New(st.Pool(), o.notifier).Routes(r, authMW)
	otc.New(st.Pool(), o.pdf, o.notifier, o.gateway).Routes(r, authMW)
	inventory.New(st.Pool()).Routes(r, authMW)

	return r
}
