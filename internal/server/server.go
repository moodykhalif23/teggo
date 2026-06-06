package server

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	"b2bcommerce/internal/ai"
	"b2bcommerce/internal/auth"
	"b2bcommerce/internal/blob"
	"b2bcommerce/internal/imageproc"
	"b2bcommerce/internal/inventory"
	"b2bcommerce/internal/modules/account"
	authmod "b2bcommerce/internal/modules/auth"
	"b2bcommerce/internal/modules/cart"
	"b2bcommerce/internal/modules/catalog"
	"b2bcommerce/internal/modules/cms"
	"b2bcommerce/internal/modules/cpq"
	"b2bcommerce/internal/modules/crm"
	"b2bcommerce/internal/modules/customers"
	"b2bcommerce/internal/modules/dam"
	erpmod "b2bcommerce/internal/modules/erp"
	"b2bcommerce/internal/modules/field"
	"b2bcommerce/internal/modules/health"
	"b2bcommerce/internal/modules/assistant"
	"b2bcommerce/internal/modules/integration"
	"b2bcommerce/internal/modules/marketplace"
	"b2bcommerce/internal/modules/otc"
	"b2bcommerce/internal/modules/pricing"
	"b2bcommerce/internal/modules/reporting"
	"b2bcommerce/internal/modules/sales"
	"b2bcommerce/internal/modules/settings"
	shippingmod "b2bcommerce/internal/modules/shipping"
	ssomod "b2bcommerce/internal/modules/sso"
	taxmod "b2bcommerce/internal/modules/tax"
	"b2bcommerce/internal/modules/tenancy"
	"b2bcommerce/internal/modules/wfadmin"
	"b2bcommerce/internal/payments/gateway"
	mw "b2bcommerce/internal/server/middleware"
	shippingeng "b2bcommerce/internal/shipping"
	"b2bcommerce/internal/store"
)

type notifier interface {
	EnqueueEmail(ctx context.Context, to, template string, data map[string]any) error
	EmitEvent(ctx context.Context, event string, payload map[string]any) error
}

// options holds optional dependencies wired in by the caller.
type options struct {
	recompute    pricing.Enqueuer
	pdf          otc.PDFEnqueuer
	notifier     notifier
	gateway      gateway.Gateway
	logger       *slog.Logger
	maxBodyBytes int64
	blobStore    blob.Store
	imageProc    imageproc.Processor
	rendition    dam.RenditionEnqueuer
	punchoutURL  string
	ediSenderID  string
	punchoutTTL  time.Duration
	shipProvider shippingeng.Adapter
	aiProvider   ai.Provider
}

// Option configures optional server dependencies.
type Option func(*options)

// WithLogger sets the structured logger used by the request-logging middleware.
// Defaults to slog.Default() when unset.
func WithLogger(l *slog.Logger) Option {
	return func(o *options) { o.logger = l }
}

// WithMaxBodyBytes caps accepted request-body size (bytes). Defaults to 1 MiB.
func WithMaxBodyBytes(n int64) Option {
	return func(o *options) { o.maxBodyBytes = n }
}

// WithMedia wires the DAM module's blob store and image processor. Without a
// blob store the DAM routes are not mounted.
func WithMedia(store blob.Store, proc imageproc.Processor) Option {
	return func(o *options) { o.blobStore = store; o.imageProc = proc }
}

// WithRendition wires the async rendition enqueuer used after media upload.
func WithRendition(e dam.RenditionEnqueuer) Option {
	return func(o *options) { o.rendition = e }
}

// WithIntegration configures the Punchout/EDI module: the storefront landing
// URL for punchout, our outbound cXML/EDI sender identity, and the punchout
// session TTL.
func WithIntegration(storefrontURL, ediSenderID string, ttl time.Duration) Option {
	return func(o *options) { o.punchoutURL = storefrontURL; o.ediSenderID = ediSenderID; o.punchoutTTL = ttl }
}

// WithShippingProvider selects the shipping rate/label/track provider (e.g. a
// MockCarrier, or a real FedEx/UPS adapter). Defaults to the local table-rate.
func WithShippingProvider(p shippingeng.Adapter) Option {
	return func(o *options) { o.shipProvider = p }
}

func WithRecompute(e pricing.Enqueuer) Option {
	return func(o *options) { o.recompute = e }
}

// WithInvoicePDF wires the invoice-PDF enqueuer into the order-to-cash module.
func WithInvoicePDF(e otc.PDFEnqueuer) Option {
	return func(o *options) { o.pdf = e }
}

// WithNotifier wires the transactional-email enqueuer into the sales + OTC
// modules (order confirmation, quote sent, invoice issued).
func WithNotifier(n notifier) Option {
	return func(o *options) { o.notifier = n }
}

// WithPaymentGateway sets the card processor for storefront card payments.
// Defaults to the deterministic Mock gateway when unset.
func WithPaymentGateway(g gateway.Gateway) Option {
	return func(o *options) { o.gateway = g }
}

// WithAIProvider selects the assistant's decision engine. Defaults to the
// deterministic local engine when unset.
func WithAIProvider(p ai.Provider) Option {
	return func(o *options) { o.aiProvider = p }
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
	if o.aiProvider == nil {
		o.aiProvider = ai.NewDeterministicProvider() // offline, reproducible default
	}
	if o.logger == nil {
		o.logger = slog.Default()
	}
	if o.maxBodyBytes == 0 {
		o.maxBodyBytes = 1 << 20 // 1 MiB
	}

	r := chi.NewRouter()

	r.Use(chimw.RequestID)
	r.Use(chimw.RealIP)
	r.Use(mw.SecureHeaders)
	r.Use(mw.RequestLogger(o.logger))
	r.Use(chimw.Recoverer)
	r.Use(mw.MaxBytes(o.maxBodyBytes))
	r.Use(chimw.Timeout(30 * time.Second))

	authMW := mw.Authenticator(issuer)
	// Throttle credential endpoints to blunt brute-force / credential stuffing.
	loginLimit := mw.RateLimit(10, time.Minute)

	// Modules mount their own routes. Add new modules here as they land.
	health.New(st).Routes(r)
	authmod.New(st, issuer).Routes(r, loginLimit)
	catalog.New(st.Queries()).RoutesWithOptionalAuth(r, authMW, mw.OptionalAuthenticator(issuer))
	customers.New(st.Queries()).Routes(r, authMW)
	account.New(st.Queries()).Routes(r, authMW)
	mp := marketplace.New(st.Pool())
	mp.Routes(r, authMW)
	mp.RoutesVendor(r, authMW)
	assistant.New(st.Queries(), o.aiProvider).Routes(r, authMW)
	settings.New(st.Pool()).RoutesWithOptionalAuth(r, authMW, mw.OptionalAuthenticator(issuer))
	pricing.New(st.Queries(), o.recompute).Routes(r, authMW)
	cart.New(st.Queries()).Routes(r, authMW)
	sales.New(st.Pool(), o.notifier).Routes(r, authMW)
	otc.New(st.Pool(), o.pdf, o.notifier, o.gateway, issuer).Routes(r, authMW)
	inventory.New(st.Pool()).Routes(r, authMW)
	crm.New(st.Pool()).Routes(r, authMW)
	wfadmin.New(st.Pool()).Routes(r, authMW)
	cms.New(st.Pool(), issuer).Routes(r, authMW)
	reporting.New(st.Pool()).Routes(r, authMW)
	tenancy.New(st.Pool()).Routes(r, authMW)
	if o.blobStore != nil {
		proc := o.imageProc
		if proc == nil {
			proc = imageproc.GoProcessor{}
		}
		dam.New(st.Pool(), o.blobStore, proc, issuer, o.rendition).Routes(r, authMW)
	}
	integration.New(st.Pool(), issuer, o.punchoutURL, o.ediSenderID, o.punchoutTTL).Routes(r, authMW)
	field.New(st.Pool()).Routes(r, authMW)
	cpq.New(st.Pool()).Routes(r, authMW)
	taxmod.New(st.Pool()).Routes(r, authMW)
	shippingmod.NewWithProvider(st.Pool(), o.shipProvider).Routes(r, authMW)
	erpmod.New(st.Pool()).Routes(r, authMW)
	ssomod.New(st.Pool(), issuer).Routes(r, authMW)

	// Wrap the router so HTTP server metrics (request count, duration) flow to
	// the configured OpenTelemetry MeterProvider. No-op when telemetry is off.
	return otelhttp.NewHandler(r, "teggo.http")
}
