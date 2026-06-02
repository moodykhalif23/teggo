package catalog

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	mw "b2bcommerce/internal/server/middleware"
	"b2bcommerce/internal/server/response"
	"b2bcommerce/internal/store"
)

type Handler struct {
	store *store.Store
}

func New(s *store.Store) *Handler { return &Handler{store: s} }

// Routes registers both a public storefront route and a permission-gated admin
// route, demonstrating the two security contexts.
func (h *Handler) Routes(r chi.Router, authMW func(http.Handler) http.Handler) {
	// Public storefront listing (no auth).
	r.Get("/storefront/products", h.list)

	// Admin listing requires auth + the product.view permission.
	r.Group(func(ar chi.Router) {
		ar.Use(authMW)
		ar.With(mw.RequirePermission("product.view")).Get("/admin/products", h.list)
	})
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	limit := atoiDefault(r.URL.Query().Get("page_size"), 24)
	page := atoiDefault(r.URL.Query().Get("page"), 1)
	if page < 1 {
		page = 1
	}
	offset := (page - 1) * limit

	// Org scoping: storefront would resolve from the website/host; demo uses org 1.
	orgID := int64(1)

	items, err := h.store.ListActiveProducts(r.Context(), orgID, int32(limit), int32(offset))
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not list products")
		return
	}
	if items == nil {
		items = []store.Product{}
	}
	response.JSON(w, http.StatusOK, map[string]any{
		"items": items,
		"page":  page,
	})
}

func atoiDefault(s string, def int) int {
	if s == "" {
		return def
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return n
}
