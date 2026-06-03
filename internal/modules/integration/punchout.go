package integration

import (
	"crypto/subtle"
	"io"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"b2bcommerce/internal/cxml"
	"b2bcommerce/internal/money"
	"b2bcommerce/internal/server/response"
	"b2bcommerce/internal/store/gen"
)

// defaultWebsite is the website punchout carts are created under (host-routing
// across all storefront endpoints is a documented follow-up).
const defaultWebsite = 1

func writeXML(w http.ResponseWriter, status int, body []byte) {
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

// punchoutSetup authenticates a PunchOutSetupRequest by the partner's shared
// secret and returns a start URL the buyer's browser is redirected to.
func (h *Handler) punchoutSetup(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeXML(w, http.StatusBadRequest, cxml.ErrorResponse(400, "could not read request"))
		return
	}
	setup, err := cxml.ParseSetupRequest(body)
	if err != nil || setup.SenderIdentity == "" {
		writeXML(w, http.StatusBadRequest, cxml.ErrorResponse(400, "malformed PunchOutSetupRequest"))
		return
	}
	partner, err := h.q.GetCxmlPartnerByIdentity(r.Context(), &setup.SenderIdentity)
	if err != nil {
		writeXML(w, http.StatusUnauthorized, cxml.ErrorResponse(401, "unknown trading partner"))
		return
	}
	// Constant-time shared-secret check.
	if partner.SharedSecret == nil ||
		subtle.ConstantTimeCompare([]byte(*partner.SharedSecret), []byte(setup.SharedSecret)) != 1 {
		writeXML(w, http.StatusUnauthorized, cxml.ErrorResponse(401, "invalid credentials"))
		return
	}
	if partner.CustomerID == nil {
		writeXML(w, http.StatusBadRequest, cxml.ErrorResponse(400, "partner has no mapped customer"))
		return
	}
	if setup.ReturnURL == "" {
		writeXML(w, http.StatusBadRequest, cxml.ErrorResponse(400, "missing BrowserFormPost URL"))
		return
	}

	sess, err := h.q.CreatePunchoutSession(r.Context(), gen.CreatePunchoutSessionParams{
		TradingPartnerID: partner.ID, CustomerID: *partner.CustomerID,
		BuyerCookie: setup.BuyerCookie, Operation: setup.Operation,
		ReturnUrl: setup.ReturnURL, ExpiresAt: time.Now().Add(h.ttl),
	})
	if err != nil {
		writeXML(w, http.StatusInternalServerError, cxml.ErrorResponse(500, "could not create session"))
		return
	}
	startURL := scheme(r) + "://" + r.Host + "/punchout/start/" + sess.PublicID.String()
	writeXML(w, http.StatusOK, cxml.SetupResponse(startURL))
}

// punchoutStart validates the session, ensures a cart, mints a storefront token
// bound to the mapped customer, and redirects the buyer into the storefront.
func (h *Handler) punchoutStart(w http.ResponseWriter, r *http.Request) {
	sess, ok := h.liveSession(w, r, false)
	if !ok {
		return
	}
	partner, err := h.q.GetTradingPartnerByID(r.Context(), sess.TradingPartnerID)
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "partner lookup failed")
		return
	}
	// Ensure the buyer has an active cart bound to the session.
	cart, err := h.q.GetActiveCart(r.Context(), gen.GetActiveCartParams{CustomerID: sess.CustomerID, WebsiteID: defaultWebsite})
	if err != nil {
		cart, err = h.q.CreateCart(r.Context(), gen.CreateCartParams{
			CustomerID: sess.CustomerID, WebsiteID: defaultWebsite, Currency: "USD",
		})
		if err != nil {
			response.Fail(w, http.StatusInternalServerError, "internal", "could not create cart")
			return
		}
	}
	_ = h.q.SetPunchoutCart(r.Context(), gen.SetPunchoutCartParams{ID: sess.ID, CartID: &cart.ID})

	token, err := h.issuer.IssueStorefront(0, partner.OrganizationID, sess.CustomerID)
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not issue token")
		return
	}
	if r.URL.Query().Get("format") == "json" {
		response.JSON(w, http.StatusOK, map[string]any{
			"token": token, "cart_id": cart.ID, "cart_public_id": cart.PublicID.String(),
			"session": sess.PublicID.String(),
		})
		return
	}
	// Hand off to the storefront in punchout mode.
	sep := "?"
	if containsByte(h.storefrontURL, '?') {
		sep = "&"
	}
	http.Redirect(w, r, h.storefrontURL+sep+"punchout_token="+token+"&session="+sess.PublicID.String(), http.StatusFound)
}

// punchoutTransfer builds the PunchOutOrderMessage from the session cart and
// returns the auto-posting form that hands it back to the procurement system.
func (h *Handler) punchoutTransfer(w http.ResponseWriter, r *http.Request) {
	sess, ok := h.liveSession(w, r, true)
	if !ok {
		return
	}
	if sess.CartID == nil {
		response.Fail(w, http.StatusBadRequest, "no_cart", "session has no cart to transfer")
		return
	}
	cart, err := h.q.GetCartByID(r.Context(), *sess.CartID)
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "cart lookup failed")
		return
	}
	items, err := h.q.ListCartItems(r.Context(), *sess.CartID)
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not load cart")
		return
	}
	lines := make([]cxml.Line, 0, len(items))
	totals := make([]string, 0, len(items))
	for _, it := range items {
		lineTotal, _ := money.LineTotal(it.Quantity, it.UnitPrice)
		totals = append(totals, lineTotal)
		lines = append(lines, cxml.Line{
			SupplierPartID: it.Sku, Description: it.Name, UnitOfMeasure: it.Unit,
			Quantity: it.Quantity, UnitPrice: it.UnitPrice,
		})
	}
	total, _ := money.Sum(totals...)
	if total == "" {
		total = "0"
	}
	msg := cxml.OrderMessage(h.senderID, sess.BuyerCookie, cart.Currency, total, lines)

	_ = h.q.SetPunchoutStatus(r.Context(), gen.SetPunchoutStatusParams{ID: sess.ID, Status: "returned"})

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(cxml.AutoPostForm(sess.ReturnUrl, msg))
}

func (h *Handler) liveSession(w http.ResponseWriter, r *http.Request, transferring bool) (gen.PunchoutSession, bool) {
	pid, err := uuid.Parse(chi.URLParam(r, "publicID"))
	if err != nil {
		response.Fail(w, http.StatusBadRequest, "bad_request", "invalid session id")
		return gen.PunchoutSession{}, false
	}
	sess, err := h.q.GetPunchoutSessionByPublicID(r.Context(), pid)
	if err != nil {
		response.Fail(w, http.StatusNotFound, "not_found", "session not found")
		return gen.PunchoutSession{}, false
	}
	if time.Now().After(sess.ExpiresAt) {
		_ = h.q.SetPunchoutStatus(r.Context(), gen.SetPunchoutStatusParams{ID: sess.ID, Status: "expired"})
		response.Fail(w, http.StatusForbidden, "expired", "punchout session has expired")
		return gen.PunchoutSession{}, false
	}
	if sess.Status == "returned" {
		response.Fail(w, http.StatusConflict, "already_returned", "cart already transferred")
		return gen.PunchoutSession{}, false
	}
	if sess.Status != "active" {
		response.Fail(w, http.StatusForbidden, "inactive", "session is not active")
		return gen.PunchoutSession{}, false
	}
	return sess, true
}

func scheme(r *http.Request) string {
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		return "https"
	}
	return "http"
}

func containsByte(s string, b byte) bool {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return true
		}
	}
	return false
}
