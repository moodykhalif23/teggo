// Package audit exposes the audit trail over HTTP: a filterable, paginated
// viewer and a CSV export. All endpoints require the sensitive audit.view
// permission on the admin surface. The trail itself is written by the audit
// middleware (internal/audit); this module is read-only.
package audit

import (
	"encoding/csv"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	mw "b2bcommerce/internal/server/middleware"
	"b2bcommerce/internal/server/response"
	"b2bcommerce/internal/store/gen"
)

const exportCap = 10000

type Handler struct {
	pool *pgxpool.Pool
	q    *gen.Queries
}

func New(pool *pgxpool.Pool) *Handler { return &Handler{pool: pool, q: gen.New(pool)} }

func (h *Handler) Routes(r chi.Router, authMW func(http.Handler) http.Handler) {
	r.Group(func(ar chi.Router) {
		ar.Use(authMW)
		ar.Use(mw.RequireAudience("admin"))
		// Static segment before the {id} param route (chi matches literals first).
		ar.With(mw.RequirePermission("audit.view")).Get("/admin/audit/export", h.export)
		ar.With(mw.RequirePermission("audit.view")).Get("/admin/audit", h.list)
		ar.With(mw.RequirePermission("audit.view")).Get("/admin/audit/{id}", h.get)
	})
}

func orgID(r *http.Request) (int64, bool) {
	c, ok := mw.ClaimsFrom(r.Context())
	if !ok {
		return 0, false
	}
	return c.OrgID, true
}

// ---- filters --------------------------------------------------------------

type filters struct {
	actor      *int64
	audience   *string
	action     *string
	entityType *string
	entityID   *int64
	from       pgtype.Timestamptz
	to         pgtype.Timestamptz
}

func parseFilters(r *http.Request) filters {
	q := r.URL.Query()
	f := filters{from: parseTime(q.Get("from")), to: parseTime(q.Get("to"))}
	if v := q.Get("actor"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			f.actor = &n
		}
	}
	if v := q.Get("audience"); v != "" {
		f.audience = &v
	}
	if v := q.Get("action"); v != "" {
		f.action = &v
	}
	if v := q.Get("entity_type"); v != "" {
		f.entityType = &v
	}
	if v := q.Get("entity_id"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			f.entityID = &n
		}
	}
	return f
}

// parseTime accepts an RFC3339 timestamp or a bare YYYY-MM-DD date.
func parseTime(s string) pgtype.Timestamptz {
	if s == "" {
		return pgtype.Timestamptz{}
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02"} {
		if t, err := time.Parse(layout, s); err == nil {
			return pgtype.Timestamptz{Time: t, Valid: true}
		}
	}
	return pgtype.Timestamptz{}
}

// ---- handlers -------------------------------------------------------------

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	org, ok := orgID(r)
	if !ok {
		response.Fail(w, http.StatusUnauthorized, "unauthorized", "no claims")
		return
	}
	f := parseFilters(r)
	limit := 50
	if l, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && l > 0 && l <= 200 {
		limit = l
	}
	offset := 0
	if o, err := strconv.Atoi(r.URL.Query().Get("offset")); err == nil && o > 0 {
		offset = o
	}
	rows, err := h.q.ListAuditLog(r.Context(), gen.ListAuditLogParams{
		OrganizationID: org, Actor: f.actor, Audience: f.audience, Action: f.action,
		EntityType: f.entityType, EntityID: f.entityID, FromTs: f.from, ToTs: f.to,
		Lim: int32(limit), Off: int32(offset),
	})
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not list audit log")
		return
	}
	// Total is computed only for the first page (it scans the filtered set); later
	// pages page through with the same filters.
	total := int64(-1)
	if offset == 0 {
		total, _ = h.q.CountAuditLog(r.Context(), gen.CountAuditLogParams{
			OrganizationID: org, Actor: f.actor, Audience: f.audience, Action: f.action,
			EntityType: f.entityType, EntityID: f.entityID, FromTs: f.from, ToTs: f.to,
		})
	}
	items := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		items = append(items, auditDTO(row))
	}
	response.JSON(w, http.StatusOK, map[string]any{"items": items, "total": total, "limit": limit, "offset": offset})
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	org, ok := orgID(r)
	if !ok {
		response.Fail(w, http.StatusUnauthorized, "unauthorized", "no claims")
		return
	}
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		response.Fail(w, http.StatusBadRequest, "bad_request", "invalid id")
		return
	}
	row, err := h.q.GetAuditLog(r.Context(), gen.GetAuditLogParams{ID: id, OrganizationID: org})
	if errors.Is(err, pgx.ErrNoRows) {
		response.Fail(w, http.StatusNotFound, "not_found", "audit entry not found")
		return
	}
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not load audit entry")
		return
	}
	response.JSON(w, http.StatusOK, auditDTO(row))
}

func (h *Handler) export(w http.ResponseWriter, r *http.Request) {
	org, ok := orgID(r)
	if !ok {
		response.Fail(w, http.StatusUnauthorized, "unauthorized", "no claims")
		return
	}
	f := parseFilters(r)
	rows, err := h.q.ExportAuditLog(r.Context(), gen.ExportAuditLogParams{
		OrganizationID: org, Actor: f.actor, Audience: f.audience, Action: f.action,
		EntityType: f.entityType, EntityID: f.entityID, FromTs: f.from, ToTs: f.to,
		Lim: exportCap,
	})
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not export audit log")
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", "attachment; filename=\"audit-log-"+time.Now().UTC().Format("20060102")+".csv\"")
	cw := csv.NewWriter(w)
	_ = cw.Write([]string{
		"timestamp", "actor_user_id", "audience", "action", "entity_type", "entity_id",
		"method", "path", "status", "ip", "request_id", "summary",
	})
	for _, row := range rows {
		_ = cw.Write([]string{
			row.CreatedAt.UTC().Format(time.RFC3339),
			intPtrStr(row.ActorUserID), row.ActorAudience, row.Action, row.EntityType, intPtrStr(row.EntityID),
			row.Method, row.Path, strconv.Itoa(int(row.StatusCode)), row.Ip, row.RequestID, row.Summary,
		})
	}
	cw.Flush()
}

func auditDTO(row gen.AuditLog) map[string]any {
	return map[string]any{
		"id":             row.ID,
		"created_at":     row.CreatedAt.Format(time.RFC3339),
		"actor_user_id":  row.ActorUserID,
		"actor_audience": row.ActorAudience,
		"action":         row.Action,
		"entity_type":    row.EntityType,
		"entity_id":      row.EntityID,
		"method":         row.Method,
		"path":           row.Path,
		"status_code":    row.StatusCode,
		"ip":             row.Ip,
		"user_agent":     row.UserAgent,
		"request_id":     row.RequestID,
		"summary":        row.Summary,
		"metadata":       raw(row.Metadata),
	}
}

func raw(b []byte) json.RawMessage {
	if len(b) == 0 {
		return json.RawMessage("null")
	}
	return json.RawMessage(b)
}

func intPtrStr(p *int64) string {
	if p == nil {
		return ""
	}
	return strconv.FormatInt(*p, 10)
}
