package otc

import (
	"net/http"
	"time"

	"b2bcommerce/internal/money"
	"b2bcommerce/internal/server/response"
)

// AR aging + dunning (revenue ops). The aging report buckets the org's unpaid
// invoices by how far past due they are; the sweep flips past-due invoices to
// 'overdue' and dunns the customer (the same work the scheduled mark_overdue
// automation action does, exposed for a manual run from the admin).

type agingInvoice struct {
	PublicID    string `json:"public_id"`
	CustomerID  int64  `json:"customer_id"`
	Status      string `json:"status"`
	GrandTotal  string `json:"grand_total"`
	Currency    string `json:"currency"`
	DueAt       string `json:"due_at,omitempty"`
	DaysOverdue int    `json:"days_overdue"`
	Bucket      string `json:"bucket"`
}

// bucketFor classifies an invoice by days past due.
func bucketFor(days int) string {
	switch {
	case days <= 0:
		return "current"
	case days <= 30:
		return "1-30"
	case days <= 60:
		return "31-60"
	case days <= 90:
		return "61-90"
	default:
		return "90+"
	}
}

func (h *Handler) invoiceAging(w http.ResponseWriter, r *http.Request) {
	a, ok := admin(r)
	if !ok {
		unauthorized(w)
		return
	}
	rows, err := h.q.ListOpenInvoicesForOrg(r.Context(), a.orgID)
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not load open invoices")
		return
	}
	now := time.Now()
	buckets := map[string]string{"current": "0", "1-30": "0", "31-60": "0", "61-90": "0", "90+": "0"}
	items := make([]agingInvoice, 0, len(rows))
	for _, inv := range rows {
		days := 0
		due := ""
		if inv.DueAt.Valid {
			days = int(now.Sub(inv.DueAt.Time).Hours() / 24)
			due = inv.DueAt.Time.Format(time.RFC3339)
		}
		b := bucketFor(days)
		buckets[b], _ = money.Sum(buckets[b], inv.GrandTotal)
		items = append(items, agingInvoice{
			PublicID: inv.PublicID.String(), CustomerID: inv.CustomerID, Status: inv.Status,
			GrandTotal: inv.GrandTotal, Currency: inv.Currency, DueAt: due, DaysOverdue: days, Bucket: b,
		})
	}
	total, _ := money.Sum(buckets["current"], buckets["1-30"], buckets["31-60"], buckets["61-90"], buckets["90+"])
	response.JSON(w, http.StatusOK, map[string]any{"buckets": buckets, "open_total": total, "items": items})
}

func (h *Handler) runOverdueSweep(w http.ResponseWriter, r *http.Request) {
	a, ok := admin(r)
	if !ok {
		unauthorized(w)
		return
	}
	marked, err := h.q.MarkOverdueInvoicesForOrg(r.Context(), a.orgID)
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not run overdue sweep")
		return
	}
	// Dunn each newly-overdue invoice's primary contact (best-effort).
	if h.notify != nil {
		for _, inv := range marked {
			users, err := h.q.ListCustomerUsers(r.Context(), inv.CustomerID)
			if err != nil || len(users) == 0 {
				continue
			}
			_ = h.notify.EnqueueEmail(r.Context(), users[0].Email, "invoice_overdue", map[string]any{
				"name":           users[0].FullName,
				"invoice_number": "INV-" + inv.PublicID.String()[:8],
				"amount":         inv.GrandTotal,
				"currency":       inv.Currency,
			})
		}
	}
	response.JSON(w, http.StatusOK, map[string]any{"marked_overdue": len(marked)})
}
