// Package edi implements the slice of ANSI X12 needed for B2B order exchange
// (Pack 3 §3): parsing an inbound 850 (purchase order) and emitting 855 (PO
// acknowledgement), 856 (ASN), and 810 (invoice). It is a pragmatic subset that
// round-trips the documents this platform generates and the common shapes
// trading partners send; it is not a full X12 validator. Segments are
// terminated by '~' and elements separated by '*'.
package edi

import (
	"fmt"
	"strings"
)

const (
	elemSep = "*"
	segTerm = "~"
)

// PurchaseOrder is the parsed subset of an inbound 850.
type PurchaseOrder struct {
	PONumber string
	Date     string
	Currency string
	Lines    []POLine
}

// POLine is one ordered line. SKU is the supplier/vendor part number.
type POLine struct {
	LineNo    string
	SKU       string
	UOM       string
	Quantity  string
	UnitPrice string
}

// Parse850 parses an X12 850. It returns an error (never a partial PO) if the
// document is not a well-formed 850 with a PO number and at least one usable
// line.
func Parse850(raw string) (PurchaseOrder, error) {
	segs := segments(raw)
	var po PurchaseOrder
	var isPO bool
	for _, s := range segs {
		switch s[0] {
		case "ST":
			if len(s) > 1 && s[1] == "850" {
				isPO = true
			}
		case "BEG":
			po.PONumber = at(s, 3)
			po.Date = at(s, 5)
		case "CUR":
			po.Currency = at(s, 2)
		case "PO1":
			line := POLine{LineNo: at(s, 1), Quantity: at(s, 2), UOM: at(s, 3), UnitPrice: at(s, 4)}
			line.SKU = productID(s)
			po.Lines = append(po.Lines, line)
		}
	}
	if !isPO {
		return PurchaseOrder{}, fmt.Errorf("not an 850 transaction set")
	}
	if po.PONumber == "" {
		return PurchaseOrder{}, fmt.Errorf("missing PO number (BEG03)")
	}
	if len(po.Lines) == 0 {
		return PurchaseOrder{}, fmt.Errorf("no PO1 line items")
	}
	for i, l := range po.Lines {
		if l.SKU == "" || l.Quantity == "" {
			return PurchaseOrder{}, fmt.Errorf("line %d missing SKU or quantity", i+1)
		}
	}
	if po.Currency == "" {
		po.Currency = "USD"
	}
	return po, nil
}

// productID extracts the part number from a PO1 segment's qualifier/value pairs
// (elements 6+), preferring vendor part (VP/VN) then buyer part (BP), then any.
func productID(s []string) string {
	var fallback string
	for i := 6; i+1 < len(s); i += 2 {
		qual, val := s[i], s[i+1]
		if val == "" {
			continue
		}
		switch qual {
		case "VP", "VN":
			return val
		case "BP", "IN", "UP", "SK":
			if fallback == "" {
				fallback = val
			}
		default:
			if fallback == "" {
				fallback = val
			}
		}
	}
	return fallback
}

// ---- outbound encoders ----------------------------------------------------

// Envelope carries the interchange identifiers shared by every outbound doc.
type Envelope struct {
	SenderID      string // ISA06 / GS02
	ReceiverID    string // ISA08 / GS03
	ControlNumber string // ST02 + interchange/group control
	Date          string // YYYYMMDD
	Time          string // HHMM
}

// AckLine acknowledges one ordered line in an 855.
type AckLine struct {
	LineNo    string
	SKU       string
	Quantity  string
	UOM       string
	UnitPrice string
	Status    string // IA accepted / IR rejected / IB backordered
}

// Encode855 builds a PO acknowledgement for the given PO.
func Encode855(env Envelope, poNumber string, lines []AckLine) string {
	var w writer
	w.envelope(env, "855", "PR")
	w.seg("BAK", "00", "AC", poNumber, "", env.Date)
	for _, l := range lines {
		st := l.Status
		if st == "" {
			st = "IA"
		}
		w.seg("PO1", l.LineNo, l.Quantity, def(l.UOM, "EA"), l.UnitPrice, "", "VP", l.SKU)
		w.seg("ACK", st, l.Quantity, def(l.UOM, "EA"))
	}
	w.seg("CTT", itoa(len(lines)))
	return w.close(env)
}

// InvoiceLine is one billed line in an 810.
type InvoiceLine struct {
	LineNo    string
	SKU       string
	Quantity  string
	UOM       string
	UnitPrice string
	Amount    string
}

// Encode810 builds an invoice document.
func Encode810(env Envelope, invoiceNumber, poNumber, total string, lines []InvoiceLine) string {
	var w writer
	w.envelope(env, "810", "IN")
	w.seg("BIG", env.Date, invoiceNumber, "", poNumber)
	for _, l := range lines {
		w.seg("IT1", l.LineNo, l.Quantity, def(l.UOM, "EA"), l.UnitPrice, "", "VP", l.SKU)
	}
	w.seg("TDS", total)
	w.seg("CTT", itoa(len(lines)))
	return w.close(env)
}

// ShipLine is one shipped line in an 856 ASN.
type ShipLine struct {
	SKU      string
	Quantity string
	UOM      string
}

// Encode856 builds an Advance Ship Notice for a shipment against a PO.
func Encode856(env Envelope, shipmentRef, poNumber string, lines []ShipLine) string {
	var w writer
	w.envelope(env, "856", "SH")
	w.seg("BSN", "00", shipmentRef, env.Date, env.Time)
	// HL hierarchy: shipment(1) → order(2) → items(3..).
	w.seg("HL", "1", "", "S")
	w.seg("HL", "2", "1", "O")
	w.seg("PRF", poNumber)
	for i, l := range lines {
		w.seg("HL", itoa(3+i), "2", "I")
		w.seg("LIN", "", "VP", l.SKU)
		w.seg("SN1", "", l.Quantity, def(l.UOM, "EA"))
	}
	return w.close(env)
}

// ---- low-level segment writer ---------------------------------------------

type writer struct {
	sb       strings.Builder
	segCount int // ST..SE inclusive
}

// envelope writes ISA/GS/ST openers. functionalID is the GS01 (e.g. PR, IN, SH).
func (w *writer) envelope(env Envelope, txnSet, functionalID string) {
	// ISA has 16 fixed-width-ish elements; values are illustrative for self-host.
	w.raw("ISA", "00", pad("", 10), "00", pad("", 10), "ZZ", padR(env.SenderID, 15),
		"ZZ", padR(env.ReceiverID, 15), env.Date[2:], env.Time, "U", "00401",
		padL(env.ControlNumber, 9), "0", "P", ">")
	w.raw("GS", functionalID, env.SenderID, env.ReceiverID, env.Date, env.Time, env.ControlNumber, "X", "004010")
	w.seg("ST", txnSet, padL(env.ControlNumber, 4))
}

// seg writes a counted transaction-set segment.
func (w *writer) seg(name string, elems ...string) {
	w.segCount++
	w.raw(name, elems...)
}

// raw writes a segment without counting (envelope framing).
func (w *writer) raw(name string, elems ...string) {
	w.sb.WriteString(name)
	for _, e := range elems {
		w.sb.WriteString(elemSep)
		w.sb.WriteString(e)
	}
	w.sb.WriteString(segTerm)
}

// close writes SE/GE/IEA trailers (SE count includes ST and SE).
func (w *writer) close(env Envelope) string {
	w.raw("SE", itoa(w.segCount+1), padL(env.ControlNumber, 4))
	w.raw("GE", "1", env.ControlNumber)
	w.raw("IEA", "1", padL(env.ControlNumber, 9))
	return w.sb.String()
}

// ---- helpers --------------------------------------------------------------

func segments(raw string) [][]string {
	var out [][]string
	for _, part := range strings.Split(raw, segTerm) {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		out = append(out, strings.Split(part, elemSep))
	}
	return out
}

func at(s []string, i int) string {
	if i < len(s) {
		return strings.TrimSpace(s[i])
	}
	return ""
}

func def(v, d string) string {
	if v == "" {
		return d
	}
	return v
}

func itoa(n int) string { return fmt.Sprintf("%d", n) }

func pad(s string, n int) string { return padR(s, n) }
func padR(s string, n int) string { // right-pad with spaces
	for len(s) < n {
		s += " "
	}
	return s
}
func padL(s string, n int) string { // left-pad with zeros
	for len(s) < n {
		s = "0" + s
	}
	return s
}
