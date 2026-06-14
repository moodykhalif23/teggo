package catalog

// Product images & bulk CSV import/export. Images reuse the DAM pipeline: a file
// is uploaded once via POST /admin/media (dedup + renditions), then linked here
// by media_asset_id. A product is capped at 5 images. Import/export move the
// catalog in/out as CSV for ERP and spreadsheet workflows.

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"b2bcommerce/internal/changelog"
	"b2bcommerce/internal/server/response"
	"b2bcommerce/internal/store/gen"
)

// maxProductImages caps a product's gallery so listings stay clean and uploads
// bounded. Enforced on add; the UI also disables adding past this.
const maxProductImages = 5

// productImage is the gallery projection returned to the admin UI.
type productImage struct {
	ID           int64   `json:"id"`
	MediaAssetID *int64  `json:"media_asset_id"`
	URL          string  `json:"url"`
	Alt          *string `json:"alt,omitempty"`
	SortOrder    int32   `json:"sort_order"`
	Status       *string `json:"status,omitempty"`
	Width        *int32  `json:"width,omitempty"`
	Height       *int32  `json:"height,omitempty"`
}

// requireOwnedProduct returns the org id and confirms the product exists in it.
func (h *Handler) requireOwnedProduct(w http.ResponseWriter, r *http.Request) (org, id int64, ok bool) {
	org, hasOrg := orgID(r)
	if !hasOrg {
		response.Fail(w, http.StatusUnauthorized, "unauthorized", "no claims")
		return 0, 0, false
	}
	id, err := pathID(r)
	if err != nil {
		response.Fail(w, http.StatusBadRequest, "bad_request", "invalid id")
		return 0, 0, false
	}
	if _, err := h.q.GetProductByID(r.Context(), gen.GetProductByIDParams{OrganizationID: org, ID: id}); err != nil {
		response.Fail(w, http.StatusNotFound, "not_found", "product not found")
		return 0, 0, false
	}
	return org, id, true
}

func (h *Handler) listImages(w http.ResponseWriter, r *http.Request) {
	_, id, ok := h.requireOwnedProduct(w, r)
	if !ok {
		return
	}
	rows, err := h.q.ListProductImages(r.Context(), id)
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not list images")
		return
	}
	out := make([]productImage, 0, len(rows))
	for _, m := range rows {
		out = append(out, productImage{
			ID: m.ID, MediaAssetID: m.MediaAssetID, URL: m.Url, Alt: m.Alt,
			SortOrder: m.SortOrder, Status: m.AssetStatus, Width: m.Width, Height: m.Height,
		})
	}
	response.JSON(w, http.StatusOK, map[string]any{"items": out})
}

func (h *Handler) addImage(w http.ResponseWriter, r *http.Request) {
	org, id, ok := h.requireOwnedProduct(w, r)
	if !ok {
		return
	}
	var req struct {
		MediaAssetID int64   `json:"media_asset_id"`
		Alt          *string `json:"alt"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.MediaAssetID == 0 {
		response.Fail(w, http.StatusBadRequest, "bad_request", "media_asset_id is required")
		return
	}
	count, err := h.q.CountProductImages(r.Context(), id)
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not count images")
		return
	}
	if count >= maxProductImages {
		response.Fail(w, http.StatusConflict, "limit_reached", fmt.Sprintf("a product can have at most %d images", maxProductImages))
		return
	}
	// The asset must belong to the caller's org; its URL is denormalized onto the row.
	asset, err := h.q.GetMediaAssetForOrg(r.Context(), gen.GetMediaAssetForOrgParams{ID: req.MediaAssetID, OrganizationID: org})
	if err != nil {
		response.Fail(w, http.StatusBadRequest, "bad_request", "media asset not found in organization")
		return
	}
	sort, err := h.q.MaxProductImageSort(r.Context(), id)
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not order image")
		return
	}
	assetID := req.MediaAssetID
	img, err := h.q.CreateProductImage(r.Context(), gen.CreateProductImageParams{
		ProductID: id, MediaAssetID: &assetID, Url: asset.Url, Alt: req.Alt, SortOrder: sort + 1,
	})
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not add image")
		return
	}
	response.JSON(w, http.StatusCreated, productImage{
		ID: img.ID, MediaAssetID: img.MediaAssetID, URL: img.Url, Alt: img.Alt, SortOrder: img.SortOrder,
	})
}

func (h *Handler) deleteImage(w http.ResponseWriter, r *http.Request) {
	_, id, ok := h.requireOwnedProduct(w, r)
	if !ok {
		return
	}
	imageID, err := strconv.ParseInt(chi.URLParam(r, "imageID"), 10, 64)
	if err != nil {
		response.Fail(w, http.StatusBadRequest, "bad_request", "invalid image id")
		return
	}
	n, err := h.q.DeleteProductImage(r.Context(), gen.DeleteProductImageParams{ID: imageID, ProductID: id})
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not delete image")
		return
	}
	if n == 0 {
		response.Fail(w, http.StatusNotFound, "not_found", "image not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---- Bulk CSV import / export -------------------------------------------

func (h *Handler) exportCSV(w http.ResponseWriter, r *http.Request) {
	org, ok := orgID(r)
	if !ok {
		response.Fail(w, http.StatusUnauthorized, "unauthorized", "no claims")
		return
	}
	rows, err := h.q.ExportProductsAdmin(r.Context(), org)
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not export products")
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="products.csv"`)
	cw := csv.NewWriter(w)
	_ = cw.Write([]string{"sku", "name", "slug", "type", "status", "unit", "cost_price", "description", "attributes"})
	for _, p := range rows {
		desc := ""
		if p.Description != nil {
			desc = *p.Description
		}
		attrs := strings.TrimSpace(string(p.Attributes))
		if attrs == "" {
			attrs = "{}"
		}
		_ = cw.Write([]string{p.Sku, p.Name, p.Slug, p.Type, p.Status, p.Unit, p.CostPrice, desc, attrs})
	}
	cw.Flush()
}

// importRow reports the outcome of a single CSV row.
type importRow struct {
	Row    int    `json:"row"`
	SKU    string `json:"sku"`
	Action string `json:"action"` // created | updated | error
	Error  string `json:"error,omitempty"`
}

// maxImportRows guards against runaway uploads (the body size is already capped
// by the MaxBytes middleware; this bounds row processing on top of that).
const maxImportRows = 5000

func (h *Handler) importCSV(w http.ResponseWriter, r *http.Request) {
	org, ok := orgID(r)
	if !ok {
		response.Fail(w, http.StatusUnauthorized, "unauthorized", "no claims")
		return
	}
	file, _, err := r.FormFile("file")
	if err != nil {
		response.Fail(w, http.StatusBadRequest, "bad_request", "a CSV file is required (multipart field 'file')")
		return
	}
	defer file.Close()

	cr := csv.NewReader(file)
	cr.FieldsPerRecord = -1 // tolerate ragged rows; we map by header name
	header, err := cr.Read()
	if err != nil {
		response.Fail(w, http.StatusBadRequest, "bad_request", "could not read CSV header")
		return
	}
	col := map[string]int{}
	for i, name := range header {
		// Strip a UTF-8 BOM that spreadsheet exporters prepend to the first cell.
		col[strings.ToLower(strings.TrimSpace(strings.TrimPrefix(name, "\ufeff")))] = i
	}
	for _, required := range []string{"sku", "name", "slug"} {
		if _, ok := col[required]; !ok {
			response.Fail(w, http.StatusBadRequest, "bad_request", fmt.Sprintf("CSV must include a %q column", required))
			return
		}
	}
	field := func(rec []string, key string) string {
		if i, ok := col[key]; ok && i < len(rec) {
			return strings.TrimSpace(rec[i])
		}
		return ""
	}

	results := make([]importRow, 0)
	created, updated, errCount := 0, 0, 0
	line := 1 // header is line 1
	for {
		rec, rerr := cr.Read()
		if rerr == io.EOF {
			break
		}
		line++
		if len(results) >= maxImportRows {
			break
		}
		entry := importRow{Row: line}
		if rerr != nil {
			entry.Action, entry.Error = "error", "malformed CSV row"
			results, errCount = append(results, entry), errCount+1
			continue
		}
		sku, name, slug := field(rec, "sku"), field(rec, "name"), field(rec, "slug")
		entry.SKU = sku
		if sku == "" || name == "" || slug == "" {
			entry.Action, entry.Error = "error", "sku, name and slug are required"
			results, errCount = append(results, entry), errCount+1
			continue
		}
		pr := productRequest{SKU: sku, Name: name, Slug: slug, Type: field(rec, "type"), Status: field(rec, "status"), Unit: field(rec, "unit")}
		pr.CostPrice = field(rec, "cost_price")
		costGiven := pr.CostPrice != "" // when absent on update, preserve the existing cost
		if d := field(rec, "description"); d != "" {
			pr.Description = &d
		}
		if a := field(rec, "attributes"); a != "" {
			if !json.Valid([]byte(a)) {
				entry.Action, entry.Error = "error", "attributes is not valid JSON"
				results, errCount = append(results, entry), errCount+1
				continue
			}
			pr.Attributes = json.RawMessage(a)
		}
		pr.defaults()

		existing, lookupErr := h.q.GetProductBySKU(r.Context(), gen.GetProductBySKUParams{OrganizationID: org, Sku: sku})
		switch {
		case lookupErr == nil:
			cost := pr.CostPrice
			if !costGiven {
				cost = existing.CostPrice // no cost column → keep the existing cost, don't zero it
			}
			var p gen.Product
			uerr := h.inTx(r.Context(), func(ctx context.Context, qtx *gen.Queries) error {
				var e error
				if p, e = qtx.UpdateProduct(ctx, gen.UpdateProductParams{
					OrganizationID: org, ID: existing.ID, Sku: pr.SKU, Type: pr.Type, Name: pr.Name, Slug: pr.Slug,
					Description: pr.Description, Status: pr.Status, Attributes: pr.Attributes, Unit: pr.Unit,
				}); e != nil {
					return e
				}
				p, e = qtx.SetProductCost(ctx, gen.SetProductCostParams{OrganizationID: org, ID: p.ID, CostPrice: cost})
				return e
			})
			if uerr != nil {
				entry.Action, entry.Error = "error", "could not update (duplicate slug?)"
				errCount++
				break
			}
			changelog.Record(r.Context(), h.q, org, nil, "product", p.ID, "upsert", toAdminProduct(p))
			updated++
			entry.Action = "updated"
		case errors.Is(lookupErr, pgx.ErrNoRows):
			var p gen.Product
			cerr := h.inTx(r.Context(), func(ctx context.Context, qtx *gen.Queries) error {
				var e error
				if p, e = qtx.CreateProduct(ctx, gen.CreateProductParams{
					OrganizationID: org, Sku: pr.SKU, Type: pr.Type, Name: pr.Name, Slug: pr.Slug,
					Description: pr.Description, Status: pr.Status, Attributes: pr.Attributes, Unit: pr.Unit,
				}); e != nil {
					return e
				}
				p, e = qtx.SetProductCost(ctx, gen.SetProductCostParams{OrganizationID: org, ID: p.ID, CostPrice: pr.CostPrice})
				return e
			})
			if cerr != nil {
				entry.Action, entry.Error = "error", "could not create (duplicate slug?)"
				errCount++
				break
			}
			changelog.Record(r.Context(), h.q, org, nil, "product", p.ID, "upsert", toAdminProduct(p))
			created++
			entry.Action = "created"
		default:
			entry.Action, entry.Error = "error", "lookup failed"
			errCount++
		}
		results = append(results, entry)
	}
	response.JSON(w, http.StatusOK, map[string]any{
		"created": created, "updated": updated, "errors": errCount, "results": results,
	})
}
