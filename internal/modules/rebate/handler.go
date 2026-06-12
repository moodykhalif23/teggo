// Package rebate is the HTTP surface for rebates / volume incentives: admin
// program + tier management, an accrual report (derived from orders), period-end
// settlement (snapshots the amount + issues a credit note), and a buyer-facing
// statement. The period/tier maths live in internal/rebates.
package rebate

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"b2bcommerce/internal/money"
	"b2bcommerce/internal/rebates"
	mw "b2bcommerce/internal/server/middleware"
	"b2bcommerce/internal/server/response"
	"b2bcommerce/internal/store/gen"
)

type Handler struct {
	pool *pgxpool.Pool
	q    *gen.Queries
}

func New(pool *pgxpool.Pool) *Handler { return &Handler{pool: pool, q: gen.New(pool)} }

func (h *Handler) Routes(r chi.Router, authMW func(http.Handler) http.Handler) {
	r.Group(func(ar chi.Router) {
		ar.Use(authMW)
		ar.Use(mw.RequireAudience("admin"))
		v := mw.RequirePermission("rebate.view")
		m := mw.RequirePermission("rebate.manage")

		ar.With(v).Get("/admin/rebates", h.listPrograms)
		ar.With(m).Post("/admin/rebates", h.createProgram)
		ar.With(v).Get("/admin/rebates/{id}", h.getProgram)
		ar.With(m).Put("/admin/rebates/{id}", h.updateProgram)
		ar.With(m).Delete("/admin/rebates/{id}", h.deleteProgram)
		ar.With(m).Post("/admin/rebates/{id}/tiers", h.addTier)
		ar.With(m).Delete("/admin/rebates/{id}/tiers/{tierID}", h.deleteTier)
		ar.With(v).Get("/admin/rebates/{id}/report", h.report)
		ar.With(v).Get("/admin/rebates/{id}/settlements", h.listSettlements)
		ar.With(m).Post("/admin/rebates/{id}/settle", h.settle)
	})
	r.Group(func(sr chi.Router) {
		sr.Use(authMW)
		sr.Use(mw.RequireAudience("storefront"))
		sr.Get("/storefront/rebates", h.statement)
	})
}

func orgID(r *http.Request) (int64, bool) {
	c, ok := mw.ClaimsFrom(r.Context())
	if !ok {
		return 0, false
	}
	return c.OrgID, true
}
func pathID(r *http.Request) (int64, error) { return strconv.ParseInt(chi.URLParam(r, "id"), 10, 64) }

// ---- DTOs ----

type tierDTO struct {
	ID          int64  `json:"id"`
	MinAmount   string `json:"min_amount"`
	RatePercent string `json:"rate_percent"`
}
type programDTO struct {
	ID          int64     `json:"id"`
	PublicID    string    `json:"public_id"`
	Name        string    `json:"name"`
	Description *string   `json:"description,omitempty"`
	CustomerID  *int64    `json:"customer_id,omitempty"`
	Period      string    `json:"period"`
	Currency    string    `json:"currency"`
	IsActive    bool      `json:"is_active"`
	Tiers       []tierDTO `json:"tiers,omitempty"`
}

func toProgram(p gen.RebateProgram) programDTO {
	return programDTO{
		ID: p.ID, PublicID: p.PublicID.String(), Name: p.Name, Description: p.Description,
		CustomerID: p.CustomerID, Period: p.Period, Currency: p.Currency, IsActive: p.IsActive,
	}
}

func (h *Handler) loadTiers(r *http.Request, programID int64) []tierDTO {
	rows, _ := h.q.ListRebateTiers(r.Context(), programID)
	out := make([]tierDTO, 0, len(rows))
	for _, t := range rows {
		out = append(out, tierDTO{ID: t.ID, MinAmount: t.MinAmount, RatePercent: t.RatePercent})
	}
	return out
}

func engineTiers(rows []tierDTO) []rebates.Tier {
	t := make([]rebates.Tier, len(rows))
	for i, r := range rows {
		t[i] = rebates.Tier{MinAmount: r.MinAmount, RatePercent: r.RatePercent}
	}
	return t
}

// ---- programs ----

func (h *Handler) listPrograms(w http.ResponseWriter, r *http.Request) {
	org, ok := orgID(r)
	if !ok {
		response.Fail(w, http.StatusUnauthorized, "unauthorized", "no claims")
		return
	}
	rows, err := h.q.ListRebatePrograms(r.Context(), org)
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not list programs")
		return
	}
	items := make([]programDTO, 0, len(rows))
	for _, p := range rows {
		items = append(items, toProgram(p))
	}
	response.JSON(w, http.StatusOK, map[string]any{"items": items})
}

type programInput struct {
	Name        string  `json:"name"`
	Description *string `json:"description"`
	CustomerID  *int64  `json:"customer_id"`
	Period      string  `json:"period"`
	Currency    string  `json:"currency"`
	IsActive    *bool   `json:"is_active"`
}

func (in *programInput) normalize() string {
	in.Name = strings.TrimSpace(in.Name)
	in.Currency = strings.ToUpper(strings.TrimSpace(in.Currency))
	if in.Name == "" {
		return "name is required"
	}
	switch in.Period {
	case "monthly", "quarterly", "annual":
	default:
		return "period must be monthly, quarterly or annual"
	}
	if len(in.Currency) != 3 {
		return "currency must be a 3-letter code"
	}
	return ""
}

func (h *Handler) createProgram(w http.ResponseWriter, r *http.Request) {
	org, ok := orgID(r)
	if !ok {
		response.Fail(w, http.StatusUnauthorized, "unauthorized", "no claims")
		return
	}
	var in programInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		response.Fail(w, http.StatusBadRequest, "bad_request", "invalid body")
		return
	}
	if msg := in.normalize(); msg != "" {
		response.Fail(w, http.StatusBadRequest, "bad_request", msg)
		return
	}
	active := true
	if in.IsActive != nil {
		active = *in.IsActive
	}
	p, err := h.q.CreateRebateProgram(r.Context(), gen.CreateRebateProgramParams{
		OrganizationID: org, Name: in.Name, Description: in.Description, CustomerID: in.CustomerID,
		Period: in.Period, Currency: in.Currency, IsActive: active,
	})
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not create program")
		return
	}
	response.JSON(w, http.StatusCreated, toProgram(p))
}

func (h *Handler) getProgram(w http.ResponseWriter, r *http.Request) {
	p, ok := h.requireProgram(w, r)
	if !ok {
		return
	}
	dto := toProgram(p)
	dto.Tiers = h.loadTiers(r, p.ID)
	response.JSON(w, http.StatusOK, dto)
}

func (h *Handler) updateProgram(w http.ResponseWriter, r *http.Request) {
	p, ok := h.requireProgram(w, r)
	if !ok {
		return
	}
	var in programInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		response.Fail(w, http.StatusBadRequest, "bad_request", "invalid body")
		return
	}
	if msg := in.normalize(); msg != "" {
		response.Fail(w, http.StatusBadRequest, "bad_request", msg)
		return
	}
	active := true
	if in.IsActive != nil {
		active = *in.IsActive
	}
	updated, err := h.q.UpdateRebateProgram(r.Context(), gen.UpdateRebateProgramParams{
		OrganizationID: p.OrganizationID, ID: p.ID, Name: in.Name, Description: in.Description,
		CustomerID: in.CustomerID, Period: in.Period, Currency: in.Currency, IsActive: active,
	})
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not update program")
		return
	}
	dto := toProgram(updated)
	dto.Tiers = h.loadTiers(r, updated.ID)
	response.JSON(w, http.StatusOK, dto)
}

func (h *Handler) deleteProgram(w http.ResponseWriter, r *http.Request) {
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
	n, err := h.q.DeleteRebateProgram(r.Context(), gen.DeleteRebateProgramParams{OrganizationID: org, ID: id})
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not delete program")
		return
	}
	if n == 0 {
		response.Fail(w, http.StatusNotFound, "not_found", "program not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// requireProgram loads the path program scoped to the caller's org.
func (h *Handler) requireProgram(w http.ResponseWriter, r *http.Request) (gen.RebateProgram, bool) {
	org, ok := orgID(r)
	if !ok {
		response.Fail(w, http.StatusUnauthorized, "unauthorized", "no claims")
		return gen.RebateProgram{}, false
	}
	id, err := pathID(r)
	if err != nil {
		response.Fail(w, http.StatusBadRequest, "bad_request", "invalid id")
		return gen.RebateProgram{}, false
	}
	p, err := h.q.GetRebateProgram(r.Context(), gen.GetRebateProgramParams{OrganizationID: org, ID: id})
	if err != nil {
		response.Fail(w, http.StatusNotFound, "not_found", "program not found")
		return gen.RebateProgram{}, false
	}
	return p, true
}

// ---- tiers ----

func (h *Handler) addTier(w http.ResponseWriter, r *http.Request) {
	p, ok := h.requireProgram(w, r)
	if !ok {
		return
	}
	var req struct {
		MinAmount   string `json:"min_amount"`
		RatePercent string `json:"rate_percent"`
		SortOrder   int32  `json:"sort_order"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Fail(w, http.StatusBadRequest, "bad_request", "invalid body")
		return
	}
	if mv, err := money.Parse(req.MinAmount); err != nil || mv.Sign() < 0 {
		response.Fail(w, http.StatusBadRequest, "bad_request", "min_amount must be a non-negative number")
		return
	}
	if rv, err := money.Parse(req.RatePercent); err != nil || rv.Sign() < 0 {
		response.Fail(w, http.StatusBadRequest, "bad_request", "rate_percent must be a non-negative number")
		return
	}
	t, err := h.q.CreateRebateTier(r.Context(), gen.CreateRebateTierParams{ProgramID: p.ID, MinAmount: req.MinAmount, RatePercent: req.RatePercent, SortOrder: req.SortOrder})
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not add tier")
		return
	}
	response.JSON(w, http.StatusCreated, tierDTO{ID: t.ID, MinAmount: t.MinAmount, RatePercent: t.RatePercent})
}

func (h *Handler) deleteTier(w http.ResponseWriter, r *http.Request) {
	org, ok := orgID(r)
	if !ok {
		response.Fail(w, http.StatusUnauthorized, "unauthorized", "no claims")
		return
	}
	tierID, err := strconv.ParseInt(chi.URLParam(r, "tierID"), 10, 64)
	if err != nil {
		response.Fail(w, http.StatusBadRequest, "bad_request", "invalid tier id")
		return
	}
	n, err := h.q.DeleteRebateTier(r.Context(), gen.DeleteRebateTierParams{OrganizationID: org, ID: tierID})
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not delete tier")
		return
	}
	if n == 0 {
		response.Fail(w, http.StatusNotFound, "not_found", "tier not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---- report + settle ----

func refTime(r *http.Request) time.Time {
	if s := r.URL.Query().Get("ref"); s != "" {
		if d, err := time.Parse("2006-01-02", s); err == nil {
			return d
		}
	}
	return time.Now().UTC()
}

type reportRow struct {
	CustomerID      int64  `json:"customer_id"`
	QualifyingTotal string `json:"qualifying_total"`
	Orders          int64  `json:"orders"`
	RatePercent     string `json:"rate_percent"`
	RebateAmount    string `json:"rebate_amount"`
}

func (h *Handler) report(w http.ResponseWriter, r *http.Request) {
	p, ok := h.requireProgram(w, r)
	if !ok {
		return
	}
	tiers := engineTiers(h.loadTiers(r, p.ID))
	start, end, key := rebates.PeriodWindow(p.Period, refTime(r))
	rows, err := h.q.RebateQualifyingTotals(r.Context(), gen.RebateQualifyingTotalsParams{
		OrganizationID: p.OrganizationID, Currency: p.Currency, CreatedAt: start, CreatedAt_2: end, Customer: p.CustomerID,
	})
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not compute accruals")
		return
	}
	out := make([]reportRow, 0, len(rows))
	for _, row := range rows {
		rr := reportRow{CustomerID: row.CustomerID, QualifyingTotal: row.Total, Orders: row.Orders, RatePercent: "0", RebateAmount: "0"}
		if rate, _, ok := rebates.Applicable(row.Total, tiers); ok {
			rr.RatePercent = rate
			if amt, e := rebates.Rebate(row.Total, rate); e == nil {
				rr.RebateAmount = amt
			}
		}
		out = append(out, rr)
	}
	response.JSON(w, http.StatusOK, map[string]any{"period_key": key, "currency": p.Currency, "rows": out})
}

type settleResult struct {
	PeriodKey   string `json:"period_key"`
	Settled     int    `json:"settled"`
	Skipped     int    `json:"skipped"`
	TotalRebate string `json:"total_rebate"`
}

func (h *Handler) settle(w http.ResponseWriter, r *http.Request) {
	p, ok := h.requireProgram(w, r)
	if !ok {
		return
	}
	tiers := engineTiers(h.loadTiers(r, p.ID))
	start, end, key := rebates.PeriodWindow(p.Period, refTime(r))

	rows, err := h.q.RebateQualifyingTotals(r.Context(), gen.RebateQualifyingTotalsParams{
		OrganizationID: p.OrganizationID, Currency: p.Currency, CreatedAt: start, CreatedAt_2: end, Customer: p.CustomerID,
	})
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not compute accruals")
		return
	}
	// Already-settled customers for this period (idempotent re-runs skip them).
	settled := map[int64]bool{}
	if existing, e := h.q.ListRebateSettlementsForProgram(r.Context(), p.ID); e == nil {
		for _, s := range existing {
			if s.PeriodKey == key {
				settled[s.CustomerID] = true
			}
		}
	}

	tx, err := h.pool.Begin(r.Context())
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not start settlement")
		return
	}
	defer tx.Rollback(r.Context()) //nolint:errcheck
	q := gen.New(tx)

	count, skipped := 0, 0
	totalRebate := "0"
	for _, row := range rows {
		if settled[row.CustomerID] {
			skipped++
			continue
		}
		rate, _, ok := rebates.Applicable(row.Total, tiers)
		if !ok {
			skipped++
			continue // below the lowest tier — no rebate earned
		}
		amt, e := rebates.Rebate(row.Total, rate)
		if e != nil {
			skipped++
			continue
		}
		// Issue a credit note for the rebate (no return/invoice link).
		cid := row.CustomerID
		cn, e := q.CreateCreditNote(r.Context(), gen.CreateCreditNoteParams{CustomerID: cid, Amount: amt, Currency: p.Currency})
		if e != nil {
			response.Fail(w, http.StatusInternalServerError, "internal", "could not issue credit note")
			return
		}
		cnID := cn.ID
		if _, e := q.CreateRebateSettlement(r.Context(), gen.CreateRebateSettlementParams{
			ProgramID: p.ID, CustomerID: cid, PeriodKey: key, QualifyingTotal: row.Total,
			RatePercent: rate, RebateAmount: amt, Currency: p.Currency, Status: "issued", CreditNoteID: &cnID,
		}); e != nil {
			response.Fail(w, http.StatusInternalServerError, "internal", "could not record settlement")
			return
		}
		totalRebate, _ = money.Sum(totalRebate, amt)
		count++
	}
	if err := tx.Commit(r.Context()); err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not commit settlement")
		return
	}
	response.JSON(w, http.StatusOK, settleResult{PeriodKey: key, Settled: count, Skipped: skipped, TotalRebate: totalRebate})
}

func (h *Handler) listSettlements(w http.ResponseWriter, r *http.Request) {
	p, ok := h.requireProgram(w, r)
	if !ok {
		return
	}
	rows, err := h.q.ListRebateSettlementsForProgram(r.Context(), p.ID)
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not list settlements")
		return
	}
	response.JSON(w, http.StatusOK, map[string]any{"items": settlementDTOs(rows)})
}

type settlementDTO struct {
	ID              int64  `json:"id"`
	CustomerID      int64  `json:"customer_id"`
	PeriodKey       string `json:"period_key"`
	QualifyingTotal string `json:"qualifying_total"`
	RatePercent     string `json:"rate_percent"`
	RebateAmount    string `json:"rebate_amount"`
	Currency        string `json:"currency"`
	Status          string `json:"status"`
	ProgramName     string `json:"program_name,omitempty"`
}

func settlementDTOs(rows []gen.RebateSettlement) []settlementDTO {
	out := make([]settlementDTO, 0, len(rows))
	for _, s := range rows {
		out = append(out, settlementDTO{
			ID: s.ID, CustomerID: s.CustomerID, PeriodKey: s.PeriodKey, QualifyingTotal: s.QualifyingTotal,
			RatePercent: s.RatePercent, RebateAmount: s.RebateAmount, Currency: s.Currency, Status: s.Status,
		})
	}
	return out
}

// ---- storefront statement ----

func (h *Handler) statement(w http.ResponseWriter, r *http.Request) {
	c, ok := mw.ClaimsFrom(r.Context())
	if !ok || c.CustomerID == 0 {
		response.Fail(w, http.StatusUnauthorized, "unauthorized", "no customer context")
		return
	}
	// Settled rebates (the buyer's earned credits).
	rows, err := h.q.ListRebateSettlementsForCustomer(r.Context(), gen.ListRebateSettlementsForCustomerParams{CustomerID: c.CustomerID, OrganizationID: c.OrgID})
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not load statement")
		return
	}
	history := make([]settlementDTO, 0, len(rows))
	for _, s := range rows {
		history = append(history, settlementDTO{
			ID: s.ID, CustomerID: s.CustomerID, PeriodKey: s.PeriodKey, QualifyingTotal: s.QualifyingTotal,
			RatePercent: s.RatePercent, RebateAmount: s.RebateAmount, Currency: s.Currency, Status: s.Status, ProgramName: s.ProgramName,
		})
	}

	// Current-period progress for active programs that apply to this buyer.
	type progress struct {
		Program         string `json:"program"`
		PeriodKey       string `json:"period_key"`
		Currency        string `json:"currency"`
		QualifyingTotal string `json:"qualifying_total"`
		RatePercent     string `json:"rate_percent"`
		ProjectedRebate string `json:"projected_rebate"`
	}
	var prog []progress
	if programs, e := h.q.ListActiveRebateProgramsForCustomer(r.Context(), gen.ListActiveRebateProgramsForCustomerParams{OrganizationID: c.OrgID, CustomerID: &c.CustomerID}); e == nil {
		for _, p := range programs {
			tiers := engineTiers(h.loadTiers(r, p.ID))
			start, end, key := rebates.PeriodWindow(p.Period, time.Now().UTC())
			cid := c.CustomerID
			totals, terr := h.q.RebateQualifyingTotals(r.Context(), gen.RebateQualifyingTotalsParams{
				OrganizationID: c.OrgID, Currency: p.Currency, CreatedAt: start, CreatedAt_2: end, Customer: &cid,
			})
			total := "0"
			if terr == nil && len(totals) > 0 {
				total = totals[0].Total
			}
			pr := progress{Program: p.Name, PeriodKey: key, Currency: p.Currency, QualifyingTotal: total, RatePercent: "0", ProjectedRebate: "0"}
			if rate, _, ok := rebates.Applicable(total, tiers); ok {
				pr.RatePercent = rate
				if amt, e := rebates.Rebate(total, rate); e == nil {
					pr.ProjectedRebate = amt
				}
			}
			prog = append(prog, pr)
		}
	}
	response.JSON(w, http.StatusOK, map[string]any{"settlements": history, "current": prog})
}
