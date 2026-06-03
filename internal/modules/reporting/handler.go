// Package reporting serves operational dashboards (Pack 3 §1, V1): sales over
// time, top products, and headline KPIs — all read from precomputed materialized
// views (mv_daily_sales, mv_top_products) so dashboard loads issue bounded, fast
// queries. The views are refreshed on a schedule (river periodic job) and can be
// refreshed on demand. Admin-only, gated by report.view. The custom report
// builder / schedules are V2 and not built yet.
package reporting

import (
	"context"
	"math/big"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"b2bcommerce/internal/money"
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

		ar.With(mw.RequirePermission("report.view")).Get("/admin/reports/summary", h.summary)
		ar.With(mw.RequirePermission("report.view")).Get("/admin/reports/sales", h.sales)
		ar.With(mw.RequirePermission("report.view")).Get("/admin/reports/top-products", h.topProducts)
		ar.With(mw.RequirePermission("report.view")).Post("/admin/reports/refresh", h.refresh)
	})
}

func orgID(r *http.Request) (int64, bool) {
	c, ok := mw.ClaimsFrom(r.Context())
	if !ok {
		return 0, false
	}
	return c.OrgID, true
}

func today() time.Time { return time.Now().UTC().Truncate(24 * time.Hour) }

func dateParam(s string, def time.Time) pgtype.Date {
	if s != "" {
		if t, err := time.Parse("2006-01-02", s); err == nil {
			return pgtype.Date{Time: t, Valid: true}
		}
	}
	return pgtype.Date{Time: def, Valid: true}
}

// RefreshViews refreshes the reporting materialized views concurrently (needs
// their unique indexes). Exported so the periodic river job reuses it. Must run
// outside a transaction (CONCURRENTLY restriction) — pool.Exec satisfies that.
func RefreshViews(ctx context.Context, pool *pgxpool.Pool) error {
	for _, v := range []string{"mv_daily_sales", "mv_top_products"} {
		if _, err := pool.Exec(ctx, "REFRESH MATERIALIZED VIEW CONCURRENTLY "+v); err != nil {
			return err
		}
	}
	return nil
}

// ---- handlers -------------------------------------------------------------

func (h *Handler) summary(w http.ResponseWriter, r *http.Request) {
	org, ok := orgID(r)
	if !ok {
		response.Fail(w, http.StatusUnauthorized, "unauthorized", "no claims")
		return
	}
	days := 30
	if d, err := strconv.Atoi(r.URL.Query().Get("days")); err == nil && d > 0 {
		days = d
	}
	since := pgtype.Date{Time: today().AddDate(0, 0, -days), Valid: true}
	row, err := h.q.SalesSummary(r.Context(), gen.SalesSummaryParams{OrganizationID: org, Day: since})
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not compute summary")
		return
	}
	// Average order value (display metric) = revenue / orders.
	aov := "0"
	if row.OrderCount > 0 {
		if rev, err := money.Parse(row.Revenue); err == nil {
			aov = money.Format(new(big.Rat).Quo(rev, new(big.Rat).SetInt64(row.OrderCount)))
		}
	}
	response.JSON(w, http.StatusOK, map[string]any{
		"days": days, "order_count": row.OrderCount, "revenue": row.Revenue, "avg_order_value": aov,
	})
}

func (h *Handler) sales(w http.ResponseWriter, r *http.Request) {
	org, ok := orgID(r)
	if !ok {
		response.Fail(w, http.StatusUnauthorized, "unauthorized", "no claims")
		return
	}
	from := dateParam(r.URL.Query().Get("from"), today().AddDate(0, 0, -30))
	to := dateParam(r.URL.Query().Get("to"), today())
	rows, err := h.q.DailySales(r.Context(), gen.DailySalesParams{OrganizationID: org, Day: from, Day_2: to})
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not load sales")
		return
	}
	items := make([]map[string]any, 0, len(rows))
	for _, d := range rows {
		items = append(items, map[string]any{
			"day": d.Day.Time.Format("2006-01-02"), "order_count": d.OrderCount, "revenue": d.Revenue,
		})
	}
	response.JSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *Handler) topProducts(w http.ResponseWriter, r *http.Request) {
	org, ok := orgID(r)
	if !ok {
		response.Fail(w, http.StatusUnauthorized, "unauthorized", "no claims")
		return
	}
	// Default to the current month (matches mv_top_products' date_trunc('month')).
	month := today()
	month = time.Date(month.Year(), month.Month(), 1, 0, 0, 0, 0, time.UTC)
	if m := r.URL.Query().Get("month"); m != "" {
		if t, err := time.Parse("2006-01-02", m); err == nil {
			month = time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
		}
	}
	limit := 10
	if l, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && l > 0 {
		limit = l
	}
	rows, err := h.q.TopProducts(r.Context(), gen.TopProductsParams{
		OrganizationID: org, Month: pgtype.Date{Time: month, Valid: true}, Limit: int32(limit),
	})
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not load top products")
		return
	}
	items := make([]map[string]any, 0, len(rows))
	for _, t := range rows {
		items = append(items, map[string]any{
			"product_id": t.ProductID, "sku": t.Sku, "name": t.Name, "qty": t.Qty, "revenue": t.Revenue,
		})
	}
	response.JSON(w, http.StatusOK, map[string]any{"month": month.Format("2006-01-02"), "items": items})
}

func (h *Handler) refresh(w http.ResponseWriter, r *http.Request) {
	if _, ok := orgID(r); !ok {
		response.Fail(w, http.StatusUnauthorized, "unauthorized", "no claims")
		return
	}
	if err := RefreshViews(r.Context(), h.pool); err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not refresh reporting views")
		return
	}
	response.JSON(w, http.StatusOK, map[string]any{"refreshed": true})
}
