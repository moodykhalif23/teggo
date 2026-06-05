// Package account implements storefront-facing buyer self-service for the
// authenticated buying company: saved addresses (this slice), and — as later
// slices land — company users and order approvals. Every route is scoped to the
// customer-user's company from the storefront JWT (never the request body), so
// a buyer can only ever read or mutate their own company's data.
package account

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"b2bcommerce/internal/auth"
	mw "b2bcommerce/internal/server/middleware"
	"b2bcommerce/internal/server/response"
	"b2bcommerce/internal/store/gen"
)

type Handler struct {
	q *gen.Queries
}

func New(q *gen.Queries) *Handler { return &Handler{q: q} }

func (h *Handler) Routes(r chi.Router, authMW func(http.Handler) http.Handler) {
	r.Group(func(sr chi.Router) {
		sr.Use(authMW)
		sr.Use(mw.RequireAudience("storefront"))

		sr.Get("/storefront/account/addresses", h.listAddresses)
		sr.Post("/storefront/account/addresses", h.createAddress)

		sr.Get("/storefront/account/company", h.getCompany)
		sr.Get("/storefront/account/users", h.listUsers)
		sr.Post("/storefront/account/users", h.createUser)
		sr.Patch("/storefront/account/users/{id}", h.updateUser)

		sr.Get("/storefront/account/approvals", h.listApprovals)
		sr.Post("/storefront/account/approvals/{publicID}/approve", h.approveOrder)
		sr.Post("/storefront/account/approvals/{publicID}/reject", h.rejectOrder)
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

// currentUser loads the authenticated customer-user, scoped to their company.
func (h *Handler) currentUser(r *http.Request, p principal) (gen.GetCustomerUserRow, bool) {
	if p.customerUserID == nil {
		return gen.GetCustomerUserRow{}, false
	}
	u, err := h.q.GetCustomerUser(r.Context(), gen.GetCustomerUserParams{ID: *p.customerUserID, CustomerID: p.customerID})
	if err != nil {
		return gen.GetCustomerUserRow{}, false
	}
	return u, true
}

// requireAdmin gates company-management routes to the company's admin role.
// The storefront token carries no role, so the caller's role is read from the
// DB on each call.
func (h *Handler) requireAdmin(w http.ResponseWriter, r *http.Request, p principal) (gen.GetCustomerUserRow, bool) {
	u, ok := h.currentUser(r, p)
	if !ok {
		response.Fail(w, http.StatusUnauthorized, "unauthorized", "no customer context")
		return gen.GetCustomerUserRow{}, false
	}
	if u.Role != "admin" {
		response.Fail(w, http.StatusForbidden, "forbidden", "company-admin role required")
		return gen.GetCustomerUserRow{}, false
	}
	return u, true
}

func validRole(role string) bool {
	return role == "buyer" || role == "approver" || role == "admin"
}

// ---- Company profile -----------------------------------------------------

func (h *Handler) getCompany(w http.ResponseWriter, r *http.Request) {
	p, ok := actor(r)
	if !ok {
		response.Fail(w, http.StatusUnauthorized, "unauthorized", "no customer context")
		return
	}
	me, ok := h.currentUser(r, p)
	if !ok {
		response.Fail(w, http.StatusUnauthorized, "unauthorized", "no customer context")
		return
	}
	c, err := h.q.GetCustomer(r.Context(), gen.GetCustomerParams{OrganizationID: p.orgID, ID: p.customerID})
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not load company")
		return
	}
	response.JSON(w, http.StatusOK, map[string]any{
		"company": map[string]any{
			"id":                 c.ID,
			"name":               c.Name,
			"tax_id":             c.TaxID,
			"payment_terms_days": c.PaymentTermsDays,
			"credit_limit":       c.CreditLimit,
		},
		"me": map[string]any{
			"id":             me.ID,
			"email":          me.Email,
			"full_name":      me.FullName,
			"role":           me.Role,
			"spending_limit": me.SpendingLimit,
		},
	})
}

// ---- Company users -------------------------------------------------------

func (h *Handler) listUsers(w http.ResponseWriter, r *http.Request) {
	p, ok := actor(r)
	if !ok {
		response.Fail(w, http.StatusUnauthorized, "unauthorized", "no customer context")
		return
	}
	if _, ok := h.requireAdmin(w, r, p); !ok {
		return
	}
	rows, err := h.q.ListCustomerUsers(r.Context(), p.customerID)
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not list users")
		return
	}
	if rows == nil {
		rows = []gen.ListCustomerUsersRow{}
	}
	response.JSON(w, http.StatusOK, map[string]any{"items": rows})
}

func (h *Handler) createUser(w http.ResponseWriter, r *http.Request) {
	p, ok := actor(r)
	if !ok {
		response.Fail(w, http.StatusUnauthorized, "unauthorized", "no customer context")
		return
	}
	if _, ok := h.requireAdmin(w, r, p); !ok {
		return
	}
	var req struct {
		Email         string  `json:"email"`
		Password      string  `json:"password"`
		FullName      string  `json:"full_name"`
		Role          string  `json:"role"`
		SpendingLimit *string `json:"spending_limit"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Fail(w, http.StatusBadRequest, "bad_request", "invalid body")
		return
	}
	if req.Email == "" || req.Password == "" || req.FullName == "" {
		response.Fail(w, http.StatusBadRequest, "bad_request", "email, password, full_name required")
		return
	}
	if req.Role == "" {
		req.Role = "buyer"
	}
	if !validRole(req.Role) {
		response.Fail(w, http.StatusBadRequest, "bad_request", "role must be buyer, approver or admin")
		return
	}
	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not hash password")
		return
	}
	u, err := h.q.CreateCustomerUser(r.Context(), gen.CreateCustomerUserParams{
		CustomerID: p.customerID, Email: req.Email, PasswordHash: hash,
		FullName: req.FullName, Role: req.Role, SpendingLimit: req.SpendingLimit,
	})
	if err != nil {
		// UNIQUE (customer_id, email) — a user with this email already exists.
		response.Fail(w, http.StatusConflict, "conflict", "a user with this email already exists")
		return
	}
	response.JSON(w, http.StatusCreated, u)
}

func (h *Handler) updateUser(w http.ResponseWriter, r *http.Request) {
	p, ok := actor(r)
	if !ok {
		response.Fail(w, http.StatusUnauthorized, "unauthorized", "no customer context")
		return
	}
	admin, ok := h.requireAdmin(w, r, p)
	if !ok {
		return
	}
	targetID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		response.Fail(w, http.StatusBadRequest, "bad_request", "invalid id")
		return
	}
	target, err := h.q.GetCustomerUser(r.Context(), gen.GetCustomerUserParams{ID: targetID, CustomerID: p.customerID})
	if err != nil {
		response.Fail(w, http.StatusNotFound, "not_found", "user not found")
		return
	}
	var req struct {
		FullName      *string `json:"full_name"`
		Role          *string `json:"role"`
		SpendingLimit *string `json:"spending_limit"`
		IsActive      *bool   `json:"is_active"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Fail(w, http.StatusBadRequest, "bad_request", "invalid body")
		return
	}
	// Merge onto the existing row (PATCH semantics).
	fullName := target.FullName
	if req.FullName != nil && *req.FullName != "" {
		fullName = *req.FullName
	}
	role := target.Role
	if req.Role != nil {
		if !validRole(*req.Role) {
			response.Fail(w, http.StatusBadRequest, "bad_request", "role must be buyer, approver or admin")
			return
		}
		role = *req.Role
	}
	spendingLimit := target.SpendingLimit
	if req.SpendingLimit != nil {
		spendingLimit = req.SpendingLimit
	}
	isActive := target.IsActive
	if req.IsActive != nil {
		isActive = *req.IsActive
	}
	// Lockout guard: an admin cannot strip their own admin role or deactivate
	// themselves (would orphan the company's only management path).
	if target.ID == admin.ID && (role != "admin" || !isActive) {
		response.Fail(w, http.StatusBadRequest, "bad_request", "you cannot remove your own admin access")
		return
	}
	u, err := h.q.UpdateCustomerUser(r.Context(), gen.UpdateCustomerUserParams{
		ID: targetID, CustomerID: p.customerID, FullName: fullName, Role: role,
		SpendingLimit: spendingLimit, IsActive: isActive,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			response.Fail(w, http.StatusNotFound, "not_found", "user not found")
			return
		}
		response.Fail(w, http.StatusInternalServerError, "internal", "could not update user")
		return
	}
	response.JSON(w, http.StatusOK, u)
}

// requireApprover gates approval routes to the approver or admin roles.
func (h *Handler) requireApprover(w http.ResponseWriter, r *http.Request, p principal) (gen.GetCustomerUserRow, bool) {
	u, ok := h.currentUser(r, p)
	if !ok {
		response.Fail(w, http.StatusUnauthorized, "unauthorized", "no customer context")
		return gen.GetCustomerUserRow{}, false
	}
	if u.Role != "approver" && u.Role != "admin" {
		response.Fail(w, http.StatusForbidden, "forbidden", "approver or admin role required")
		return gen.GetCustomerUserRow{}, false
	}
	return u, true
}

// ---- Order approvals -----------------------------------------------------

// listApprovals returns the company's orders awaiting approval (held on_hold
// at placement because they exceeded the buyer's spending limit).
func (h *Handler) listApprovals(w http.ResponseWriter, r *http.Request) {
	p, ok := actor(r)
	if !ok {
		response.Fail(w, http.StatusUnauthorized, "unauthorized", "no customer context")
		return
	}
	if _, ok := h.requireApprover(w, r, p); !ok {
		return
	}
	rows, err := h.q.ListOrdersForCustomerByStatus(r.Context(), gen.ListOrdersForCustomerByStatusParams{CustomerID: p.customerID, Status: "on_hold"})
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not list approvals")
		return
	}
	items := make([]map[string]any, 0, len(rows))
	for _, o := range rows {
		items = append(items, map[string]any{
			"public_id":      o.PublicID.String(),
			"grand_total":    o.GrandTotal,
			"currency":       o.Currency,
			"placed_by_user": o.CustomerUserID,
			"requested_at":   o.CreatedAt,
		})
	}
	response.JSON(w, http.StatusOK, map[string]any{"items": items})
}

// loadHeldOrder loads an on_hold order for the caller's company and enforces
// separation of duties: an approver may not approve/reject an order they placed
// themselves. Returns the order and the approver row.
func (h *Handler) loadHeldOrder(w http.ResponseWriter, r *http.Request, p principal, approver gen.GetCustomerUserRow) (gen.Order, bool) {
	pid, err := uuid.Parse(chi.URLParam(r, "publicID"))
	if err != nil {
		response.Fail(w, http.StatusBadRequest, "bad_request", "invalid id")
		return gen.Order{}, false
	}
	order, err := h.q.GetOrderByPublicID(r.Context(), pid)
	if err != nil || order.CustomerID != p.customerID {
		response.Fail(w, http.StatusNotFound, "not_found", "order not found")
		return gen.Order{}, false
	}
	if order.Status != "on_hold" {
		response.Fail(w, http.StatusConflict, "conflict", "order is not awaiting approval")
		return gen.Order{}, false
	}
	if order.CustomerUserID != nil && *order.CustomerUserID == approver.ID {
		response.Fail(w, http.StatusForbidden, "forbidden", "you cannot approve an order you placed")
		return gen.Order{}, false
	}
	return order, true
}

func (h *Handler) decideOrder(w http.ResponseWriter, r *http.Request, toStatus, verb string) {
	p, ok := actor(r)
	if !ok {
		response.Fail(w, http.StatusUnauthorized, "unauthorized", "no customer context")
		return
	}
	approver, ok := h.requireApprover(w, r, p)
	if !ok {
		return
	}
	order, ok := h.loadHeldOrder(w, r, p, approver)
	if !ok {
		return
	}
	updated, err := h.q.SetOrderStatus(r.Context(), gen.SetOrderStatusParams{ID: order.ID, Status: toStatus})
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not update order")
		return
	}
	from := "on_hold"
	if err := h.q.AddOrderStatusHistory(r.Context(), gen.AddOrderStatusHistoryParams{
		OrderID: order.ID, FromStatus: &from, ToStatus: toStatus,
		ChangedBy: "customer_user:" + strconv.FormatInt(approver.ID, 10),
		Note:      strPtr(verb + " by company approver"),
	}); err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not record decision")
		return
	}
	response.JSON(w, http.StatusOK, map[string]any{"public_id": updated.PublicID.String(), "status": updated.Status})
}

func (h *Handler) approveOrder(w http.ResponseWriter, r *http.Request) {
	// Approval releases the hold; the order resumes the normal pending flow.
	h.decideOrder(w, r, "pending", "approved")
}

func (h *Handler) rejectOrder(w http.ResponseWriter, r *http.Request) {
	h.decideOrder(w, r, "cancelled", "rejected")
}

func strPtr(s string) *string { return &s }

// ---- Addresses -----------------------------------------------------------

func (h *Handler) listAddresses(w http.ResponseWriter, r *http.Request) {
	p, ok := actor(r)
	if !ok {
		response.Fail(w, http.StatusUnauthorized, "unauthorized", "no customer context")
		return
	}
	rows, err := h.q.ListCustomerAddresses(r.Context(), p.customerID)
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not list addresses")
		return
	}
	if rows == nil {
		rows = []gen.CustomerAddress{}
	}
	response.JSON(w, http.StatusOK, map[string]any{"items": rows})
}

func (h *Handler) createAddress(w http.ResponseWriter, r *http.Request) {
	p, ok := actor(r)
	if !ok {
		response.Fail(w, http.StatusUnauthorized, "unauthorized", "no customer context")
		return
	}
	var req struct {
		Type       string  `json:"type"`
		IsDefault  bool    `json:"is_default"`
		Line1      string  `json:"line1"`
		Line2      *string `json:"line2"`
		City       string  `json:"city"`
		Region     *string `json:"region"`
		PostalCode *string `json:"postal_code"`
		Country    string  `json:"country"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Fail(w, http.StatusBadRequest, "bad_request", "invalid body")
		return
	}
	if req.Line1 == "" || req.City == "" || len(req.Country) != 2 {
		response.Fail(w, http.StatusBadRequest, "bad_request", "line1, city, 2-letter country required")
		return
	}
	if req.Type != "billing" && req.Type != "shipping" {
		response.Fail(w, http.StatusBadRequest, "bad_request", "type must be billing or shipping")
		return
	}
	a, err := h.q.CreateCustomerAddress(r.Context(), gen.CreateCustomerAddressParams{
		CustomerID: p.customerID,
		Type:       req.Type,
		IsDefault:  req.IsDefault,
		Line1:      req.Line1,
		Line2:      req.Line2,
		City:       req.City,
		Region:     req.Region,
		PostalCode: req.PostalCode,
		Country:    req.Country,
	})
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not create address")
		return
	}
	response.JSON(w, http.StatusCreated, a)
}
