package crm

import (
	"net/http"
	"sort"
	"strconv"
	"time"

	"b2bcommerce/internal/server/response"
)

// accountHealth surfaces churn-risk signals per customer from order history: an
// established account is "at risk" when it's overdue to order (well past its own
// average cadence) or its recent 90-day order count dropped vs the prior 90.
// Optional filters: ?rep_id= (assigned rep) and ?at_risk=true.
func (h *Handler) accountHealth(w http.ResponseWriter, r *http.Request) {
	a, ok := admin(r)
	if !ok {
		unauthorized(w)
		return
	}
	rows, err := h.q.AccountHealth(r.Context(), a.orgID)
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not compute account health")
		return
	}
	var repFilter *int64
	if s := r.URL.Query().Get("rep_id"); s != "" {
		if n, err := strconv.ParseInt(s, 10, 64); err == nil {
			repFilter = &n
		}
	}
	atRiskOnly := r.URL.Query().Get("at_risk") == "true"

	type account struct {
		CustomerID      int64  `json:"customer_id"`
		Name            string `json:"name"`
		RepID           *int64 `json:"rep_id"`
		OrderCount      int64  `json:"order_count"`
		LastOrdered     string `json:"last_ordered"`
		DaysSince       int    `json:"days_since"`
		AvgIntervalDays int    `json:"avg_interval_days"`
		LifetimeValue   string `json:"lifetime_value"`
		RecentCount     int64  `json:"recent_count"`
		PriorCount      int64  `json:"prior_count"`
		AtRisk          bool   `json:"at_risk"`
		Reason          string `json:"reason"`
	}
	now := time.Now()
	out := make([]account, 0, len(rows))
	for _, row := range rows {
		if repFilter != nil && (row.RepID == nil || *row.RepID != *repFilter) {
			continue
		}
		daysSince := int(now.Sub(row.LastOrdered).Hours() / 24)
		avg := 0
		established := row.OrderCount >= 2
		if established {
			span := row.LastOrdered.Sub(row.FirstOrdered).Hours() / 24
			avg = int(span / float64(row.OrderCount-1))
		}
		atRisk, reason := false, ""
		if established {
			if avg > 0 && float64(daysSince) > 1.5*float64(avg) {
				atRisk, reason = true, "overdue to reorder (well past its usual cadence)"
			} else if row.PriorCount > 0 && row.RecentCount < row.PriorCount {
				atRisk, reason = true, "ordering less than the prior quarter"
			}
		}
		if atRiskOnly && !atRisk {
			continue
		}
		out = append(out, account{
			CustomerID: row.CustomerID, Name: row.Name, RepID: row.RepID, OrderCount: row.OrderCount,
			LastOrdered: row.LastOrdered.Format(time.RFC3339), DaysSince: daysSince, AvgIntervalDays: avg,
			LifetimeValue: row.LifetimeValue, RecentCount: row.RecentCount, PriorCount: row.PriorCount,
			AtRisk: atRisk, Reason: reason,
		})
	}
	// At-risk first, then by longest silence.
	sort.Slice(out, func(i, j int) bool {
		if out[i].AtRisk != out[j].AtRisk {
			return out[i].AtRisk
		}
		return out[i].DaysSince > out[j].DaysSince
	})
	response.JSON(w, http.StatusOK, map[string]any{"items": out})
}
