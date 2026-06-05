package account

import (
	"net/http"
	"sort"
	"time"

	"b2bcommerce/internal/server/response"
)

// reorderSuggestions infers each product's reorder cadence from the buyer's
// order history and returns the items that are due (or overdue) for reorder —
// time since the last order has reached the average interval between orders.
// A smart-replenishment nudge built purely from existing order data.
func (h *Handler) reorderSuggestions(w http.ResponseWriter, r *http.Request) {
	p, ok := actor(r)
	if !ok {
		response.Fail(w, http.StatusUnauthorized, "unauthorized", "no customer context")
		return
	}
	rows, err := h.q.ReorderCadence(r.Context(), p.customerID)
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not compute suggestions")
		return
	}
	type suggestion struct {
		Slug            string `json:"slug"`
		SKU             string `json:"sku"`
		Name            string `json:"name"`
		Unit            string `json:"unit"`
		OrderCount      int64  `json:"order_count"`
		LastOrdered     string `json:"last_ordered"`
		AvgIntervalDays int    `json:"avg_interval_days"`
		DaysSince       int    `json:"days_since"`
		DaysOverdue     int    `json:"days_overdue"`
	}
	now := time.Now()
	out := make([]suggestion, 0, len(rows))
	for _, row := range rows {
		span := row.LastOrdered.Sub(row.FirstOrdered).Hours() / 24
		avg := int(span / float64(row.OrderCount-1)) // intervals = orders - 1
		if avg <= 0 {
			continue // sub-day cadence is noise, skip
		}
		daysSince := int(now.Sub(row.LastOrdered).Hours() / 24)
		if daysSince < avg {
			continue // not due yet
		}
		out = append(out, suggestion{
			Slug: row.Slug, SKU: row.Sku, Name: row.Name, Unit: row.Unit,
			OrderCount: row.OrderCount, LastOrdered: row.LastOrdered.Format(time.RFC3339),
			AvgIntervalDays: avg, DaysSince: daysSince, DaysOverdue: daysSince - avg,
		})
	}
	// Most-overdue first.
	sort.Slice(out, func(i, j int) bool { return out[i].DaysOverdue > out[j].DaysOverdue })
	response.JSON(w, http.StatusOK, map[string]any{"items": out})
}
