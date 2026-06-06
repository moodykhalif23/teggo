package sales

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"b2bcommerce/internal/inventory"
	"b2bcommerce/internal/modules/marketplace"
	"b2bcommerce/internal/money"
	mw "b2bcommerce/internal/server/middleware"
	"b2bcommerce/internal/server/response"
	"b2bcommerce/internal/store/gen"
	"b2bcommerce/internal/tax"
	"b2bcommerce/internal/workflow"
)

// countryOf extracts the "country" field from an address JSON snapshot, for tax
// region resolution. Empty when absent (→ untaxed).
func countryOf(addr []byte) string {
	if len(addr) == 0 {
		return ""
	}
	var a struct {
		Country string `json:"country"`
	}
	_ = json.Unmarshal(addr, &a)
	return a.Country
}

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

		// Tax the accepted quote's lines onto the order (0 when no rates).
		taxLines := make([]tax.OrderLine, len(items))
		for i, it := range items {
			taxLines[i] = tax.OrderLine{ProductID: it.ProductID, Amount: it.RowTotal}
		}
		perLineTax, taxTotal, taxErr := h.tax.ComputeOrderTax(r.Context(), quote.OrganizationID, countryOf(shipping), taxLines)
		if taxErr != nil {
			return taxErr
		}
		grand, _ := money.Sum(subtotal, taxTotal)

		order, e = q.CreateOrder(r.Context(), gen.CreateOrderParams{
			OrganizationID: quote.OrganizationID, WebsiteID: quote.WebsiteID, CustomerID: quote.CustomerID,
			CustomerUserID: cc.customerUserID, QuoteID: &quote.ID, Currency: quote.Currency,
			BillingAddress: billing, ShippingAddress: shipping,
			Subtotal: subtotal, TaxTotal: taxTotal, ShippingTotal: "0", GrandTotal: grand,
		})
		if e != nil {
			return e
		}
		for i, it := range items {
			if e := h.addOrderItemSnapshot(r.Context(), q, quote.OrganizationID, order.ID, it.ProductID, it.Quantity, it.Unit, it.UnitPrice, it.RowTotal, perLineTax[i]); e != nil {
				return e
			}
		}
		// Fan the order into per-vendor sub-orders + commission (no-op when all
		// lines are operator-owned).
		if e := marketplace.SplitOrder(r.Context(), q, quote.OrganizationID, order.ID, quote.Currency); e != nil {
			return e
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
	h.emailOrderConfirmation(r.Context(), order)
	h.renderOrder(w, r, order)
}

// ---- storefront: place order from the active cart (§9) -------------------

type addressInput struct {
	Line1      string  `json:"line1"`
	Line2      *string `json:"line2"`
	City       string  `json:"city"`
	Region     *string `json:"region"`
	PostalCode *string `json:"postal_code"`
	Country    string  `json:"country"`
}

// placeOrderFromCart converts the customer's active cart into an order in one
// transaction: snapshot line prices (already snapshotted on the cart) + addresses,
// mark the cart converted, write a status-history row. Credit/approval gate (§10)
// is deferred — the order is created `pending`.
func (h *Handler) placeOrderFromCart(w http.ResponseWriter, r *http.Request) {
	cc, ok := customer(r)
	if !ok {
		unauthorized(w)
		return
	}
	var req struct {
		PoNumber              *string       `json:"po_number"`
		RequestedDeliveryDate *time.Time    `json:"requested_delivery_date"`
		BillingAddress        *addressInput `json:"billing_address"`
		ShippingAddress       *addressInput `json:"shipping_address"`
		ShippingAmount        *string       `json:"shipping_amount"` // chosen shipping rate
		CostCenter            *string       `json:"cost_center"`     // procurement budget attribution
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	costCenter := ""
	if req.CostCenter != nil {
		costCenter = *req.CostCenter
	}

	ws, err := h.q.GetDefaultWebsite(r.Context(), cc.orgID)
	if err != nil {
		response.Fail(w, http.StatusBadRequest, "bad_request", "no website configured")
		return
	}
	cart, err := h.q.GetActiveCart(r.Context(), gen.GetActiveCartParams{CustomerID: cc.customerID, WebsiteID: ws.ID})
	if err != nil {
		response.Fail(w, http.StatusUnprocessableEntity, "empty_cart", "no active cart to check out")
		return
	}
	items, err := h.q.ListCartItems(r.Context(), cart.ID)
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not load cart")
		return
	}
	if len(items) == 0 {
		response.Fail(w, http.StatusUnprocessableEntity, "empty_cart", "cart is empty")
		return
	}

	var order gen.Order
	err = h.tx(r.Context(), func(q *gen.Queries) error {
		var totals []string
		type line struct {
			pid                                int64
			sku, name, qty, unit, price, total string
		}
		var lines []line
		for _, it := range items {
			rt, e := money.RowTotal(it.Quantity, it.UnitPrice, "0")
			if e != nil {
				return e
			}
			lines = append(lines, line{it.ProductID, it.Sku, it.Name, it.Quantity, it.Unit, it.UnitPrice, rt})
			totals = append(totals, rt)
		}
		subtotal, _ := money.Sum(totals...)

		billing := addrSnapshot(r.Context(), q, cc.customerID, "billing", req.BillingAddress)
		shipping := addrSnapshot(r.Context(), q, cc.customerID, "shipping", req.ShippingAddress)

		// Tax (per-line VAT by destination + product class) + shipping snapshot
		// onto the order. Both default to 0 when nothing is configured/chosen.
		taxLines := make([]tax.OrderLine, len(lines))
		for i, ln := range lines {
			taxLines[i] = tax.OrderLine{ProductID: ln.pid, Amount: ln.total}
		}
		perLineTax, taxTotal, taxErr := h.tax.ComputeOrderTax(r.Context(), cc.orgID, countryOf(shipping), taxLines)
		if taxErr != nil {
			return taxErr
		}
		shippingTotal := "0"
		if req.ShippingAmount != nil {
			if _, perr := money.Parse(*req.ShippingAmount); perr == nil {
				shippingTotal = *req.ShippingAmount
			}
		}
		grand, _ := money.Sum(subtotal, taxTotal, shippingTotal)

		// Procurement budget gate: if an active budget governs this customer +
		// cost center, block the order when it would exceed the remaining budget
		// for the current period. No budget configured -> no gate.
		if budget, e := q.GetActiveBudget(r.Context(), gen.GetActiveBudgetParams{CustomerID: cc.customerID, CostCenter: costCenter}); e == nil {
			cc2 := costCenter
			spent, e := q.SpendForCustomerPeriod(r.Context(), gen.SpendForCustomerPeriodParams{
				CustomerID: cc.customerID, CostCenter: &cc2, CreatedAt: periodStart(budget.Period, time.Now()),
			})
			if e != nil {
				return e
			}
			projected, _ := money.Sum(spent, grand)
			if cmp, _ := money.Cmp(projected, budget.Amount); cmp > 0 {
				return errBudgetExceeded
			}
		}

		var e error
		order, e = q.CreateOrder(r.Context(), gen.CreateOrderParams{
			OrganizationID: cc.orgID, WebsiteID: ws.ID, CustomerID: cc.customerID,
			CustomerUserID: cc.customerUserID, Currency: cart.Currency,
			PoNumber: req.PoNumber, RequestedDeliveryDate: datePtr(req.RequestedDeliveryDate),
			BillingAddress: billing, ShippingAddress: shipping,
			Subtotal: subtotal, TaxTotal: taxTotal, ShippingTotal: shippingTotal, GrandTotal: grand,
		})
		if e != nil {
			return e
		}
		if costCenter != "" {
			if e := q.SetOrderCostCenter(r.Context(), gen.SetOrderCostCenterParams{ID: order.ID, CostCenter: &costCenter}); e != nil {
				return e
			}
		}
		for i, ln := range lines {
			if _, e := q.AddOrderItem(r.Context(), gen.AddOrderItemParams{
				OrderID: order.ID, ProductID: ln.pid, Sku: ln.sku, Name: ln.name,
				Quantity: ln.qty, Unit: ln.unit, UnitPrice: ln.price, TaxAmount: perLineTax[i], RowTotal: ln.total,
			}); e != nil {
				return e
			}
		}
		// Fan the order into per-vendor sub-orders + commission (no-op when all
		// lines are operator-owned).
		if e := marketplace.SplitOrder(r.Context(), q, cc.orgID, order.ID, cart.Currency); e != nil {
			return e
		}
		if e := q.MarkCartConverted(r.Context(), cart.ID); e != nil {
			return e
		}
		by := "customer_user:0"
		if cc.customerUserID != nil {
			by = "customer_user:" + itoa(*cc.customerUserID)
		}
		return q.AddOrderStatusHistory(r.Context(), gen.AddOrderStatusHistoryParams{
			OrderID: order.ID, FromStatus: nil, ToStatus: "pending", ChangedBy: by, Note: strPtr("placed from cart"),
		})
	})
	if errors.Is(err, errBudgetExceeded) {
		response.Fail(w, http.StatusUnprocessableEntity, "budget_exceeded", "this order would exceed the cost-center budget for the period")
		return
	}
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not place order")
		return
	}
	h.emailOrderConfirmation(r.Context(), order)
	h.renderOrder(w, r, order)
}

// addrSnapshot returns a JSON address: the request-provided one if present,
// otherwise the customer's default address of that type ("{}" if none).
func addrSnapshot(ctx context.Context, q *gen.Queries, customerID int64, typ string, provided *addressInput) []byte {
	if provided != nil {
		b, _ := json.Marshal(provided)
		return b
	}
	addr, err := q.GetCustomerDefaultAddress(ctx, gen.GetCustomerDefaultAddressParams{CustomerID: customerID, Type: typ})
	if err != nil {
		return []byte("{}")
	}
	b, _ := json.Marshal(addr)
	return b
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

		shipping := h.snapshotAddress(r.Context(), q, req.CustomerID, "shipping")
		taxLines := make([]tax.OrderLine, len(lines))
		for i, ln := range lines {
			taxLines[i] = tax.OrderLine{ProductID: ln.pid, Amount: ln.rt}
		}
		perLineTax, taxTotal, taxErr := h.tax.ComputeOrderTax(r.Context(), a.orgID, countryOf(shipping), taxLines)
		if taxErr != nil {
			return taxErr
		}
		grand, _ := money.Sum(subtotal, taxTotal)

		var e error
		order, e = q.CreateOrder(r.Context(), gen.CreateOrderParams{
			OrganizationID: a.orgID, WebsiteID: ws.ID, CustomerID: req.CustomerID,
			PlacedBySalesRepID: a.userID, Currency: req.Currency, PoNumber: req.PoNumber,
			RequestedDeliveryDate: datePtr(req.RequestedDeliveryDate),
			BillingAddress:        h.snapshotAddress(r.Context(), q, req.CustomerID, "billing"),
			ShippingAddress:       shipping,
			Subtotal:              subtotal, TaxTotal: taxTotal, ShippingTotal: "0", GrandTotal: grand,
		})
		if e != nil {
			return e
		}
		for i, ln := range lines {
			if e := h.addOrderItemSnapshot(r.Context(), q, a.orgID, order.ID, ln.pid, ln.qty, ln.unit, ln.price, ln.rt, perLineTax[i]); e != nil {
				return e
			}
		}
		// Fan the order into per-vendor sub-orders + commission (no-op when all
		// lines are operator-owned).
		if e := marketplace.SplitOrder(r.Context(), q, a.orgID, order.ID, req.Currency); e != nil {
			return e
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
	by := "rep:0"
	if a.userID != nil {
		by = "rep:" + itoa(*a.userID)
	}

	// The allowed transitions come from the `order_default` workflow definition
	// (migration 0014), not a hardcoded map. The engine validates current→target
	// against the DB, then atomically updates the instance + log together with
	// our in-tx hook (sync orders.status, reserve stock on confirm, history row).
	inst, err := h.wf.EnsureInstance(r.Context(), a.orgID, "order_default", "order", order.ID, order.Status)
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not load order workflow")
		return
	}
	hook := func(q *gen.Queries, from, to gen.WorkflowState) error {
		if _, e := q.SetOrderStatus(r.Context(), gen.SetOrderStatusParams{ID: order.ID, Status: to.Code}); e != nil {
			return e
		}
		// Confirming an order reserves stock for tracked lines (§8). Untracked
		// products are skipped; insufficient stock (no backorder) aborts the tx.
		if to.Code == "confirmed" {
			if e := inventory.ReserveForOrder(r.Context(), q, a.orgID, order.ID, by); e != nil {
				return e
			}
		}
		fromCode := from.Code
		return q.AddOrderStatusHistory(r.Context(), gen.AddOrderStatusHistoryParams{
			OrderID: order.ID, FromStatus: &fromCode, ToStatus: to.Code, ChangedBy: by, Note: req.Note,
		})
	}
	actor := workflow.Actor{Type: "rep", ID: a.userID}
	data := map[string]any{"grand_total": order.GrandTotal}
	// The actor's permissions feed the has_permission guard (e.g. `order.approve`
	// gates resuming an order out of on_hold — the approval step).
	if c, ok := mw.ClaimsFrom(r.Context()); ok {
		data["actor_permissions"] = c.Permissions
	}
	// Supply the buyer's spending limit so the amount_lte_limit guard on the
	// `confirm` transition can gate over-limit orders for approval. Absent/zero
	// limit → the guard is inert (most orders confirm freely).
	if order.CustomerUserID != nil {
		if lim, err := h.q.GetCustomerUserSpendingLimit(r.Context(), *order.CustomerUserID); err == nil && lim != nil {
			data["spending_limit"] = *lim
		}
	}
	if _, err := h.wf.ApplyTransitionTo(r.Context(), inst.ID, req.Status, actor, data, hook); err != nil {
		switch {
		case errors.Is(err, workflow.ErrNoTransition), errors.Is(err, workflow.ErrFinalState):
			response.Fail(w, http.StatusConflict, "invalid_transition", "cannot move from "+order.Status+" to "+req.Status)
		case errors.Is(err, inventory.ErrInsufficientStock):
			response.Fail(w, http.StatusConflict, "insufficient_stock", "not enough stock to confirm this order")
		default:
			var ge *workflow.GuardError
			if errors.As(err, &ge) {
				response.Fail(w, http.StatusConflict, "blocked", ge.Reason)
				return
			}
			response.Fail(w, http.StatusInternalServerError, "internal", "could not update status")
		}
		return
	}
	updated, err := h.q.GetOrderByID(r.Context(), gen.GetOrderByIDParams{OrganizationID: a.orgID, ID: order.ID})
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not load order")
		return
	}
	// Emit a domain event so automation rules on order.status_changed can fire
	// (e.g. notify the customer). Fire-and-forget; never blocks the response.
	if h.notify != nil {
		if err := h.notify.EmitEvent(r.Context(), "order.status_changed", map[string]any{
			"order_id":     updated.ID,
			"customer_id":  updated.CustomerID,
			"from":         order.Status,
			"to":           updated.Status,
			"status":       updated.Status,
			"order_number": "ORD-" + updated.PublicID.String()[:8],
			"grand_total":  updated.GrandTotal,
		}); err != nil {
			slog.WarnContext(r.Context(), "emit domain event failed", "event", "order.status_changed", "order_id", updated.ID, "err", err)
		}
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
func (h *Handler) addOrderItemSnapshot(ctx context.Context, q *gen.Queries, orgID, orderID, productID int64, qty, unit, unitPrice, rowTotal, taxAmount string) error {
	p, err := q.GetProductByID(ctx, gen.GetProductByIDParams{OrganizationID: orgID, ID: productID})
	sku, name := "", ""
	if err == nil {
		sku, name = p.Sku, p.Name
	}
	if taxAmount == "" {
		taxAmount = "0"
	}
	_, err = q.AddOrderItem(ctx, gen.AddOrderItemParams{
		OrderID: orderID, ProductID: productID, Sku: sku, Name: name,
		Quantity: qty, Unit: unit, UnitPrice: unitPrice, TaxAmount: taxAmount, RowTotal: rowTotal,
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

// emailOrderConfirmation enqueues an order-confirmation email to the customer's
// primary contact (no-op when no notifier is wired or the customer has no users).
func (h *Handler) emailOrderConfirmation(ctx context.Context, order gen.Order) {
	if h.notify == nil {
		return
	}
	to, name := h.primaryContact(ctx, order.CustomerID)
	if to == "" {
		return
	}
	if err := h.notify.EnqueueEmail(ctx, to, "order_confirmation", map[string]any{
		"name":         name,
		"order_number": "ORD-" + order.PublicID.String()[:8],
		"total":        order.GrandTotal,
		"currency":     order.Currency,
	}); err != nil {
		slog.WarnContext(ctx, "enqueue email failed", "template", "order_confirmation", "order_id", order.ID, "err", err)
	}
}

func strPtr(s string) *string { return &s }

func datePtr(t *time.Time) pgtype.Date {
	if t == nil {
		return pgtype.Date{}
	}
	return pgtype.Date{Time: *t, Valid: true}
}

// errBudgetExceeded aborts an order-placement tx when a cost-center budget would
// be exceeded (mapped to 422 by the caller).
var errBudgetExceeded = errors.New("budget exceeded")

// periodStart returns the start of the current budget period.
func periodStart(period string, now time.Time) time.Time {
	y, m, _ := now.Date()
	loc := now.Location()
	switch period {
	case "annual":
		return time.Date(y, 1, 1, 0, 0, 0, 0, loc)
	case "quarterly":
		qm := time.Month((int(m)-1)/3*3 + 1)
		return time.Date(y, qm, 1, 0, 0, 0, 0, loc)
	default: // monthly
		return time.Date(y, m, 1, 0, 0, 0, 0, loc)
	}
}
