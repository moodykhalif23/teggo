// Package exports is the data-export center: it streams full-record exports of
// the core business entities (orders, order line items, customers, invoices) for
// the customer's own finance/BI/budgeting systems. Distinct from the report
// builder (which aggregates) — this dumps raw rows joined to human-readable
// names, org-scoped.
//
// CSV is streamed straight from a database cursor, so an export is effectively
// unbounded with constant memory (industrial catalogs export fine). XLSX is
// built in memory and therefore capped. A manifest advertises the datasets the
// caller may export; each download is gated by that entity's own view permission
// (least privilege) and recorded in the audit trail.
package exports

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"b2bcommerce/internal/audit"
	"b2bcommerce/internal/auth"
	"b2bcommerce/internal/export"
	mw "b2bcommerce/internal/server/middleware"
	"b2bcommerce/internal/server/response"
)

// xlsxCap bounds the in-memory XLSX build. CSV streams from a cursor and is not
// capped.
const xlsxCap = 100000

type dataset struct {
	key, label, description, permission string
	columns                             []export.Column
	// sql is org-scoped on $1, ordered, and carries NO LIMIT — the CSV path
	// streams every row; the XLSX path appends "LIMIT $2".
	sql  string
	scan func(pgx.Rows) ([]string, error)
}

var order = []string{"orders", "order-items", "customers", "invoices"}

type Handler struct {
	pool     *pgxpool.Pool
	auditRec *audit.Recorder
	datasets map[string]dataset
}

func New(pool *pgxpool.Pool) *Handler {
	return &Handler{pool: pool, datasets: buildDatasets()}
}

// WithAudit records each export in the audit trail (exports are reads, so the
// audit middleware doesn't see them). No-op when unset.
func (h *Handler) WithAudit(rec *audit.Recorder) *Handler { h.auditRec = rec; return h }

func (h *Handler) Routes(r chi.Router, authMW func(http.Handler) http.Handler) {
	r.Group(func(ar chi.Router) {
		ar.Use(authMW)
		ar.Use(mw.RequireAudience("admin"))
		ar.With(mw.RequirePermission("report.view")).Get("/admin/exports", h.manifest)
		ar.With(mw.RequirePermission("report.view")).Get("/admin/exports/{dataset}", h.download)
	})
}

func (h *Handler) manifest(w http.ResponseWriter, r *http.Request) {
	claims, ok := mw.ClaimsFrom(r.Context())
	if !ok {
		response.Fail(w, http.StatusUnauthorized, "unauthorized", "no claims")
		return
	}
	items := make([]map[string]any, 0, len(order))
	for _, key := range order {
		ds := h.datasets[key]
		if !can(claims.Permissions, ds.permission) {
			continue
		}
		items = append(items, map[string]any{
			"key": ds.key, "label": ds.label, "description": ds.description,
			"formats": []string{"csv", "xlsx"},
		})
	}
	response.JSON(w, http.StatusOK, map[string]any{"datasets": items})
}

func (h *Handler) download(w http.ResponseWriter, r *http.Request) {
	claims, ok := mw.ClaimsFrom(r.Context())
	if !ok {
		response.Fail(w, http.StatusUnauthorized, "unauthorized", "no claims")
		return
	}
	ds, found := h.datasets[chi.URLParam(r, "dataset")]
	if !found {
		response.Fail(w, http.StatusNotFound, "not_found", "unknown dataset")
		return
	}
	if !can(claims.Permissions, ds.permission) {
		response.Fail(w, http.StatusForbidden, "forbidden", "missing "+ds.permission)
		return
	}

	format := r.URL.Query().Get("format")
	stamp := time.Now().UTC().Format("20060102")
	var err error
	if format == "xlsx" {
		err = h.writeXLSX(w, r, ds, stamp)
	} else {
		format = "csv"
		err = h.streamCSV(w, r, ds, claims.OrgID, stamp)
	}
	if err != nil {
		// Headers/body may already be partially written on the streaming path; a
		// clean error response is only possible before the first byte (handled
		// inside the writers). Nothing more to do here but skip the audit.
		return
	}
	h.recordExport(r, claims, ds, format)
}

// streamCSV streams the whole dataset from a cursor — bounded memory, unbounded
// rows. The query is opened first so a failure yields a clean 500 before any
// body is written.
func (h *Handler) streamCSV(w http.ResponseWriter, r *http.Request, ds dataset, org int64, stamp string) error {
	rows, err := h.pool.Query(r.Context(), ds.sql, org)
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not open export")
		return err
	}
	defer rows.Close()

	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", "attachment; filename=\""+ds.key+"-"+stamp+".csv\"")
	stream, err := export.NewCSVStream(w, ds.columns)
	if err != nil {
		return err
	}
	for rows.Next() {
		cells, serr := ds.scan(rows)
		if serr != nil {
			return serr
		}
		if werr := stream.Write(cells); werr != nil {
			return werr
		}
	}
	if rows.Err() != nil {
		return rows.Err()
	}
	return stream.Flush()
}

// writeXLSX buffers up to xlsxCap rows and writes a single-sheet workbook.
func (h *Handler) writeXLSX(w http.ResponseWriter, r *http.Request, ds dataset, stamp string) error {
	claims, _ := mw.ClaimsFrom(r.Context())
	rows, err := h.pool.Query(r.Context(), ds.sql+" LIMIT $2", claims.OrgID, xlsxCap)
	if err != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not build export")
		return err
	}
	defer rows.Close()
	t := export.Table{Columns: ds.columns}
	for rows.Next() {
		cells, serr := ds.scan(rows)
		if serr != nil {
			response.Fail(w, http.StatusInternalServerError, "internal", "could not read export")
			return serr
		}
		t.Rows = append(t.Rows, cells)
	}
	if rows.Err() != nil {
		response.Fail(w, http.StatusInternalServerError, "internal", "could not read export")
		return rows.Err()
	}
	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Set("Content-Disposition", "attachment; filename=\""+ds.key+"-"+stamp+".xlsx\"")
	return export.WriteXLSX(w, t, ds.label)
}

func (h *Handler) recordExport(r *http.Request, claims *auth.Claims, ds dataset, format string) {
	if h.auditRec == nil {
		return
	}
	var actor *int64
	if id, err := strconv.ParseInt(claims.Subject, 10, 64); err == nil {
		actor = &id
	}
	h.auditRec.Record(r, audit.Event{
		OrgID: claims.OrgID, ActorID: actor, Audience: claims.Audience,
		Action: "exports.download", EntityType: ds.key, StatusCode: http.StatusOK,
		Summary:  "Exported " + ds.label + " (" + format + ")",
		Metadata: map[string]any{"format": format},
	})
}

// can reports whether the permission set includes perm (empty perm = allowed).
func can(perms []string, perm string) bool {
	if perm == "" {
		return true
	}
	for _, p := range perms {
		if p == perm {
			return true
		}
	}
	return false
}

func deref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func tsz(t pgtype.Timestamptz) string {
	if !t.Valid {
		return ""
	}
	return t.Time.UTC().Format(time.RFC3339)
}

// ---- dataset definitions (raw SQL + cursor scan → []string) ---------------

func buildDatasets() map[string]dataset {
	return map[string]dataset{
		"orders": {
			key: "orders", label: "Orders", description: "Order headers with totals and customer.",
			permission: "order.view",
			columns: []export.Column{
				{Name: "order_id"}, {Name: "customer"}, {Name: "status"}, {Name: "currency"}, {Name: "po_number"},
				{Name: "subtotal", Numeric: true}, {Name: "tax_total", Numeric: true},
				{Name: "shipping_total", Numeric: true}, {Name: "grand_total", Numeric: true}, {Name: "created_at"},
			},
			sql: `SELECT o.public_id, c.name, o.status, o.currency, o.po_number,
			             o.subtotal, o.tax_total, o.shipping_total, o.grand_total, o.created_at
			      FROM orders o JOIN customers c ON c.id = o.customer_id
			      WHERE o.organization_id = $1
			      ORDER BY o.created_at DESC, o.id DESC`,
			scan: func(rows pgx.Rows) ([]string, error) {
				var id uuid.UUID
				var customer, status, currency, sub, tax, ship, grand string
				var po *string
				var created time.Time
				if err := rows.Scan(&id, &customer, &status, &currency, &po, &sub, &tax, &ship, &grand, &created); err != nil {
					return nil, err
				}
				return []string{id.String(), customer, status, currency, deref(po), sub, tax, ship, grand, created.UTC().Format(time.RFC3339)}, nil
			},
		},
		"order-items": {
			key: "order-items", label: "Order line items", description: "Every order line — SKU, quantity, price — for spend analysis.",
			permission: "order.view",
			columns: []export.Column{
				{Name: "order_id"}, {Name: "customer"}, {Name: "sku"}, {Name: "product"},
				{Name: "quantity", Numeric: true}, {Name: "unit"}, {Name: "unit_price", Numeric: true},
				{Name: "tax_amount", Numeric: true}, {Name: "row_total", Numeric: true}, {Name: "order_status"}, {Name: "order_date"},
			},
			sql: `SELECT o.public_id, c.name, oi.sku, oi.name, oi.quantity, oi.unit,
			             oi.unit_price, oi.tax_amount, oi.row_total, o.status, o.created_at
			      FROM order_items oi
			      JOIN orders o ON o.id = oi.order_id
			      JOIN customers c ON c.id = o.customer_id
			      WHERE o.organization_id = $1
			      ORDER BY o.created_at DESC, oi.id`,
			scan: func(rows pgx.Rows) ([]string, error) {
				var id uuid.UUID
				var customer, sku, name, qty, unit, price, tax, total, status string
				var created time.Time
				if err := rows.Scan(&id, &customer, &sku, &name, &qty, &unit, &price, &tax, &total, &status, &created); err != nil {
					return nil, err
				}
				return []string{id.String(), customer, sku, name, qty, unit, price, tax, total, status, created.UTC().Format(time.RFC3339)}, nil
			},
		},
		"customers": {
			key: "customers", label: "Customers", description: "Accounts with terms, credit limit and group.",
			permission: "customer.view",
			columns: []export.Column{
				{Name: "customer_id"}, {Name: "name"}, {Name: "tax_id"},
				{Name: "payment_terms_days", Numeric: true}, {Name: "credit_limit", Numeric: true},
				{Name: "group"}, {Name: "active"}, {Name: "created_at"},
			},
			sql: `SELECT c.public_id, c.name, c.tax_id, c.payment_terms_days, c.credit_limit,
			             COALESCE(g.name, ''), c.is_active, c.created_at
			      FROM customers c
			      LEFT JOIN customer_groups g ON g.id = c.customer_group_id
			      WHERE c.organization_id = $1 AND c.deleted_at IS NULL
			      ORDER BY c.created_at DESC, c.id DESC`,
			scan: func(rows pgx.Rows) ([]string, error) {
				var id uuid.UUID
				var name, group, credit string
				var taxID *string
				var terms int32
				var active bool
				var created time.Time
				if err := rows.Scan(&id, &name, &taxID, &terms, &credit, &group, &active, &created); err != nil {
					return nil, err
				}
				return []string{id.String(), name, deref(taxID), strconv.Itoa(int(terms)), credit, group, strconv.FormatBool(active), created.UTC().Format(time.RFC3339)}, nil
			},
		},
		"invoices": {
			key: "invoices", label: "Invoices", description: "Invoices with status, totals and due dates.",
			permission: "invoice.view",
			columns: []export.Column{
				{Name: "invoice_id"}, {Name: "customer"}, {Name: "status"}, {Name: "currency"},
				{Name: "subtotal", Numeric: true}, {Name: "tax_total", Numeric: true}, {Name: "grand_total", Numeric: true},
				{Name: "issued_at"}, {Name: "due_at"}, {Name: "created_at"},
			},
			// Invoices carry no organization_id — scoped through their customer.
			sql: `SELECT i.public_id, c.name, i.status, i.currency,
			             i.subtotal, i.tax_total, i.grand_total, i.issued_at, i.due_at, i.created_at
			      FROM invoices i JOIN customers c ON c.id = i.customer_id
			      WHERE c.organization_id = $1
			      ORDER BY i.created_at DESC, i.id DESC`,
			scan: func(rows pgx.Rows) ([]string, error) {
				var id uuid.UUID
				var customer, status, currency, sub, tax, grand string
				var issued, due pgtype.Timestamptz
				var created time.Time
				if err := rows.Scan(&id, &customer, &status, &currency, &sub, &tax, &grand, &issued, &due, &created); err != nil {
					return nil, err
				}
				return []string{id.String(), customer, status, currency, sub, tax, grand, tsz(issued), tsz(due), created.UTC().Format(time.RFC3339)}, nil
			},
		},
	}
}
