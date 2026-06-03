package crm

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"b2bcommerce/internal/server/response"
	"b2bcommerce/internal/store/gen"
)

// createActivity logs a call/email/meeting/task/note against any combination of
// customer/contact/opportunity/lead (Pack 2 §1).
func (h *Handler) createActivity(w http.ResponseWriter, r *http.Request) {
	a, ok := admin(r)
	if !ok {
		unauthorized(w)
		return
	}
	var req struct {
		Type          string     `json:"type"`
		Subject       string     `json:"subject"`
		Body          *string    `json:"body"`
		CustomerID    *int64     `json:"customer_id"`
		ContactID     *int64     `json:"contact_id"`
		OpportunityID *int64     `json:"opportunity_id"`
		LeadID        *int64     `json:"lead_id"`
		Status        string     `json:"status"`
		DueAt         *time.Time `json:"due_at"`
		OccurredAt    *time.Time `json:"occurred_at"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Subject == "" {
		response.Fail(w, http.StatusBadRequest, "bad_request", "type and subject are required")
		return
	}
	switch req.Type {
	case "call", "email", "meeting", "task", "note":
	default:
		response.Fail(w, http.StatusBadRequest, "bad_request", "invalid activity type")
		return
	}
	if req.Status == "" {
		req.Status = "open"
	}
	occurred := time.Now()
	if req.OccurredAt != nil {
		occurred = *req.OccurredAt
	}
	act, err := h.q.CreateActivity(r.Context(), gen.CreateActivityParams{
		OrganizationID: a.orgID, Type: req.Type, Subject: req.Subject, Body: req.Body,
		CustomerID: req.CustomerID, ContactID: req.ContactID, OpportunityID: req.OpportunityID,
		LeadID: req.LeadID, OwnerUserID: a.userID, Status: req.Status,
		DueAt: tsPtr(req.DueAt), OccurredAt: occurred,
	})
	if err != nil {
		response.Fail(w, http.StatusBadRequest, "bad_request", "could not create activity")
		return
	}
	response.JSON(w, http.StatusCreated, act)
}

// customerTimeline returns the unified activity timeline for a customer — its
// own activities plus those on its contacts and opportunities (Pack 2 §1.2/§1.4).
func (h *Handler) customerTimeline(w http.ResponseWriter, r *http.Request) {
	a, ok := admin(r)
	if !ok {
		unauthorized(w)
		return
	}
	cid, err := pathID(r)
	if err != nil {
		response.Fail(w, http.StatusBadRequest, "bad_request", "invalid id")
		return
	}
	rows, err := h.q.CustomerTimeline(r.Context(), gen.CustomerTimelineParams{OrganizationID: a.orgID, CustomerID: &cid})
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not load timeline")
		return
	}
	if rows == nil {
		rows = []gen.Activity{}
	}
	response.JSON(w, http.StatusOK, map[string]any{"items": rows})
}

// tsPtr converts an optional time into a pgtype.Timestamptz (CreateActivity
// COALESCEs a null occurred_at to now()).
func tsPtr(t *time.Time) pgtype.Timestamptz {
	if t == nil {
		return pgtype.Timestamptz{}
	}
	return pgtype.Timestamptz{Time: *t, Valid: true}
}
