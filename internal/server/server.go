package server

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"

	"b2bcommerce/internal/auth"
	authmod "b2bcommerce/internal/modules/auth"
	"b2bcommerce/internal/modules/catalog"
	"b2bcommerce/internal/modules/health"
	mw "b2bcommerce/internal/server/middleware"
	"b2bcommerce/internal/store"
)

// New builds the fully-wired HTTP handler.
func New(st *store.Store, issuer *auth.Issuer) http.Handler {
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
	catalog.New(st).Routes(r, authMW)

	return r
}
