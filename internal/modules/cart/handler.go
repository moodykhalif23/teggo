// Package cart implements the cart & shopping-list module (Implementation Pack
// 1 §5). It is storefront-facing: every route is scoped to the authenticated
// customer-user's buying company (from the storefront token), and line prices
// are resolved live from price lists + assignments at add-time and re-validated
// on demand. No price resolved => "price on request" (RFQ path).
package cart

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"b2bcommerce/internal/fx"
	"b2bcommerce/internal/money"
	"b2bcommerce/internal/pricing"
	"b2bcommerce/internal/promotions"
	mw "b2bcommerce/internal/server/middleware"
	"b2bcommerce/internal/server/response"
	"b2bcommerce/internal/store/gen"
)

// cartPromoCandidates maps active promotion rows into the pure engine's input.
func cartPromoCandidates(rows []gen.Promotion) []promotions.Candidate {
	out := make([]promotions.Candidate, len(rows))
	for i, p := range rows {
		var starts, ends *time.Time
		if p.StartsAt.Valid {
			t := p.StartsAt.Time
			starts = &t
		}
		if p.EndsAt.Valid {
			t := p.EndsAt.Time
			ends = &t
		}
		out[i] = promotions.Candidate{
			ID: p.ID, Name: p.Name, Code: p.Code, DiscountType: p.DiscountType,
			DiscountValue: p.DiscountValue, MinSubtotal: p.MinSubtotal,
			StartsAt: starts, EndsAt: ends,
			MaxRedemptions: p.MaxRedemptions, TimesRedeemed: p.TimesRedeemed, Priority: p.Priority,
		}
	}
	return out
}

type Handler struct {
	q *gen.Queries
}

func New(q *gen.Queries) *Handler { return &Handler{q: q} }

func (h *Handler) Routes(r chi.Router, authMW func(http.Handler) http.Handler) {
	r.Group(func(sr chi.Router) {
		sr.Use(authMW)
		sr.Use(mw.RequireAudience("storefront"))

		sr.Get("/storefront/cart", h.getCart)
		sr.Post("/storefront/cart/items", h.addItem)
		sr.Patch("/storefront/cart/items/{id}", h.updateItem)
		sr.Delete("/storefront/cart/items/{id}", h.removeItem)
		sr.Post("/storefront/cart/revalidate", h.revalidate)
		sr.Post("/storefront/cart/reorder", h.reorder)
		sr.Post("/storefront/cart/bulk", h.addBulk)
		sr.Post("/storefront/cart/coupon", h.applyCoupon)
		sr.Delete("/storefront/cart/coupon", h.removeCoupon)
		sr.Get("/storefront/currencies", h.currencies)
		sr.Get("/storefront/products/{slug}/pricing", h.productPricing)

		sr.Get("/storefront/shopping-lists", h.listLists)
		sr.Post("/storefront/shopping-lists", h.createList)
		sr.Patch("/storefront/shopping-lists/{id}", h.renameList)
		sr.Delete("/storefront/shopping-lists/{id}", h.deleteList)
		sr.Get("/storefront/shopping-lists/{id}/items", h.listListItems)
		sr.Post("/storefront/shopping-lists/{id}/items", h.addListItem)
		sr.Patch("/storefront/shopping-lists/{id}/items/{itemID}", h.updateListItem)
		sr.Delete("/storefront/shopping-lists/{id}/items/{itemID}", h.removeListItem)
		sr.Post("/storefront/shopping-lists/{id}/convert-to-cart", h.convertList)
	})
}

// principal is the authenticated customer-user context.
type principal struct {
	orgID          int64
	customerID     int64
	customerUserID *int64
}

func actor(r *http.Request) (principal, bool) {
	c, ok := mw.ClaimsFrom(r.Context())
	if !ok || c.CustomerID == 0 {
		return principal{}, false
	}
	p := principal{orgID: c.OrgID, customerID: c.CustomerID}
	if id, err := strconv.ParseInt(c.Subject, 10, 64); err == nil && id != 0 {
		p.customerUserID = &id
	}
	return p, true
}

// activeCart returns the customer's active cart, creating one (in the org's
// default website + currency) if none exists.
func (h *Handler) activeCart(r *http.Request, p principal) (gen.Cart, error) {
	ws, err := h.q.GetDefaultWebsite(r.Context(), p.orgID)
	if err != nil {
		return gen.Cart{}, err
	}
	c, err := h.q.GetActiveCart(r.Context(), gen.GetActiveCartParams{CustomerID: p.customerID, WebsiteID: ws.ID})
	if err == nil {
		return c, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return gen.Cart{}, err
	}
	return h.q.CreateCart(r.Context(), gen.CreateCartParams{
		CustomerID: p.customerID, CustomerUserID: p.customerUserID,
		WebsiteID: ws.ID, Currency: ws.DefaultCurrency,
	})
}

// resolvePrice resolves the customer's contract price for a product at a
// quantity, live from price lists + assignments (no cache). Returns ("", false)
// when nothing resolves (price-on-request).
func (h *Handler) resolvePrice(r *http.Request, p principal, websiteID int64, productID int64, unit, qty, currency string) (string, bool, error) {
	wid := websiteID
	row, err := h.q.ResolvePriceTier(r.Context(), gen.ResolvePriceTierParams{
		ID: p.customerID, ProductID: productID, Unit: unit, Column4: qty, Currency: currency,
		WebsiteID: &wid, ValidFrom: pgtype.Timestamptz{Time: time.Now(), Valid: true},
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	adjusted, err := h.applyPriceRules(r, p, productID, row.Value)
	if err != nil {
		return "", false, err
	}
	return adjusted, true, nil
}

// applyPriceRules adjusts a resolved base price by the org's active
// price-adjustment rules (PRD §7.2), matched on the buyer's customer group and
// the product's attributes. No rules configured -> base returned unchanged.
func (h *Handler) applyPriceRules(r *http.Request, p principal, productID int64, base string) (string, error) {
	rules, err := h.q.ListActivePriceAdjustmentRules(r.Context(), p.orgID)
	if err != nil {
		return "", err
	}
	if len(rules) == 0 {
		return base, nil
	}
	eng := make([]pricing.Rule, len(rules))
	for i, ru := range rules {
		eng[i] = pricing.Rule{
			CustomerGroupID: ru.CustomerGroupID, AttributeKey: ru.AttributeKey, AttributeValue: ru.AttributeValue,
			AdjustmentType: ru.AdjustmentType, AdjustmentValue: ru.AdjustmentValue, Priority: ru.Priority,
		}
	}
	attrs := map[string]string{}
	if prod, err := h.q.GetProductByID(r.Context(), gen.GetProductByIDParams{OrganizationID: p.orgID, ID: productID}); err == nil {
		var m map[string]any
		if json.Unmarshal(prod.Attributes, &m) == nil {
			for k, v := range m {
				attrs[k] = fmt.Sprintf("%v", v)
			}
		}
	}
	var grp *int64
	if cu, err := h.q.GetCustomer(r.Context(), gen.GetCustomerParams{OrganizationID: p.orgID, ID: p.customerID}); err == nil {
		grp = cu.CustomerGroupID
	}
	return pricing.Apply(base, attrs, grp, eng)
}

// ---- Cart ----------------------------------------------------------------

type cartItemDTO struct {
	ID        int64  `json:"id"`
	ProductID int64  `json:"product_id"`
	SKU       string `json:"sku"`
	Name      string `json:"name"`
	Quantity  string `json:"quantity"`
	Unit      string `json:"unit"`
	UnitPrice string `json:"unit_price"`
	RowTotal  string `json:"row_total"`
}

func (h *Handler) renderCart(w http.ResponseWriter, r *http.Request, p principal, c gen.Cart) {
	rows, err := h.q.ListCartItems(r.Context(), c.ID)
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not load cart")
		return
	}
	items := make([]cartItemDTO, 0, len(rows))
	totals := make([]string, 0, len(rows))
	for _, it := range rows {
		rt, err := money.LineTotal(it.Quantity, it.UnitPrice)
		if err != nil {
			response.Fail(w, http.StatusInternalServerError, "internal", "bad line amount")
			return
		}
		totals = append(totals, rt)
		items = append(items, cartItemDTO{
			ID: it.ID, ProductID: it.ProductID, SKU: it.Sku, Name: it.Name,
			Quantity: it.Quantity, Unit: it.Unit, UnitPrice: it.UnitPrice, RowTotal: rt,
		})
	}
	subtotal, _ := money.Sum(totals...)

	// Promotions: preview the best applicable discount (automatic, or the coupon
	// on the cart). The discount is re-evaluated and locked at checkout.
	discount := "0"
	discountLabel := ""
	code := ""
	if c.CouponCode != nil {
		code = *c.CouponCode
	}
	if cands, e := h.q.ListActivePromotions(r.Context(), p.orgID); e == nil && len(cands) > 0 {
		if res := promotions.Evaluate(subtotal, code, time.Now(), cartPromoCandidates(cands)); res.Promotion != nil {
			discount = res.Discount
			discountLabel = res.Label
		}
	}
	grand, _ := money.Sub(subtotal, discount)

	resp := map[string]any{
		"public_id":       c.PublicID.String(),
		"currency":        c.Currency,
		"items":           items,
		"subtotal":        subtotal,
		"discount_amount": discount,
		"grand_total":     grand,
	}
	if c.CouponCode != nil {
		resp["coupon_code"] = *c.CouponCode
	}
	if discountLabel != "" {
		resp["discount_label"] = discountLabel
	}

	// Optional display-currency conversion (indicative; the cart transacts in its
	// own currency). ?currency=USD converts the totals via the latest FX rate.
	if dc := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("currency"))); dc != "" && dc != c.Currency {
		if rate, ok, _ := fx.NewService(h.q).Rate(r.Context(), p.orgID, c.Currency, dc); ok {
			ds, _ := fx.Convert(subtotal, rate)
			dd, _ := fx.Convert(discount, rate)
			dg, _ := fx.Convert(grand, rate)
			resp["display"] = map[string]any{
				"currency": dc, "rate": rate, "subtotal": ds, "discount_amount": dd, "grand_total": dg,
			}
		}
	}
	response.JSON(w, http.StatusOK, resp)
}

// currencies lists the display currencies available from the org's base currency
// (for the storefront currency selector).
func (h *Handler) currencies(w http.ResponseWriter, r *http.Request) {
	p, ok := actor(r)
	if !ok {
		response.Fail(w, http.StatusUnauthorized, "unauthorized", "no customer context")
		return
	}
	ws, err := h.q.GetDefaultWebsite(r.Context(), p.orgID)
	if err != nil {
		response.Fail(w, http.StatusBadRequest, "bad_request", "no website configured")
		return
	}
	codes, err := h.q.ListQuoteCurrencies(r.Context(), gen.ListQuoteCurrenciesParams{OrganizationID: p.orgID, BaseCurrency: ws.DefaultCurrency})
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not list currencies")
		return
	}
	if codes == nil {
		codes = []string{}
	}
	response.JSON(w, http.StatusOK, map[string]any{"base": ws.DefaultCurrency, "currencies": codes})
}

// applyCoupon attaches a coupon code to the cart after validating it names an
// active, in-window, under-cap promotion. The discount is previewed by renderCart
// and locked at checkout. A code below its minimum subtotal is still accepted —
// it simply yields no discount until the cart qualifies.
func (h *Handler) applyCoupon(w http.ResponseWriter, r *http.Request) {
	p, ok := actor(r)
	if !ok {
		response.Fail(w, http.StatusUnauthorized, "unauthorized", "no customer context")
		return
	}
	var req struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Code) == "" {
		response.Fail(w, http.StatusBadRequest, "bad_request", "coupon code is required")
		return
	}
	c, err := h.activeCart(r, p)
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not load cart")
		return
	}
	cands, _ := h.q.ListActivePromotions(r.Context(), p.orgID)
	now := time.Now()
	want := strings.ToLower(strings.TrimSpace(req.Code))
	canonical := ""
	for _, pr := range cands {
		if pr.Code == nil || strings.ToLower(strings.TrimSpace(*pr.Code)) != want {
			continue
		}
		if pr.StartsAt.Valid && now.Before(pr.StartsAt.Time) {
			continue
		}
		if pr.EndsAt.Valid && now.After(pr.EndsAt.Time) {
			continue
		}
		if pr.MaxRedemptions != nil && pr.TimesRedeemed >= *pr.MaxRedemptions {
			continue
		}
		canonical = strings.TrimSpace(*pr.Code) // store the promotion's own casing
		break
	}
	if canonical == "" {
		response.Fail(w, http.StatusUnprocessableEntity, "invalid_coupon", "that coupon code isn't valid")
		return
	}
	if err := h.q.SetCartCoupon(r.Context(), gen.SetCartCouponParams{ID: c.ID, CouponCode: &canonical}); err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not apply coupon")
		return
	}
	c.CouponCode = &canonical
	h.renderCart(w, r, p, c)
}

// removeCoupon clears any coupon code from the cart.
func (h *Handler) removeCoupon(w http.ResponseWriter, r *http.Request) {
	p, ok := actor(r)
	if !ok {
		response.Fail(w, http.StatusUnauthorized, "unauthorized", "no customer context")
		return
	}
	c, err := h.activeCart(r, p)
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not load cart")
		return
	}
	if err := h.q.SetCartCoupon(r.Context(), gen.SetCartCouponParams{ID: c.ID, CouponCode: nil}); err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not remove coupon")
		return
	}
	c.CouponCode = nil
	h.renderCart(w, r, p, c)
}

func (h *Handler) getCart(w http.ResponseWriter, r *http.Request) {
	p, ok := actor(r)
	if !ok {
		response.Fail(w, http.StatusUnauthorized, "unauthorized", "no customer context")
		return
	}
	c, err := h.activeCart(r, p)
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not load cart")
		return
	}
	h.renderCart(w, r, p, c)
}

func (h *Handler) addItem(w http.ResponseWriter, r *http.Request) {
	p, ok := actor(r)
	if !ok {
		response.Fail(w, http.StatusUnauthorized, "unauthorized", "no customer context")
		return
	}
	var req struct {
		ProductPublicID string `json:"product_public_id"`
		Quantity        string `json:"quantity"`
		Unit            string `json:"unit"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Fail(w, http.StatusBadRequest, "bad_request", "invalid body")
		return
	}
	pid, err := uuid.Parse(req.ProductPublicID)
	if err != nil {
		response.Fail(w, http.StatusBadRequest, "bad_request", "valid product_public_id required")
		return
	}
	if req.Quantity == "" {
		req.Quantity = "1"
	}
	if req.Unit == "" {
		req.Unit = "each"
	}
	productID, err := h.q.GetBuyableProductIDByPublicID(r.Context(), gen.GetBuyableProductIDByPublicIDParams{OrganizationID: p.orgID, PublicID: pid})
	if err != nil {
		response.Fail(w, http.StatusNotFound, "not_found", "product not found")
		return
	}

	c, err := h.activeCart(r, p)
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not load cart")
		return
	}

	price, ok, err := h.resolvePrice(r, p, c.WebsiteID, productID, req.Unit, req.Quantity, c.Currency)
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not resolve price")
		return
	}
	if !ok {
		response.Fail(w, http.StatusConflict, "price_on_request", "no price for this product; request a quote")
		return
	}

	if _, err := h.q.UpsertCartItem(r.Context(), gen.UpsertCartItemParams{
		CartID: c.ID, ProductID: productID, Quantity: req.Quantity, Unit: req.Unit, UnitPrice: price,
	}); err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not add item")
		return
	}
	h.renderCart(w, r, p, c)
}

func (h *Handler) updateItem(w http.ResponseWriter, r *http.Request) {
	p, ok := actor(r)
	if !ok {
		response.Fail(w, http.StatusUnauthorized, "unauthorized", "no customer context")
		return
	}
	itemID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		response.Fail(w, http.StatusBadRequest, "bad_request", "invalid id")
		return
	}
	var req struct {
		Quantity string `json:"quantity"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Quantity == "" {
		response.Fail(w, http.StatusBadRequest, "bad_request", "quantity required")
		return
	}
	c, err := h.activeCart(r, p)
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not load cart")
		return
	}
	if _, err := h.q.UpdateCartItemQuantity(r.Context(), gen.UpdateCartItemQuantityParams{ID: itemID, CartID: c.ID, Quantity: req.Quantity}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			response.Fail(w, http.StatusNotFound, "not_found", "cart item not found")
			return
		}
		response.Fail(w, http.StatusInternalServerError, "internal", "could not update item")
		return
	}
	h.renderCart(w, r, p, c)
}

func (h *Handler) removeItem(w http.ResponseWriter, r *http.Request) {
	p, ok := actor(r)
	if !ok {
		response.Fail(w, http.StatusUnauthorized, "unauthorized", "no customer context")
		return
	}
	itemID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		response.Fail(w, http.StatusBadRequest, "bad_request", "invalid id")
		return
	}
	c, err := h.activeCart(r, p)
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not load cart")
		return
	}
	n, err := h.q.DeleteCartItem(r.Context(), gen.DeleteCartItemParams{ID: itemID, CartID: c.ID})
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not remove item")
		return
	}
	if n == 0 {
		response.Fail(w, http.StatusNotFound, "not_found", "cart item not found")
		return
	}
	h.renderCart(w, r, p, c)
}

// revalidate re-resolves each line's price against the current combined_prices
// (price drift is handled gracefully — the cart is updated and the drift
// reported, not rejected).
func (h *Handler) revalidate(w http.ResponseWriter, r *http.Request) {
	p, ok := actor(r)
	if !ok {
		response.Fail(w, http.StatusUnauthorized, "unauthorized", "no customer context")
		return
	}
	c, err := h.activeCart(r, p)
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not load cart")
		return
	}
	rows, err := h.q.ListCartItems(r.Context(), c.ID)
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not load cart")
		return
	}
	type drift struct {
		ItemID   int64  `json:"item_id"`
		OldPrice string `json:"old_price"`
		NewPrice string `json:"new_price"`
	}
	changes := []drift{}
	for _, it := range rows {
		price, ok, err := h.resolvePrice(r, p, c.WebsiteID, it.ProductID, it.Unit, it.Quantity, c.Currency)
		if err != nil {
			response.Fail(w, http.StatusInternalServerError, "internal", "could not resolve price")
			return
		}
		if !ok || price == it.UnitPrice {
			continue
		}
		if err := h.q.UpdateCartItemPrice(r.Context(), gen.UpdateCartItemPriceParams{ID: it.ID, CartID: c.ID, UnitPrice: price}); err != nil {
			response.Fail(w, http.StatusInternalServerError, "internal", "could not update price")
			return
		}
		changes = append(changes, drift{ItemID: it.ID, OldPrice: it.UnitPrice, NewPrice: price})
	}
	response.JSON(w, http.StatusOK, map[string]any{"changed": changes})
}

// reorder copies the line items of a previously placed order into the active
// cart, re-resolving current prices (an order's historical price is a snapshot;
// the cart must reflect the buyer's current contract price). Items with no
// resolvable price, or whose product no longer exists, are skipped and their
// SKUs reported. Mirrors convertList.
func (h *Handler) reorder(w http.ResponseWriter, r *http.Request) {
	p, ok := actor(r)
	if !ok {
		response.Fail(w, http.StatusUnauthorized, "unauthorized", "no customer context")
		return
	}
	var req struct {
		OrderPublicID string `json:"order_public_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Fail(w, http.StatusBadRequest, "bad_request", "invalid body")
		return
	}
	oid, err := uuid.Parse(req.OrderPublicID)
	if err != nil {
		response.Fail(w, http.StatusBadRequest, "bad_request", "valid order_public_id required")
		return
	}
	order, err := h.q.GetOrderByPublicID(r.Context(), oid)
	if err != nil || order.CustomerID != p.customerID {
		// Scope to the buying company — never leak another customer's order.
		response.Fail(w, http.StatusNotFound, "not_found", "order not found")
		return
	}
	items, err := h.q.ListOrderItems(r.Context(), order.ID)
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not load order items")
		return
	}
	c, err := h.activeCart(r, p)
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not load cart")
		return
	}
	skipped := []string{}
	for _, it := range items {
		price, priced, err := h.resolvePrice(r, p, c.WebsiteID, it.ProductID, it.Unit, it.Quantity, c.Currency)
		if err != nil {
			response.Fail(w, http.StatusInternalServerError, "internal", "could not resolve price")
			return
		}
		if !priced {
			skipped = append(skipped, it.Sku)
			continue
		}
		if _, err := h.q.UpsertCartItem(r.Context(), gen.UpsertCartItemParams{
			CartID: c.ID, ProductID: it.ProductID, Quantity: it.Quantity, Unit: it.Unit, UnitPrice: price,
		}); err != nil {
			response.Fail(w, http.StatusInternalServerError, "internal", "could not add item to cart")
			return
		}
	}
	response.JSON(w, http.StatusOK, map[string]any{"cart_public_id": c.PublicID.String(), "skipped_skus": skipped})
}

// addBulk is the quick-order path: a buyer pastes / uploads a list of SKUs and
// quantities and the whole batch is added to the active cart in one call.
// Unknown SKUs and price-on-request items are reported per line rather than
// failing the whole request, so the buyer can fix only the offending rows.
func (h *Handler) addBulk(w http.ResponseWriter, r *http.Request) {
	p, ok := actor(r)
	if !ok {
		response.Fail(w, http.StatusUnauthorized, "unauthorized", "no customer context")
		return
	}
	var req struct {
		Lines []struct {
			SKU      string `json:"sku"`
			Quantity string `json:"quantity"`
			Unit     string `json:"unit"`
		} `json:"lines"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Fail(w, http.StatusBadRequest, "bad_request", "invalid body")
		return
	}
	if len(req.Lines) == 0 {
		response.Fail(w, http.StatusBadRequest, "bad_request", "at least one line required")
		return
	}
	c, err := h.activeCart(r, p)
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not load cart")
		return
	}
	added := 0
	notFound := []string{}
	priceOnRequest := []string{}
	for _, ln := range req.Lines {
		sku := strings.TrimSpace(ln.SKU)
		if sku == "" {
			continue
		}
		qty := strings.TrimSpace(ln.Quantity)
		if qty == "" {
			qty = "1"
		}
		unit := strings.TrimSpace(ln.Unit)
		if unit == "" {
			unit = "each"
		}
		prod, err := h.q.GetProductBySKU(r.Context(), gen.GetProductBySKUParams{OrganizationID: p.orgID, Sku: sku})
		if err != nil || prod.ApprovalStatus != "approved" {
			// Unknown SKU, or an unapproved vendor listing: not buyable.
			notFound = append(notFound, sku)
			continue
		}
		price, priced, err := h.resolvePrice(r, p, c.WebsiteID, prod.ID, unit, qty, c.Currency)
		if err != nil {
			response.Fail(w, http.StatusInternalServerError, "internal", "could not resolve price")
			return
		}
		if !priced {
			priceOnRequest = append(priceOnRequest, sku)
			continue
		}
		if _, err := h.q.UpsertCartItem(r.Context(), gen.UpsertCartItemParams{
			CartID: c.ID, ProductID: prod.ID, Quantity: qty, Unit: unit, UnitPrice: price,
		}); err != nil {
			response.Fail(w, http.StatusInternalServerError, "internal", "could not add item")
			return
		}
		added++
	}
	response.JSON(w, http.StatusOK, map[string]any{
		"cart_public_id":   c.PublicID.String(),
		"added":            added,
		"not_found_skus":   notFound,
		"price_on_request": priceOnRequest,
	})
}

// productPricing returns the authenticated buyer's contract price tiers for a
// product (the volume breaks resolved into combined_prices). An empty tier list
// means price-on-request for this buyer. Lets the storefront show "buy N+ at X".
func (h *Handler) productPricing(w http.ResponseWriter, r *http.Request) {
	p, ok := actor(r)
	if !ok {
		response.Fail(w, http.StatusUnauthorized, "unauthorized", "no customer context")
		return
	}
	slug := chi.URLParam(r, "slug")
	ws, err := h.q.GetDefaultWebsite(r.Context(), p.orgID)
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "no website configured")
		return
	}
	rows, err := h.q.ResolvePriceTiersForSlug(r.Context(), gen.ResolvePriceTiersForSlugParams{
		ID: p.customerID, Slug: slug, OrganizationID: p.orgID, Currency: ws.DefaultCurrency,
		WebsiteID: &ws.ID, ValidFrom: pgtype.Timestamptz{Time: time.Now(), Valid: true},
	})
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not load pricing")
		return
	}
	type tier struct {
		Unit        string `json:"unit"`
		MinQuantity string `json:"min_quantity"`
		Value       string `json:"value"`
	}
	tiers := make([]tier, 0, len(rows))
	for _, row := range rows {
		tiers = append(tiers, tier{Unit: row.Unit, MinQuantity: row.MinQuantity, Value: row.Value})
	}
	response.JSON(w, http.StatusOK, map[string]any{
		"currency":         ws.DefaultCurrency,
		"price_on_request": len(tiers) == 0,
		"tiers":            tiers,
	})
}

// ---- Shopping lists ------------------------------------------------------

func (h *Handler) listLists(w http.ResponseWriter, r *http.Request) {
	p, ok := actor(r)
	if !ok {
		response.Fail(w, http.StatusUnauthorized, "unauthorized", "no customer context")
		return
	}
	rows, err := h.q.ListShoppingLists(r.Context(), p.customerID)
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not list shopping lists")
		return
	}
	if rows == nil {
		rows = []gen.ShoppingList{}
	}
	response.JSON(w, http.StatusOK, map[string]any{"items": rows})
}

func (h *Handler) createList(w http.ResponseWriter, r *http.Request) {
	p, ok := actor(r)
	if !ok {
		response.Fail(w, http.StatusUnauthorized, "unauthorized", "no customer context")
		return
	}
	var req struct {
		Name      string `json:"name"`
		IsDefault bool   `json:"is_default"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" {
		response.Fail(w, http.StatusBadRequest, "bad_request", "name required")
		return
	}
	l, err := h.q.CreateShoppingList(r.Context(), gen.CreateShoppingListParams{
		CustomerID: p.customerID, CustomerUserID: p.customerUserID, Name: req.Name, IsDefault: req.IsDefault,
	})
	if err != nil {
		// The partial unique index rejects a second default list.
		response.Fail(w, http.StatusConflict, "conflict", "could not create list (a default may already exist)")
		return
	}
	response.JSON(w, http.StatusCreated, l)
}

func (h *Handler) loadList(w http.ResponseWriter, r *http.Request, p principal) (gen.ShoppingList, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		response.Fail(w, http.StatusBadRequest, "bad_request", "invalid id")
		return gen.ShoppingList{}, false
	}
	l, err := h.q.GetShoppingList(r.Context(), gen.GetShoppingListParams{ID: id, CustomerID: p.customerID})
	if err != nil {
		response.Fail(w, http.StatusNotFound, "not_found", "shopping list not found")
		return gen.ShoppingList{}, false
	}
	return l, true
}

func (h *Handler) listListItems(w http.ResponseWriter, r *http.Request) {
	p, ok := actor(r)
	if !ok {
		response.Fail(w, http.StatusUnauthorized, "unauthorized", "no customer context")
		return
	}
	l, ok := h.loadList(w, r, p)
	if !ok {
		return
	}
	rows, err := h.q.ListShoppingListItems(r.Context(), l.ID)
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not list items")
		return
	}
	if rows == nil {
		rows = []gen.ListShoppingListItemsRow{}
	}
	response.JSON(w, http.StatusOK, map[string]any{"items": rows})
}

func (h *Handler) addListItem(w http.ResponseWriter, r *http.Request) {
	p, ok := actor(r)
	if !ok {
		response.Fail(w, http.StatusUnauthorized, "unauthorized", "no customer context")
		return
	}
	l, ok := h.loadList(w, r, p)
	if !ok {
		return
	}
	var req struct {
		ProductPublicID string `json:"product_public_id"`
		Quantity        string `json:"quantity"`
		Unit            string `json:"unit"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Fail(w, http.StatusBadRequest, "bad_request", "invalid body")
		return
	}
	pid, err := uuid.Parse(req.ProductPublicID)
	if err != nil {
		response.Fail(w, http.StatusBadRequest, "bad_request", "valid product_public_id required")
		return
	}
	if req.Quantity == "" {
		req.Quantity = "1"
	}
	if req.Unit == "" {
		req.Unit = "each"
	}
	productID, err := h.q.GetBuyableProductIDByPublicID(r.Context(), gen.GetBuyableProductIDByPublicIDParams{OrganizationID: p.orgID, PublicID: pid})
	if err != nil {
		response.Fail(w, http.StatusNotFound, "not_found", "product not found")
		return
	}
	item, err := h.q.UpsertShoppingListItem(r.Context(), gen.UpsertShoppingListItemParams{
		ShoppingListID: l.ID, ProductID: productID, Quantity: req.Quantity, Unit: req.Unit,
	})
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not add item")
		return
	}
	response.JSON(w, http.StatusCreated, item)
}

func (h *Handler) updateListItem(w http.ResponseWriter, r *http.Request) {
	p, ok := actor(r)
	if !ok {
		response.Fail(w, http.StatusUnauthorized, "unauthorized", "no customer context")
		return
	}
	l, ok := h.loadList(w, r, p)
	if !ok {
		return
	}
	itemID, err := strconv.ParseInt(chi.URLParam(r, "itemID"), 10, 64)
	if err != nil {
		response.Fail(w, http.StatusBadRequest, "bad_request", "invalid item id")
		return
	}
	var req struct {
		Quantity string `json:"quantity"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Quantity == "" {
		response.Fail(w, http.StatusBadRequest, "bad_request", "quantity required")
		return
	}
	item, err := h.q.UpdateShoppingListItem(r.Context(), gen.UpdateShoppingListItemParams{ID: itemID, ShoppingListID: l.ID, Quantity: req.Quantity})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			response.Fail(w, http.StatusNotFound, "not_found", "list item not found")
			return
		}
		response.Fail(w, http.StatusInternalServerError, "internal", "could not update item")
		return
	}
	response.JSON(w, http.StatusOK, item)
}

func (h *Handler) removeListItem(w http.ResponseWriter, r *http.Request) {
	p, ok := actor(r)
	if !ok {
		response.Fail(w, http.StatusUnauthorized, "unauthorized", "no customer context")
		return
	}
	l, ok := h.loadList(w, r, p)
	if !ok {
		return
	}
	itemID, err := strconv.ParseInt(chi.URLParam(r, "itemID"), 10, 64)
	if err != nil {
		response.Fail(w, http.StatusBadRequest, "bad_request", "invalid item id")
		return
	}
	n, err := h.q.DeleteShoppingListItem(r.Context(), gen.DeleteShoppingListItemParams{ID: itemID, ShoppingListID: l.ID})
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not remove item")
		return
	}
	if n == 0 {
		response.Fail(w, http.StatusNotFound, "not_found", "list item not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) renameList(w http.ResponseWriter, r *http.Request) {
	p, ok := actor(r)
	if !ok {
		response.Fail(w, http.StatusUnauthorized, "unauthorized", "no customer context")
		return
	}
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		response.Fail(w, http.StatusBadRequest, "bad_request", "invalid id")
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Name) == "" {
		response.Fail(w, http.StatusBadRequest, "bad_request", "name required")
		return
	}
	l, err := h.q.RenameShoppingList(r.Context(), gen.RenameShoppingListParams{ID: id, CustomerID: p.customerID, Name: strings.TrimSpace(req.Name)})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			response.Fail(w, http.StatusNotFound, "not_found", "shopping list not found")
			return
		}
		response.Fail(w, http.StatusInternalServerError, "internal", "could not rename list")
		return
	}
	response.JSON(w, http.StatusOK, l)
}

func (h *Handler) deleteList(w http.ResponseWriter, r *http.Request) {
	p, ok := actor(r)
	if !ok {
		response.Fail(w, http.StatusUnauthorized, "unauthorized", "no customer context")
		return
	}
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		response.Fail(w, http.StatusBadRequest, "bad_request", "invalid id")
		return
	}
	n, err := h.q.DeleteShoppingList(r.Context(), gen.DeleteShoppingListParams{ID: id, CustomerID: p.customerID})
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not delete list")
		return
	}
	if n == 0 {
		response.Fail(w, http.StatusNotFound, "not_found", "shopping list not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// convertList copies a shopping list into the active cart, resolving current
// prices. Items with no resolvable price are skipped and reported.
func (h *Handler) convertList(w http.ResponseWriter, r *http.Request) {
	p, ok := actor(r)
	if !ok {
		response.Fail(w, http.StatusUnauthorized, "unauthorized", "no customer context")
		return
	}
	l, ok := h.loadList(w, r, p)
	if !ok {
		return
	}
	items, err := h.q.ListShoppingListItems(r.Context(), l.ID)
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not load list items")
		return
	}
	c, err := h.activeCart(r, p)
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not load cart")
		return
	}
	skipped := []int64{}
	for _, it := range items {
		price, priced, err := h.resolvePrice(r, p, c.WebsiteID, it.ProductID, it.Unit, it.Quantity, c.Currency)
		if err != nil {
			response.Fail(w, http.StatusInternalServerError, "internal", "could not resolve price")
			return
		}
		if !priced {
			skipped = append(skipped, it.ProductID)
			continue
		}
		if _, err := h.q.UpsertCartItem(r.Context(), gen.UpsertCartItemParams{
			CartID: c.ID, ProductID: it.ProductID, Quantity: it.Quantity, Unit: it.Unit, UnitPrice: price,
		}); err != nil {
			response.Fail(w, http.StatusInternalServerError, "internal", "could not add item to cart")
			return
		}
	}
	if skipped == nil {
		skipped = []int64{}
	}
	response.JSON(w, http.StatusOK, map[string]any{"cart_public_id": c.PublicID.String(), "skipped_product_ids": skipped})
}
