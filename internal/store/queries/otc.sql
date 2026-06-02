-- Order-to-cash queries — Implementation Pack 1 §7.

-- ===== Shipments ===========================================================

-- name: CreateShipment :one
INSERT INTO shipments (order_id, carrier, tracking_number)
VALUES ($1, $2, $3)
RETURNING *;

-- name: AddShipmentItem :one
INSERT INTO shipment_items (shipment_id, order_item_id, quantity)
VALUES ($1, $2, $3)
RETURNING *;

-- name: GetShipment :one
SELECT * FROM shipments WHERE id = $1;

-- name: ListShipmentsForOrder :many
SELECT * FROM shipments WHERE order_id = $1 ORDER BY created_at;

-- name: ListShipmentItems :many
SELECT * FROM shipment_items WHERE shipment_id = $1 ORDER BY id;

-- name: SetShipmentStatus :one
UPDATE shipments SET status = $2, shipped_at = COALESCE($3, shipped_at) WHERE id = $1 RETURNING *;

-- ShippedQtyForOrderItem returns the total already shipped for an order line,
-- used to cap new shipment quantities (§7 AC).
-- name: ShippedQtyForOrderItem :one
SELECT COALESCE(SUM(quantity), 0)::numeric(15,4) AS shipped FROM shipment_items WHERE order_item_id = $1;

-- name: GetOrderItem :one
SELECT * FROM order_items WHERE id = $1 AND order_id = $2;

-- ===== Invoices ============================================================

-- name: CreateInvoice :one
INSERT INTO invoices (order_id, customer_id, status, currency, subtotal, tax_total, grand_total, issued_at, due_at)
VALUES ($1, $2, 'issued', $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: AddInvoiceItem :one
INSERT INTO invoice_items (invoice_id, description, quantity, unit_price, tax_amount, row_total)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: GetInvoice :one
SELECT i.* FROM invoices i
JOIN customers c ON c.id = i.customer_id
WHERE c.organization_id = $1 AND i.id = $2;

-- name: GetInvoiceByIDInternal :one
SELECT * FROM invoices WHERE id = $1;

-- name: GetInvoiceByPublicID :one
SELECT * FROM invoices WHERE public_id = $1;

-- name: ListInvoiceItems :many
SELECT * FROM invoice_items WHERE invoice_id = $1 ORDER BY id;

-- name: ListInvoicesForOrder :many
SELECT * FROM invoices WHERE order_id = $1 ORDER BY created_at;

-- name: ListInvoicesForCustomer :many
SELECT * FROM invoices WHERE customer_id = $1 ORDER BY created_at DESC;

-- name: SetInvoicePDFURL :exec
UPDATE invoices SET pdf_url = $2 WHERE id = $1;

-- name: SetInvoiceStatus :one
UPDATE invoices SET status = $2 WHERE id = $1 RETURNING *;

-- SumOpenInvoices totals a customer's unpaid (issued/overdue) invoices, used to
-- enforce the credit limit when paying on terms.
-- name: SumOpenInvoices :one
SELECT COALESCE(SUM(grand_total), 0)::numeric(15,4) AS open_total
FROM invoices WHERE customer_id = $1 AND status IN ('issued','overdue');

-- ===== Payments ============================================================

-- name: CreatePayment :one
INSERT INTO payments (invoice_id, order_id, customer_id, method, gateway, gateway_reference, amount, currency, status, captured_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
RETURNING *;

-- name: GetPayment :one
SELECT * FROM payments WHERE id = $1;

-- name: ListPaymentsForInvoice :many
SELECT * FROM payments WHERE invoice_id = $1 ORDER BY created_at;

-- name: SetPaymentStatus :one
UPDATE payments SET status = $2, captured_at = COALESCE($3, captured_at) WHERE id = $1 RETURNING *;

-- SumCapturedForInvoice totals captured payments against an invoice.
-- name: SumCapturedForInvoice :one
SELECT COALESCE(SUM(amount), 0)::numeric(15,4) AS captured
FROM payments WHERE invoice_id = $1 AND status = 'captured';

-- ===== Customer billing terms =============================================

-- name: GetCustomerBilling :one
SELECT id, organization_id, payment_terms_days, credit_limit
FROM customers WHERE id = $1 AND deleted_at IS NULL;
