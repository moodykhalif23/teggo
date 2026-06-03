// Package catalog implements the Catalog & PIM module (Implementation Pack 1 §3):
// products, categories (queried via the subtree CTE), attributes/families, and
// the JSONB faceted filter. It serves two security contexts off one handler —
// public /storefront/* reads and bearer+permission gated /admin/* management.
package catalog

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	mw "b2bcommerce/internal/server/middleware"
	"b2bcommerce/internal/server/response"
	"b2bcommerce/internal/store/gen"
)

type Handler struct {
	q *gen.Queries
}

func New(q *gen.Queries) *Handler { return &Handler{q: q} }

func (h *Handler) Routes(r chi.Router, authMW func(http.Handler) http.Handler) {
	// Public storefront reads.
	r.Get("/storefront/products", h.storefrontList)
	r.Get("/storefront/products/{slug}", h.storefrontGet)

	// Admin (bearer + permission gated).
	r.Group(func(ar chi.Router) {
		ar.Use(authMW)

		ar.With(mw.RequirePermission("product.view")).Get("/admin/products", h.adminList)
		ar.With(mw.RequirePermission("product.manage")).Post("/admin/products", h.adminCreate)
		ar.With(mw.RequirePermission("product.view")).Get("/admin/products/{id}", h.adminGet)
		ar.With(mw.RequirePermission("product.manage")).Put("/admin/products/{id}", h.adminUpdate)
		ar.With(mw.RequirePermission("product.manage")).Delete("/admin/products/{id}", h.adminDelete)
		ar.With(mw.RequirePermission("product.view")).Get("/admin/products/{id}/categories", h.listProductCategories)
		ar.With(mw.RequirePermission("product.manage")).Post("/admin/products/{id}/categories", h.assignProductCategory)

		ar.With(mw.RequirePermission("category.view")).Get("/admin/categories", h.listCategories)
		ar.With(mw.RequirePermission("category.manage")).Post("/admin/categories", h.createCategory)

		ar.With(mw.RequirePermission("attribute.view")).Get("/admin/attributes", h.listAttributes)
		ar.With(mw.RequirePermission("attribute.manage")).Post("/admin/attributes", h.createAttribute)
		ar.With(mw.RequirePermission("attribute.view")).Get("/admin/attribute-families", h.listFamilies)
		ar.With(mw.RequirePermission("attribute.manage")).Post("/admin/attribute-families", h.createFamily)
	})
}

func orgID(r *http.Request) (int64, bool) {
	claims, ok := mw.ClaimsFrom(r.Context())
	if !ok {
		return 0, false
	}
	return claims.OrgID, true
}

func pathID(r *http.Request) (int64, error) {
	return strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
}

// ---- DTOs ----------------------------------------------------------------

// storefrontProduct is the customer-facing projection (no internal id/org).
type storefrontProduct struct {
	PublicID    string          `json:"public_id"`
	SKU         string          `json:"sku"`
	Name        string          `json:"name"`
	Slug        string          `json:"slug"`
	Description *string         `json:"description,omitempty"`
	Status      string          `json:"status"`
	Attributes  json.RawMessage `json:"attributes"`
	Unit        string          `json:"unit"`
}

// adminProduct is the full back-office projection.
type adminProduct struct {
	ID                int64           `json:"id"`
	PublicID          string          `json:"public_id"`
	SKU               string          `json:"sku"`
	Type              string          `json:"type"`
	Name              string          `json:"name"`
	Slug              string          `json:"slug"`
	Description       *string         `json:"description,omitempty"`
	Status            string          `json:"status"`
	Attributes        json.RawMessage `json:"attributes"`
	Unit              string          `json:"unit"`
	ParentID          *int64          `json:"parent_id"`
	AttributeFamilyID *int64          `json:"attribute_family_id"`
}

func rawJSON(b []byte) json.RawMessage {
	if len(b) == 0 {
		return json.RawMessage("{}")
	}
	return json.RawMessage(b)
}

func toAdminProduct(p gen.Product) adminProduct {
	return adminProduct{
		ID: p.ID, PublicID: p.PublicID.String(), SKU: p.Sku, Type: p.Type,
		Name: p.Name, Slug: p.Slug, Description: p.Description, Status: p.Status,
		Attributes: rawJSON(p.Attributes), Unit: p.Unit,
		ParentID: p.ParentID, AttributeFamilyID: p.AttributeFamilyID,
	}
}

// ---- Storefront ----------------------------------------------------------

func (h *Handler) storefrontList(w http.ResponseWriter, r *http.Request) {
	// Org is resolved from the website/host in production; demo uses org 1.
	orgID := int64(1)
	limit := atoiDefault(r.URL.Query().Get("page_size"), 24)
	page := atoiDefault(r.URL.Query().Get("page"), 1)
	if page < 1 {
		page = 1
	}
	offset := (page - 1) * limit

	var items []storefrontProduct

	switch {
	case r.URL.Query().Get("q") != "":
		// Full-text keyword search, ranked by relevance (PRD §14).
		rows, err := h.q.SearchActiveProducts(r.Context(), gen.SearchActiveProductsParams{
			OrganizationID: orgID, WebsearchToTsquery: r.URL.Query().Get("q"),
			Limit: int32(limit), Offset: int32(offset),
		})
		if err != nil {
			response.Fail(w, http.StatusInternalServerError, "internal", "could not search products")
			return
		}
		for _, p := range rows {
			items = append(items, storefrontProduct{
				PublicID: p.PublicID.String(), SKU: p.Sku, Name: p.Name, Slug: p.Slug,
				Description: p.Description, Status: p.Status, Attributes: rawJSON(p.Attributes), Unit: p.Unit,
			})
		}

	case r.URL.Query().Get("category") != "":
		cat, err := h.q.GetCategoryBySlug(r.Context(), gen.GetCategoryBySlugParams{
			OrganizationID: orgID, Slug: r.URL.Query().Get("category"),
		})
		if err != nil {
			// Unknown category resolves to an empty result, not an error.
			response.JSON(w, http.StatusOK, map[string]any{"items": []storefrontProduct{}, "page": page})
			return
		}
		rows, err := h.q.ListActiveProductsInCategory(r.Context(), gen.ListActiveProductsInCategoryParams{
			OrganizationID: orgID, ID: cat.ID, Limit: int32(limit), Offset: int32(offset),
		})
		if err != nil {
			response.Fail(w, http.StatusInternalServerError, "internal", "could not list products")
			return
		}
		for _, p := range rows {
			items = append(items, storefrontProduct{
				PublicID: p.PublicID.String(), SKU: p.Sku, Name: p.Name, Slug: p.Slug,
				Description: p.Description, Status: p.Status, Attributes: rawJSON(p.Attributes), Unit: p.Unit,
			})
		}

	case r.URL.Query().Get("filter") != "":
		// ?filter is a JSON object of attribute equalities (§12.5).
		filter := r.URL.Query().Get("filter")
		if !json.Valid([]byte(filter)) {
			response.Fail(w, http.StatusBadRequest, "bad_request", "filter must be a JSON object")
			return
		}
		rows, err := h.q.FilterActiveProductsByAttributes(r.Context(), gen.FilterActiveProductsByAttributesParams{
			OrganizationID: orgID, Attributes: []byte(filter), Limit: int32(limit), Offset: int32(offset),
		})
		if err != nil {
			response.Fail(w, http.StatusInternalServerError, "internal", "could not filter products")
			return
		}
		for _, p := range rows {
			items = append(items, storefrontProduct{
				PublicID: p.PublicID.String(), SKU: p.Sku, Name: p.Name, Slug: p.Slug,
				Description: p.Description, Status: p.Status, Attributes: rawJSON(p.Attributes), Unit: p.Unit,
			})
		}

	default:
		rows, err := h.q.ListActiveProducts(r.Context(), gen.ListActiveProductsParams{
			OrganizationID: orgID, Limit: int32(limit), Offset: int32(offset),
		})
		if err != nil {
			response.Fail(w, http.StatusInternalServerError, "internal", "could not list products")
			return
		}
		for _, p := range rows {
			items = append(items, storefrontProduct{
				PublicID: p.PublicID.String(), SKU: p.Sku, Name: p.Name, Slug: p.Slug,
				Description: p.Description, Status: p.Status, Attributes: rawJSON(p.Attributes), Unit: p.Unit,
			})
		}
	}

	if items == nil {
		items = []storefrontProduct{}
	}
	response.JSON(w, http.StatusOK, map[string]any{"items": items, "page": page})
}

func (h *Handler) storefrontGet(w http.ResponseWriter, r *http.Request) {
	orgID := int64(1)
	p, err := h.q.GetProductBySlug(r.Context(), gen.GetProductBySlugParams{
		OrganizationID: orgID, Slug: chi.URLParam(r, "slug"),
	})
	if err != nil {
		response.Fail(w, http.StatusNotFound, "not_found", "product not found")
		return
	}
	response.JSON(w, http.StatusOK, storefrontProduct{
		PublicID: p.PublicID.String(), SKU: p.Sku, Name: p.Name, Slug: p.Slug,
		Description: p.Description, Status: p.Status, Attributes: rawJSON(p.Attributes), Unit: p.Unit,
	})
}

// ---- Admin products ------------------------------------------------------

type productRequest struct {
	SKU               string          `json:"sku"`
	Type              string          `json:"type"`
	Name              string          `json:"name"`
	Slug              string          `json:"slug"`
	Description       *string         `json:"description"`
	Status            string          `json:"status"`
	Attributes        json.RawMessage `json:"attributes"`
	Unit              string          `json:"unit"`
	ParentID          *int64          `json:"parent_id"`
	AttributeFamilyID *int64          `json:"attribute_family_id"`
}

func (pr *productRequest) defaults() {
	if pr.Type == "" {
		pr.Type = "simple"
	}
	if pr.Status == "" {
		pr.Status = "draft"
	}
	if pr.Unit == "" {
		pr.Unit = "each"
	}
	if len(pr.Attributes) == 0 {
		pr.Attributes = json.RawMessage("{}")
	}
}

func (h *Handler) adminList(w http.ResponseWriter, r *http.Request) {
	org, ok := orgID(r)
	if !ok {
		response.Fail(w, http.StatusUnauthorized, "unauthorized", "no claims")
		return
	}
	limit := atoiDefault(r.URL.Query().Get("page_size"), 24)
	page := atoiDefault(r.URL.Query().Get("page"), 1)
	if page < 1 {
		page = 1
	}
	offset := int32((page - 1) * limit)
	q := r.URL.Query().Get("q")

	var rows []gen.Product
	var total int64
	if q != "" {
		// Full-text search (PRD §14), ranked by relevance.
		var err error
		rows, err = h.q.SearchProductsAdmin(r.Context(), gen.SearchProductsAdminParams{
			OrganizationID: org, WebsearchToTsquery: q, Limit: int32(limit), Offset: offset,
		})
		if err != nil {
			response.Fail(w, http.StatusInternalServerError, "internal", "could not search products")
			return
		}
		if total, err = h.q.CountSearchProductsAdmin(r.Context(), gen.CountSearchProductsAdminParams{
			OrganizationID: org, WebsearchToTsquery: q,
		}); err != nil {
			response.Fail(w, http.StatusInternalServerError, "internal", "could not count products")
			return
		}
	} else {
		var err error
		rows, err = h.q.ListProductsAdmin(r.Context(), gen.ListProductsAdminParams{
			OrganizationID: org, Limit: int32(limit), Offset: offset,
		})
		if err != nil {
			response.Fail(w, http.StatusInternalServerError, "internal", "could not list products")
			return
		}
		if total, err = h.q.CountProductsAdmin(r.Context(), org); err != nil {
			response.Fail(w, http.StatusInternalServerError, "internal", "could not count products")
			return
		}
	}
	items := make([]adminProduct, 0, len(rows))
	for _, p := range rows {
		items = append(items, toAdminProduct(p))
	}
	response.JSON(w, http.StatusOK, map[string]any{"items": items, "page": page, "total": total})
}

func (h *Handler) adminCreate(w http.ResponseWriter, r *http.Request) {
	org, ok := orgID(r)
	if !ok {
		response.Fail(w, http.StatusUnauthorized, "unauthorized", "no claims")
		return
	}
	var req productRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Fail(w, http.StatusBadRequest, "bad_request", "invalid body")
		return
	}
	if req.SKU == "" || req.Name == "" || req.Slug == "" {
		response.Fail(w, http.StatusBadRequest, "bad_request", "sku, name, slug are required")
		return
	}
	req.defaults()
	p, err := h.q.CreateProduct(r.Context(), gen.CreateProductParams{
		OrganizationID: org, Sku: req.SKU, Type: req.Type, Name: req.Name, Slug: req.Slug,
		Description: req.Description, Status: req.Status, Attributes: req.Attributes,
		Unit: req.Unit, ParentID: req.ParentID, AttributeFamilyID: req.AttributeFamilyID,
	})
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not create product")
		return
	}
	response.JSON(w, http.StatusCreated, toAdminProduct(p))
}

func (h *Handler) adminGet(w http.ResponseWriter, r *http.Request) {
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
	p, err := h.q.GetProductByID(r.Context(), gen.GetProductByIDParams{OrganizationID: org, ID: id})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			response.Fail(w, http.StatusNotFound, "not_found", "product not found")
			return
		}
		response.Fail(w, http.StatusInternalServerError, "internal", "could not load product")
		return
	}
	response.JSON(w, http.StatusOK, toAdminProduct(p))
}

func (h *Handler) adminUpdate(w http.ResponseWriter, r *http.Request) {
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
	var req productRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Fail(w, http.StatusBadRequest, "bad_request", "invalid body")
		return
	}
	if req.SKU == "" || req.Name == "" || req.Slug == "" {
		response.Fail(w, http.StatusBadRequest, "bad_request", "sku, name, slug are required")
		return
	}
	req.defaults()
	p, err := h.q.UpdateProduct(r.Context(), gen.UpdateProductParams{
		OrganizationID: org, ID: id, Sku: req.SKU, Type: req.Type, Name: req.Name, Slug: req.Slug,
		Description: req.Description, Status: req.Status, Attributes: req.Attributes,
		Unit: req.Unit, ParentID: req.ParentID, AttributeFamilyID: req.AttributeFamilyID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			response.Fail(w, http.StatusNotFound, "not_found", "product not found")
			return
		}
		response.Fail(w, http.StatusInternalServerError, "internal", "could not update product")
		return
	}
	response.JSON(w, http.StatusOK, toAdminProduct(p))
}

func (h *Handler) adminDelete(w http.ResponseWriter, r *http.Request) {
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
	n, err := h.q.SoftDeleteProduct(r.Context(), gen.SoftDeleteProductParams{OrganizationID: org, ID: id})
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not delete product")
		return
	}
	if n == 0 {
		response.Fail(w, http.StatusNotFound, "not_found", "product not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) listProductCategories(w http.ResponseWriter, r *http.Request) {
	if _, ok := orgID(r); !ok {
		response.Fail(w, http.StatusUnauthorized, "unauthorized", "no claims")
		return
	}
	id, err := pathID(r)
	if err != nil {
		response.Fail(w, http.StatusBadRequest, "bad_request", "invalid id")
		return
	}
	ids, err := h.q.ListProductCategoryIDs(r.Context(), id)
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not list categories")
		return
	}
	if ids == nil {
		ids = []int64{}
	}
	response.JSON(w, http.StatusOK, map[string]any{"category_ids": ids})
}

func (h *Handler) assignProductCategory(w http.ResponseWriter, r *http.Request) {
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
	var req struct {
		CategoryID int64 `json:"category_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.CategoryID == 0 {
		response.Fail(w, http.StatusBadRequest, "bad_request", "category_id is required")
		return
	}
	// Both product and category must belong to the caller's org.
	if _, err := h.q.GetProductByID(r.Context(), gen.GetProductByIDParams{OrganizationID: org, ID: id}); err != nil {
		response.Fail(w, http.StatusNotFound, "not_found", "product not found")
		return
	}
	if _, err := h.q.GetCategory(r.Context(), gen.GetCategoryParams{OrganizationID: org, ID: req.CategoryID}); err != nil {
		response.Fail(w, http.StatusBadRequest, "bad_request", "category not found in organization")
		return
	}
	if err := h.q.AssignProductToCategory(r.Context(), gen.AssignProductToCategoryParams{ProductID: id, CategoryID: req.CategoryID}); err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not assign category")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---- Categories ----------------------------------------------------------

func (h *Handler) listCategories(w http.ResponseWriter, r *http.Request) {
	org, ok := orgID(r)
	if !ok {
		response.Fail(w, http.StatusUnauthorized, "unauthorized", "no claims")
		return
	}
	rows, err := h.q.ListCategories(r.Context(), org)
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not list categories")
		return
	}
	if rows == nil {
		rows = []gen.Category{}
	}
	response.JSON(w, http.StatusOK, map[string]any{"items": rows})
}

func (h *Handler) createCategory(w http.ResponseWriter, r *http.Request) {
	org, ok := orgID(r)
	if !ok {
		response.Fail(w, http.StatusUnauthorized, "unauthorized", "no claims")
		return
	}
	var req struct {
		ParentID  *int64 `json:"parent_id"`
		Name      string `json:"name"`
		Slug      string `json:"slug"`
		SortOrder int32  `json:"sort_order"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" || req.Slug == "" {
		response.Fail(w, http.StatusBadRequest, "bad_request", "name and slug are required")
		return
	}
	c, err := h.q.CreateCategory(r.Context(), gen.CreateCategoryParams{
		OrganizationID: org, ParentID: req.ParentID, Name: req.Name, Slug: req.Slug, SortOrder: req.SortOrder,
	})
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not create category")
		return
	}
	response.JSON(w, http.StatusCreated, c)
}

// ---- Attributes & families ----------------------------------------------

func (h *Handler) listAttributes(w http.ResponseWriter, r *http.Request) {
	org, ok := orgID(r)
	if !ok {
		response.Fail(w, http.StatusUnauthorized, "unauthorized", "no claims")
		return
	}
	rows, err := h.q.ListAttributes(r.Context(), org)
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not list attributes")
		return
	}
	if rows == nil {
		rows = []gen.Attribute{}
	}
	response.JSON(w, http.StatusOK, map[string]any{"items": rows})
}

func (h *Handler) createAttribute(w http.ResponseWriter, r *http.Request) {
	org, ok := orgID(r)
	if !ok {
		response.Fail(w, http.StatusUnauthorized, "unauthorized", "no claims")
		return
	}
	var req struct {
		Code          string          `json:"code"`
		Label         string          `json:"label"`
		DataType      string          `json:"data_type"`
		Options       json.RawMessage `json:"options"`
		IsFilterable  bool            `json:"is_filterable"`
		IsVariantAxis bool            `json:"is_variant_axis"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Code == "" || req.Label == "" || req.DataType == "" {
		response.Fail(w, http.StatusBadRequest, "bad_request", "code, label, data_type are required")
		return
	}
	a, err := h.q.CreateAttribute(r.Context(), gen.CreateAttributeParams{
		OrganizationID: org, Code: req.Code, Label: req.Label, DataType: req.DataType,
		Options: req.Options, IsFilterable: req.IsFilterable, IsVariantAxis: req.IsVariantAxis,
	})
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not create attribute")
		return
	}
	response.JSON(w, http.StatusCreated, a)
}

func (h *Handler) listFamilies(w http.ResponseWriter, r *http.Request) {
	org, ok := orgID(r)
	if !ok {
		response.Fail(w, http.StatusUnauthorized, "unauthorized", "no claims")
		return
	}
	rows, err := h.q.ListAttributeFamilies(r.Context(), org)
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not list families")
		return
	}
	if rows == nil {
		rows = []gen.AttributeFamily{}
	}
	response.JSON(w, http.StatusOK, map[string]any{"items": rows})
}

func (h *Handler) createFamily(w http.ResponseWriter, r *http.Request) {
	org, ok := orgID(r)
	if !ok {
		response.Fail(w, http.StatusUnauthorized, "unauthorized", "no claims")
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" {
		response.Fail(w, http.StatusBadRequest, "bad_request", "name is required")
		return
	}
	f, err := h.q.CreateAttributeFamily(r.Context(), gen.CreateAttributeFamilyParams{OrganizationID: org, Name: req.Name})
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not create family")
		return
	}
	response.JSON(w, http.StatusCreated, f)
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
