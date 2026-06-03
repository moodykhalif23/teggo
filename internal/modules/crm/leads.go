package crm

import (
	"encoding/json"
	"net/http"

	"b2bcommerce/internal/server/response"
	"b2bcommerce/internal/store/gen"
)

func (h *Handler) createLead(w http.ResponseWriter, r *http.Request) {
	a, ok := admin(r)
	if !ok {
		unauthorized(w)
		return
	}
	var req struct {
		Source      string  `json:"source"`
		CompanyName *string `json:"company_name"`
		ContactName *string `json:"contact_name"`
		Email       *string `json:"email"`
		Phone       *string `json:"phone"`
		Notes       *string `json:"notes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Fail(w, http.StatusBadRequest, "bad_request", "invalid body")
		return
	}
	if req.Source == "" {
		req.Source = "manual"
	}
	lead, err := h.q.CreateLead(r.Context(), gen.CreateLeadParams{
		OrganizationID: a.orgID, Source: req.Source, CompanyName: req.CompanyName,
		ContactName: req.ContactName, Email: req.Email, Phone: req.Phone,
		Notes: req.Notes, OwnerUserID: a.userID,
	})
	if err != nil {
		response.Fail(w, http.StatusBadRequest, "bad_request", "could not create lead (check source)")
		return
	}
	response.JSON(w, http.StatusCreated, lead)
}

func (h *Handler) listLeads(w http.ResponseWriter, r *http.Request) {
	a, ok := admin(r)
	if !ok {
		unauthorized(w)
		return
	}
	rows, err := h.q.ListLeads(r.Context(), gen.ListLeadsParams{OrganizationID: a.orgID, Limit: 200, Offset: 0})
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not list leads")
		return
	}
	if rows == nil {
		rows = []gen.Lead{}
	}
	response.JSON(w, http.StatusOK, map[string]any{"items": rows})
}

func (h *Handler) getLead(w http.ResponseWriter, r *http.Request) {
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
	lead, err := h.q.GetLead(r.Context(), gen.GetLeadParams{OrganizationID: a.orgID, ID: id})
	if err != nil {
		notFound(w, "lead")
		return
	}
	response.JSON(w, http.StatusOK, lead)
}

// convertLead turns a lead into a (customer + contact + opportunity) in one
// transaction (Pack 2 §1.2). Conversion is idempotent: a lead already in the
// converted state is rejected with 409.
func (h *Handler) convertLead(w http.ResponseWriter, r *http.Request) {
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
	lead, err := h.q.GetLead(r.Context(), gen.GetLeadParams{OrganizationID: a.orgID, ID: id})
	if err != nil {
		notFound(w, "lead")
		return
	}
	if lead.Status == "converted" {
		response.Fail(w, http.StatusConflict, "already_converted", "lead has already been converted")
		return
	}

	companyName := deref(lead.CompanyName, "New Customer")
	contactName := deref(lead.ContactName, companyName)
	currency := h.defaultCurrency(r.Context(), a.orgID)

	var customerID, contactID, oppID int64
	err = h.tx(r.Context(), func(q *gen.Queries) error {
		cust, e := q.CreateCustomer(r.Context(), gen.CreateCustomerParams{
			OrganizationID: a.orgID, Name: companyName, CreditLimit: "0",
		})
		if e != nil {
			return e
		}
		customerID = cust.ID

		contact, e := q.CreateContact(r.Context(), gen.CreateContactParams{
			OrganizationID: a.orgID, CustomerID: &cust.ID, FullName: contactName,
			Email: lead.Email, Phone: lead.Phone,
		})
		if e != nil {
			return e
		}
		contactID = contact.ID

		pl, e := q.GetDefaultPipeline(r.Context(), a.orgID)
		if e != nil {
			return e
		}
		stage, e := q.FirstStage(r.Context(), pl.ID)
		if e != nil {
			return e
		}
		opp, e := q.CreateOpportunity(r.Context(), gen.CreateOpportunityParams{
			OrganizationID: a.orgID, CustomerID: cust.ID, ContactID: &contact.ID,
			PipelineID: pl.ID, StageID: stage.ID, Name: companyName + " opportunity",
			Amount: "0", Currency: currency, OwnerUserID: a.userID,
		})
		if e != nil {
			return e
		}
		oppID = opp.ID
		if e := q.AddOpportunityStageHistory(r.Context(), gen.AddOpportunityStageHistoryParams{
			OpportunityID: opp.ID, ToStageID: stage.ID, ChangedBy: a.userID,
		}); e != nil {
			return e
		}
		// Idempotency guard: only flips a not-yet-converted lead.
		if _, e := q.MarkLeadConverted(r.Context(), gen.MarkLeadConvertedParams{
			OrganizationID: a.orgID, ID: lead.ID, ConvertedCustomerID: &cust.ID,
		}); e != nil {
			return e
		}
		return nil
	})
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not convert lead")
		return
	}
	response.JSON(w, http.StatusOK, map[string]any{
		"lead_id": lead.ID, "customer_id": customerID, "contact_id": contactID, "opportunity_id": oppID,
	})
}

func deref(s *string, def string) string {
	if s == nil || *s == "" {
		return def
	}
	return *s
}
