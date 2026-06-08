// Package customers implements the B2B customer/account module (Implementation
// Pack 1 §2): customer companies with hierarchy, customer users, addresses, and
// groups. All routes are admin (bearer + permission gated) and organization
// scoped — the org is taken from the authenticated principal's JWT claims, never
// from the request body, so tenant isolation cannot be spoofed.
package customers

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"b2bcommerce/internal/auth"
	"b2bcommerce/internal/changelog"
	mw "b2bcommerce/internal/server/middleware"
	"b2bcommerce/internal/server/response"
	"b2bcommerce/internal/store/gen"
)

type Handler struct {
	q *gen.Queries
}

func New(q *gen.Queries) *Handler { return &Handler{q: q} }

func (h *Handler) Routes(r chi.Router, authMW func(http.Handler) http.Handler) {
	r.Group(func(ar chi.Router) {
		ar.Use(authMW)
		ar.Use(mw.RequireAudience("admin"))

		ar.With(mw.RequirePermission("customer.view")).Get("/admin/customers", h.list)
		ar.With(mw.RequirePermission("customer.manage")).Post("/admin/customers", h.create)
		ar.With(mw.RequirePermission("customer.view")).Get("/admin/customers/{id}", h.get)
		ar.With(mw.RequirePermission("customer.manage")).Put("/admin/customers/{id}", h.update)
		ar.With(mw.RequirePermission("customer.manage")).Delete("/admin/customers/{id}", h.softDelete)
		ar.With(mw.RequirePermission("customer.view")).Get("/admin/customers/{id}/hierarchy", h.hierarchy)

		ar.With(mw.RequirePermission("customer.view")).Get("/admin/customer-groups", h.listGroups)
		ar.With(mw.RequirePermission("customer.manage")).Post("/admin/customer-groups", h.createGroup)
		ar.With(mw.RequirePermission("customer.view")).Get("/admin/customer-groups/{id}/customers", h.listGroupCustomers)

		ar.With(mw.RequirePermission("customer.view")).Get("/admin/customers/{id}/users", h.listUsers)
		ar.With(mw.RequirePermission("customer.manage")).Post("/admin/customers/{id}/users", h.createUser)

		ar.With(mw.RequirePermission("customer.view")).Get("/admin/customers/{id}/addresses", h.listAddresses)
		ar.With(mw.RequirePermission("customer.manage")).Post("/admin/customers/{id}/addresses", h.createAddress)

		ar.With(mw.RequirePermission("customer.view")).Get("/admin/customers/{id}/budgets", h.listBudgets)
		ar.With(mw.RequirePermission("customer.manage")).Post("/admin/customers/{id}/budgets", h.createBudget)
		ar.With(mw.RequirePermission("customer.manage")).Delete("/admin/customers/{id}/budgets/{budgetID}", h.deleteBudget)
	})
}

// orgID resolves the tenant boundary from the authenticated claims.
func orgID(r *http.Request) (int64, bool) {
	claims, ok := mw.ClaimsFrom(r.Context())
	if !ok {
		return 0, false
	}
	return claims.OrgID, true
}

func pathID(r *http.Request) (int64, error) {
	return strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
}

// ---- DTOs ----------------------------------------------------------------

type customerDTO struct {
	ID                 int64   `json:"id"`
	PublicID           string  `json:"public_id"`
	ParentID           *int64  `json:"parent_id"`
	CustomerGroupID    *int64  `json:"customer_group_id"`
	Name               string  `json:"name"`
	TaxID              *string `json:"tax_id"`
	PaymentTermsDays   int32   `json:"payment_terms_days"`
	CreditLimit        string  `json:"credit_limit"`
	DefaultPriceListID *int64  `json:"default_price_list_id"`
	AssignedSalesRepID *int64  `json:"assigned_sales_rep_id"`
	IsActive           bool    `json:"is_active"`
}

func toCustomerDTO(c gen.Customer) customerDTO {
	return customerDTO{
		ID:                 c.ID,
		PublicID:           c.PublicID.String(),
		ParentID:           c.ParentID,
		CustomerGroupID:    c.CustomerGroupID,
		Name:               c.Name,
		TaxID:              c.TaxID,
		PaymentTermsDays:   c.PaymentTermsDays,
		CreditLimit:        c.CreditLimit,
		DefaultPriceListID: c.DefaultPriceListID,
		AssignedSalesRepID: c.AssignedSalesRepID,
		IsActive:           c.IsActive,
	}
}

type createCustomerRequest struct {
	Name               string  `json:"name"`
	TaxID              *string `json:"tax_id"`
	PaymentTermsDays   int32   `json:"payment_terms_days"`
	CreditLimit        string  `json:"credit_limit"`
	CustomerGroupID    *int64  `json:"customer_group_id"`
	ParentID           *int64  `json:"parent_id"`
	AssignedSalesRepID *int64  `json:"assigned_sales_rep_id"`
}

// ---- Customers -----------------------------------------------------------

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	org, ok := orgID(r)
	if !ok {
		response.Fail(w, http.StatusUnauthorized, "unauthorized", "no claims")
		return
	}
	limit := atoiDefault(r.URL.Query().Get("page_size"), 24)
	page := atoiDefault(r.URL.Query().Get("page"), 1)
	if page < 1 {
		page = 1
	}
	offset := (page - 1) * limit

	rows, err := h.q.ListCustomers(r.Context(), gen.ListCustomersParams{
		OrganizationID: org,
		Limit:          int32(limit),
		Offset:         int32(offset),
	})
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not list customers")
		return
	}
	total, err := h.q.CountCustomers(r.Context(), org)
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not count customers")
		return
	}
	items := make([]customerDTO, 0, len(rows))
	for _, c := range rows {
		items = append(items, toCustomerDTO(c))
	}
	response.JSON(w, http.StatusOK, map[string]any{"items": items, "page": page, "total": total})
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	org, ok := orgID(r)
	if !ok {
		response.Fail(w, http.StatusUnauthorized, "unauthorized", "no claims")
		return
	}
	var req createCustomerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Fail(w, http.StatusBadRequest, "bad_request", "invalid body")
		return
	}
	if req.Name == "" {
		response.Fail(w, http.StatusBadRequest, "bad_request", "name is required")
		return
	}
	if req.CreditLimit == "" {
		req.CreditLimit = "0"
	}
	// A new customer cannot create a cycle, but its parent must belong to the
	// same org (guards cross-tenant parenting).
	if req.ParentID != nil {
		if _, err := h.q.GetCustomer(r.Context(), gen.GetCustomerParams{OrganizationID: org, ID: *req.ParentID}); err != nil {
			response.Fail(w, http.StatusBadRequest, "bad_request", "parent_id not found in organization")
			return
		}
	}

	c, err := h.q.CreateCustomer(r.Context(), gen.CreateCustomerParams{
		OrganizationID:     org,
		ParentID:           req.ParentID,
		CustomerGroupID:    req.CustomerGroupID,
		Name:               req.Name,
		TaxID:              req.TaxID,
		PaymentTermsDays:   req.PaymentTermsDays,
		CreditLimit:        req.CreditLimit,
		AssignedSalesRepID: req.AssignedSalesRepID,
	})
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not create customer")
		return
	}
	// Record for field-sync: visible to the assigned rep's device.
	changelog.Record(r.Context(), h.q, org, c.AssignedSalesRepID, "customer", c.ID, "upsert", toCustomerDTO(c))
	response.JSON(w, http.StatusCreated, toCustomerDTO(c))
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	org, ok := orgID(r)
	if !ok {
		response.Fail(w, http.StatusUnauthorized, "unauthorized", "no claims")
		return
	}
	id, err := pathID(r)
	if err != nil {
		response.Fail(w, http.StatusBadRequest, "bad_request", "invalid id")
		return
	}
	c, err := h.q.GetCustomer(r.Context(), gen.GetCustomerParams{OrganizationID: org, ID: id})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			response.Fail(w, http.StatusNotFound, "not_found", "customer not found")
			return
		}
		response.Fail(w, http.StatusInternalServerError, "internal", "could not load customer")
		return
	}
	response.JSON(w, http.StatusOK, toCustomerDTO(c))
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	org, ok := orgID(r)
	if !ok {
		response.Fail(w, http.StatusUnauthorized, "unauthorized", "no claims")
		return
	}
	id, err := pathID(r)
	if err != nil {
		response.Fail(w, http.StatusBadRequest, "bad_request", "invalid id")
		return
	}
	var req createCustomerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Fail(w, http.StatusBadRequest, "bad_request", "invalid body")
		return
	}
	if req.CreditLimit == "" {
		req.CreditLimit = "0"
	}

	// Cycle safety (Pack 1 §2 AC): re-parenting cannot point a customer at itself
	// or any of its descendants. A cycle would form iff this customer is an
	// ancestor of (or equal to) the proposed parent.
	if req.ParentID != nil {
		if *req.ParentID == id {
			response.Fail(w, http.StatusBadRequest, "bad_request", "customer cannot be its own parent")
			return
		}
		ancestors, err := h.q.CustomerAncestors(r.Context(), gen.CustomerAncestorsParams{ID: *req.ParentID, OrganizationID: org})
		if err != nil {
			response.Fail(w, http.StatusBadRequest, "bad_request", "parent_id not found in organization")
			return
		}
		for _, a := range ancestors {
			if a.ID == id {
				response.Fail(w, http.StatusBadRequest, "bad_request", "re-parenting would create a cycle")
				return
			}
		}
	}

	c, err := h.q.UpdateCustomer(r.Context(), gen.UpdateCustomerParams{
		OrganizationID:     org,
		ID:                 id,
		Name:               req.Name,
		TaxID:              req.TaxID,
		PaymentTermsDays:   req.PaymentTermsDays,
		CreditLimit:        req.CreditLimit,
		CustomerGroupID:    req.CustomerGroupID,
		ParentID:           req.ParentID,
		AssignedSalesRepID: req.AssignedSalesRepID,
		IsActive:           true,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			response.Fail(w, http.StatusNotFound, "not_found", "customer not found")
			return
		}
		response.Fail(w, http.StatusInternalServerError, "internal", "could not update customer")
		return
	}
	changelog.Record(r.Context(), h.q, org, c.AssignedSalesRepID, "customer", c.ID, "upsert", toCustomerDTO(c))
	response.JSON(w, http.StatusOK, toCustomerDTO(c))
}

func (h *Handler) softDelete(w http.ResponseWriter, r *http.Request) {
	org, ok := orgID(r)
	if !ok {
		response.Fail(w, http.StatusUnauthorized, "unauthorized", "no claims")
		return
	}
	id, err := pathID(r)
	if err != nil {
		response.Fail(w, http.StatusBadRequest, "bad_request", "invalid id")
		return
	}
	n, err := h.q.SoftDeleteCustomer(r.Context(), gen.SoftDeleteCustomerParams{OrganizationID: org, ID: id})
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not delete customer")
		return
	}
	if n == 0 {
		response.Fail(w, http.StatusNotFound, "not_found", "customer not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) hierarchy(w http.ResponseWriter, r *http.Request) {
	org, ok := orgID(r)
	if !ok {
		response.Fail(w, http.StatusUnauthorized, "unauthorized", "no claims")
		return
	}
	id, err := pathID(r)
	if err != nil {
		response.Fail(w, http.StatusBadRequest, "bad_request", "invalid id")
		return
	}
	rows, err := h.q.CustomerAncestors(r.Context(), gen.CustomerAncestorsParams{ID: id, OrganizationID: org})
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not load hierarchy")
		return
	}
	ancestors := make([]map[string]any, 0, len(rows))
	for _, a := range rows {
		ancestors = append(ancestors, map[string]any{"id": a.ID, "depth": a.Depth})
	}
	response.JSON(w, http.StatusOK, map[string]any{"customer_id": id, "ancestors": ancestors})
}

// ---- Customer groups -----------------------------------------------------

func (h *Handler) listGroups(w http.ResponseWriter, r *http.Request) {
	org, ok := orgID(r)
	if !ok {
		response.Fail(w, http.StatusUnauthorized, "unauthorized", "no claims")
		return
	}
	rows, err := h.q.ListCustomerGroups(r.Context(), org)
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not list groups")
		return
	}
	if rows == nil {
		rows = []gen.CustomerGroup{}
	}
	response.JSON(w, http.StatusOK, map[string]any{"items": rows})
}

func (h *Handler) createGroup(w http.ResponseWriter, r *http.Request) {
	org, ok := orgID(r)
	if !ok {
		response.Fail(w, http.StatusUnauthorized, "unauthorized", "no claims")
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" {
		response.Fail(w, http.StatusBadRequest, "bad_request", "name is required")
		return
	}
	g, err := h.q.CreateCustomerGroup(r.Context(), gen.CreateCustomerGroupParams{OrganizationID: org, Name: req.Name})
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not create group")
		return
	}
	response.JSON(w, http.StatusCreated, g)
}

// listGroupCustomers returns the customers assigned to one group (the members),
// scoped to the tenant. Powers the drill-in from the customer-groups list.
func (h *Handler) listGroupCustomers(w http.ResponseWriter, r *http.Request) {
	org, ok := orgID(r)
	if !ok {
		response.Fail(w, http.StatusUnauthorized, "unauthorized", "no claims")
		return
	}
	gid, err := pathID(r)
	if err != nil {
		response.Fail(w, http.StatusBadRequest, "bad_request", "invalid group id")
		return
	}
	rows, err := h.q.ListCustomersByGroup(r.Context(), gen.ListCustomersByGroupParams{OrganizationID: org, CustomerGroupID: &gid})
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not list group customers")
		return
	}
	items := make([]customerDTO, 0, len(rows))
	for _, c := range rows {
		items = append(items, toCustomerDTO(c))
	}
	response.JSON(w, http.StatusOK, map[string]any{"items": items, "total": len(items)})
}

// ---- Customer users ------------------------------------------------------

func (h *Handler) listUsers(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireCustomer(w, r); !ok {
		return
	}
	id, _ := pathID(r)
	rows, err := h.q.ListCustomerUsers(r.Context(), id)
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
	if _, ok := h.requireCustomer(w, r); !ok {
		return
	}
	id, _ := pathID(r)
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
	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not hash password")
		return
	}
	u, err := h.q.CreateCustomerUser(r.Context(), gen.CreateCustomerUserParams{
		CustomerID:    id,
		Email:         req.Email,
		PasswordHash:  hash,
		FullName:      req.FullName,
		Role:          req.Role,
		SpendingLimit: req.SpendingLimit,
	})
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not create customer user")
		return
	}
	response.JSON(w, http.StatusCreated, u)
}

// ---- Addresses -----------------------------------------------------------

func (h *Handler) listAddresses(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireCustomer(w, r); !ok {
		return
	}
	id, _ := pathID(r)
	rows, err := h.q.ListCustomerAddresses(r.Context(), id)
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
	if _, ok := h.requireCustomer(w, r); !ok {
		return
	}
	id, _ := pathID(r)
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
		CustomerID: id,
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

// requireCustomer verifies the path customer exists within the caller's org
// (so sub-resource routes can't read across tenants). Returns the org id.
func (h *Handler) requireCustomer(w http.ResponseWriter, r *http.Request) (int64, bool) {
	org, ok := orgID(r)
	if !ok {
		response.Fail(w, http.StatusUnauthorized, "unauthorized", "no claims")
		return 0, false
	}
	id, err := pathID(r)
	if err != nil {
		response.Fail(w, http.StatusBadRequest, "bad_request", "invalid id")
		return 0, false
	}
	if _, err := h.q.GetCustomer(r.Context(), gen.GetCustomerParams{OrganizationID: org, ID: id}); err != nil {
		response.Fail(w, http.StatusNotFound, "not_found", "customer not found")
		return 0, false
	}
	return org, true
}

func atoiDefault(s string, def int) int {
	if s == "" {
		return def
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return n
}
