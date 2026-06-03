// Package integration implements B2B procurement integration (Pack 3 §3):
// cXML/OCI Punchout and X12 EDI. Procurement systems punch out into the
// storefront and transfer carts back; purchase orders arrive as EDI 850 and
// become orders, with 855/810/856 emitted back. Admin manages trading partners
// and inspects the EDI document log.
package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	mw "b2bcommerce/internal/server/middleware"
	"b2bcommerce/internal/server/response"
	"b2bcommerce/internal/store/gen"
)

// TokenIssuer mints storefront tokens for punchout buyers. *auth.Issuer fits.
type TokenIssuer interface {
	IssueStorefront(customerUserID, orgID, customerID int64) (string, error)
}

type Handler struct {
	pool          *pgxpool.Pool
	q             *gen.Queries
	issuer        TokenIssuer
	storefrontURL string // where the buyer lands after punchout start
	senderID      string // our identity on outbound cXML/EDI
	ttl           time.Duration
}

func New(pool *pgxpool.Pool, issuer TokenIssuer, storefrontURL, senderID string, ttl time.Duration) *Handler {
	if storefrontURL == "" {
		storefrontURL = "/"
	}
	if senderID == "" {
		senderID = "TEGGO"
	}
	if ttl <= 0 {
		ttl = time.Hour
	}
	return &Handler{pool: pool, q: gen.New(pool), issuer: issuer, storefrontURL: storefrontURL, senderID: senderID, ttl: ttl}
}

func (h *Handler) Routes(r chi.Router, authMW func(http.Handler) http.Handler) {
	// Punchout + EDI ingest are partner-authenticated (cXML shared secret /
	// partner-scoped endpoint), not bearer-gated.
	r.Post("/punchout/setup", h.punchoutSetup)
	r.Get("/punchout/start/{publicID}", h.punchoutStart)
	r.Post("/punchout/transfer/{publicID}", h.punchoutTransfer)
	r.Post("/edi/inbound/{partnerID}", h.ediInbound)

	r.Group(func(ar chi.Router) {
		ar.Use(authMW)
		ar.Use(mw.RequireAudience("admin"))

		ar.With(mw.RequirePermission("integration.view")).Get("/admin/trading-partners", h.listPartners)
		ar.With(mw.RequirePermission("integration.manage")).Post("/admin/trading-partners", h.createPartner)
		ar.With(mw.RequirePermission("integration.view")).Get("/admin/trading-partners/{id}", h.getPartner)
		ar.With(mw.RequirePermission("integration.manage")).Put("/admin/trading-partners/{id}", h.updatePartner)

		ar.With(mw.RequirePermission("integration.view")).Get("/admin/edi/documents", h.listEDIDocuments)
		ar.With(mw.RequirePermission("integration.manage")).Post("/admin/edi/810", h.outbound810)
		ar.With(mw.RequirePermission("integration.manage")).Post("/admin/edi/856", h.outbound856)
	})
}

func orgID(r *http.Request) (int64, bool) {
	c, ok := mw.ClaimsFrom(r.Context())
	if !ok {
		return 0, false
	}
	return c.OrgID, true
}

func pathInt(r *http.Request, key string) (int64, error) {
	return strconv.ParseInt(chi.URLParam(r, key), 10, 64)
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

// ---- admin: trading partners ----------------------------------------------

// renderPartner omits the shared secret; callers learn only whether one is set.
func renderPartner(p gen.TradingPartner) map[string]any {
	return map[string]any{
		"id": p.ID, "name": p.Name, "protocol": p.Protocol, "transport": p.Transport,
		"customer_id": p.CustomerID, "identity": p.Identity, "has_secret": p.SharedSecret != nil && *p.SharedSecret != "",
		"config": json.RawMessage(nonEmpty(p.Config)), "is_active": p.IsActive,
		"created_at": p.CreatedAt.Format(time.RFC3339),
	}
}

func nonEmpty(b []byte) []byte {
	if len(b) == 0 {
		return []byte("{}")
	}
	return b
}

type partnerInput struct {
	Name         string          `json:"name"`
	Protocol     string          `json:"protocol"`
	Transport    *string         `json:"transport"`
	CustomerID   *int64          `json:"customer_id"`
	Identity     *string         `json:"identity"`
	SharedSecret *string         `json:"shared_secret"`
	Config       json.RawMessage `json:"config"`
	IsActive     *bool           `json:"is_active"`
}

func validProtocol(p string) bool {
	switch p {
	case "cxml", "oci", "edi_x12", "edifact":
		return true
	}
	return false
}

func (h *Handler) listPartners(w http.ResponseWriter, r *http.Request) {
	org, ok := orgID(r)
	if !ok {
		response.Fail(w, http.StatusUnauthorized, "unauthorized", "no claims")
		return
	}
	rows, err := h.q.ListTradingPartners(r.Context(), org)
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not list partners")
		return
	}
	items := make([]map[string]any, 0, len(rows))
	for _, p := range rows {
		items = append(items, renderPartner(p))
	}
	response.JSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *Handler) createPartner(w http.ResponseWriter, r *http.Request) {
	org, ok := orgID(r)
	if !ok {
		response.Fail(w, http.StatusUnauthorized, "unauthorized", "no claims")
		return
	}
	var in partnerInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.Name == "" || !validProtocol(in.Protocol) {
		response.Fail(w, http.StatusBadRequest, "bad_request", "name and a valid protocol are required")
		return
	}
	cfg := []byte("{}")
	if len(in.Config) > 0 {
		cfg = in.Config
	}
	active := true
	if in.IsActive != nil {
		active = *in.IsActive
	}
	p, err := h.q.CreateTradingPartner(r.Context(), gen.CreateTradingPartnerParams{
		OrganizationID: org, CustomerID: in.CustomerID, Name: in.Name, Protocol: in.Protocol,
		Transport: in.Transport, Identity: in.Identity, SharedSecret: in.SharedSecret, Config: cfg, IsActive: active,
	})
	if err != nil {
		response.Fail(w, http.StatusConflict, "conflict", "partner identity may already be in use")
		return
	}
	response.JSON(w, http.StatusCreated, renderPartner(p))
}

func (h *Handler) getPartner(w http.ResponseWriter, r *http.Request) {
	p, ok := h.loadPartner(w, r)
	if !ok {
		return
	}
	response.JSON(w, http.StatusOK, renderPartner(p))
}

func (h *Handler) updatePartner(w http.ResponseWriter, r *http.Request) {
	org, ok := orgID(r)
	if !ok {
		response.Fail(w, http.StatusUnauthorized, "unauthorized", "no claims")
		return
	}
	id, err := pathInt(r, "id")
	if err != nil {
		response.Fail(w, http.StatusBadRequest, "bad_request", "invalid id")
		return
	}
	cur, err := h.q.GetTradingPartner(r.Context(), gen.GetTradingPartnerParams{OrganizationID: org, ID: id})
	if err != nil {
		response.Fail(w, http.StatusNotFound, "not_found", "partner not found")
		return
	}
	var in partnerInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.Name == "" || !validProtocol(in.Protocol) {
		response.Fail(w, http.StatusBadRequest, "bad_request", "name and a valid protocol are required")
		return
	}
	// Keep the existing secret when the caller doesn't send a new one.
	secret := cur.SharedSecret
	if in.SharedSecret != nil {
		secret = in.SharedSecret
	}
	cfg := cur.Config
	if len(in.Config) > 0 {
		cfg = in.Config
	}
	active := cur.IsActive
	if in.IsActive != nil {
		active = *in.IsActive
	}
	p, err := h.q.UpdateTradingPartner(r.Context(), gen.UpdateTradingPartnerParams{
		OrganizationID: org, ID: id, CustomerID: in.CustomerID, Name: in.Name, Protocol: in.Protocol,
		Transport: in.Transport, Identity: in.Identity, SharedSecret: secret, Config: cfg, IsActive: active,
	})
	if err != nil {
		response.Fail(w, http.StatusConflict, "conflict", "update failed (identity in use?)")
		return
	}
	response.JSON(w, http.StatusOK, renderPartner(p))
}

func (h *Handler) loadPartner(w http.ResponseWriter, r *http.Request) (gen.TradingPartner, bool) {
	org, ok := orgID(r)
	if !ok {
		response.Fail(w, http.StatusUnauthorized, "unauthorized", "no claims")
		return gen.TradingPartner{}, false
	}
	id, err := pathInt(r, "id")
	if err != nil {
		response.Fail(w, http.StatusBadRequest, "bad_request", "invalid id")
		return gen.TradingPartner{}, false
	}
	p, err := h.q.GetTradingPartner(r.Context(), gen.GetTradingPartnerParams{OrganizationID: org, ID: id})
	if err != nil {
		response.Fail(w, http.StatusNotFound, "not_found", "partner not found")
		return gen.TradingPartner{}, false
	}
	return p, true
}

func (h *Handler) listEDIDocuments(w http.ResponseWriter, r *http.Request) {
	org, ok := orgID(r)
	if !ok {
		response.Fail(w, http.StatusUnauthorized, "unauthorized", "no claims")
		return
	}
	rows, err := h.q.ListEDIDocuments(r.Context(), org)
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not list documents")
		return
	}
	items := make([]map[string]any, 0, len(rows))
	for _, d := range rows {
		items = append(items, map[string]any{
			"id": d.ID, "trading_partner_id": d.TradingPartnerID, "direction": d.Direction,
			"doc_type": d.DocType, "status": d.Status, "control_number": d.ControlNumber,
			"related_entity_type": d.RelatedEntityType, "related_entity_id": d.RelatedEntityID,
			"error": d.Error, "created_at": d.CreatedAt.Format(time.RFC3339),
		})
	}
	response.JSON(w, http.StatusOK, map[string]any{"items": items})
}
