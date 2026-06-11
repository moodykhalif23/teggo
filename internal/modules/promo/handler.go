// Package promo is the admin CRUD surface for promotions & coupons. The
// evaluation engine lives in internal/promotions; this module manages the
// records the cart/checkout reads. Gated by promotion.view / promotion.manage.
package promo

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"b2bcommerce/internal/money"
	mw "b2bcommerce/internal/server/middleware"
	"b2bcommerce/internal/server/response"
	"b2bcommerce/internal/store/gen"
)

type Handler struct {
	q *gen.Queries
}

func New(q *gen.Queries) *Handler { return &Handler{q: q} }

func (h *Handler) Routes(r chi.Router, authMW func(http.Handler) http.Handler) {
	r.Group(func(ar chi.Router) {
		ar.Use(authMW)
		ar.Use(mw.RequireAudience("admin"))

		ar.With(mw.RequirePermission("promotion.view")).Get("/admin/promotions", h.list)
		ar.With(mw.RequirePermission("promotion.manage")).Post("/admin/promotions", h.create)
		ar.With(mw.RequirePermission("promotion.view")).Get("/admin/promotions/{id}", h.get)
		ar.With(mw.RequirePermission("promotion.manage")).Put("/admin/promotions/{id}", h.update)
		ar.With(mw.RequirePermission("promotion.manage")).Delete("/admin/promotions/{id}", h.delete)
	})
}

func orgID(r *http.Request) (int64, bool) {
	c, ok := mw.ClaimsFrom(r.Context())
	if !ok {
		return 0, false
	}
	return c.OrgID, true
}

// ---- DTO -----------------------------------------------------------------

type promotionDTO struct {
	ID             int64      `json:"id"`
	PublicID       string     `json:"public_id"`
	Name           string     `json:"name"`
	Description    *string    `json:"description,omitempty"`
	Code           *string    `json:"code,omitempty"`
	DiscountType   string     `json:"discount_type"`
	DiscountValue  string     `json:"discount_value"`
	MinSubtotal    *string    `json:"min_subtotal,omitempty"`
	StartsAt       *time.Time `json:"starts_at,omitempty"`
	EndsAt         *time.Time `json:"ends_at,omitempty"`
	MaxRedemptions *int32     `json:"max_redemptions,omitempty"`
	TimesRedeemed  int32      `json:"times_redeemed"`
	Priority       int32      `json:"priority"`
	IsActive       bool       `json:"is_active"`
}

func tsToPtr(ts pgtype.Timestamptz) *time.Time {
	if !ts.Valid {
		return nil
	}
	t := ts.Time
	return &t
}

func ptrToTS(t *time.Time) pgtype.Timestamptz {
	if t == nil {
		return pgtype.Timestamptz{}
	}
	return pgtype.Timestamptz{Time: *t, Valid: true}
}

func toDTO(p gen.Promotion) promotionDTO {
	return promotionDTO{
		ID: p.ID, PublicID: p.PublicID.String(), Name: p.Name, Description: p.Description,
		Code: p.Code, DiscountType: p.DiscountType, DiscountValue: p.DiscountValue,
		MinSubtotal: p.MinSubtotal, StartsAt: tsToPtr(p.StartsAt), EndsAt: tsToPtr(p.EndsAt),
		MaxRedemptions: p.MaxRedemptions, TimesRedeemed: p.TimesRedeemed, Priority: p.Priority, IsActive: p.IsActive,
	}
}

type promotionInput struct {
	Name           string     `json:"name"`
	Description    *string    `json:"description"`
	Code           *string    `json:"code"`
	DiscountType   string     `json:"discount_type"`
	DiscountValue  string     `json:"discount_value"`
	MinSubtotal    *string    `json:"min_subtotal"`
	StartsAt       *time.Time `json:"starts_at"`
	EndsAt         *time.Time `json:"ends_at"`
	MaxRedemptions *int32     `json:"max_redemptions"`
	Priority       int32      `json:"priority"`
	IsActive       *bool      `json:"is_active"`
}

// normalize validates and cleans an input; returns an error message (or "").
func (in *promotionInput) normalize() string {
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		return "name is required"
	}
	if in.DiscountType != "percent" && in.DiscountType != "amount" {
		return "discount_type must be 'percent' or 'amount'"
	}
	v, err := money.Parse(in.DiscountValue)
	if err != nil {
		return "discount_value must be a number"
	}
	if v.Sign() < 0 {
		return "discount_value cannot be negative"
	}
	if in.DiscountType == "percent" {
		if cmp, _ := money.Cmp(in.DiscountValue, "100"); cmp > 0 {
			return "a percent discount cannot exceed 100"
		}
	}
	// Empty/whitespace code → automatic promotion (NULL).
	if in.Code != nil {
		c := strings.TrimSpace(*in.Code)
		if c == "" {
			in.Code = nil
		} else {
			in.Code = &c
		}
	}
	if in.MinSubtotal != nil {
		if _, err := money.Parse(*in.MinSubtotal); err != nil {
			return "min_subtotal must be a number"
		}
	}
	if in.StartsAt != nil && in.EndsAt != nil && in.EndsAt.Before(*in.StartsAt) {
		return "ends_at must be after starts_at"
	}
	return ""
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	org, ok := orgID(r)
	if !ok {
		response.Fail(w, http.StatusUnauthorized, "unauthorized", "no claims")
		return
	}
	rows, err := h.q.ListPromotions(r.Context(), org)
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not list promotions")
		return
	}
	items := make([]promotionDTO, 0, len(rows))
	for _, p := range rows {
		items = append(items, toDTO(p))
	}
	response.JSON(w, http.StatusOK, map[string]any{"items": items})
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
	p, err := h.q.GetPromotion(r.Context(), gen.GetPromotionParams{OrganizationID: org, ID: id})
	if err != nil {
		response.Fail(w, http.StatusNotFound, "not_found", "promotion not found")
		return
	}
	response.JSON(w, http.StatusOK, toDTO(p))
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	org, ok := orgID(r)
	if !ok {
		response.Fail(w, http.StatusUnauthorized, "unauthorized", "no claims")
		return
	}
	var in promotionInput
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
	p, err := h.q.CreatePromotion(r.Context(), gen.CreatePromotionParams{
		OrganizationID: org, Name: in.Name, Description: in.Description, Code: in.Code,
		DiscountType: in.DiscountType, DiscountValue: in.DiscountValue, MinSubtotal: in.MinSubtotal,
		StartsAt: ptrToTS(in.StartsAt), EndsAt: ptrToTS(in.EndsAt),
		MaxRedemptions: in.MaxRedemptions, Priority: in.Priority, IsActive: active,
	})
	if err != nil {
		if isDuplicateCode(err) {
			response.Fail(w, http.StatusConflict, "conflict", "a promotion with this code already exists")
			return
		}
		response.Fail(w, http.StatusInternalServerError, "internal", "could not create promotion")
		return
	}
	response.JSON(w, http.StatusCreated, toDTO(p))
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
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
	var in promotionInput
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
	p, err := h.q.UpdatePromotion(r.Context(), gen.UpdatePromotionParams{
		OrganizationID: org, ID: id, Name: in.Name, Description: in.Description, Code: in.Code,
		DiscountType: in.DiscountType, DiscountValue: in.DiscountValue, MinSubtotal: in.MinSubtotal,
		StartsAt: ptrToTS(in.StartsAt), EndsAt: ptrToTS(in.EndsAt),
		MaxRedemptions: in.MaxRedemptions, Priority: in.Priority, IsActive: active,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			response.Fail(w, http.StatusNotFound, "not_found", "promotion not found")
			return
		}
		if isDuplicateCode(err) {
			response.Fail(w, http.StatusConflict, "conflict", "a promotion with this code already exists")
			return
		}
		response.Fail(w, http.StatusInternalServerError, "internal", "could not update promotion")
		return
	}
	response.JSON(w, http.StatusOK, toDTO(p))
}

func (h *Handler) delete(w http.ResponseWriter, r *http.Request) {
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
	n, err := h.q.DeletePromotion(r.Context(), gen.DeletePromotionParams{OrganizationID: org, ID: id})
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not delete promotion")
		return
	}
	if n == 0 {
		response.Fail(w, http.StatusNotFound, "not_found", "promotion not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// isDuplicateCode detects the unique coupon-code index violation (SQLSTATE 23505).
func isDuplicateCode(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
