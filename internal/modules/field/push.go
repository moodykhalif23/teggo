package field

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"b2bcommerce/internal/changelog"
	"b2bcommerce/internal/money"
	"b2bcommerce/internal/server/response"
	"b2bcommerce/internal/store/gen"
)

// readOnlyEntities cannot be pushed from a device (pulled, never written).
var readOnlyEntities = map[string]bool{
	"product": true, "catalog": true, "category": true,
	"customer": true, "price": true, "pricing": true,
}

type pushChange struct {
	ClientChangeID string          `json:"client_change_id"`
	EntityType     string          `json:"entity_type"`
	Op             string          `json:"op"`
	Payload        json.RawMessage `json:"payload"`
	BaseUpdatedAt  string          `json:"base_updated_at"`
}

// push applies a batch of client changes idempotently. Each result carries a
// per-change status (applied | conflict | rejected) per Pack 3 §4.5 — the batch
// itself is always 200; 409/403 semantics live in the per-change status.
func (h *Handler) push(w http.ResponseWriter, r *http.Request) {
	rep, org, ok := principal(r)
	if !ok {
		response.Fail(w, http.StatusUnauthorized, "unauthorized", "no claims")
		return
	}
	var body struct {
		DeviceUUID string       `json:"device_uuid"`
		Changes    []pushChange `json:"changes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		response.Fail(w, http.StatusBadRequest, "bad_request", "invalid body")
		return
	}
	dev, err := h.device(r, rep, body.DeviceUUID, "")
	if err != nil {
		response.Fail(w, http.StatusBadRequest, "bad_request", "valid device_uuid required")
		return
	}

	results := make([]map[string]any, 0, len(body.Changes))
	for _, ch := range body.Changes {
		results = append(results, h.applyOne(r.Context(), org, rep, dev.ID, ch))
	}

	cursor, _ := h.q.MaxScopedCursor(r.Context(), gen.MaxScopedCursorParams{OrganizationID: org, ScopeRepID: &rep, Column3: 0})
	response.JSON(w, http.StatusOK, map[string]any{"results": results, "cursor": cursor})
}

func (h *Handler) applyOne(ctx context.Context, org, rep, deviceID int64, ch pushChange) map[string]any {
	ccid, err := uuid.Parse(ch.ClientChangeID)
	if err != nil {
		return result(ch.ClientChangeID, "rejected", nil, nil, "invalid client_change_id")
	}
	// Idempotent replay: a previously-seen client_change_id returns its prior
	// outcome, applying nothing new.
	if prior, err := h.q.GetPushLog(ctx, gen.GetPushLogParams{DeviceID: deviceID, ClientChangeID: ccid}); err == nil {
		return map[string]any{
			"client_change_id": ch.ClientChangeID, "status": prior.Status,
			"server_entity_id": prior.ServerEntityID, "replayed": true,
		}
	}

	status, serverID, serverRec, detail := h.dispatch(ctx, org, rep, ch)

	// Record the outcome (idempotency backstop via UNIQUE(device, client_change_id)).
	var detailJSON []byte
	if detail != "" {
		detailJSON, _ = json.Marshal(map[string]string{"reason": detail})
	}
	_, _ = h.q.CreatePushLog(ctx, gen.CreatePushLogParams{
		DeviceID: deviceID, ClientChangeID: ccid, EntityType: ch.EntityType, Op: ch.Op,
		Status: status, ServerEntityID: serverID, Detail: detailJSON,
	})

	res := result(ch.ClientChangeID, status, serverID, serverRec, detail)
	return res
}

// dispatch routes a change to its entity handler and returns the outcome.
func (h *Handler) dispatch(ctx context.Context, org, rep int64, ch pushChange) (status string, serverID *int64, serverRec any, detail string) {
	if readOnlyEntities[ch.EntityType] {
		return "rejected", nil, nil, "entity is read-only on device"
	}
	switch ch.EntityType {
	case "activity":
		return h.applyActivity(ctx, org, rep, ch)
	case "order":
		return h.applyOrder(ctx, org, rep, ch)
	default:
		return "rejected", nil, nil, "unsupported entity type: " + ch.EntityType
	}
}

// applyActivity creates (append-only) or edits (last-write-wins) an activity.
func (h *Handler) applyActivity(ctx context.Context, org, rep int64, ch pushChange) (string, *int64, any, string) {
	var p struct {
		ID         int64   `json:"id"`
		Type       string  `json:"type"`
		Subject    string  `json:"subject"`
		Body       *string `json:"body"`
		Status     string  `json:"status"`
		CustomerID *int64  `json:"customer_id"`
	}
	if err := json.Unmarshal(ch.Payload, &p); err != nil {
		return "rejected", nil, nil, "invalid activity payload"
	}

	if p.ID > 0 { // edit of an existing activity
		act, err := h.q.GetActivity(ctx, gen.GetActivityParams{OrganizationID: org, ID: p.ID})
		if err != nil {
			return "rejected", nil, nil, "activity not found"
		}
		if act.OwnerUserID == nil || *act.OwnerUserID != rep {
			return "rejected", nil, nil, "not your activity"
		}
		// last-write-wins by updated_at: if the server copy moved on since the
		// client's base, reject and hand back the server record to reconcile.
		if base, ok := parseTime(ch.BaseUpdatedAt); ok && act.UpdatedAt.After(base) {
			return "conflict", &act.ID, act, "server record is newer"
		}
		if p.Status == "" {
			p.Status = act.Status
		}
		upd, err := h.q.UpdateActivity(ctx, gen.UpdateActivityParams{
			OrganizationID: org, ID: p.ID, Subject: def(p.Subject, act.Subject), Body: p.Body, Status: p.Status,
		})
		if err != nil {
			return "rejected", nil, nil, "update failed"
		}
		changelog.Record(ctx, h.q, org, &rep, "activity", upd.ID, "upsert", upd)
		return "applied", &upd.ID, upd, ""
	}

	// create (append-only)
	if p.Subject == "" || !validActivityType(p.Type) {
		return "rejected", nil, nil, "type and subject are required"
	}
	if p.Status == "" {
		p.Status = "open"
	}
	act, err := h.q.CreateActivity(ctx, gen.CreateActivityParams{
		OrganizationID: org, Type: p.Type, Subject: p.Subject, Body: p.Body,
		CustomerID: p.CustomerID, OwnerUserID: &rep, Status: p.Status, OccurredAt: time.Now(),
	})
	if err != nil {
		return "rejected", nil, nil, "create failed"
	}
	changelog.Record(ctx, h.q, org, &rep, "activity", act.ID, "upsert", act)
	return "applied", &act.ID, act, ""
}

// applyOrder creates an order from a pushed draft (append-only; never conflicts).
func (h *Handler) applyOrder(ctx context.Context, org, rep int64, ch pushChange) (string, *int64, any, string) {
	var p struct {
		ID         int64  `json:"id"`
		CustomerID int64  `json:"customer_id"`
		Currency   string `json:"currency"`
		Lines      []struct {
			SKU       string `json:"sku"`
			Quantity  string `json:"quantity"`
			UnitPrice string `json:"unit_price"`
		} `json:"lines"`
	}
	if err := json.Unmarshal(ch.Payload, &p); err != nil {
		return "rejected", nil, nil, "invalid order payload"
	}
	if p.ID > 0 {
		return "rejected", nil, nil, "offline order edits are not supported"
	}
	if p.CustomerID == 0 || len(p.Lines) == 0 {
		return "rejected", nil, nil, "customer_id and at least one line required"
	}
	if p.Currency == "" {
		p.Currency = "USD"
	}

	type ml struct {
		prod                 gen.Product
		qty, price, rowTotal string
	}
	mapped := make([]ml, 0, len(p.Lines))
	var totals []string
	for _, l := range p.Lines {
		prod, err := h.q.GetProductBySKU(ctx, gen.GetProductBySKUParams{OrganizationID: org, Sku: l.SKU})
		if err != nil {
			return "rejected", nil, nil, "unknown SKU: " + l.SKU
		}
		price := def(l.UnitPrice, "0")
		rt, _ := money.LineTotal(l.Quantity, price)
		totals = append(totals, rt)
		mapped = append(mapped, ml{prod: prod, qty: l.Quantity, price: price, rowTotal: rt})
	}
	subtotal, _ := money.Sum(totals...)
	if subtotal == "" {
		subtotal = "0"
	}

	var orderID int64
	var publicID string
	err := h.tx(ctx, func(q *gen.Queries) error {
		po := "FIELD-" + strings.SplitN(ch.ClientChangeID, "-", 2)[0]
		o, err := q.CreateOrder(ctx, gen.CreateOrderParams{
			OrganizationID: org, WebsiteID: 1, CustomerID: p.CustomerID, PlacedBySalesRepID: &rep,
			Currency: p.Currency, PoNumber: &po, BillingAddress: []byte("{}"), ShippingAddress: []byte("{}"),
			Subtotal: subtotal, TaxTotal: "0", ShippingTotal: "0", GrandTotal: subtotal, DiscountTotal: "0",
		})
		if err != nil {
			return err
		}
		orderID, publicID = o.ID, o.PublicID.String()
		for _, m := range mapped {
			unit := def(m.prod.Unit, "each")
			if _, err := q.AddOrderItem(ctx, gen.AddOrderItemParams{
				OrderID: o.ID, ProductID: m.prod.ID, Sku: m.prod.Sku, Name: m.prod.Name,
				Quantity: m.qty, Unit: unit, UnitPrice: m.price, TaxAmount: "0", RowTotal: m.rowTotal,
			}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return "rejected", nil, nil, "could not create order"
	}
	rec := map[string]any{"id": orderID, "public_id": publicID, "grand_total": subtotal, "currency": p.Currency}
	changelog.Record(ctx, h.q, org, &rep, "order", orderID, "upsert", rec)
	return "applied", &orderID, rec, ""
}

func (h *Handler) tx(ctx context.Context, fn func(*gen.Queries) error) error {
	t, err := h.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer t.Rollback(ctx) //nolint:errcheck
	if err := fn(gen.New(t)); err != nil {
		return err
	}
	return t.Commit(ctx)
}

// ---- helpers --------------------------------------------------------------

func result(ccid, status string, serverID *int64, serverRec any, detail string) map[string]any {
	m := map[string]any{"client_change_id": ccid, "status": status}
	if serverID != nil {
		m["server_entity_id"] = *serverID
	}
	if serverRec != nil && (status == "applied" || status == "conflict") {
		m["server_record"] = serverRec
	}
	if detail != "" {
		m["detail"] = detail
	}
	return m
}

func validActivityType(t string) bool {
	switch t {
	case "call", "email", "meeting", "task", "note":
		return true
	}
	return false
}

func parseTime(s string) (time.Time, bool) {
	if s == "" {
		return time.Time{}, false
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, true
	}
	return time.Time{}, false
}

func def(v, d string) string {
	if v == "" {
		return d
	}
	return v
}

func jsonRaw(b []byte) json.RawMessage {
	if len(b) == 0 {
		return json.RawMessage("null")
	}
	return json.RawMessage(b)
}

func tsString(t pgtype.Timestamptz) any {
	if !t.Valid {
		return nil
	}
	return t.Time.Format(time.RFC3339)
}
