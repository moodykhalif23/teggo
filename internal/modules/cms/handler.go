// Package cms implements the content management system (Pack 2 §2): block-based
// content pages, navigation menus, media assets, and redirects, served to the
// Nuxt storefront. Page block trees are JSONB so new block types are additive
// (validated app-side against a registry). Admin editing is gated by
// cms.view / cms.manage; the storefront read endpoints are public (SEO), with
// optional customer targeting when a storefront token is presented.
package cms

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"b2bcommerce/internal/ai"
	"b2bcommerce/internal/auth"
	mw "b2bcommerce/internal/server/middleware"
	"b2bcommerce/internal/server/response"
	"b2bcommerce/internal/store/gen"
)

// storefrontOrg is the fallback org when the request host matches no configured
// website domain (see resolveWebsite in public.go — mirrors catalog.resolveOrg).
const storefrontOrg int64 = 1

type Handler struct {
	q        *gen.Queries
	issuer   *auth.Issuer
	designer ai.PageDesigner
}

func New(pool *pgxpool.Pool, issuer *auth.Issuer, designer ai.PageDesigner) *Handler {
	return &Handler{q: gen.New(pool), issuer: issuer, designer: designer}
}

func (h *Handler) Routes(r chi.Router, authMW func(http.Handler) http.Handler) {
	// Admin surface.
	r.Group(func(ar chi.Router) {
		ar.Use(authMW)
		ar.Use(mw.RequireAudience("admin"))

		ar.With(mw.RequirePermission("cms.view")).Get("/admin/pages", h.listPages)
		ar.With(mw.RequirePermission("cms.manage")).Post("/admin/pages", h.createPage)
		ar.With(mw.RequirePermission("cms.manage")).Post("/admin/pages/ai-generate", h.generatePage)
		ar.With(mw.RequirePermission("cms.view")).Get("/admin/pages/{id}", h.getPage)
		ar.With(mw.RequirePermission("cms.manage")).Put("/admin/pages/{id}", h.updatePage)
		ar.With(mw.RequirePermission("cms.manage")).Post("/admin/pages/{id}/publish", h.publishPage)
		ar.With(mw.RequirePermission("cms.manage")).Post("/admin/pages/{id}/archive", h.archivePage)

		ar.With(mw.RequirePermission("cms.manage")).Post("/admin/menus", h.createMenu)
		ar.With(mw.RequirePermission("cms.manage")).Post("/admin/menus/{id}/items", h.addMenuItem)
	})

	// Public storefront surface (no auth; targeting reads an optional token).
	r.Get("/storefront/pages/{slug}", h.getStorefrontPage)
	r.Get("/storefront/menus/{code}", h.getStorefrontMenu)
	r.Get("/storefront/redirects/resolve", h.resolveRedirect)
}

func orgID(r *http.Request) (int64, bool) {
	c, ok := mw.ClaimsFrom(r.Context())
	if !ok {
		return 0, false
	}
	return c.OrgID, true
}

func pathID(r *http.Request) (int64, error) {
	return strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
}

// raw renders JSONB bytes as raw JSON (avoid base64), defaulting to def.
func raw(b []byte, def string) json.RawMessage {
	if len(b) == 0 {
		return json.RawMessage(def)
	}
	return json.RawMessage(b)
}

func pageJSON(p gen.ContentPage) map[string]any {
	var published any
	if p.PublishedAt.Valid {
		published = p.PublishedAt.Time
	}
	return map[string]any{
		"id": p.ID, "public_id": p.PublicID.String(), "website_id": p.WebsiteID,
		"locale": p.Locale, "slug": p.Slug, "title": p.Title, "status": p.Status,
		"blocks": raw(p.Blocks, "[]"), "seo": raw(p.Seo, "{}"),
		"target_customer_group_id": p.TargetCustomerGroupID, "published_at": published,
	}
}

// ---- admin: pages ---------------------------------------------------------

func (h *Handler) listPages(w http.ResponseWriter, r *http.Request) {
	org, ok := orgID(r)
	if !ok {
		response.Fail(w, http.StatusUnauthorized, "unauthorized", "no claims")
		return
	}
	rows, err := h.q.ListPagesAdmin(r.Context(), org)
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not list pages")
		return
	}
	items := make([]map[string]any, 0, len(rows))
	for _, p := range rows {
		items = append(items, pageJSON(p))
	}
	response.JSON(w, http.StatusOK, map[string]any{"items": items})
}

type pageInput struct {
	Title                 string          `json:"title"`
	Slug                  string          `json:"slug"`
	Locale                string          `json:"locale"`
	Blocks                json.RawMessage `json:"blocks"`
	Seo                   json.RawMessage `json:"seo"`
	TargetCustomerGroupID *int64          `json:"target_customer_group_id"`
}

func (in *pageInput) normalize() error {
	if in.Title == "" || in.Slug == "" {
		return errBad("title and slug are required")
	}
	if in.Locale == "" {
		in.Locale = "en"
	}
	if len(in.Blocks) == 0 {
		in.Blocks = json.RawMessage("[]")
	}
	if len(in.Seo) == 0 {
		in.Seo = json.RawMessage("{}")
	}
	return validateBlocks(in.Blocks)
}

func (h *Handler) createPage(w http.ResponseWriter, r *http.Request) {
	org, ok := orgID(r)
	if !ok {
		response.Fail(w, http.StatusUnauthorized, "unauthorized", "no claims")
		return
	}
	ws, err := h.q.GetDefaultWebsite(r.Context(), org)
	if err != nil {
		response.Fail(w, http.StatusBadRequest, "bad_request", "no website configured")
		return
	}
	var in pageInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		response.Fail(w, http.StatusBadRequest, "bad_request", "invalid body")
		return
	}
	if err := in.normalize(); err != nil {
		response.Fail(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	p, err := h.q.CreatePage(r.Context(), gen.CreatePageParams{
		WebsiteID: ws.ID, Locale: in.Locale, Slug: in.Slug, Title: in.Title,
		Blocks: in.Blocks, Seo: in.Seo, TargetCustomerGroupID: in.TargetCustomerGroupID,
	})
	if err != nil {
		response.Fail(w, http.StatusConflict, "conflict", "could not create page (slug may already exist)")
		return
	}
	response.JSON(w, http.StatusCreated, pageJSON(p))
}

// ---- admin: AI page generation -------------------------------------------

type generateInput struct {
	Prompt string `json:"prompt"`
}

// generatePage turns a natural-language brief into a block tree (the same shape
// the builder edits and the storefront renders). The designer may be the
// offline template engine or Claude; either way the output is sanitized and
// re-validated here before it is returned — the model is never trusted.
func (h *Handler) generatePage(w http.ResponseWriter, r *http.Request) {
	org, ok := orgID(r)
	if !ok {
		response.Fail(w, http.StatusUnauthorized, "unauthorized", "no claims")
		return
	}
	var in generateInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		response.Fail(w, http.StatusBadRequest, "bad_request", "invalid body")
		return
	}
	if strings.TrimSpace(in.Prompt) == "" {
		response.Fail(w, http.StatusBadRequest, "bad_request", "prompt is required")
		return
	}

	// Org-scoped categories let product-grid blocks reference real categories and
	// give us the set to clamp the generator's output against.
	cats, _ := h.q.ListCategories(r.Context(), org)
	dcats := make([]ai.DesignCategory, 0, len(cats))
	valid := make(map[int64]bool, len(cats))
	var firstCat int64
	for i, c := range cats {
		dcats = append(dcats, ai.DesignCategory{ID: c.ID, Name: c.Name})
		valid[c.ID] = true
		if i == 0 {
			firstCat = c.ID
		}
	}

	blocks, notes, err := h.designer.Design(r.Context(), ai.DesignRequest{Prompt: in.Prompt, Categories: dcats})
	if err != nil {
		response.Fail(w, http.StatusBadGateway, "ai_unavailable", "could not generate a page right now")
		return
	}
	sanitizeGeneratedBlocks(blocks, valid, firstCat)

	rawBlocks, err := json.Marshal(blocks)
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not encode blocks")
		return
	}
	if err := validateBlocks(rawBlocks); err != nil {
		response.Fail(w, http.StatusUnprocessableEntity, "bad_generation", "the generator produced invalid blocks: "+err.Error())
		return
	}
	response.JSON(w, http.StatusOK, map[string]any{"blocks": json.RawMessage(rawBlocks), "notes": notes})
}

// sanitizeGeneratedBlocks repairs generator output before it is trusted: it
// guarantees each block has an id and clamps every product-grid source to a real
// category (dropping the source if no category exists).
func sanitizeGeneratedBlocks(blocks []ai.Block, valid map[int64]bool, fallback int64) {
	for i, b := range blocks {
		if id, _ := b["id"].(string); strings.TrimSpace(id) == "" {
			b["id"] = "g" + strconv.Itoa(i+1)
		}
		if b["type"] != "product-grid" {
			continue
		}
		props, _ := b["props"].(map[string]any)
		if props == nil {
			continue
		}
		src, _ := props["source"].(map[string]any)
		if src == nil {
			continue
		}
		src["kind"] = "category"
		id, ok := asCatID(src["category_id"])
		if !ok || !valid[id] {
			if fallback != 0 {
				src["category_id"] = fallback
			} else {
				delete(props, "source") // no categories exist — leave an empty grid
			}
		}
	}
}

func asCatID(v any) (int64, bool) {
	switch n := v.(type) {
	case float64:
		return int64(n), true
	case int64:
		return n, true
	case int:
		return int64(n), true
	}
	return 0, false
}

func (h *Handler) getPage(w http.ResponseWriter, r *http.Request) {
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
	p, err := h.q.GetPageAdmin(r.Context(), gen.GetPageAdminParams{ID: id, OrganizationID: org})
	if err != nil {
		response.Fail(w, http.StatusNotFound, "not_found", "page not found")
		return
	}
	response.JSON(w, http.StatusOK, pageJSON(p))
}

func (h *Handler) updatePage(w http.ResponseWriter, r *http.Request) {
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
	existing, err := h.q.GetPageAdmin(r.Context(), gen.GetPageAdminParams{ID: id, OrganizationID: org})
	if err != nil {
		response.Fail(w, http.StatusNotFound, "not_found", "page not found")
		return
	}
	var in pageInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		response.Fail(w, http.StatusBadRequest, "bad_request", "invalid body")
		return
	}
	if err := in.normalize(); err != nil {
		response.Fail(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	p, err := h.q.UpdatePage(r.Context(), gen.UpdatePageParams{
		ID: id, WebsiteID: existing.WebsiteID, Title: in.Title, Slug: in.Slug, Locale: in.Locale,
		Blocks: in.Blocks, Seo: in.Seo, TargetCustomerGroupID: in.TargetCustomerGroupID,
	})
	if err != nil {
		response.Fail(w, http.StatusConflict, "conflict", "could not update page (slug may clash)")
		return
	}
	response.JSON(w, http.StatusOK, pageJSON(p))
}

func (h *Handler) publishPage(w http.ResponseWriter, r *http.Request) { h.setStatus(w, r, "published") }
func (h *Handler) archivePage(w http.ResponseWriter, r *http.Request) { h.setStatus(w, r, "archived") }

func (h *Handler) setStatus(w http.ResponseWriter, r *http.Request, status string) {
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
	// Ownership check before mutating.
	if _, err := h.q.GetPageAdmin(r.Context(), gen.GetPageAdminParams{ID: id, OrganizationID: org}); err != nil {
		response.Fail(w, http.StatusNotFound, "not_found", "page not found")
		return
	}
	p, err := h.q.SetPageStatus(r.Context(), gen.SetPageStatusParams{ID: id, Status: status})
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not update status")
		return
	}
	response.JSON(w, http.StatusOK, pageJSON(p))
}

// ---- admin: menus ---------------------------------------------------------
// (Media management moved to the DAM module — internal/modules/dam.)

func (h *Handler) createMenu(w http.ResponseWriter, r *http.Request) {
	org, ok := orgID(r)
	if !ok {
		response.Fail(w, http.StatusUnauthorized, "unauthorized", "no claims")
		return
	}
	ws, err := h.q.GetDefaultWebsite(r.Context(), org)
	if err != nil {
		response.Fail(w, http.StatusBadRequest, "bad_request", "no website configured")
		return
	}
	var req struct {
		Code string `json:"code"`
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Code == "" || req.Name == "" {
		response.Fail(w, http.StatusBadRequest, "bad_request", "code and name are required")
		return
	}
	m, err := h.q.CreateMenu(r.Context(), gen.CreateMenuParams{WebsiteID: ws.ID, Code: req.Code, Name: req.Name})
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not create menu")
		return
	}
	response.JSON(w, http.StatusCreated, m)
}

func (h *Handler) addMenuItem(w http.ResponseWriter, r *http.Request) {
	if _, ok := orgID(r); !ok {
		response.Fail(w, http.StatusUnauthorized, "unauthorized", "no claims")
		return
	}
	menuID, err := pathID(r)
	if err != nil {
		response.Fail(w, http.StatusBadRequest, "bad_request", "invalid id")
		return
	}
	var req struct {
		Label     string  `json:"label"`
		URL       *string `json:"url"`
		SortOrder int32   `json:"sort_order"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Label == "" {
		response.Fail(w, http.StatusBadRequest, "bad_request", "label is required")
		return
	}
	it, err := h.q.AddMenuItem(r.Context(), gen.AddMenuItemParams{
		MenuID: menuID, Label: req.Label, Url: req.URL, SortOrder: req.SortOrder,
	})
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not add menu item")
		return
	}
	response.JSON(w, http.StatusCreated, it)
}
