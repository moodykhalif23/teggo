package marketplace

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	mw "b2bcommerce/internal/server/middleware"
	"b2bcommerce/internal/server/response"
	"b2bcommerce/internal/store/gen"
)

// RoutesVendor mounts the vendor self-service portal (audience 'vendor'). Every
// route is scoped to the authenticated vendor taken from the JWT claims, never a
// path or body parameter, so a vendor can only ever see its own data.
func (h *Handler) RoutesVendor(r chi.Router, authMW func(http.Handler) http.Handler) {
	r.Group(func(vr chi.Router) {
		vr.Use(authMW)
		vr.Use(mw.RequireAudience("vendor"))

		vr.Get("/vendor/me", h.vendorMe)
		vr.Get("/vendor/dashboard", h.vendorDashboard)
		vr.Get("/vendor/products", h.vendorProducts)
		vr.Get("/vendor/orders", h.vendorOrders)
		vr.Get("/vendor/orders/{id}", h.vendorOrderDetail)
		vr.Patch("/vendor/orders/{id}/status", h.vendorOrderStatus)
		vr.Get("/vendor/payouts", h.vendorPayouts)
	})
}

// vendorCtx is the authenticated vendor principal.
type vendorCtx struct {
	orgID    int64
	vendorID int64
}

func vendorOf(r *http.Request) (vendorCtx, bool) {
	c, ok := mw.ClaimsFrom(r.Context())
	if !ok || c.VendorID == 0 {
		return vendorCtx{}, false
	}
	return vendorCtx{orgID: c.OrgID, vendorID: c.VendorID}, true
}

func (h *Handler) vendorMe(w http.ResponseWriter, r *http.Request) {
	vc, ok := vendorOf(r)
	if !ok {
		response.Fail(w, http.StatusUnauthorized, "unauthorized", "no vendor claims")
		return
	}
	v, err := h.q.GetVendor(r.Context(), gen.GetVendorParams{ID: vc.vendorID, OrganizationID: vc.orgID})
	if err != nil {
		response.Fail(w, http.StatusNotFound, "not_found", "vendor not found")
		return
	}
	response.JSON(w, http.StatusOK, toVendorDTO(v))
}

func (h *Handler) vendorDashboard(w http.ResponseWriter, r *http.Request) {
	vc, ok := vendorOf(r)
	if !ok {
		response.Fail(w, http.StatusUnauthorized, "unauthorized", "no vendor claims")
		return
	}
	s, err := h.q.VendorSalesSummary(r.Context(), vc.vendorID)
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not load summary")
		return
	}
	response.JSON(w, http.StatusOK, map[string]any{
		"order_count":      s.OrderCount,
		"gross_total":      s.GrossTotal,
		"commission_total": s.CommissionTotal,
		"net_total":        s.NetTotal,
	})
}

func (h *Handler) vendorProducts(w http.ResponseWriter, r *http.Request) {
	vc, ok := vendorOf(r)
	if !ok {
		response.Fail(w, http.StatusUnauthorized, "unauthorized", "no vendor claims")
		return
	}
	rows, err := h.q.ListProductsByVendor(r.Context(), &vc.vendorID)
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not list products")
		return
	}
	if rows == nil {
		rows = []gen.ListProductsByVendorRow{}
	}
	response.JSON(w, http.StatusOK, map[string]any{"items": rows})
}

func (h *Handler) vendorOrders(w http.ResponseWriter, r *http.Request) {
	vc, ok := vendorOf(r)
	if !ok {
		response.Fail(w, http.StatusUnauthorized, "unauthorized", "no vendor claims")
		return
	}
	rows, err := h.q.ListVendorOrdersForVendor(r.Context(), vc.vendorID)
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not list orders")
		return
	}
	items := make([]map[string]any, 0, len(rows))
	for _, vo := range rows {
		items = append(items, map[string]any{
			"id":               vo.ID,
			"public_id":        vo.PublicID.String(),
			"order_public_id":  vo.OrderPublicID.String(),
			"status":           vo.Status,
			"order_status":     vo.OrderStatus,
			"currency":         vo.Currency,
			"gross_total":      vo.GrossTotal,
			"commission_total": vo.CommissionTotal,
			"net_total":        vo.NetTotal,
			"payout_id":        vo.PayoutID,
			"created_at":       vo.CreatedAt,
		})
	}
	response.JSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *Handler) vendorOrderDetail(w http.ResponseWriter, r *http.Request) {
	vc, ok := vendorOf(r)
	if !ok {
		response.Fail(w, http.StatusUnauthorized, "unauthorized", "no vendor claims")
		return
	}
	id, err := pathID(r)
	if err != nil {
		response.Fail(w, http.StatusBadRequest, "bad_request", "invalid id")
		return
	}
	vo, err := h.q.GetVendorOrderForVendor(r.Context(), gen.GetVendorOrderForVendorParams{ID: id, VendorID: vc.vendorID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			response.Fail(w, http.StatusNotFound, "not_found", "order not found")
			return
		}
		response.Fail(w, http.StatusInternalServerError, "internal", "could not load order")
		return
	}
	vid := vc.vendorID
	lines, err := h.q.ListVendorOrderItems(r.Context(), gen.ListVendorOrderItemsParams{OrderID: vo.OrderID, VendorID: &vid})
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not load lines")
		return
	}
	if lines == nil {
		lines = []gen.ListVendorOrderItemsRow{}
	}
	response.JSON(w, http.StatusOK, map[string]any{
		"id":               vo.ID,
		"public_id":        vo.PublicID.String(),
		"order_public_id":  vo.OrderPublicID.String(),
		"status":           vo.Status,
		"currency":         vo.Currency,
		"gross_total":      vo.GrossTotal,
		"commission_rate":  vo.CommissionRate,
		"commission_total": vo.CommissionTotal,
		"net_total":        vo.NetTotal,
		"items":            lines,
	})
}

// vendorOrderTransitions is the allowed fulfilment lifecycle for a vendor's
// sub-order. A vendor advances its own fulfilment; the parent buyer order keeps
// its independent workflow.
var vendorOrderTransitions = map[string][]string{
	"pending":  {"accepted", "cancelled"},
	"accepted": {"shipped", "cancelled"},
	"shipped":  {"delivered"},
}

func (h *Handler) vendorOrderStatus(w http.ResponseWriter, r *http.Request) {
	vc, ok := vendorOf(r)
	if !ok {
		response.Fail(w, http.StatusUnauthorized, "unauthorized", "no vendor claims")
		return
	}
	id, err := pathID(r)
	if err != nil {
		response.Fail(w, http.StatusBadRequest, "bad_request", "invalid id")
		return
	}
	var req struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Status == "" {
		response.Fail(w, http.StatusBadRequest, "bad_request", "status is required")
		return
	}
	vo, err := h.q.GetVendorOrderForVendor(r.Context(), gen.GetVendorOrderForVendorParams{ID: id, VendorID: vc.vendorID})
	if err != nil {
		response.Fail(w, http.StatusNotFound, "not_found", "order not found")
		return
	}
	allowed := false
	for _, to := range vendorOrderTransitions[vo.Status] {
		if to == req.Status {
			allowed = true
			break
		}
	}
	if !allowed {
		response.Fail(w, http.StatusConflict, "invalid_transition", "cannot move from "+vo.Status+" to "+req.Status)
		return
	}
	updated, err := h.q.SetVendorOrderStatus(r.Context(), gen.SetVendorOrderStatusParams{ID: id, VendorID: vc.vendorID, Status: req.Status})
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not update status")
		return
	}
	response.JSON(w, http.StatusOK, map[string]any{"id": updated.ID, "status": updated.Status})
}
