package catalog

// Product content translations (i18n, Roadmap Tier 3 #8). Admin manages per-locale
// name/description; storefront reads resolve a translation by ?locale and fall
// back to the base product. Gated by product.view / product.manage.

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"b2bcommerce/internal/server/response"
	"b2bcommerce/internal/store/gen"
)

// localeOf returns the requested display locale (?locale=…), trimmed. Empty means
// the base product content.
func localeOf(r *http.Request) string {
	return strings.TrimSpace(r.URL.Query().Get("locale"))
}

type translationDTO struct {
	Locale      string  `json:"locale"`
	Name        string  `json:"name"`
	Description *string `json:"description,omitempty"`
}

func (h *Handler) listTranslations(w http.ResponseWriter, r *http.Request) {
	org, ok := orgID(r)
	if !ok {
		response.Fail(w, http.StatusUnauthorized, "unauthorized", "no claims")
		return
	}
	id, err := pathID(r)
	if err != nil {
		response.Fail(w, http.StatusBadRequest, "bad_request", "invalid id")
		return
	}
	if _, err := h.q.GetProductByID(r.Context(), gen.GetProductByIDParams{OrganizationID: org, ID: id}); err != nil {
		response.Fail(w, http.StatusNotFound, "not_found", "product not found")
		return
	}
	rows, err := h.q.ListProductTranslations(r.Context(), id)
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not list translations")
		return
	}
	items := make([]translationDTO, 0, len(rows))
	for _, t := range rows {
		items = append(items, translationDTO{Locale: t.Locale, Name: t.Name, Description: t.Description})
	}
	response.JSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *Handler) upsertTranslation(w http.ResponseWriter, r *http.Request) {
	org, ok := orgID(r)
	if !ok {
		response.Fail(w, http.StatusUnauthorized, "unauthorized", "no claims")
		return
	}
	id, err := pathID(r)
	if err != nil {
		response.Fail(w, http.StatusBadRequest, "bad_request", "invalid id")
		return
	}
	if _, err := h.q.GetProductByID(r.Context(), gen.GetProductByIDParams{OrganizationID: org, ID: id}); err != nil {
		response.Fail(w, http.StatusNotFound, "not_found", "product not found")
		return
	}
	var req struct {
		Locale      string  `json:"locale"`
		Name        string  `json:"name"`
		Description *string `json:"description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Fail(w, http.StatusBadRequest, "bad_request", "invalid body")
		return
	}
	req.Locale = strings.TrimSpace(req.Locale)
	if req.Locale == "" || strings.TrimSpace(req.Name) == "" {
		response.Fail(w, http.StatusBadRequest, "bad_request", "locale and name are required")
		return
	}
	t, err := h.q.UpsertProductTranslation(r.Context(), gen.UpsertProductTranslationParams{
		ProductID: id, Locale: req.Locale, Name: strings.TrimSpace(req.Name), Description: req.Description,
	})
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not save translation")
		return
	}
	response.JSON(w, http.StatusOK, translationDTO{Locale: t.Locale, Name: t.Name, Description: t.Description})
}

func (h *Handler) deleteTranslation(w http.ResponseWriter, r *http.Request) {
	org, ok := orgID(r)
	if !ok {
		response.Fail(w, http.StatusUnauthorized, "unauthorized", "no claims")
		return
	}
	id, err := pathID(r)
	if err != nil {
		response.Fail(w, http.StatusBadRequest, "bad_request", "invalid id")
		return
	}
	if _, err := h.q.GetProductByID(r.Context(), gen.GetProductByIDParams{OrganizationID: org, ID: id}); err != nil {
		response.Fail(w, http.StatusNotFound, "not_found", "product not found")
		return
	}
	locale := strings.TrimSpace(chi.URLParam(r, "locale"))
	n, err := h.q.DeleteProductTranslation(r.Context(), gen.DeleteProductTranslationParams{ProductID: id, Lower: locale})
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not delete translation")
		return
	}
	if n == 0 {
		response.Fail(w, http.StatusNotFound, "not_found", "translation not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// storefrontLocales lists the locales available for content (the website default
// plus any configured product-translation locales) for the storefront selector.
func (h *Handler) storefrontLocales(w http.ResponseWriter, r *http.Request) {
	orgID := h.resolveOrg(r)
	def := "en"
	host := r.Host
	if i := strings.IndexByte(host, ':'); i >= 0 {
		host = host[:i]
	}
	if ws, err := h.q.GetWebsiteByDomain(r.Context(), host); err == nil && ws.DefaultLocale != "" {
		def = ws.DefaultLocale
	}
	locales, _ := h.q.DistinctTranslationLocales(r.Context(), orgID)
	if locales == nil {
		locales = []string{}
	}
	response.JSON(w, http.StatusOK, map[string]any{"default": def, "locales": locales})
}
