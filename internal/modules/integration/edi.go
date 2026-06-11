package integration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"b2bcommerce/internal/edi"
	"b2bcommerce/internal/money"
	"b2bcommerce/internal/server/response"
	"b2bcommerce/internal/store/gen"
)

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func envNow(senderID, receiverID, control string) edi.Envelope {
	now := time.Now()
	return edi.Envelope{
		SenderID: senderID, ReceiverID: receiverID, ControlNumber: control,
		Date: now.Format("20060102"), Time: now.Format("1504"),
	}
}

// ediInbound ingests an X12 850 on a partner-scoped endpoint, stores it raw,
// maps it to an order (all-or-nothing), and emits an 855 acknowledgement.
func (h *Handler) ediInbound(w http.ResponseWriter, r *http.Request) {
	pid, err := pathInt(r, "partnerID")
	if err != nil {
		response.Fail(w, http.StatusBadRequest, "bad_request", "invalid partner id")
		return
	}
	partner, err := h.q.GetTradingPartnerByID(r.Context(), pid)
	if err != nil {
		response.Fail(w, http.StatusNotFound, "not_found", "trading partner not found")
		return
	}
	if !partner.IsActive {
		response.Fail(w, http.StatusForbidden, "inactive", "trading partner is inactive")
		return
	}
	if partner.Protocol != "edi_x12" && partner.Protocol != "edifact" {
		response.Fail(w, http.StatusBadRequest, "wrong_protocol", "partner is not an EDI partner")
		return
	}
	if partner.CustomerID == nil {
		response.Fail(w, http.StatusUnprocessableEntity, "no_customer", "partner has no mapped customer")
		return
	}
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		response.Fail(w, http.StatusBadRequest, "bad_request", "could not read body")
		return
	}

	po, perr := edi.Parse850(string(raw))
	control := ""
	if perr == nil {
		control = po.PONumber
	}

	// Store the raw document first (idempotent on PO control number).
	doc, err := h.q.CreateEDIDocument(r.Context(), gen.CreateEDIDocumentParams{
		OrganizationID: partner.OrganizationID, TradingPartnerID: partner.ID,
		Direction: "inbound", DocType: "850", Status: "received",
		ControlNumber: ptr(control), RawPayload: string(raw),
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			response.Fail(w, http.StatusConflict, "duplicate", "this PO has already been received")
			return
		}
		response.Fail(w, http.StatusInternalServerError, "internal", "could not store document")
		return
	}

	if perr != nil {
		h.failDoc(r.Context(), doc.ID, "parse 850: "+perr.Error())
		response.Fail(w, http.StatusUnprocessableEntity, "parse_error", perr.Error())
		return
	}

	// Map every line to a catalog product before touching the order — all or
	// nothing (the acceptance criterion: never partially applied).
	type mline struct {
		product              gen.Product
		qty, price, rowTotal string
	}
	mapped := make([]mline, 0, len(po.Lines))
	var subtotalParts []string
	for _, l := range po.Lines {
		prod, err := h.q.GetProductBySKU(r.Context(), gen.GetProductBySKUParams{OrganizationID: partner.OrganizationID, Sku: l.SKU})
		if err != nil {
			h.failDoc(r.Context(), doc.ID, "unknown SKU: "+l.SKU)
			response.Fail(w, http.StatusUnprocessableEntity, "unknown_sku", "no product for SKU "+l.SKU)
			return
		}
		price := l.UnitPrice
		if price == "" {
			price = "0"
		}
		rt, _ := money.LineTotal(l.Quantity, price)
		subtotalParts = append(subtotalParts, rt)
		mapped = append(mapped, mline{product: prod, qty: l.Quantity, price: price, rowTotal: rt})
	}
	subtotal, _ := money.Sum(subtotalParts...)
	if subtotal == "" {
		subtotal = "0"
	}

	var orderID int64
	err = h.tx(r.Context(), func(q *gen.Queries) error {
		o, err := q.CreateOrder(r.Context(), gen.CreateOrderParams{
			OrganizationID: partner.OrganizationID, WebsiteID: defaultWebsite, CustomerID: *partner.CustomerID,
			Currency: po.Currency, PoNumber: ptr(po.PONumber),
			BillingAddress: []byte("{}"), ShippingAddress: []byte("{}"),
			Subtotal: subtotal, TaxTotal: "0", ShippingTotal: "0", GrandTotal: subtotal,
		})
		if err != nil {
			return err
		}
		orderID = o.ID
		for _, m := range mapped {
			unit := m.product.Unit
			if unit == "" {
				unit = "each"
			}
			if _, err := q.AddOrderItem(r.Context(), gen.AddOrderItemParams{
				OrderID: o.ID, ProductID: m.product.ID, Sku: m.product.Sku, Name: m.product.Name,
				Quantity: m.qty, Unit: unit, UnitPrice: m.price, TaxAmount: "0", RowTotal: m.rowTotal,
			}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		h.failDoc(r.Context(), doc.ID, "create order: "+err.Error())
		response.Fail(w, http.StatusInternalServerError, "internal", "could not create order from PO")
		return
	}

	parsed, _ := json.Marshal(po)
	_, _ = h.q.SetEDIResult(r.Context(), gen.SetEDIResultParams{
		ID: doc.ID, Status: "processed", Parsed: parsed,
		RelatedEntityType: ptr("order"), RelatedEntityID: &orderID,
	})

	// Emit an 855 acknowledgement and store it as an outbound document.
	ackLines := make([]edi.AckLine, 0, len(mapped))
	for i, m := range mapped {
		ackLines = append(ackLines, edi.AckLine{
			LineNo: strconv.Itoa(i + 1), SKU: m.product.Sku, Quantity: m.qty,
			UOM: "EA", UnitPrice: m.price, Status: "IA",
		})
	}
	ack := edi.Encode855(envNow(h.senderID, deref(partner.Identity), po.PONumber), po.PONumber, ackLines)
	ackDoc, _ := h.q.CreateEDIDocument(r.Context(), gen.CreateEDIDocumentParams{
		OrganizationID: partner.OrganizationID, TradingPartnerID: partner.ID,
		Direction: "outbound", DocType: "855", Status: "sent",
		ControlNumber: ptr(po.PONumber), RawPayload: ack,
	})

	response.JSON(w, http.StatusOK, map[string]any{
		"order_id": orderID, "po_number": po.PONumber, "lines": len(mapped),
		"document_id": doc.ID, "ack_document_id": ackDoc.ID,
	})
}

// failDoc marks an inbound document errored with a reason.
func (h *Handler) failDoc(ctx context.Context, id int64, reason string) {
	_, _ = h.q.SetEDIResult(ctx, gen.SetEDIResultParams{ID: id, Status: "error", Error: &reason})
}

// ---- outbound 810 / 856 ---------------------------------------------------

func (h *Handler) outbound810(w http.ResponseWriter, r *http.Request) {
	org, ok := orgID(r)
	if !ok {
		response.Fail(w, http.StatusUnauthorized, "unauthorized", "no claims")
		return
	}
	var req struct {
		InvoiceID        int64 `json:"invoice_id"`
		TradingPartnerID int64 `json:"trading_partner_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.InvoiceID == 0 || req.TradingPartnerID == 0 {
		response.Fail(w, http.StatusBadRequest, "bad_request", "invoice_id and trading_partner_id required")
		return
	}
	partner, err := h.q.GetTradingPartner(r.Context(), gen.GetTradingPartnerParams{OrganizationID: org, ID: req.TradingPartnerID})
	if err != nil {
		response.Fail(w, http.StatusNotFound, "not_found", "partner not found")
		return
	}
	inv, err := h.q.GetInvoice(r.Context(), gen.GetInvoiceParams{OrganizationID: org, ID: req.InvoiceID})
	if err != nil {
		response.Fail(w, http.StatusNotFound, "not_found", "invoice not found")
		return
	}
	items, _ := h.q.ListInvoiceItems(r.Context(), inv.ID)
	order, _ := h.q.GetOrderByID(r.Context(), gen.GetOrderByIDParams{OrganizationID: org, ID: inv.OrderID})

	lines := make([]edi.InvoiceLine, 0, len(items))
	for i, it := range items {
		lines = append(lines, edi.InvoiceLine{
			LineNo: strconv.Itoa(i + 1), Quantity: it.Quantity, UOM: "EA",
			UnitPrice: it.UnitPrice, Amount: it.RowTotal,
		})
	}
	control := fmt.Sprintf("%09d", inv.ID)
	invoiceNumber := "INV-" + strconv.FormatInt(inv.ID, 10)
	payload := edi.Encode810(envNow(h.senderID, deref(partner.Identity), control), invoiceNumber, deref(order.PoNumber), inv.GrandTotal, lines)

	doc, err := h.storeOutbound(r.Context(), org, partner.ID, "810", control, payload, "invoice", inv.ID)
	if err != nil {
		response.Fail(w, http.StatusConflict, "duplicate", "an 810 for this invoice was already generated")
		return
	}
	response.JSON(w, http.StatusCreated, map[string]any{"document_id": doc.ID, "control_number": control, "payload": payload})
}

func (h *Handler) outbound856(w http.ResponseWriter, r *http.Request) {
	org, ok := orgID(r)
	if !ok {
		response.Fail(w, http.StatusUnauthorized, "unauthorized", "no claims")
		return
	}
	var req struct {
		OrderID          int64 `json:"order_id"`
		TradingPartnerID int64 `json:"trading_partner_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.OrderID == 0 || req.TradingPartnerID == 0 {
		response.Fail(w, http.StatusBadRequest, "bad_request", "order_id and trading_partner_id required")
		return
	}
	partner, err := h.q.GetTradingPartner(r.Context(), gen.GetTradingPartnerParams{OrganizationID: org, ID: req.TradingPartnerID})
	if err != nil {
		response.Fail(w, http.StatusNotFound, "not_found", "partner not found")
		return
	}
	order, err := h.q.GetOrderByID(r.Context(), gen.GetOrderByIDParams{OrganizationID: org, ID: req.OrderID})
	if err != nil {
		response.Fail(w, http.StatusNotFound, "not_found", "order not found")
		return
	}
	items, _ := h.q.ListOrderItems(r.Context(), order.ID)
	lines := make([]edi.ShipLine, 0, len(items))
	for _, it := range items {
		lines = append(lines, edi.ShipLine{SKU: it.Sku, Quantity: it.Quantity, UOM: it.Unit})
	}
	control := fmt.Sprintf("%09d", order.ID)
	shipmentRef := "SHP-" + strconv.FormatInt(order.ID, 10)
	payload := edi.Encode856(envNow(h.senderID, deref(partner.Identity), control), shipmentRef, deref(order.PoNumber), lines)

	doc, err := h.storeOutbound(r.Context(), org, partner.ID, "856", control, payload, "order", order.ID)
	if err != nil {
		response.Fail(w, http.StatusConflict, "duplicate", "an 856 for this order was already generated")
		return
	}
	response.JSON(w, http.StatusCreated, map[string]any{"document_id": doc.ID, "control_number": control, "payload": payload})
}

func (h *Handler) storeOutbound(ctx context.Context, org, partnerID int64, docType, control, payload, entityType string, entityID int64) (gen.EdiDocument, error) {
	doc, err := h.q.CreateEDIDocument(ctx, gen.CreateEDIDocumentParams{
		OrganizationID: org, TradingPartnerID: partnerID, Direction: "outbound",
		DocType: docType, Status: "sent", ControlNumber: ptr(control), RawPayload: payload,
	})
	if err != nil {
		return gen.EdiDocument{}, err
	}
	_, _ = h.q.SetEDIResult(ctx, gen.SetEDIResultParams{
		ID: doc.ID, Status: "sent", RelatedEntityType: ptr(entityType), RelatedEntityID: &entityID,
	})
	return doc, nil
}

func ptr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
