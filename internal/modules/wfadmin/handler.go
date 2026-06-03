// Package wfadmin exposes admin management of the workflow engine and the
// automation rules (Pack 2 §3): list workflow definitions with their states and
// transitions, edit a transition's guards/actions, and CRUD automation rules —
// all without code deploys. Admin-only, gated by workflow.view / workflow.manage.
package wfadmin

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	mw "b2bcommerce/internal/server/middleware"
	"b2bcommerce/internal/server/response"
	"b2bcommerce/internal/store/gen"
)

type Handler struct {
	q *gen.Queries
}

func New(pool *pgxpool.Pool) *Handler { return &Handler{q: gen.New(pool)} }

func (h *Handler) Routes(r chi.Router, authMW func(http.Handler) http.Handler) {
	r.Group(func(ar chi.Router) {
		ar.Use(authMW)
		ar.Use(mw.RequireAudience("admin"))

		ar.With(mw.RequirePermission("workflow.view")).Get("/admin/workflows", h.listWorkflows)
		ar.With(mw.RequirePermission("workflow.view")).Get("/admin/workflows/{id}", h.getWorkflow)
		ar.With(mw.RequirePermission("workflow.manage")).Patch("/admin/workflow-transitions/{id}", h.updateTransition)

		ar.With(mw.RequirePermission("workflow.view")).Get("/admin/automation-rules", h.listRules)
		ar.With(mw.RequirePermission("workflow.manage")).Post("/admin/automation-rules", h.createRule)
		ar.With(mw.RequirePermission("workflow.manage")).Patch("/admin/automation-rules/{id}", h.updateRule)
	})
}

func orgID(r *http.Request) (int64, bool) {
	c, ok := mw.ClaimsFrom(r.Context())
	if !ok {
		return 0, false
	}
	return c.OrgID, true
}

func pathID(r *http.Request) (int64, error) {
	return strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
}

// rawOrEmpty renders JSONB bytes as raw JSON (so it isn't base64-encoded), or
// an empty array when unset.
func rawOrEmpty(b []byte) json.RawMessage {
	if len(b) == 0 {
		return json.RawMessage("[]")
	}
	return json.RawMessage(b)
}

// validJSONArray reports whether raw is a well-formed JSON array.
func validJSONArray(raw json.RawMessage) bool {
	if !json.Valid(raw) {
		return false
	}
	var arr []any
	return json.Unmarshal(raw, &arr) == nil
}

// ---- workflows ------------------------------------------------------------

func (h *Handler) workflowJSON(r *http.Request, def gen.WorkflowDefinition) (map[string]any, error) {
	states, err := h.q.ListWorkflowStates(r.Context(), def.ID)
	if err != nil {
		return nil, err
	}
	trans, err := h.q.ListWorkflowTransitions(r.Context(), def.ID)
	if err != nil {
		return nil, err
	}
	code := map[int64]string{}
	stateOut := make([]map[string]any, 0, len(states))
	for _, s := range states {
		code[s.ID] = s.Code
		stateOut = append(stateOut, map[string]any{
			"id": s.ID, "code": s.Code, "label": s.Label,
			"is_initial": s.IsInitial, "is_final": s.IsFinal, "sort_order": s.SortOrder,
		})
	}
	transOut := make([]map[string]any, 0, len(trans))
	for _, t := range trans {
		from := ""
		if t.FromStateID != nil {
			from = code[*t.FromStateID]
		}
		transOut = append(transOut, map[string]any{
			"id": t.ID, "code": t.Code, "label": t.Label,
			"from": from, "to": code[t.ToStateID],
			"guards": rawOrEmpty(t.Guards), "actions": rawOrEmpty(t.Actions),
		})
	}
	return map[string]any{
		"id": def.ID, "code": def.Code, "entity_type": def.EntityType,
		"name": def.Name, "is_active": def.IsActive,
		"states": stateOut, "transitions": transOut,
	}, nil
}

func (h *Handler) listWorkflows(w http.ResponseWriter, r *http.Request) {
	org, ok := orgID(r)
	if !ok {
		response.Fail(w, http.StatusUnauthorized, "unauthorized", "no claims")
		return
	}
	defs, err := h.q.ListWorkflowDefinitions(r.Context(), org)
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not list workflows")
		return
	}
	items := make([]map[string]any, 0, len(defs))
	for _, d := range defs {
		j, err := h.workflowJSON(r, d)
		if err != nil {
			response.Fail(w, http.StatusInternalServerError, "internal", "could not load workflow")
			return
		}
		items = append(items, j)
	}
	response.JSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *Handler) getWorkflow(w http.ResponseWriter, r *http.Request) {
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
	def, err := h.q.GetWorkflowDefinition(r.Context(), gen.GetWorkflowDefinitionParams{OrganizationID: org, ID: id})
	if err != nil {
		response.Fail(w, http.StatusNotFound, "not_found", "workflow not found")
		return
	}
	j, err := h.workflowJSON(r, def)
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not load workflow")
		return
	}
	response.JSON(w, http.StatusOK, j)
}

func (h *Handler) updateTransition(w http.ResponseWriter, r *http.Request) {
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
	var req struct {
		Guards  json.RawMessage `json:"guards"`
		Actions json.RawMessage `json:"actions"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Fail(w, http.StatusBadRequest, "bad_request", "invalid body")
		return
	}
	if len(req.Guards) == 0 {
		req.Guards = json.RawMessage("[]")
	}
	if len(req.Actions) == 0 {
		req.Actions = json.RawMessage("[]")
	}
	if !validJSONArray(req.Guards) || !validJSONArray(req.Actions) {
		response.Fail(w, http.StatusBadRequest, "bad_request", "guards and actions must be JSON arrays")
		return
	}
	t, err := h.q.UpdateWorkflowTransitionConfig(r.Context(), gen.UpdateWorkflowTransitionConfigParams{
		OrganizationID: org, ID: id, Guards: req.Guards, Actions: req.Actions,
	})
	if err != nil {
		response.Fail(w, http.StatusNotFound, "not_found", "transition not found in your organization")
		return
	}
	response.JSON(w, http.StatusOK, map[string]any{
		"id": t.ID, "code": t.Code, "label": t.Label,
		"guards": rawOrEmpty(t.Guards), "actions": rawOrEmpty(t.Actions),
	})
}

// ---- automation rules -----------------------------------------------------

func ruleJSON(rl gen.AutomationRule) map[string]any {
	return map[string]any{
		"id": rl.ID, "name": rl.Name, "trigger_event": rl.TriggerEvent,
		"conditions": rawOrEmpty(rl.Conditions), "actions": rawOrEmpty(rl.Actions),
		"is_active": rl.IsActive,
	}
}

func (h *Handler) listRules(w http.ResponseWriter, r *http.Request) {
	org, ok := orgID(r)
	if !ok {
		response.Fail(w, http.StatusUnauthorized, "unauthorized", "no claims")
		return
	}
	rules, err := h.q.ListAutomationRules(r.Context(), org)
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not list rules")
		return
	}
	items := make([]map[string]any, 0, len(rules))
	for _, rl := range rules {
		items = append(items, ruleJSON(rl))
	}
	response.JSON(w, http.StatusOK, map[string]any{"items": items})
}

type ruleInput struct {
	Name         string          `json:"name"`
	TriggerEvent string          `json:"trigger_event"`
	Conditions   json.RawMessage `json:"conditions"`
	Actions      json.RawMessage `json:"actions"`
	IsActive     *bool           `json:"is_active"`
}

func (in *ruleInput) normalize() bool {
	if in.Name == "" || in.TriggerEvent == "" {
		return false
	}
	if len(in.Conditions) == 0 {
		in.Conditions = json.RawMessage("[]")
	}
	if len(in.Actions) == 0 {
		in.Actions = json.RawMessage("[]")
	}
	return validJSONArray(in.Conditions) && validJSONArray(in.Actions)
}

func (h *Handler) createRule(w http.ResponseWriter, r *http.Request) {
	org, ok := orgID(r)
	if !ok {
		response.Fail(w, http.StatusUnauthorized, "unauthorized", "no claims")
		return
	}
	var in ruleInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || !in.normalize() {
		response.Fail(w, http.StatusBadRequest, "bad_request", "name, trigger_event, and JSON-array conditions/actions are required")
		return
	}
	active := true
	if in.IsActive != nil {
		active = *in.IsActive
	}
	rl, err := h.q.CreateAutomationRule(r.Context(), gen.CreateAutomationRuleParams{
		OrganizationID: org, Name: in.Name, TriggerEvent: in.TriggerEvent,
		Conditions: in.Conditions, Actions: in.Actions, IsActive: active,
	})
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not create rule")
		return
	}
	response.JSON(w, http.StatusCreated, ruleJSON(rl))
}

func (h *Handler) updateRule(w http.ResponseWriter, r *http.Request) {
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
	existing, err := h.q.GetAutomationRule(r.Context(), gen.GetAutomationRuleParams{OrganizationID: org, ID: id})
	if err != nil {
		response.Fail(w, http.StatusNotFound, "not_found", "rule not found")
		return
	}
	var in ruleInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		response.Fail(w, http.StatusBadRequest, "bad_request", "invalid body")
		return
	}
	// PATCH semantics: fall back to existing values for omitted fields.
	if in.Name == "" {
		in.Name = existing.Name
	}
	if in.TriggerEvent == "" {
		in.TriggerEvent = existing.TriggerEvent
	}
	if len(in.Conditions) == 0 {
		in.Conditions = existing.Conditions
	}
	if len(in.Actions) == 0 {
		in.Actions = existing.Actions
	}
	if !validJSONArray(in.Conditions) || !validJSONArray(in.Actions) {
		response.Fail(w, http.StatusBadRequest, "bad_request", "conditions and actions must be JSON arrays")
		return
	}
	active := existing.IsActive
	if in.IsActive != nil {
		active = *in.IsActive
	}
	rl, err := h.q.UpdateAutomationRule(r.Context(), gen.UpdateAutomationRuleParams{
		OrganizationID: org, ID: id, Name: in.Name, TriggerEvent: in.TriggerEvent,
		Conditions: in.Conditions, Actions: in.Actions, IsActive: active,
	})
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not update rule")
		return
	}
	response.JSON(w, http.StatusOK, ruleJSON(rl))
}
