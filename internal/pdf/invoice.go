package pdf

import (
	"bytes"
	"html/template"
)

// Address is the display form of an order's snapshotted billing/shipping
// address. Field names match both the storefront address-input shape and the
// marshalled customer-address row, so either JSON snapshot unmarshals cleanly.
type Address struct {
	Line1      string `json:"line1"`
	Line2      string `json:"line2"`
	City       string `json:"city"`
	Region     string `json:"region"`
	PostalCode string `json:"postal_code"`
	Country    string `json:"country"`
}

// Empty reports whether the address has no usable lines (an unset "{}" snapshot).
func (a Address) Empty() bool {
	return a.Line1 == "" && a.City == "" && a.Country == ""
}

// InvoiceLine is one billed line.
type InvoiceLine struct {
	Description string
	Quantity    string
	UnitPrice   string
	RowTotal    string
}

// InvoiceData is everything the invoice template needs; the caller formats
// dates/amounts into strings so this package stays free of DB types.
type InvoiceData struct {
	OrganizationName string
	CustomerName     string
	Number           string // short, human-facing invoice reference
	Status           string
	Currency         string
	IssuedAt         string
	DueAt            string
	OrderNumber      string
	PONumber         string
	Billing          Address
	Shipping         Address
	Lines            []InvoiceLine
	Subtotal         string
	TaxTotal         string
	GrandTotal       string
}

// RenderInvoiceHTML renders the invoice document to a standalone HTML string,
// suitable for handing to a Renderer.
func RenderInvoiceHTML(d InvoiceData) (string, error) {
	var buf bytes.Buffer
	if err := invoiceTmpl.Execute(&buf, d); err != nil {
		return "", err
	}
	return buf.String(), nil
}

var invoiceTmpl = template.Must(template.New("invoice").Parse(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>Invoice {{.Number}}</title>
<style>
  * { box-sizing: border-box; }
  body { font-family: -apple-system, Segoe UI, Roboto, Helvetica, Arial, sans-serif; color: #1e293b; margin: 48px; font-size: 13px; }
  .head { display: flex; justify-content: space-between; align-items: flex-start; border-bottom: 2px solid #0ea5e9; padding-bottom: 16px; }
  .org { font-size: 20px; font-weight: 700; }
  .doc { text-align: right; }
  .doc h1 { margin: 0; font-size: 26px; letter-spacing: 1px; color: #0ea5e9; }
  .muted { color: #64748b; }
  .meta { display: flex; gap: 48px; margin: 24px 0; }
  .meta h3 { margin: 0 0 4px; font-size: 11px; text-transform: uppercase; letter-spacing: .05em; color: #64748b; }
  table { width: 100%; border-collapse: collapse; margin-top: 16px; }
  th { text-align: left; font-size: 11px; text-transform: uppercase; letter-spacing: .05em; color: #64748b; border-bottom: 1px solid #cbd5e1; padding: 8px 6px; }
  td { padding: 8px 6px; border-bottom: 1px solid #eef2f7; }
  td.num, th.num { text-align: right; font-variant-numeric: tabular-nums; }
  .totals { margin-top: 16px; margin-left: auto; width: 280px; }
  .totals .row { display: flex; justify-content: space-between; padding: 6px 0; }
  .totals .grand { border-top: 2px solid #0ea5e9; margin-top: 6px; padding-top: 10px; font-size: 16px; font-weight: 700; }
  .foot { margin-top: 48px; color: #94a3b8; font-size: 11px; }
</style>
</head>
<body>
  <div class="head">
    <div class="org">{{.OrganizationName}}</div>
    <div class="doc">
      <h1>INVOICE</h1>
      <div class="muted">{{.Number}}</div>
      <div class="muted">Status: {{.Status}}</div>
    </div>
  </div>

  <div class="meta">
    <div>
      <h3>Billed to</h3>
      <div><strong>{{.CustomerName}}</strong></div>
      {{if not .Billing.Empty}}
        <div>{{.Billing.Line1}}</div>
        {{if .Billing.Line2}}<div>{{.Billing.Line2}}</div>{{end}}
        <div>{{.Billing.City}}{{if .Billing.Region}}, {{.Billing.Region}}{{end}} {{.Billing.PostalCode}}</div>
        <div>{{.Billing.Country}}</div>
      {{end}}
    </div>
    {{if not .Shipping.Empty}}
    <div>
      <h3>Ship to</h3>
      <div>{{.Shipping.Line1}}</div>
      {{if .Shipping.Line2}}<div>{{.Shipping.Line2}}</div>{{end}}
      <div>{{.Shipping.City}}{{if .Shipping.Region}}, {{.Shipping.Region}}{{end}} {{.Shipping.PostalCode}}</div>
      <div>{{.Shipping.Country}}</div>
    </div>
    {{end}}
    <div>
      <h3>Details</h3>
      <div class="muted">Issued</div><div>{{.IssuedAt}}</div>
      <div class="muted">Due</div><div>{{.DueAt}}</div>
      <div class="muted">Order</div><div>{{.OrderNumber}}</div>
      {{if .PONumber}}<div class="muted">PO number</div><div>{{.PONumber}}</div>{{end}}
    </div>
  </div>

  <table>
    <thead>
      <tr>
        <th>Description</th>
        <th class="num">Qty</th>
        <th class="num">Unit price</th>
        <th class="num">Amount</th>
      </tr>
    </thead>
    <tbody>
      {{range .Lines}}
      <tr>
        <td>{{.Description}}</td>
        <td class="num">{{.Quantity}}</td>
        <td class="num">{{.UnitPrice}}</td>
        <td class="num">{{.RowTotal}}</td>
      </tr>
      {{end}}
    </tbody>
  </table>

  <div class="totals">
    <div class="row"><span>Subtotal</span><span>{{.Subtotal}} {{.Currency}}</span></div>
    <div class="row"><span>Tax</span><span>{{.TaxTotal}} {{.Currency}}</span></div>
    <div class="row grand"><span>Total</span><span>{{.GrandTotal}} {{.Currency}}</span></div>
  </div>

  <div class="foot">Generated by Teggo · Invoice {{.Number}}</div>
</body>
</html>
`))
