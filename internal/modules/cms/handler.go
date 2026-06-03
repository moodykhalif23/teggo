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

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"b2bcommerce/internal/auth"
	mw "b2bcommerce/internal/server/middleware"
	"b2bcommerce/internal/server/response"
	"b2bcommerce/internal/store/gen"
)

// storefrontOrg is the demo org/website resolution (mirrors the catalog
// storefront, which also serves org 1; real multi-website resolves from host).
const storefrontOrg int64 = 1

type Handler struct {
	q      *gen.Queries
	issuer *auth.Issuer
}

func New(pool *pgxpool.Pool, issuer *auth.Issuer) *Handler {
	return &Handler{q: gen.New(pool), issuer: issuer}
}

func (h *Handler) Routes(r chi.Router, authMW func(http.Handler) http.Handler) {
	// Admin surface.
	r.Group(func(ar chi.Router) {
		ar.Use(authMW)
		ar.Use(mw.RequireAudience("admin"))

		ar.With(mw.RequirePermission("cms.view")).Get("/admin/pages", h.listPages)
		ar.With(mw.RequirePermission("cms.manage")).Post("/admin/pages", h.createPage)
		ar.With(mw.RequirePermission("cms.view")).Get("/admin/pages/{id}", h.getPage)
		ar.With(mw.RequirePermission("cms.manage")).Put("/admin/pages/{id}", h.updatePage)
		ar.With(mw.RequirePermission("cms.manage")).Post("/admin/pages/{id}/publish", h.publishPage)
		ar.With(mw.RequirePermission("cms.manage")).Post("/admin/pages/{id}/archive", h.archivePage)

		ar.With(mw.RequirePermission("cms.view")).Get("/admin/media", h.listMedia)
		ar.With(mw.RequirePermission("cms.manage")).Post("/admin/media", h.createMedia)
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

// ---- admin: media + menus -------------------------------------------------

func (h *Handler) listMedia(w http.ResponseWriter, r *http.Request) {
	org, ok := orgID(r)
	if !ok {
		response.Fail(w, http.StatusUnauthorized, "unauthorized", "no claims")
		return
	}
	rows, err := h.q.ListMediaAssets(r.Context(), gen.ListMediaAssetsParams{OrganizationID: org, Limit: 200, Offset: 0})
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not list media")
		return
	}
	if rows == nil {
		rows = []gen.MediaAsset{}
	}
	response.JSON(w, http.StatusOK, map[string]any{"items": rows})
}

func (h *Handler) createMedia(w http.ResponseWriter, r *http.Request) {
	org, ok := orgID(r)
	if !ok {
		response.Fail(w, http.StatusUnauthorized, "unauthorized", "no claims")
		return
	}
	var req struct {
		URL      string  `json:"url"`
		MimeType *string `json:"mime_type"`
		Width    *int32  `json:"width"`
		Height   *int32  `json:"height"`
		Alt      *string `json:"alt"`
		Folder   *string `json:"folder"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.URL == "" {
		response.Fail(w, http.StatusBadRequest, "bad_request", "url is required")
		return
	}
	a, err := h.q.CreateMediaAsset(r.Context(), gen.CreateMediaAssetParams{
		OrganizationID: org, Url: req.URL, MimeType: req.MimeType,
		Width: req.Width, Height: req.Height, Alt: req.Alt, Folder: req.Folder,
	})
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not save media")
		return
	}
	response.JSON(w, http.StatusCreated, a)
}

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
