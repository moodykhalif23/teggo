package cms

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"b2bcommerce/internal/server/response"
	"b2bcommerce/internal/store/gen"
)

// knownBlockTypes is the server-side registry; block payloads validate against
// it on save. Adding a type = add it here + a Nuxt renderer component (no DB
// migration), matching the additive block model (§2.4).
var knownBlockTypes = map[string]bool{
	"hero": true, "rich-text": true, "product-grid": true, "banner": true, "cta": true,
}

type block struct {
	Type  string          `json:"type"`
	ID    string          `json:"id"`
	Props json.RawMessage `json:"props"`
}

func errBad(msg string) error { return errors.New(msg) }

// validateBlocks ensures blocks is a JSON array of typed blocks with known types.
func validateBlocks(rawBlocks []byte) error {
	if !json.Valid(rawBlocks) {
		return errBad("blocks must be valid JSON")
	}
	var blocks []block
	if err := json.Unmarshal(rawBlocks, &blocks); err != nil {
		return errBad("blocks must be an array of {type,id,props}")
	}
	for _, b := range blocks {
		if b.Type == "" {
			return errBad("each block needs a type")
		}
		if !knownBlockTypes[b.Type] {
			return errBad("unknown block type: " + b.Type)
		}
	}
	return nil
}

// ---- storefront reads -----------------------------------------------------

// getStorefrontPage serves a published page by slug. Targeted pages are filtered
// by the requesting customer's group (read from an optional storefront token);
// product-grid blocks have their category source resolved into product summaries.
func (h *Handler) getStorefrontPage(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	locale := r.URL.Query().Get("locale")
	if locale == "" {
		locale = "en"
	}
	ws, err := h.q.GetDefaultWebsite(r.Context(), storefrontOrg)
	if err != nil {
		response.Fail(w, http.StatusNotFound, "not_found", "no website")
		return
	}
	p, err := h.q.GetPublishedPage(r.Context(), gen.GetPublishedPageParams{WebsiteID: ws.ID, Locale: locale, Slug: slug})
	if err != nil {
		response.Fail(w, http.StatusNotFound, "not_found", "page not found")
		return
	}

	// Customer-group targeting: a targeted page is only served to a customer in
	// that group; untargeted pages are public.
	if p.TargetCustomerGroupID != nil {
		gid, ok := h.customerGroup(r)
		if !ok || gid == nil || *gid != *p.TargetCustomerGroupID {
			response.Fail(w, http.StatusNotFound, "not_found", "page not found")
			return
		}
	}

	out := pageJSON(p)
	out["blocks"] = h.resolveBlocks(r, p.Blocks)
	response.JSON(w, http.StatusOK, out)
}

// resolveBlocks expands product-grid blocks' category source into product
// summaries (resolved at render time, §2.2). Other blocks pass through.
func (h *Handler) resolveBlocks(r *http.Request, rawBlocks []byte) []map[string]any {
	var blocks []map[string]any
	if err := json.Unmarshal(rawBlocks, &blocks); err != nil {
		return []map[string]any{}
	}
	for _, b := range blocks {
		if b["type"] != "product-grid" {
			continue
		}
		props, _ := b["props"].(map[string]any)
		if props == nil {
			continue
		}
		source, _ := props["source"].(map[string]any)
		if source == nil || source["kind"] != "category" {
			continue
		}
		catID, ok := asInt64(source["category_id"])
		if !ok {
			continue
		}
		limit := int32(8)
		if l, ok := asInt64(source["limit"]); ok && l > 0 {
			limit = int32(l)
		}
		rows, err := h.q.ListActiveProductsInCategory(r.Context(), gen.ListActiveProductsInCategoryParams{
			OrganizationID: storefrontOrg, ID: catID, Limit: limit, Offset: 0,
		})
		if err != nil {
			continue
		}
		products := make([]map[string]any, 0, len(rows))
		for _, p := range rows {
			products = append(products, map[string]any{
				"public_id": p.PublicID.String(), "sku": p.Sku, "name": p.Name,
				"slug": p.Slug, "unit": p.Unit,
			})
		}
		props["products"] = products
		b["props"] = props
	}
	return blocks
}

func (h *Handler) getStorefrontMenu(w http.ResponseWriter, r *http.Request) {
	code := chi.URLParam(r, "code")
	ws, err := h.q.GetDefaultWebsite(r.Context(), storefrontOrg)
	if err != nil {
		response.Fail(w, http.StatusNotFound, "not_found", "no website")
		return
	}
	m, err := h.q.GetMenuByCode(r.Context(), gen.GetMenuByCodeParams{WebsiteID: ws.ID, Code: code})
	if err != nil {
		response.Fail(w, http.StatusNotFound, "not_found", "menu not found")
		return
	}
	items, err := h.q.ListMenuItems(r.Context(), m.ID)
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not load menu")
		return
	}
	if items == nil {
		items = []gen.MenuItem{}
	}
	response.JSON(w, http.StatusOK, map[string]any{"code": m.Code, "name": m.Name, "items": items})
}

func (h *Handler) resolveRedirect(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	if path == "" {
		response.Fail(w, http.StatusBadRequest, "bad_request", "path is required")
		return
	}
	ws, err := h.q.GetDefaultWebsite(r.Context(), storefrontOrg)
	if err != nil {
		response.Fail(w, http.StatusNotFound, "not_found", "no website")
		return
	}
	rd, err := h.q.GetRedirect(r.Context(), gen.GetRedirectParams{WebsiteID: ws.ID, FromPath: path})
	if err != nil {
		response.Fail(w, http.StatusNotFound, "not_found", "no redirect")
		return
	}
	response.JSON(w, http.StatusOK, map[string]any{"to": rd.ToPath, "status_code": rd.StatusCode})
}

// ---- helpers --------------------------------------------------------------

// customerGroup reads an optional storefront bearer token and returns the
// requesting customer's group id (for content targeting).
func (h *Handler) customerGroup(r *http.Request) (*int64, bool) {
	authz := r.Header.Get("Authorization")
	if !strings.HasPrefix(authz, "Bearer ") {
		return nil, false
	}
	claims, err := h.issuer.Parse(strings.TrimPrefix(authz, "Bearer "))
	if err != nil || claims.CustomerID == 0 {
		return nil, false
	}
	cust, err := h.q.GetCustomer(r.Context(), gen.GetCustomerParams{OrganizationID: claims.OrgID, ID: claims.CustomerID})
	if err != nil {
		return nil, false
	}
	return cust.CustomerGroupID, true
}

func asInt64(v any) (int64, bool) {
	switch n := v.(type) {
	case float64:
		return int64(n), true
	case int64:
		return n, true
	case int:
		return int64(n), true
	default:
		return 0, false
	}
}
