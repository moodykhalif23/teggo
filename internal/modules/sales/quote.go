package sales

import (
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

type quoteLineInput struct {
	ProductID int64  `json:"product_id"`
	Quantity  string `json:"quantity"`
	Unit      string `json:"unit"`
	UnitPrice string `json:"unit_price"`
	Discount  string `json:"discount"`
}

// ---- admin: build & send -------------------------------------------------

// quoteFromRFQ creates a draft quote from a submitted RFQ, copying its items
// 1:1 and seeding unit prices from the customer's combined_prices (the rep then
// edits). Pack 1 §6 AC.
func (h *Handler) quoteFromRFQ(w http.ResponseWriter, r *http.Request) {
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
	rfq, err := h.q.GetRFQByID(r.Context(), gen.GetRFQByIDParams{OrganizationID: a.orgID, ID: id})
	if err != nil {
		notFound(w, "RFQ")
		return
	}
	ws, err := h.q.GetDefaultWebsite(r.Context(), a.orgID)
	if err != nil {
		response.Fail(w, http.StatusBadRequest, "bad_request", "no website configured")
		return
	}
	rfqItems, err := h.q.ListRFQItems(r.Context(), rfq.ID)
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not load RFQ items")
		return
	}

	var quote gen.Quote
	err = h.tx(r.Context(), func(q *gen.Queries) error {
		var e error
		quote, e = q.CreateQuote(r.Context(), gen.CreateQuoteParams{
			OrganizationID: a.orgID, WebsiteID: ws.ID, CustomerID: rfq.CustomerID,
			RfqID: &rfq.ID, SalesRepUserID: a.userID, Currency: ws.DefaultCurrency,
		})
		if e != nil {
			return e
		}
		var totals []string
		for _, it := range rfqItems {
			price := "0.0000"
			cp, perr := q.GetCombinedPrice(r.Context(), gen.GetCombinedPriceParams{
				CustomerID: rfq.CustomerID, ProductID: it.ProductID, Unit: it.Unit, Column4: it.Quantity, Currency: ws.DefaultCurrency,
			})
			if perr == nil {
				price = cp.Value
			}
			rowTotal, _ := money.RowTotal(it.Quantity, price, "0")
			if _, e := q.AddQuoteItem(r.Context(), gen.AddQuoteItemParams{
				QuoteID: quote.ID, ProductID: it.ProductID, Quantity: it.Quantity, Unit: it.Unit,
				UnitPrice: price, Discount: "0", RowTotal: rowTotal,
			}); e != nil {
				return e
			}
			totals = append(totals, rowTotal)
		}
		subtotal, _ := money.Sum(totals...)
		return q.SetQuoteSubtotal(r.Context(), gen.SetQuoteSubtotalParams{ID: quote.ID, Subtotal: subtotal})
	})
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not create quote from RFQ")
		return
	}
	q2, _ := h.q.GetQuoteByID(r.Context(), gen.GetQuoteByIDParams{OrganizationID: a.orgID, ID: quote.ID})
	h.renderQuote(w, r, q2)
}

func (h *Handler) createQuote(w http.ResponseWriter, r *http.Request) {
	a, ok := admin(r)
	if !ok {
		unauthorized(w)
		return
	}
	var req struct {
		CustomerID int64            `json:"customer_id"`
		Currency   string           `json:"currency"`
		ValidUntil *time.Time       `json:"valid_until"`
		Items      []quoteLineInput `json:"items"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.CustomerID == 0 {
		response.Fail(w, http.StatusBadRequest, "bad_request", "customer_id is required")
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

	var quote gen.Quote
	err = h.tx(r.Context(), func(q *gen.Queries) error {
		var e error
		quote, e = q.CreateQuote(r.Context(), gen.CreateQuoteParams{
			OrganizationID: a.orgID, WebsiteID: ws.ID, CustomerID: req.CustomerID,
			RfqID: nil, SalesRepUserID: a.userID, Currency: req.Currency, ValidUntil: tsPtr(req.ValidUntil),
		})
		if e != nil {
			return e
		}
		return h.replaceQuoteItems(r, q, quote.ID, req.Items)
	})
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not create quote")
		return
	}
	q2, _ := h.q.GetQuoteByID(r.Context(), gen.GetQuoteByIDParams{OrganizationID: a.orgID, ID: quote.ID})
	h.renderQuote(w, r, q2)
}

func (h *Handler) editQuote(w http.ResponseWriter, r *http.Request) {
	a, ok := admin(r)
	if !ok {
		unauthorized(w)
		return
	}
	quote, ok := h.loadAdminQuote(w, r, a)
	if !ok {
		return
	}
	if quote.Status == "accepted" || quote.Status == "declined" || quote.Status == "expired" {
		response.Fail(w, http.StatusConflict, "invalid_state", "quote is final and cannot be edited")
		return
	}
	var req struct {
		Items []quoteLineInput `json:"items"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Fail(w, http.StatusBadRequest, "bad_request", "invalid body")
		return
	}
	if err := h.tx(r.Context(), func(q *gen.Queries) error {
		return h.replaceQuoteItems(r, q, quote.ID, req.Items)
	}); err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not edit quote")
		return
	}
	q2, _ := h.q.GetQuoteByID(r.Context(), gen.GetQuoteByIDParams{OrganizationID: a.orgID, ID: quote.ID})
	h.renderQuote(w, r, q2)
}

// sendQuote moves draft/sent/revised -> sent, bumps the version, and snapshots
// the quote into quote_revisions; if it came from an RFQ, the RFQ moves to quoted.
func (h *Handler) sendQuote(w http.ResponseWriter, r *http.Request) {
	a, ok := admin(r)
	if !ok {
		unauthorized(w)
		return
	}
	quote, ok := h.loadAdminQuote(w, r, a)
	if !ok {
		return
	}
	if quote.Status != "draft" && quote.Status != "sent" && quote.Status != "revised" {
		response.Fail(w, http.StatusConflict, "invalid_state", "quote cannot be sent from its current state")
		return
	}
	var req struct {
		ValidUntil *time.Time `json:"valid_until"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)

	var sent gen.Quote
	err := h.tx(r.Context(), func(q *gen.Queries) error {
		var e error
		sent, e = q.SendQuote(r.Context(), gen.SendQuoteParams{ID: quote.ID, ValidUntil: tsPtr(req.ValidUntil)})
		if e != nil {
			return e
		}
		items, e := q.ListQuoteItems(r.Context(), quote.ID)
		if e != nil {
			return e
		}
		snap, _ := json.Marshal(map[string]any{"quote": sent, "items": items})
		createdBy := "rep:0"
		if a.userID != nil {
			createdBy = "rep:" + itoa(*a.userID)
		}
		if e := q.CreateQuoteRevision(r.Context(), gen.CreateQuoteRevisionParams{
			QuoteID: sent.ID, Version: sent.Version, Snapshot: snap, CreatedBy: createdBy,
		}); e != nil {
			return e
		}
		if sent.RfqID != nil {
			if _, e := q.SetRFQStatus(r.Context(), gen.SetRFQStatusParams{ID: *sent.RfqID, Status: "quoted"}); e != nil {
				return e
			}
		}
		return nil
	})
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not send quote")
		return
	}
	if h.notify != nil {
		if to, name := h.primaryContact(r.Context(), sent.CustomerID); to != "" {
			_ = h.notify.EnqueueEmail(r.Context(), to, "quote_sent", map[string]any{
				"name":         name,
				"quote_number": "Q-" + sent.PublicID.String()[:8],
				"total":        sent.Subtotal,
				"currency":     sent.Currency,
			})
		}
	}
	h.renderQuote(w, r, sent)
}

func (h *Handler) adminListQuotes(w http.ResponseWriter, r *http.Request) {
	a, ok := admin(r)
	if !ok {
		unauthorized(w)
		return
	}
	rows, err := h.q.ListQuotesAdmin(r.Context(), gen.ListQuotesAdminParams{OrganizationID: a.orgID, Limit: 100, Offset: 0})
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not list quotes")
		return
	}
	if rows == nil {
		rows = []gen.Quote{}
	}
	response.JSON(w, http.StatusOK, map[string]any{"items": rows})
}

func (h *Handler) adminGetQuote(w http.ResponseWriter, r *http.Request) {
	a, ok := admin(r)
	if !ok {
		unauthorized(w)
		return
	}
	quote, ok := h.loadAdminQuote(w, r, a)
	if !ok {
		return
	}
	h.renderQuote(w, r, quote)
}

// ---- storefront ----------------------------------------------------------

func (h *Handler) listMyQuotes(w http.ResponseWriter, r *http.Request) {
	cc, ok := customer(r)
	if !ok {
		unauthorized(w)
		return
	}
	rows, err := h.q.ListQuotesForCustomer(r.Context(), cc.customerID)
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not list quotes")
		return
	}
	if rows == nil {
		rows = []gen.Quote{}
	}
	response.JSON(w, http.StatusOK, map[string]any{"items": rows})
}

func (h *Handler) getMyQuote(w http.ResponseWriter, r *http.Request) {
	cc, ok := customer(r)
	if !ok {
		unauthorized(w)
		return
	}
	quote, ok := h.loadMyQuote(w, r, cc)
	if !ok {
		return
	}
	h.renderQuote(w, r, quote)
}

func (h *Handler) declineQuote(w http.ResponseWriter, r *http.Request) {
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
		response.Fail(w, http.StatusConflict, "invalid_state", "quote cannot be declined")
		return
	}
	updated, err := h.q.SetQuoteStatus(r.Context(), gen.SetQuoteStatusParams{ID: quote.ID, Status: "declined"})
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not decline quote")
		return
	}
	h.renderQuote(w, r, updated)
}

// ---- shared --------------------------------------------------------------

// replaceQuoteItems rewrites a quote's lines and recomputes the subtotal.
func (h *Handler) replaceQuoteItems(r *http.Request, q *gen.Queries, quoteID int64, items []quoteLineInput) error {
	if err := q.DeleteQuoteItems(r.Context(), quoteID); err != nil {
		return err
	}
	var totals []string
	for _, it := range items {
		qty := it.Quantity
		if qty == "" {
			qty = "1"
		}
		unit := it.Unit
		if unit == "" {
			unit = "each"
		}
		disc := it.Discount
		if disc == "" {
			disc = "0"
		}
		rowTotal, err := money.RowTotal(qty, it.UnitPrice, disc)
		if err != nil {
			return err
		}
		if _, err := q.AddQuoteItem(r.Context(), gen.AddQuoteItemParams{
			QuoteID: quoteID, ProductID: it.ProductID, Quantity: qty, Unit: unit,
			UnitPrice: it.UnitPrice, Discount: disc, RowTotal: rowTotal,
		}); err != nil {
			return err
		}
		totals = append(totals, rowTotal)
	}
	subtotal, _ := money.Sum(totals...)
	return q.SetQuoteSubtotal(r.Context(), gen.SetQuoteSubtotalParams{ID: quoteID, Subtotal: subtotal})
}

func (h *Handler) loadAdminQuote(w http.ResponseWriter, r *http.Request, a adminCtx) (gen.Quote, bool) {
	id, err := pathID(r)
	if err != nil {
		response.Fail(w, http.StatusBadRequest, "bad_request", "invalid id")
		return gen.Quote{}, false
	}
	quote, err := h.q.GetQuoteByID(r.Context(), gen.GetQuoteByIDParams{OrganizationID: a.orgID, ID: id})
	if err != nil {
		notFound(w, "quote")
		return gen.Quote{}, false
	}
	return quote, true
}

func (h *Handler) loadMyQuote(w http.ResponseWriter, r *http.Request, cc custCtx) (gen.Quote, bool) {
	pid, err := uuid.Parse(chi.URLParam(r, "publicID"))
	if err != nil {
		response.Fail(w, http.StatusBadRequest, "bad_request", "invalid id")
		return gen.Quote{}, false
	}
	quote, err := h.q.GetQuoteByPublicID(r.Context(), pid)
	if err != nil || quote.CustomerID != cc.customerID {
		notFound(w, "quote")
		return gen.Quote{}, false
	}
	return quote, true
}

func (h *Handler) renderQuote(w http.ResponseWriter, r *http.Request, quote gen.Quote) {
	items, err := h.q.ListQuoteItems(r.Context(), quote.ID)
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not load quote items")
		return
	}
	if items == nil {
		items = []gen.ListQuoteItemsRow{}
	}
	response.JSON(w, http.StatusOK, map[string]any{
		"id":          quote.ID,
		"public_id":   quote.PublicID.String(),
		"status":      quote.Status,
		"currency":    quote.Currency,
		"version":     quote.Version,
		"subtotal":    quote.Subtotal,
		"valid_until": quote.ValidUntil,
		"items":       items,
	})
}

func tsPtr(t *time.Time) pgtype.Timestamptz {
	if t == nil {
		return pgtype.Timestamptz{}
	}
	return pgtype.Timestamptz{Time: *t, Valid: true}
}
