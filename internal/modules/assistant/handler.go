// Package assistant exposes the AI copilot over HTTP: a buyer-facing endpoint
// (storefront audience) and a staff-facing endpoint (admin audience). Each builds
// an ai.ToolContext from the caller's JWT claims — org, audience, permissions,
// and customer/user id — so the assistant operates strictly within the caller's
// own scope. The decision engine (deterministic or Claude) is injected.
package assistant

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"b2bcommerce/internal/ai"
	"b2bcommerce/internal/ai/tools"
	mw "b2bcommerce/internal/server/middleware"
	"b2bcommerce/internal/server/response"
	"b2bcommerce/internal/store/gen"
)

type Handler struct {
	q     *gen.Queries
	agent *ai.Agent
}

func New(q *gen.Queries, provider ai.Provider) *Handler {
	reg := ai.NewRegistry(tools.All()...)
	return &Handler{q: q, agent: ai.NewAgent(provider, reg)}
}

func (h *Handler) Routes(r chi.Router, authMW func(http.Handler) http.Handler) {
	r.Group(func(sr chi.Router) {
		sr.Use(authMW)
		sr.Use(mw.RequireAudience("storefront"))
		sr.Post("/storefront/assistant", h.storefront)
	})
	r.Group(func(ar chi.Router) {
		ar.Use(authMW)
		ar.Use(mw.RequireAudience("admin"))
		ar.Post("/admin/assistant", h.admin)
	})
}

type chatRequest struct {
	Message string    `json:"message"`
	History []ai.Turn `json:"history"`
}

func (h *Handler) storefront(w http.ResponseWriter, r *http.Request) {
	c, ok := mw.ClaimsFrom(r.Context())
	if !ok || c.CustomerID == 0 {
		response.Fail(w, http.StatusUnauthorized, "unauthorized", "no buyer context")
		return
	}
	req, ok := decode(w, r)
	if !ok {
		return
	}
	tc := ai.ToolContext{
		OrgID: c.OrgID, Audience: "storefront", CustomerID: c.CustomerID,
		UserID: subID(c.Subject), Q: h.q,
	}
	h.run(w, r, tc, req)
}

func (h *Handler) admin(w http.ResponseWriter, r *http.Request) {
	c, ok := mw.ClaimsFrom(r.Context())
	if !ok {
		response.Fail(w, http.StatusUnauthorized, "unauthorized", "no admin context")
		return
	}
	req, ok := decode(w, r)
	if !ok {
		return
	}
	tc := ai.ToolContext{
		OrgID: c.OrgID, Audience: "admin", Permissions: c.Permissions,
		UserID: subID(c.Subject), Q: h.q,
	}
	h.run(w, r, tc, req)
}

func (h *Handler) run(w http.ResponseWriter, r *http.Request, tc ai.ToolContext, req chatRequest) {
	reply, err := h.agent.Handle(r.Context(), tc, req.Message, req.History)
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "the assistant is unavailable right now")
		return
	}
	response.JSON(w, http.StatusOK, reply)
}

func decode(w http.ResponseWriter, r *http.Request) (chatRequest, bool) {
	var req chatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Fail(w, http.StatusBadRequest, "bad_request", "invalid body")
		return chatRequest{}, false
	}
	return req, true
}

func subID(sub string) int64 {
	id, _ := strconv.ParseInt(sub, 10, 64)
	return id
}
