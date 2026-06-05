package wfadmin

import (
	"encoding/json"
	"net/http"

	"b2bcommerce/internal/money"
	"b2bcommerce/internal/server/response"
	"b2bcommerce/internal/store/gen"
)

// Approval routing rules: org-level amount tiers that require a given approver
// role to release a held order. Managed here; enforced in the storefront
// buyer-approval flow (internal/modules/account).

func (h *Handler) listApprovalRules(w http.ResponseWriter, r *http.Request) {
	org, ok := orgID(r)
	if !ok {
		response.Fail(w, http.StatusUnauthorized, "unauthorized", "no claims")
		return
	}
	rows, err := h.q.ListApprovalRoutingRules(r.Context(), org)
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not list approval rules")
		return
	}
	if rows == nil {
		rows = []gen.ApprovalRoutingRule{}
	}
	response.JSON(w, http.StatusOK, map[string]any{"items": rows})
}

func (h *Handler) createApprovalRule(w http.ResponseWriter, r *http.Request) {
	org, ok := orgID(r)
	if !ok {
		response.Fail(w, http.StatusUnauthorized, "unauthorized", "no claims")
		return
	}
	var req struct {
		MinAmount    string  `json:"min_amount"`
		MaxAmount    *string `json:"max_amount"`
		RequiredRole string  `json:"required_role"`
		SortOrder    int32   `json:"sort_order"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Fail(w, http.StatusBadRequest, "bad_request", "invalid body")
		return
	}
	if req.RequiredRole != "approver" && req.RequiredRole != "admin" {
		response.Fail(w, http.StatusBadRequest, "bad_request", "required_role must be approver or admin")
		return
	}
	if req.MinAmount == "" {
		req.MinAmount = "0"
	}
	if _, err := money.Parse(req.MinAmount); err != nil {
		response.Fail(w, http.StatusBadRequest, "bad_request", "min_amount must be numeric")
		return
	}
	if req.MaxAmount != nil {
		if _, err := money.Parse(*req.MaxAmount); err != nil {
			response.Fail(w, http.StatusBadRequest, "bad_request", "max_amount must be numeric")
			return
		}
		// Reject an inverted band early (the DB CHECK also guards this).
		if c, _ := money.Cmp(*req.MaxAmount, req.MinAmount); c < 0 {
			response.Fail(w, http.StatusBadRequest, "bad_request", "max_amount must be >= min_amount")
			return
		}
	}
	rule, err := h.q.CreateApprovalRoutingRule(r.Context(), gen.CreateApprovalRoutingRuleParams{
		OrganizationID: org, MinAmount: req.MinAmount, MaxAmount: req.MaxAmount,
		RequiredRole: req.RequiredRole, SortOrder: req.SortOrder,
	})
	if err != nil {
		response.Fail(w, http.StatusBadRequest, "bad_request", "could not create approval rule")
		return
	}
	response.JSON(w, http.StatusCreated, rule)
}

func (h *Handler) deleteApprovalRule(w http.ResponseWriter, r *http.Request) {
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
	n, err := h.q.DeleteApprovalRoutingRule(r.Context(), gen.DeleteApprovalRoutingRuleParams{ID: id, OrganizationID: org})
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not delete approval rule")
		return
	}
	if n == 0 {
		response.Fail(w, http.StatusNotFound, "not_found", "approval rule not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
