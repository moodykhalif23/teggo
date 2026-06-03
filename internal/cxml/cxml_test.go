package cxml

import (
	"strings"
	"testing"
)

const setupXML = `<?xml version="1.0"?>
<cXML>
  <Header>
    <Sender>
      <Credential domain="NetworkId"><Identity>ACME-PROC</Identity><SharedSecret>s3cr3t</SharedSecret></Credential>
    </Sender>
  </Header>
  <Request>
    <PunchOutSetupRequest operation="create">
      <BuyerCookie>cookie-123</BuyerCookie>
      <BrowserFormPost><URL>https://buyer.example/return</URL></BrowserFormPost>
    </PunchOutSetupRequest>
  </Request>
</cXML>`

func TestParseSetupRequest(t *testing.T) {
	s, err := ParseSetupRequest([]byte(setupXML))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if s.SenderIdentity != "ACME-PROC" || s.SharedSecret != "s3cr3t" {
		t.Errorf("credential = %q/%q", s.SenderIdentity, s.SharedSecret)
	}
	if s.Operation != "create" || s.BuyerCookie != "cookie-123" {
		t.Errorf("op/cookie = %q/%q", s.Operation, s.BuyerCookie)
	}
	if s.ReturnURL != "https://buyer.example/return" {
		t.Errorf("return url = %q", s.ReturnURL)
	}
}

func TestOrderMessageAndForm(t *testing.T) {
	lines := []Line{{SupplierPartID: "WIDGET-1", Description: "Widget", Quantity: "2", UnitPrice: "25.00", UnitOfMeasure: "EA"}}
	msg := OrderMessage("TEGGO", "cookie-123", "USD", "50.00", lines)
	s := string(msg)
	for _, want := range []string{"PunchOutOrderMessage", "cookie-123", "WIDGET-1", "50.00", `currency="USD"`} {
		if !strings.Contains(s, want) {
			t.Errorf("order message missing %q", want)
		}
	}
	form := string(AutoPostForm("https://buyer.example/return", msg))
	if !strings.Contains(form, "https://buyer.example/return") || !strings.Contains(form, "cxml-base64") {
		t.Errorf("auto-post form malformed: %s", form)
	}
}

func TestErrorResponse(t *testing.T) {
	if !strings.Contains(string(ErrorResponse(401, "invalid credentials")), `code="401"`) {
		t.Error("error response missing status code")
	}
}
