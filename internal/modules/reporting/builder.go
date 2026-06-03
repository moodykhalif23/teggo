package reporting

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"b2bcommerce/internal/report"
	mw "b2bcommerce/internal/server/middleware"
	"b2bcommerce/internal/server/response"
	"b2bcommerce/internal/store/gen"
)

func pathInt(r *http.Request, key string) (int64, error) {
	return strconv.ParseInt(chi.URLParam(r, key), 10, 64)
}

// claimsUserID returns the admin user id from the subject claim, or nil.
func claimsUserID(r *http.Request) *int64 {
	c, ok := mw.ClaimsFrom(r.Context())
	if !ok {
		return nil
	}
	if id, err := strconv.ParseInt(c.Subject, 10, 64); err == nil && id != 0 {
		return &id
	}
	return nil
}

// toDefinition rebuilds the compiler model from a stored definition.
func toDefinition(d gen.ReportDefinition) report.Definition {
	def := report.Definition{Entity: d.Entity}
	_ = json.Unmarshal(d.Dimensions, &def.Dimensions)
	_ = json.Unmarshal(d.Measures, &def.Measures)
	_ = json.Unmarshal(d.Filters, &def.Filters)
	return def
}

func renderDefinition(d gen.ReportDefinition) map[string]any {
	return map[string]any{
		"id": d.ID, "name": d.Name, "entity": d.Entity,
		"dimensions": json.RawMessage(d.Dimensions),
		"measures":   json.RawMessage(d.Measures),
		"filters":    json.RawMessage(d.Filters),
		"created_at": d.CreatedAt.Format(time.RFC3339),
	}
}

type definitionInput struct {
	Name       string           `json:"name"`
	Entity     string           `json:"entity"`
	Dimensions []string         `json:"dimensions"`
	Measures   []report.Measure `json:"measures"`
	Filters    []report.Filter  `json:"filters"`
}

// validateAndMarshal compiles the definition (rejecting unknown fields/aggs/ops
// at save time) and returns the JSONB-ready column bytes.
func validateAndMarshal(org int64, in definitionInput) (dims, meas, filt []byte, err error) {
	if in.Dimensions == nil {
		in.Dimensions = []string{}
	}
	if in.Measures == nil {
		in.Measures = []report.Measure{}
	}
	if in.Filters == nil {
		in.Filters = []report.Filter{}
	}
	def := report.Definition{Entity: in.Entity, Dimensions: in.Dimensions, Measures: in.Measures, Filters: in.Filters}
	if _, err = report.Compile(org, def); err != nil {
		return nil, nil, nil, err
	}
	dims, _ = json.Marshal(in.Dimensions)
	meas, _ = json.Marshal(in.Measures)
	filt, _ = json.Marshal(in.Filters)
	return dims, meas, filt, nil
}

func (h *Handler) entities(w http.ResponseWriter, r *http.Request) {
	response.JSON(w, http.StatusOK, map[string]any{"entities": report.Schema()})
}

func (h *Handler) listDefinitions(w http.ResponseWriter, r *http.Request) {
	org, ok := orgID(r)
	if !ok {
		response.Fail(w, http.StatusUnauthorized, "unauthorized", "no claims")
		return
	}
	rows, err := h.q.ListReportDefinitions(r.Context(), org)
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not list reports")
		return
	}
	items := make([]map[string]any, 0, len(rows))
	for _, d := range rows {
		items = append(items, renderDefinition(d))
	}
	response.JSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *Handler) createDefinition(w http.ResponseWriter, r *http.Request) {
	org, ok := orgID(r)
	if !ok {
		response.Fail(w, http.StatusUnauthorized, "unauthorized", "no claims")
		return
	}
	var in definitionInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.Name == "" || in.Entity == "" {
		response.Fail(w, http.StatusBadRequest, "bad_request", "name and entity are required")
		return
	}
	dims, meas, filt, err := validateAndMarshal(org, in)
	if err != nil {
		response.Fail(w, http.StatusBadRequest, "invalid_report", err.Error())
		return
	}
	d, err := h.q.CreateReportDefinition(r.Context(), gen.CreateReportDefinitionParams{
		OrganizationID: org, Name: in.Name, Entity: in.Entity,
		Dimensions: dims, Measures: meas, Filters: filt, CreatedBy: claimsUserID(r),
	})
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not save report")
		return
	}
	response.JSON(w, http.StatusCreated, renderDefinition(d))
}

func (h *Handler) getDefinition(w http.ResponseWriter, r *http.Request) {
	d, ok := h.loadDefinition(w, r)
	if !ok {
		return
	}
	response.JSON(w, http.StatusOK, renderDefinition(d))
}

func (h *Handler) updateDefinition(w http.ResponseWriter, r *http.Request) {
	org, ok := orgID(r)
	if !ok {
		response.Fail(w, http.StatusUnauthorized, "unauthorized", "no claims")
		return
	}
	id, err := pathInt(r, "id")
	if err != nil {
		response.Fail(w, http.StatusBadRequest, "bad_request", "invalid id")
		return
	}
	var in definitionInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.Name == "" || in.Entity == "" {
		response.Fail(w, http.StatusBadRequest, "bad_request", "name and entity are required")
		return
	}
	dims, meas, filt, err := validateAndMarshal(org, in)
	if err != nil {
		response.Fail(w, http.StatusBadRequest, "invalid_report", err.Error())
		return
	}
	d, err := h.q.UpdateReportDefinition(r.Context(), gen.UpdateReportDefinitionParams{
		OrganizationID: org, ID: id, Name: in.Name, Entity: in.Entity,
		Dimensions: dims, Measures: meas, Filters: filt,
	})
	if err != nil {
		response.Fail(w, http.StatusNotFound, "not_found", "report not found")
		return
	}
	response.JSON(w, http.StatusOK, renderDefinition(d))
}

func (h *Handler) deleteDefinition(w http.ResponseWriter, r *http.Request) {
	org, ok := orgID(r)
	if !ok {
		response.Fail(w, http.StatusUnauthorized, "unauthorized", "no claims")
		return
	}
	id, err := pathInt(r, "id")
	if err != nil {
		response.Fail(w, http.StatusBadRequest, "bad_request", "invalid id")
		return
	}
	if err := h.q.DeleteReportDefinition(r.Context(), gen.DeleteReportDefinitionParams{OrganizationID: org, ID: id}); err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not delete report")
		return
	}
	response.JSON(w, http.StatusOK, map[string]any{"deleted": true})
}

func (h *Handler) runDefinition(w http.ResponseWriter, r *http.Request) {
	org, ok := orgID(r)
	if !ok {
		response.Fail(w, http.StatusUnauthorized, "unauthorized", "no claims")
		return
	}
	d, ok := h.loadDefinition(w, r)
	if !ok {
		return
	}
	def := toDefinition(d)
	runID, _ := h.q.CreateReportRun(r.Context(), gen.CreateReportRunParams{ReportDefinitionID: d.ID, Trigger: "manual"})

	cols, rows, err := report.Run(r.Context(), h.pool, org, def)
	if err != nil {
		h.finishRun(r, runID, "error", 0, nil, nil, nil, err.Error())
		response.Fail(w, http.StatusBadRequest, "run_failed", err.Error())
		return
	}
	rc := int32(len(rows))

	if r.URL.Query().Get("format") == "csv" {
		csvBytes, cerr := report.ToCSV(cols, rows)
		if cerr != nil {
			response.Fail(w, http.StatusInternalServerError, "internal", "could not render csv")
			return
		}
		name := "report-" + strconv.FormatInt(d.ID, 10) + ".csv"
		ct := "text/csv"
		fileURL := "/admin/reports/runs/" + strconv.FormatInt(runID, 10) + "/download"
		h.finishRunFile(r, runID, rc, name, ct, csvBytes, fileURL)
		w.Header().Set("Content-Type", "text/csv")
		w.Header().Set("Content-Disposition", "attachment; filename=\""+name+"\"")
		_, _ = w.Write(csvBytes)
		return
	}

	h.finishRun(r, runID, "ok", rc, nil, nil, nil, "")
	response.JSON(w, http.StatusOK, map[string]any{"columns": cols, "rows": rows, "row_count": len(rows), "run_id": runID})
}

func (h *Handler) listRuns(w http.ResponseWriter, r *http.Request) {
	d, ok := h.loadDefinition(w, r)
	if !ok {
		return
	}
	rows, err := h.q.ListReportRuns(r.Context(), d.ID)
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not list runs")
		return
	}
	items := make([]map[string]any, 0, len(rows))
	for _, run := range rows {
		items = append(items, map[string]any{
			"id": run.ID, "status": run.Status, "trigger": run.Trigger,
			"row_count": run.RowCount, "file_name": run.FileName, "file_url": run.FileUrl,
			"error": run.Error, "started_at": run.StartedAt.Format(time.RFC3339), "finished_at": tsString(run.FinishedAt),
		})
	}
	response.JSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *Handler) downloadRun(w http.ResponseWriter, r *http.Request) {
	org, ok := orgID(r)
	if !ok {
		response.Fail(w, http.StatusUnauthorized, "unauthorized", "no claims")
		return
	}
	runID, err := pathInt(r, "runID")
	if err != nil {
		response.Fail(w, http.StatusBadRequest, "bad_request", "invalid id")
		return
	}
	art, err := h.q.GetReportRunArtifact(r.Context(), gen.GetReportRunArtifactParams{ID: runID, OrganizationID: org})
	if err != nil || len(art.FileBytes) == 0 {
		response.Fail(w, http.StatusNotFound, "not_found", "no artifact for this run")
		return
	}
	ct := "application/octet-stream"
	if art.ContentType != nil {
		ct = *art.ContentType
	}
	name := "report.csv"
	if art.FileName != nil {
		name = *art.FileName
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Content-Disposition", "attachment; filename=\""+name+"\"")
	_, _ = w.Write(art.FileBytes)
}

func (h *Handler) listSchedules(w http.ResponseWriter, r *http.Request) {
	d, ok := h.loadDefinition(w, r)
	if !ok {
		return
	}
	rows, err := h.q.ListReportSchedules(r.Context(), d.ID)
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not list schedules")
		return
	}
	items := make([]map[string]any, 0, len(rows))
	for _, s := range rows {
		items = append(items, map[string]any{
			"id": s.ID, "cadence": s.Cadence, "format": s.Format,
			"recipients": json.RawMessage(s.Recipients), "is_active": s.IsActive,
			"last_run_at": tsString(s.LastRunAt),
		})
	}
	response.JSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *Handler) createSchedule(w http.ResponseWriter, r *http.Request) {
	d, ok := h.loadDefinition(w, r)
	if !ok {
		return
	}
	var req struct {
		Cadence    string   `json:"cadence"`
		Format     string   `json:"format"`
		Recipients []string `json:"recipients"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Fail(w, http.StatusBadRequest, "bad_request", "invalid body")
		return
	}
	switch req.Cadence {
	case "daily", "weekly", "monthly":
	default:
		response.Fail(w, http.StatusBadRequest, "bad_request", "cadence must be daily, weekly, or monthly")
		return
	}
	if req.Format == "" {
		req.Format = "csv"
	}
	if req.Format != "csv" && req.Format != "xlsx" {
		response.Fail(w, http.StatusBadRequest, "bad_request", "format must be csv or xlsx")
		return
	}
	if req.Recipients == nil {
		req.Recipients = []string{}
	}
	recips, _ := json.Marshal(req.Recipients)
	s, err := h.q.CreateReportSchedule(r.Context(), gen.CreateReportScheduleParams{
		ReportDefinitionID: d.ID, Cadence: req.Cadence, Format: req.Format, Recipients: recips, IsActive: true,
	})
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not create schedule")
		return
	}
	response.JSON(w, http.StatusCreated, map[string]any{
		"id": s.ID, "cadence": s.Cadence, "format": s.Format,
		"recipients": json.RawMessage(s.Recipients), "is_active": s.IsActive,
	})
}

func (h *Handler) deleteSchedule(w http.ResponseWriter, r *http.Request) {
	d, ok := h.loadDefinition(w, r)
	if !ok {
		return
	}
	schedID, err := pathInt(r, "schedID")
	if err != nil {
		response.Fail(w, http.StatusBadRequest, "bad_request", "invalid id")
		return
	}
	if err := h.q.DeleteReportSchedule(r.Context(), gen.DeleteReportScheduleParams{ID: schedID, ReportDefinitionID: d.ID}); err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not delete schedule")
		return
	}
	response.JSON(w, http.StatusOK, map[string]any{"deleted": true})
}

// loadDefinition resolves the {id} path param to the caller's own definition,
// writing the error response and returning ok=false on failure.
func (h *Handler) loadDefinition(w http.ResponseWriter, r *http.Request) (gen.ReportDefinition, bool) {
	org, ok := orgID(r)
	if !ok {
		response.Fail(w, http.StatusUnauthorized, "unauthorized", "no claims")
		return gen.ReportDefinition{}, false
	}
	id, err := pathInt(r, "id")
	if err != nil {
		response.Fail(w, http.StatusBadRequest, "bad_request", "invalid id")
		return gen.ReportDefinition{}, false
	}
	d, err := h.q.GetReportDefinition(r.Context(), gen.GetReportDefinitionParams{OrganizationID: org, ID: id})
	if err != nil {
		response.Fail(w, http.StatusNotFound, "not_found", "report not found")
		return gen.ReportDefinition{}, false
	}
	return d, true
}

func (h *Handler) finishRun(r *http.Request, runID int64, status string, rowCount int32, fileName, contentType, fileURL *string, errMsg string) {
	var ep *string
	if errMsg != "" {
		ep = &errMsg
	}
	var rc *int32
	if status == "ok" {
		rc = &rowCount
	}
	_, _ = h.q.FinishReportRun(r.Context(), gen.FinishReportRunParams{
		ID: runID, Status: status, RowCount: rc, FileName: fileName, ContentType: contentType,
		FileBytes: nil, FileUrl: fileURL, Error: ep, FinishedAt: pgtype.Timestamptz{Time: time.Now(), Valid: true},
	})
}

func (h *Handler) finishRunFile(r *http.Request, runID int64, rowCount int32, name, ct string, data []byte, fileURL string) {
	_, _ = h.q.FinishReportRun(r.Context(), gen.FinishReportRunParams{
		ID: runID, Status: "ok", RowCount: &rowCount, FileName: &name, ContentType: &ct,
		FileBytes: data, FileUrl: &fileURL, Error: nil, FinishedAt: pgtype.Timestamptz{Time: time.Now(), Valid: true},
	})
}

func tsString(t pgtype.Timestamptz) any {
	if !t.Valid {
		return nil
	}
	return t.Time.Format(time.RFC3339)
}
