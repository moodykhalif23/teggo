// Package field implements field-sales offline sync (Pack 3 §4). A rep's device
// holds a scoped local subset (their customers, catalog, pricing, own drafts)
// and syncs via a cursor-based delta protocol: GET /field/sync/pull returns the
// scoped change_log delta after a cursor; POST /field/sync/push applies
// client-generated changes idempotently under an explicit conflict policy.
package field

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	mw "b2bcommerce/internal/server/middleware"
	"b2bcommerce/internal/server/response"
	"b2bcommerce/internal/store/gen"
)

const pullBatch = 200

type Handler struct {
	pool *pgxpool.Pool
	q    *gen.Queries
}

func New(pool *pgxpool.Pool) *Handler { return &Handler{pool: pool, q: gen.New(pool)} }

func (h *Handler) Routes(r chi.Router, authMW func(http.Handler) http.Handler) {
	r.Group(func(ar chi.Router) {
		ar.Use(authMW)
		ar.Use(mw.RequireAudience("admin")) // reps are seller-side users

		ar.With(mw.RequirePermission("field.sync")).Get("/field/sync/pull", h.pull)
		ar.With(mw.RequirePermission("field.sync")).Post("/field/sync/push", h.push)
		ar.With(mw.RequirePermission("field.sync")).Get("/admin/field/devices", h.listDevices)
	})
}

// principal returns the rep (user id) and org from the JWT claims.
func principal(r *http.Request) (repID, org int64, ok bool) {
	c, ok := mw.ClaimsFrom(r.Context())
	if !ok {
		return 0, 0, false
	}
	id, err := strconv.ParseInt(c.Subject, 10, 64)
	if err != nil || id == 0 {
		return 0, 0, false
	}
	return id, c.OrgID, true
}

// device upserts (auto-registers) the calling device from a uuid + platform.
func (h *Handler) device(r *http.Request, repID int64, deviceUUID, platform string) (gen.FieldDevice, error) {
	du, err := uuid.Parse(deviceUUID)
	if err != nil {
		return gen.FieldDevice{}, err
	}
	var pf *string
	if platform != "" {
		pf = &platform
	}
	return h.q.UpsertFieldDevice(r.Context(), gen.UpsertFieldDeviceParams{UserID: repID, DeviceUuid: du, Platform: pf})
}

// pull returns the rep-scoped change_log delta after ?since, advancing the
// device's stored cursor to the new high-water mark.
func (h *Handler) pull(w http.ResponseWriter, r *http.Request) {
	rep, org, ok := principal(r)
	if !ok {
		response.Fail(w, http.StatusUnauthorized, "unauthorized", "no claims")
		return
	}
	dev, err := h.device(r, rep, r.URL.Query().Get("device"), r.URL.Query().Get("platform"))
	if err != nil {
		response.Fail(w, http.StatusBadRequest, "bad_request", "valid device uuid required")
		return
	}
	since, _ := strconv.ParseInt(r.URL.Query().Get("since"), 10, 64)

	rows, err := h.q.PullChanges(r.Context(), gen.PullChangesParams{
		OrganizationID: org, ScopeRepID: &rep, ID: since, Limit: pullBatch,
	})
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "pull failed")
		return
	}
	changes := make([]map[string]any, 0, len(rows))
	cursor := since
	for _, c := range rows {
		changes = append(changes, map[string]any{
			"id": c.ID, "entity_type": c.EntityType, "entity_id": c.EntityID,
			"op": c.Op, "payload": jsonRaw(c.Payload),
		})
		cursor = c.ID
	}
	if len(rows) < pullBatch {
		// Caught up: advance to the current high-water so an empty next pull is a no-op.
		if hw, err := h.q.MaxScopedCursor(r.Context(), gen.MaxScopedCursorParams{OrganizationID: org, ScopeRepID: &rep, Column3: since}); err == nil && hw > cursor {
			cursor = hw
		}
	}
	_ = h.q.SetDeviceCursor(r.Context(), gen.SetDeviceCursorParams{ID: dev.ID, LastSyncCursor: cursor})

	response.JSON(w, http.StatusOK, map[string]any{"cursor": cursor, "changes": changes})
}

func (h *Handler) listDevices(w http.ResponseWriter, r *http.Request) {
	_, org, ok := principal(r)
	if !ok {
		response.Fail(w, http.StatusUnauthorized, "unauthorized", "no claims")
		return
	}
	rows, err := h.q.ListFieldDevices(r.Context(), org)
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not list devices")
		return
	}
	items := make([]map[string]any, 0, len(rows))
	for _, d := range rows {
		items = append(items, map[string]any{
			"id": d.ID, "user_id": d.UserID, "user_email": d.UserEmail,
			"device_uuid": d.DeviceUuid.String(), "platform": d.Platform,
			"last_sync_cursor": d.LastSyncCursor, "last_seen_at": tsString(d.LastSeenAt),
		})
	}
	response.JSON(w, http.StatusOK, map[string]any{"items": items})
}
