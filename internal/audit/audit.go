// Package audit records a comprehensive, append-only trail of state-changing
// actions on the staff (admin/vendor) surfaces. A single middleware captures
// every mutating request automatically — who (actor + audience), what (action +
// entity), when, from where (IP / user-agent / request id), and the result
// (status code) — so coverage does not depend on remembering to instrument each
// handler. Handlers may additionally enrich the in-flight entry with a precise
// entity reference, a human summary, and a before/after snapshot for sensitive
// changes (stored under metadata.change).
package audit

import (
	"context"
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"

	mw "b2bcommerce/internal/server/middleware"
	"b2bcommerce/internal/store/gen"
)

type ctxKey struct{}

// Entry is the in-flight audit record. The middleware creates one per audited
// request, places it in the context, lets the handler enrich it, then finalizes
// and writes it once the handler returns.
type Entry struct {
	entityType string
	entityID   *int64
	summary    string
	change     map[string]any
	skip       bool
}

func withEntry(ctx context.Context, e *Entry) context.Context {
	return context.WithValue(ctx, ctxKey{}, e)
}

func fromContext(ctx context.Context) *Entry {
	e, _ := ctx.Value(ctxKey{}).(*Entry)
	return e
}

// SetEntity records the precise entity an action targeted, overriding the
// path-inferred type/id. No-op outside an audited request.
func SetEntity(ctx context.Context, entityType string, id int64) {
	if e := fromContext(ctx); e != nil {
		e.entityType = entityType
		e.entityID = &id
	}
}

// SetSummary attaches a human-readable description of the action.
func SetSummary(ctx context.Context, summary string) {
	if e := fromContext(ctx); e != nil {
		e.summary = summary
	}
}

// SetChange attaches a before/after snapshot (stored under metadata.change).
// Pass a nil before for a creation, or a nil after for a deletion. The caller
// is responsible for not passing secrets in the snapshots.
func SetChange(ctx context.Context, before, after any) {
	if e := fromContext(ctx); e != nil {
		e.change = map[string]any{"before": before, "after": after}
	}
}

// Skip suppresses auditing for the current request.
func Skip(ctx context.Context) {
	if e := fromContext(ctx); e != nil {
		e.skip = true
	}
}

// Recorder writes audit entries. It is safe for concurrent use.
type Recorder struct {
	pool   *pgxpool.Pool
	logger *slog.Logger
}

func NewRecorder(pool *pgxpool.Pool, logger *slog.Logger) *Recorder {
	if logger == nil {
		logger = slog.Default()
	}
	return &Recorder{pool: pool, logger: logger}
}

// Middleware records every state-changing request on a staff surface. It must be
// composed INSIDE the authenticated chain (so claims are present) and wraps the
// handler so handler enrichment is visible when the entry is finalized.
func (rec *Recorder) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !mutating(r.Method) {
			next.ServeHTTP(w, r)
			return
		}
		e := &Entry{}
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(sw, r.WithContext(withEntry(r.Context(), e)))
		rec.finalize(r, e, sw.status)
	})
}

func (rec *Recorder) finalize(r *http.Request, e *Entry, status int) {
	if e.skip {
		return
	}
	claims, ok := mw.ClaimsFrom(r.Context())
	if !ok {
		return // unauthenticated → no actor to attribute
	}
	// Audit the staff surfaces; buyer self-service (storefront) is out of scope.
	if claims.Audience != "admin" && claims.Audience != "vendor" {
		return
	}

	action, entityType, entityID := classify(r.Method, r.URL.Path)
	if e.entityType != "" {
		entityType = e.entityType
	}
	if e.entityID != nil {
		entityID = e.entityID
	}

	var actor *int64
	if id, err := strconv.ParseInt(claims.Subject, 10, 64); err == nil {
		actor = &id
	}

	metadata := []byte("{}")
	if e.change != nil {
		if b, err := json.Marshal(map[string]any{"change": e.change}); err == nil {
			metadata = b
		}
	}

	params := gen.CreateAuditLogParams{
		OrganizationID: claims.OrgID,
		ActorUserID:    actor,
		ActorAudience:  claims.Audience,
		Action:         action,
		EntityType:     entityType,
		EntityID:       entityID,
		Method:         r.Method,
		Path:           r.URL.Path,
		StatusCode:     int32(status),
		Ip:             clientIP(r),
		UserAgent:      truncate(r.UserAgent(), 512),
		RequestID:      chimw.GetReqID(r.Context()),
		Summary:        truncate(e.summary, 1024),
		Metadata:       metadata,
	}

	rec.write(r.Context(), params)
}

// Event is a pre-attributed audit record written directly via Record — for
// actions outside the authenticated mutation path that the middleware can't see:
// login attempts (a public route) and data exports (a read, not a mutation).
type Event struct {
	OrgID      int64
	ActorID    *int64 // nil when there is no authenticated actor (e.g. a failed login)
	Audience   string // "admin" | "vendor"
	Action     string
	EntityType string
	EntityID   *int64
	StatusCode int
	Summary    string
	Metadata   map[string]any
}

// Record writes one audit entry from e plus the request's transport metadata
// (IP, user-agent, request id). Best-effort, like the middleware's own writes.
func (rec *Recorder) Record(r *http.Request, e Event) {
	metadata := []byte("{}")
	if len(e.Metadata) > 0 {
		if b, err := json.Marshal(e.Metadata); err == nil {
			metadata = b
		}
	}
	rec.write(r.Context(), gen.CreateAuditLogParams{
		OrganizationID: e.OrgID,
		ActorUserID:    e.ActorID,
		ActorAudience:  e.Audience,
		Action:         e.Action,
		EntityType:     e.EntityType,
		EntityID:       e.EntityID,
		Method:         r.Method,
		Path:           r.URL.Path,
		StatusCode:     int32(e.StatusCode),
		Ip:             clientIP(r),
		UserAgent:      truncate(r.UserAgent(), 512),
		RequestID:      chimw.GetReqID(r.Context()),
		Summary:        truncate(e.Summary, 1024),
		Metadata:       metadata,
	})
}

// write persists one audit row. Detached from request cancellation (an audit
// write must outlive a client disconnect) but preserving context values so the
// RLS-armed pool still binds the right org. A failed write never affects the
// request the user already completed.
func (rec *Recorder) write(reqCtx context.Context, params gen.CreateAuditLogParams) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(reqCtx), 5*time.Second)
	defer cancel()
	if _, err := gen.New(rec.pool).CreateAuditLog(ctx, params); err != nil {
		rec.logger.Warn("audit write failed", "err", err, "action", params.Action, "path", params.Path)
	}
}

// classify derives a structured action, entity type and (when addressable)
// entity id from the HTTP method + path:
//
//	POST   /admin/customers          → customers.create
//	PUT    /admin/customers/12       → customers.update      (entity 12)
//	DELETE /admin/products/5         → products.delete       (entity 5)
//	POST   /admin/orders/9/confirm   → orders.confirm        (entity 9)
func classify(method, path string) (action, entityType string, entityID *int64) {
	p := strings.TrimPrefix(path, "/")
	p = strings.TrimPrefix(p, "admin/")
	p = strings.TrimPrefix(p, "vendor/")
	segs := splitNonEmpty(p)
	if len(segs) == 0 {
		return "request." + methodVerb(method), "", nil
	}
	resource := segs[0]
	var subparts []string
	for _, s := range segs[1:] {
		if n, err := strconv.ParseInt(s, 10, 64); err == nil {
			id := n
			entityID = &id
			continue
		}
		subparts = append(subparts, s)
	}
	if len(subparts) > 0 {
		return resource + "." + strings.Join(subparts, "."), resource, entityID
	}
	return resource + "." + methodVerb(method), resource, entityID
}

func methodVerb(method string) string {
	switch method {
	case http.MethodPost:
		return "create"
	case http.MethodPut, http.MethodPatch:
		return "update"
	case http.MethodDelete:
		return "delete"
	default:
		return strings.ToLower(method)
	}
}

func mutating(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}

func splitNonEmpty(p string) []string {
	parts := strings.Split(p, "/")
	out := parts[:0]
	for _, s := range parts {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

// clientIP returns the request's client IP (RealIP middleware has already set
// r.RemoteAddr to the real address); the port is stripped when present.
func clientIP(r *http.Request) string {
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}

func truncate(s string, max int) string {
	if len(s) > max {
		return s[:max]
	}
	return s
}

// statusWriter captures the response status code for the audit entry.
type statusWriter struct {
	http.ResponseWriter
	status int
	wrote  bool
}

func (w *statusWriter) WriteHeader(code int) {
	if !w.wrote {
		w.status = code
		w.wrote = true
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	w.wrote = true
	return w.ResponseWriter.Write(b)
}

// Flush passes through so streaming handlers keep working behind the wrapper.
func (w *statusWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
