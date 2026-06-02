// Package sales implements the negotiation spine (Implementation Pack 1 §6):
// RFQ -> Quote -> Order. RFQs and quote acceptance are storefront-facing
// (customer-scoped); quote building and order management are admin (sales rep).
// Orders are immutable snapshots; an accepted quote converts to an order in a
// single transaction. Status transitions are guarded by explicit state machines
// (later migratable onto the configurable workflow engine, Pack 2 §3).
package sales

import (
	"context"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	mw "b2bcommerce/internal/server/middleware"
	"b2bcommerce/internal/server/response"
	"b2bcommerce/internal/store/gen"
)

type Handler struct {
	pool *pgxpool.Pool
	q    *gen.Queries
}

func New(pool *pgxpool.Pool) *Handler { return &Handler{pool: pool, q: gen.New(pool)} }

func (h *Handler) Routes(r chi.Router, authMW func(http.Handler) http.Handler) {
	// Admin (sales rep) surface.
	r.Group(func(ar chi.Router) {
		ar.Use(authMW)
		ar.Use(mw.RequireAudience("admin"))

		ar.With(mw.RequirePermission("rfq.view")).Get("/admin/rfqs", h.adminListRFQs)
		ar.With(mw.RequirePermission("rfq.view")).Get("/admin/rfqs/{id}", h.adminGetRFQ)
		ar.With(mw.RequirePermission("quote.manage")).Post("/admin/rfqs/{id}/quote", h.quoteFromRFQ)

		ar.With(mw.RequirePermission("quote.view")).Get("/admin/quotes", h.adminListQuotes)
		ar.With(mw.RequirePermission("quote.manage")).Post("/admin/quotes", h.createQuote)
		ar.With(mw.RequirePermission("quote.view")).Get("/admin/quotes/{id}", h.adminGetQuote)
		ar.With(mw.RequirePermission("quote.manage")).Put("/admin/quotes/{id}", h.editQuote)
		ar.With(mw.RequirePermission("quote.manage")).Post("/admin/quotes/{id}/send", h.sendQuote)

		ar.With(mw.RequirePermission("order.view")).Get("/admin/orders", h.adminListOrders)
		ar.With(mw.RequirePermission("order.manage")).Post("/admin/orders", h.createOrderOnBehalf)
		ar.With(mw.RequirePermission("order.view")).Get("/admin/orders/{id}", h.adminGetOrder)
		ar.With(mw.RequirePermission("order.manage")).Patch("/admin/orders/{id}/status", h.patchOrderStatus)
	})

	// Storefront (customer) surface.
	r.Group(func(sr chi.Router) {
		sr.Use(authMW)
		sr.Use(mw.RequireAudience("storefront"))

		sr.Post("/storefront/rfqs", h.createRFQ)
		sr.Get("/storefront/rfqs", h.listMyRFQs)
		sr.Get("/storefront/rfqs/{publicID}", h.getMyRFQ)
		sr.Post("/storefront/rfqs/{publicID}/submit", h.submitRFQ)

		sr.Get("/storefront/quotes/{publicID}", h.getMyQuote)
		sr.Post("/storefront/quotes/{publicID}/accept", h.acceptQuote)
		sr.Post("/storefront/quotes/{publicID}/decline", h.declineQuote)

		sr.Get("/storefront/orders", h.listMyOrders)
		sr.Get("/storefront/orders/{publicID}", h.getMyOrder)
	})
}

// ---- actors ---------------------------------------------------------------

type adminCtx struct {
	orgID  int64
	userID *int64
}

func admin(r *http.Request) (adminCtx, bool) {
	c, ok := mw.ClaimsFrom(r.Context())
	if !ok {
		return adminCtx{}, false
	}
	a := adminCtx{orgID: c.OrgID}
	if id, err := strconv.ParseInt(c.Subject, 10, 64); err == nil && id != 0 {
		a.userID = &id
	}
	return a, true
}

type custCtx struct {
	orgID          int64
	customerID     int64
	customerUserID *int64
}

func customer(r *http.Request) (custCtx, bool) {
	c, ok := mw.ClaimsFrom(r.Context())
	if !ok || c.CustomerID == 0 {
		return custCtx{}, false
	}
	cc := custCtx{orgID: c.OrgID, customerID: c.CustomerID}
	if id, err := strconv.ParseInt(c.Subject, 10, 64); err == nil && id != 0 {
		cc.customerUserID = &id
	}
	return cc, true
}

func pathID(r *http.Request) (int64, error) {
	return strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
}

// tx runs fn inside a transaction with a tx-bound Queries; commit on success.
func (h *Handler) tx(ctx context.Context, fn func(*gen.Queries) error) error {
	t, err := h.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer t.Rollback(ctx) //nolint:errcheck // no-op after commit
	if err := fn(gen.New(t)); err != nil {
		return err
	}
	return t.Commit(ctx)
}

// ---- state machines (§6) --------------------------------------------------

var orderTransitions = map[string][]string{
	"pending":    {"confirmed", "on_hold", "cancelled"},
	"confirmed":  {"processing", "on_hold", "cancelled"},
	"processing": {"shipped", "on_hold", "cancelled"},
	"shipped":    {"delivered"},
	"delivered":  {"closed"},
	"on_hold":    {"confirmed", "cancelled"},
}

func canTransition(m map[string][]string, from, to string) bool {
	for _, t := range m[from] {
		if t == to {
			return true
		}
	}
	return false
}

func unauthorized(w http.ResponseWriter) {
	response.Fail(w, http.StatusUnauthorized, "unauthorized", "no valid context")
}

func notFound(w http.ResponseWriter, what string) {
	response.Fail(w, http.StatusNotFound, "not_found", what+" not found")
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }
