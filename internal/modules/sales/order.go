package sales

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"b2bcommerce/internal/money"
	"b2bcommerce/internal/server/response"
	"b2bcommerce/internal/store/gen"
)

// ---- storefront: accept quote -> order -----------------------------------

// acceptQuote converts an accepted quote into an order in a single transaction:
// snapshot addresses + line items, mark the quote accepted (immutable), and (if
// from an RFQ) mark the RFQ accepted. Pack 1 §6 AC.
func (h *Handler) acceptQuote(w http.ResponseWriter, r *http.Request) {
	cc, ok := customer(r)
	if !ok {
		unauthorized(w)
		return
	}
	quote, ok := h.loadMyQuote(w, r, cc)
	if !ok {
		return
	}
	if quote.Status != "sent" && quote.Status != "revised" {
		response.Fail(w, http.StatusConflict, "invalid_state", "quote is not open for acceptance")
		return
	}
	if quote.ValidUntil.Valid && quote.ValidUntil.Time.Before(time.Now()) {
		response.Fail(w, http.StatusConflict, "expired", "quote has expired")
		return
	}

	var order gen.Order
	err := h.tx(r.Context(), func(q *gen.Queries) error {
		items, e := q.ListQuoteItems(r.Context(), quote.ID)
		if e != nil {
			return e
		}
		var totals []string
		for _, it := range items {
			totals = append(totals, it.RowTotal)
		}
		subtotal, _ := money.Sum(totals...)

		billing := h.snapshotAddress(r.Context(), q, quote.CustomerID, "billing")
		shipping := h.snapshotAddress(r.Context(), q, quote.CustomerID, "shipping")

		order, e = q.CreateOrder(r.Context(), gen.CreateOrderParams{
			OrganizationID: quote.OrganizationID, WebsiteID: quote.WebsiteID, CustomerID: quote.CustomerID,
			CustomerUserID: cc.customerUserID, QuoteID: &quote.ID, Currency: quote.Currency,
			BillingAddress: billing, ShippingAddress: shipping,
			Subtotal: subtotal, TaxTotal: "0", ShippingTotal: "0", GrandTotal: subtotal,
		})
		if e != nil {
			return e
		}
		for _, it := range items {
			if e := h.addOrderItemSnapshot(r.Context(), q, quote.OrganizationID, order.ID, it.ProductID, it.Quantity, it.Unit, it.UnitPrice, it.RowTotal); e != nil {
				return e
			}
		}
		if _, e := q.SetQuoteStatus(r.Context(), gen.SetQuoteStatusParams{ID: quote.ID, Status: "accepted"}); e != nil {
			return e
		}
		if quote.RfqID != nil {
			if _, e := q.SetRFQStatus(r.Context(), gen.SetRFQStatusParams{ID: *quote.RfqID, Status: "accepted"}); e != nil {
				return e
			}
		}
		by := "customer_user:0"
		if cc.customerUserID != nil {
			by = "customer_user:" + itoa(*cc.customerUserID)
		}
		return q.AddOrderStatusHistory(r.Context(), gen.AddOrderStatusHistoryParams{
			OrderID: order.ID, FromStatus: nil, ToStatus: "pending", ChangedBy: by, Note: strPtr("created from quote acceptance"),
		})
	})
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not create order from quote")
		return
	}
	h.renderOrder(w, r, order)
}

// ---- admin: on-behalf-of order + management ------------------------------

func (h *Handler) createOrderOnBehalf(w http.ResponseWriter, r *http.Request) {
	a, ok := admin(r)
	if !ok {
		unauthorized(w)
		return
	}
	var req struct {
		CustomerID            int64      `json:"customer_id"`
		Currency              string     `json:"currency"`
		PoNumber              *string    `json:"po_number"`
		RequestedDeliveryDate *time.Time `json:"requested_delivery_date"`
		Items                 []struct {
			ProductID int64  `json:"product_id"`
			Quantity  string `json:"quantity"`
			Unit      string `json:"unit"`
			UnitPrice string `json:"unit_price"`
		} `json:"items"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.CustomerID == 0 || len(req.Items) == 0 {
		response.Fail(w, http.StatusBadRequest, "bad_request", "customer_id and at least one item required")
		return
	}
	ws, err := h.q.GetDefaultWebsite(r.Context(), a.orgID)
	if err != nil {
		response.Fail(w, http.StatusBadRequest, "bad_request", "no website configured")
		return
	}
	if req.Currency == "" {
		req.Currency = ws.DefaultCurrency
	}

	var order gen.Order
	err = h.tx(r.Context(), func(q *gen.Queries) error {
		var totals []string
		type line struct {
			pid                  int64
			qty, unit, price, rt string
		}
		var lines []line
		for _, it := range req.Items {
			qty := it.Quantity
			if qty == "" {
				qty = "1"
			}
			unit := it.Unit
			if unit == "" {
				unit = "each"
			}
			rt, e := money.RowTotal(qty, it.UnitPrice, "0")
			if e != nil {
				return e
			}
			lines = append(lines, line{it.ProductID, qty, unit, it.UnitPrice, rt})
			totals = append(totals, rt)
		}
		subtotal, _ := money.Sum(totals...)

		var e error
		order, e = q.CreateOrder(r.Context(), gen.CreateOrderParams{
			OrganizationID: a.orgID, WebsiteID: ws.ID, CustomerID: req.CustomerID,
			PlacedBySalesRepID: a.userID, Currency: req.Currency, PoNumber: req.PoNumber,
			RequestedDeliveryDate: datePtr(req.RequestedDeliveryDate),
			BillingAddress:        h.snapshotAddress(r.Context(), q, req.CustomerID, "billing"),
			ShippingAddress:       h.snapshotAddress(r.Context(), q, req.CustomerID, "shipping"),
			Subtotal:              subtotal, TaxTotal: "0", ShippingTotal: "0", GrandTotal: subtotal,
		})
		if e != nil {
			return e
		}
		for _, ln := range lines {
			if e := h.addOrderItemSnapshot(r.Context(), q, a.orgID, order.ID, ln.pid, ln.qty, ln.unit, ln.price, ln.rt); e != nil {
				return e
			}
		}
		by := "rep:0"
		if a.userID != nil {
			by = "rep:" + itoa(*a.userID)
		}
		return q.AddOrderStatusHistory(r.Context(), gen.AddOrderStatusHistoryParams{
			OrderID: order.ID, FromStatus: nil, ToStatus: "pending", ChangedBy: by, Note: strPtr("placed on behalf of customer"),
		})
	})
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not create order")
		return
	}
	h.renderOrder(w, r, order)
}

func (h *Handler) patchOrderStatus(w http.ResponseWriter, r *http.Request) {
	a, ok := admin(r)
	if !ok {
		unauthorized(w)
		return
	}
	id, err := pathID(r)
	if err != nil {
		response.Fail(w, http.StatusBadRequest, "bad_request", "invalid id")
		return
	}
	order, err := h.q.GetOrderByID(r.Context(), gen.GetOrderByIDParams{OrganizationID: a.orgID, ID: id})
	if err != nil {
		notFound(w, "order")
		return
	}
	var req struct {
		Status string  `json:"status"`
		Note   *string `json:"note"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Status == "" {
		response.Fail(w, http.StatusBadRequest, "bad_request", "status is required")
		return
	}
	if !canTransition(orderTransitions, order.Status, req.Status) {
		response.Fail(w, http.StatusConflict, "invalid_transition", "cannot move from "+order.Status+" to "+req.Status)
		return
	}
	from := order.Status
	by := "rep:0"
	if a.userID != nil {
		by = "rep:" + itoa(*a.userID)
	}
	var updated gen.Order
	err = h.tx(r.Context(), func(q *gen.Queries) error {
		var e error
		updated, e = q.SetOrderStatus(r.Context(), gen.SetOrderStatusParams{ID: order.ID, Status: req.Status})
		if e != nil {
			return e
		}
		return q.AddOrderStatusHistory(r.Context(), gen.AddOrderStatusHistoryParams{
			OrderID: order.ID, FromStatus: &from, ToStatus: req.Status, ChangedBy: by, Note: req.Note,
		})
	})
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not update status")
		return
	}
	h.renderOrder(w, r, updated)
}

func (h *Handler) adminListOrders(w http.ResponseWriter, r *http.Request) {
	a, ok := admin(r)
	if !ok {
		unauthorized(w)
		return
	}
	rows, err := h.q.ListOrdersAdmin(r.Context(), gen.ListOrdersAdminParams{OrganizationID: a.orgID, Limit: 100, Offset: 0})
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not list orders")
		return
	}
	if rows == nil {
		rows = []gen.Order{}
	}
	response.JSON(w, http.StatusOK, map[string]any{"items": rows})
}

func (h *Handler) adminGetOrder(w http.ResponseWriter, r *http.Request) {
	a, ok := admin(r)
	if !ok {
		unauthorized(w)
		return
	}
	id, err := pathID(r)
	if err != nil {
		response.Fail(w, http.StatusBadRequest, "bad_request", "invalid id")
		return
	}
	order, err := h.q.GetOrderByID(r.Context(), gen.GetOrderByIDParams{OrganizationID: a.orgID, ID: id})
	if err != nil {
		notFound(w, "order")
		return
	}
	h.renderOrder(w, r, order)
}

// ---- storefront order reads ----------------------------------------------

func (h *Handler) listMyOrders(w http.ResponseWriter, r *http.Request) {
	cc, ok := customer(r)
	if !ok {
		unauthorized(w)
		return
	}
	rows, err := h.q.ListOrdersForCustomer(r.Context(), cc.customerID)
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not list orders")
		return
	}
	if rows == nil {
		rows = []gen.Order{}
	}
	response.JSON(w, http.StatusOK, map[string]any{"items": rows})
}

func (h *Handler) getMyOrder(w http.ResponseWriter, r *http.Request) {
	cc, ok := customer(r)
	if !ok {
		unauthorized(w)
		return
	}
	pid, err := uuid.Parse(chi.URLParam(r, "publicID"))
	if err != nil {
		response.Fail(w, http.StatusBadRequest, "bad_request", "invalid id")
		return
	}
	order, err := h.q.GetOrderByPublicID(r.Context(), pid)
	if err != nil || order.CustomerID != cc.customerID {
		notFound(w, "order")
		return
	}
	h.renderOrder(w, r, order)
}

// ---- shared --------------------------------------------------------------

// snapshotAddress returns the customer's default address of a type as a JSON
// object for embedding on the order; "{}" when none exists.
func (h *Handler) snapshotAddress(ctx context.Context, q *gen.Queries, customerID int64, typ string) []byte {
	addr, err := q.GetCustomerDefaultAddress(ctx, gen.GetCustomerDefaultAddressParams{CustomerID: customerID, Type: typ})
	if err != nil {
		return []byte("{}")
	}
	b, _ := json.Marshal(addr)
	return b
}

// addOrderItemSnapshot copies the product's current sku/name onto the order line
// (orders are immutable records, not live joins).
func (h *Handler) addOrderItemSnapshot(ctx context.Context, q *gen.Queries, orgID, orderID, productID int64, qty, unit, unitPrice, rowTotal string) error {
	p, err := q.GetProductByID(ctx, gen.GetProductByIDParams{OrganizationID: orgID, ID: productID})
	sku, name := "", ""
	if err == nil {
		sku, name = p.Sku, p.Name
	}
	_, err = q.AddOrderItem(ctx, gen.AddOrderItemParams{
		OrderID: orderID, ProductID: productID, Sku: sku, Name: name,
		Quantity: qty, Unit: unit, UnitPrice: unitPrice, TaxAmount: "0", RowTotal: rowTotal,
	})
	return err
}

func (h *Handler) renderOrder(w http.ResponseWriter, r *http.Request, order gen.Order) {
	items, err := h.q.ListOrderItems(r.Context(), order.ID)
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not load order items")
		return
	}
	if items == nil {
		items = []gen.OrderItem{}
	}
	response.JSON(w, http.StatusOK, map[string]any{
		"id":          order.ID,
		"public_id":   order.PublicID.String(),
		"status":      order.Status,
		"currency":    order.Currency,
		"subtotal":    order.Subtotal,
		"grand_total": order.GrandTotal,
		"quote_id":    order.QuoteID,
		"items":       items,
	})
}

func strPtr(s string) *string { return &s }

func datePtr(t *time.Time) pgtype.Date {
	if t == nil {
		return pgtype.Date{}
	}
	return pgtype.Date{Time: *t, Valid: true}
}
