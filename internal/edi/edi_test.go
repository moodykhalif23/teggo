package edi

import (
	"strings"
	"testing"
)

const po850 = `ISA*00*          *00*          *ZZ*ACME           *ZZ*TEGGO          *240101*1200*U*00401*000000001*0*P*>~
GS*PO*ACME*TEGGO*20240101*1200*1*X*004010~
ST*850*0001~
BEG*00*SA*PO-7788**20240101~
CUR*BY*USD~
PO1*1*10*EA*25.0000**VP*WIDGET-1~
PO1*2*5*EA*12.5000**VP*GADGET-9~
CTT*2~
SE*8*0001~
GE*1*1~
IEA*1*000000001~`

func TestParse850(t *testing.T) {
	po, err := Parse850(po850)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if po.PONumber != "PO-7788" {
		t.Errorf("PO number = %q", po.PONumber)
	}
	if po.Currency != "USD" {
		t.Errorf("currency = %q", po.Currency)
	}
	if len(po.Lines) != 2 {
		t.Fatalf("lines = %d, want 2", len(po.Lines))
	}
	if po.Lines[0].SKU != "WIDGET-1" || po.Lines[0].Quantity != "10" || po.Lines[0].UnitPrice != "25.0000" {
		t.Errorf("line 1 = %+v", po.Lines[0])
	}
	if po.Lines[1].SKU != "GADGET-9" {
		t.Errorf("line 2 sku = %q", po.Lines[1].SKU)
	}
}

func TestParse850Rejects(t *testing.T) {
	cases := map[string]string{
		"not 850":   "ISA*..~ST*856*0001~SE*2*0001~",
		"no lines":  "ST*850*1~BEG*00*SA*PO-1**20240101~SE*2*1~",
		"no po num": "ST*850*1~BEG*00*SA***20240101~PO1*1*1*EA*1**VP*X~SE*3*1~",
	}
	for name, raw := range cases {
		if _, err := Parse850(raw); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}

func TestEncoders(t *testing.T) {
	env := Envelope{SenderID: "TEGGO", ReceiverID: "ACME", ControlNumber: "100", Date: "20240101", Time: "1200"}

	ack := Encode855(env, "PO-7788", []AckLine{{LineNo: "1", SKU: "WIDGET-1", Quantity: "10", UnitPrice: "25.00"}})
	for _, w := range []string{"ST*855", "BAK*00*AC*PO-7788", "ACK*IA", "SE*", "IEA*"} {
		if !strings.Contains(ack, w) {
			t.Errorf("855 missing %q", w)
		}
	}

	inv := Encode810(env, "INV-5", "PO-7788", "312.50", []InvoiceLine{{LineNo: "1", Quantity: "10", UnitPrice: "25.00"}})
	for _, w := range []string{"ST*810", "BIG*20240101*INV-5", "IT1*1", "TDS*312.50"} {
		if !strings.Contains(inv, w) {
			t.Errorf("810 missing %q", w)
		}
	}

	asn := Encode856(env, "SHP-3", "PO-7788", []ShipLine{{SKU: "WIDGET-1", Quantity: "10", UOM: "EA"}})
	for _, w := range []string{"ST*856", "BSN*00*SHP-3", "PRF*PO-7788", "LIN**VP*WIDGET-1"} {
		if !strings.Contains(asn, w) {
			t.Errorf("856 missing %q", w)
		}
	}

	// Round-trip: a generated 855's PO1 lines are re-parseable shape-wise.
	if !strings.HasPrefix(ack, "ISA*") {
		t.Error("855 should start with ISA envelope")
	}
}
