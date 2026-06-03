package crm

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"b2bcommerce/internal/server/response"
	"b2bcommerce/internal/store/gen"
)

func (h *Handler) createOpportunity(w http.ResponseWriter, r *http.Request) {
	a, ok := admin(r)
	if !ok {
		unauthorized(w)
		return
	}
	var req struct {
		CustomerID    int64   `json:"customer_id"`
		ContactID     *int64  `json:"contact_id"`
		StageID       *int64  `json:"stage_id"`
		Name          string  `json:"name"`
		Amount        string  `json:"amount"`
		Currency      string  `json:"currency"`
		ExpectedClose *string `json:"expected_close"` // YYYY-MM-DD
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.CustomerID == 0 || req.Name == "" {
		response.Fail(w, http.StatusBadRequest, "bad_request", "customer_id and name are required")
		return
	}
	pl, err := h.q.GetDefaultPipeline(r.Context(), a.orgID)
	if err != nil {
		response.Fail(w, http.StatusBadRequest, "bad_request", "no pipeline configured")
		return
	}
	// Resolve the starting stage: an explicit (in-pipeline) stage or the first.
	var stageID int64
	if req.StageID != nil {
		s, err := h.q.GetStage(r.Context(), *req.StageID)
		if err != nil || s.PipelineID != pl.ID {
			response.Fail(w, http.StatusBadRequest, "bad_request", "stage does not belong to the pipeline")
			return
		}
		stageID = s.ID
	} else {
		s, err := h.q.FirstStage(r.Context(), pl.ID)
		if err != nil {
			response.Fail(w, http.StatusInternalServerError, "internal", "pipeline has no stages")
			return
		}
		stageID = s.ID
	}
	if req.Amount == "" {
		req.Amount = "0"
	}
	if req.Currency == "" {
		req.Currency = h.defaultCurrency(r.Context(), a.orgID)
	}

	var opp gen.Opportunity
	err = h.tx(r.Context(), func(q *gen.Queries) error {
		var e error
		opp, e = q.CreateOpportunity(r.Context(), gen.CreateOpportunityParams{
			OrganizationID: a.orgID, CustomerID: req.CustomerID, ContactID: req.ContactID,
			PipelineID: pl.ID, StageID: stageID, Name: req.Name, Amount: req.Amount,
			Currency: req.Currency, ExpectedClose: parseDate(req.ExpectedClose), OwnerUserID: a.userID,
		})
		if e != nil {
			return e
		}
		return q.AddOpportunityStageHistory(r.Context(), gen.AddOpportunityStageHistoryParams{
			OpportunityID: opp.ID, ToStageID: stageID, ChangedBy: a.userID,
		})
	})
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not create opportunity")
		return
	}
	response.JSON(w, http.StatusCreated, opp)
}

func (h *Handler) listOpportunities(w http.ResponseWriter, r *http.Request) {
	a, ok := admin(r)
	if !ok {
		unauthorized(w)
		return
	}
	rows, err := h.q.ListOpportunities(r.Context(), gen.ListOpportunitiesParams{OrganizationID: a.orgID, Limit: 200, Offset: 0})
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not list opportunities")
		return
	}
	if rows == nil {
		rows = []gen.Opportunity{}
	}
	response.JSON(w, http.StatusOK, map[string]any{"items": rows})
}

func (h *Handler) getOpportunity(w http.ResponseWriter, r *http.Request) {
	a, ok := admin(r)
	if !ok {
		unauthorized(w)
		return
	}
	id, err := pathID(r)
	if err != nil {
		response.Fail(w, http.StatusBadRequest, "bad_request", "invalid id")
		return
	}
	opp, err := h.q.GetOpportunity(r.Context(), gen.GetOpportunityParams{OrganizationID: a.orgID, ID: id})
	if err != nil {
		notFound(w, "opportunity")
		return
	}
	response.JSON(w, http.StatusOK, opp)
}

// patchOpportunityStage moves an opportunity to a new stage, writing a history
// row (Pack 2 §1.2). Moving to a won/lost stage stamps closed_at; any other
// stage clears it (re-opening).
func (h *Handler) patchOpportunityStage(w http.ResponseWriter, r *http.Request) {
	a, ok := admin(r)
	if !ok {
		unauthorized(w)
		return
	}
	id, err := pathID(r)
	if err != nil {
		response.Fail(w, http.StatusBadRequest, "bad_request", "invalid id")
		return
	}
	opp, err := h.q.GetOpportunity(r.Context(), gen.GetOpportunityParams{OrganizationID: a.orgID, ID: id})
	if err != nil {
		notFound(w, "opportunity")
		return
	}
	var req struct {
		StageID int64 `json:"stage_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.StageID == 0 {
		response.Fail(w, http.StatusBadRequest, "bad_request", "stage_id is required")
		return
	}
	stage, err := h.q.GetStage(r.Context(), req.StageID)
	if err != nil || stage.PipelineID != opp.PipelineID {
		response.Fail(w, http.StatusBadRequest, "bad_request", "stage does not belong to this opportunity's pipeline")
		return
	}

	var closed pgtype.Timestamptz
	if stage.IsWon || stage.IsLost {
		closed = pgtype.Timestamptz{Time: time.Now(), Valid: true}
	}
	from := opp.StageID

	var updated gen.Opportunity
	err = h.tx(r.Context(), func(q *gen.Queries) error {
		var e error
		updated, e = q.SetOpportunityStage(r.Context(), gen.SetOpportunityStageParams{
			OrganizationID: a.orgID, ID: id, StageID: stage.ID, ClosedAt: closed,
		})
		if e != nil {
			return e
		}
		return q.AddOpportunityStageHistory(r.Context(), gen.AddOpportunityStageHistoryParams{
			OpportunityID: id, FromStageID: &from, ToStageID: stage.ID, ChangedBy: a.userID,
		})
	})
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not move stage")
		return
	}
	response.JSON(w, http.StatusOK, updated)
}

// ---- contacts -------------------------------------------------------------

func (h *Handler) listContacts(w http.ResponseWriter, r *http.Request) {
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
	rows, err := h.q.ListContactsForCustomer(r.Context(), gen.ListContactsForCustomerParams{OrganizationID: a.orgID, CustomerID: &cid})
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not list contacts")
		return
	}
	if rows == nil {
		rows = []gen.Contact{}
	}
	response.JSON(w, http.StatusOK, map[string]any{"items": rows})
}

func (h *Handler) createContact(w http.ResponseWriter, r *http.Request) {
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
	var req struct {
		FullName string  `json:"full_name"`
		Email    *string `json:"email"`
		Phone    *string `json:"phone"`
		JobTitle *string `json:"job_title"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.FullName == "" {
		response.Fail(w, http.StatusBadRequest, "bad_request", "full_name is required")
		return
	}
	contact, err := h.q.CreateContact(r.Context(), gen.CreateContactParams{
		OrganizationID: a.orgID, CustomerID: &cid, FullName: req.FullName,
		Email: req.Email, Phone: req.Phone, JobTitle: req.JobTitle,
	})
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not create contact")
		return
	}
	response.JSON(w, http.StatusCreated, contact)
}

func parseDate(s *string) pgtype.Date {
	if s == nil || *s == "" {
		return pgtype.Date{}
	}
	t, err := time.Parse("2006-01-02", *s)
	if err != nil {
		return pgtype.Date{}
	}
	return pgtype.Date{Time: t, Valid: true}
}
