package health

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"b2bcommerce/internal/server/response"
	"b2bcommerce/internal/store"
)

type Handler struct {
	store *store.Store
}

func New(s *store.Store) *Handler { return &Handler{store: s} }

func (h *Handler) Routes(r chi.Router) {
	r.Get("/healthz", h.live)
	r.Get("/readyz", h.ready)
}

func (h *Handler) live(w http.ResponseWriter, r *http.Request) {
	response.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) ready(w http.ResponseWriter, r *http.Request) {
	if err := h.store.Ping(r.Context()); err != nil {
		response.Fail(w, http.StatusServiceUnavailable, "not_ready", "database unreachable")
		return
	}
	response.JSON(w, http.StatusOK, map[string]string{"status": "ready"})
}
