package server

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"

	"b2bcommerce/internal/auth"
	authmod "b2bcommerce/internal/modules/auth"
	"b2bcommerce/internal/modules/cart"
	"b2bcommerce/internal/modules/catalog"
	"b2bcommerce/internal/modules/customers"
	"b2bcommerce/internal/modules/health"
	"b2bcommerce/internal/modules/pricing"
	"b2bcommerce/internal/modules/sales"
	mw "b2bcommerce/internal/server/middleware"
	"b2bcommerce/internal/store"
)

// options holds optional dependencies wired in by the caller.
type options struct {
	recompute pricing.Enqueuer
}

// Option configures optional server dependencies.
type Option func(*options)

func WithRecompute(e pricing.Enqueuer) Option {
	return func(o *options) { o.recompute = e }
}

// New builds the fully-wired HTTP handler.
func New(st *store.Store, issuer *auth.Issuer, opts ...Option) http.Handler {
	var o options
	for _, fn := range opts {
		fn(&o)
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
	sales.New(st.Pool()).Routes(r, authMW)

	return r
}
