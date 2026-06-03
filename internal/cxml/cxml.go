// Package cxml implements the slice of cXML needed for Ariba/Coupa-style
// PunchOut (Pack 3 §3): parsing a PunchOutSetupRequest, and emitting a
// PunchOutSetupResponse (start URL) and a PunchOutOrderMessage (the cart sent
// back to the procurement system). It is transport-agnostic — handlers wire it
// to HTTP. OCI (SAP form-field POSTs) is a separate, simpler encoding deferred
// to a follow-up.
package cxml

import (
	"encoding/base64"
	"encoding/xml"
	"fmt"
	"html"
	"strings"
)

// ---- inbound: PunchOutSetupRequest ----------------------------------------

// SetupRequest is the parsed, relevant subset of a cXML PunchOutSetupRequest.
type SetupRequest struct {
	XMLName xml.Name `xml:"cXML"`
	Header  header   `xml:"Header"`
	Request request  `xml:"Request"`
}

type header struct {
	Sender struct {
		Credential []credential `xml:"Credential"`
	} `xml:"Sender"`
	From struct {
		Credential []credential `xml:"Credential"`
	} `xml:"From"`
	To struct {
		Credential []credential `xml:"Credential"`
	} `xml:"To"`
}

type credential struct {
	Domain       string `xml:"domain,attr"`
	Identity     string `xml:"Identity"`
	SharedSecret string `xml:"SharedSecret"`
}

type request struct {
	Setup struct {
		Operation       string `xml:"operation,attr"`
		BuyerCookie     string `xml:"BuyerCookie"`
		BrowserFormPost struct {
			URL string `xml:"URL"`
		} `xml:"BrowserFormPost"`
	} `xml:"PunchOutSetupRequest"`
}

// Setup is the extracted, validated-shape view of a setup request.
type Setup struct {
	SenderIdentity string
	SharedSecret   string
	Operation      string
	BuyerCookie    string
	ReturnURL      string
}

// ParseSetupRequest parses a cXML PunchOutSetupRequest body. The sender's
// credential (identity + shared secret) is used by the caller to authenticate
// the trading partner.
func ParseSetupRequest(body []byte) (Setup, error) {
	var r SetupRequest
	if err := xml.Unmarshal(body, &r); err != nil {
		return Setup{}, fmt.Errorf("parse cXML: %w", err)
	}
	s := Setup{
		Operation:   r.Request.Setup.Operation,
		BuyerCookie: r.Request.Setup.BuyerCookie,
		ReturnURL:   r.Request.Setup.BrowserFormPost.URL,
	}
	// The authenticating credential is the Sender's (falls back to From).
	creds := r.Header.Sender.Credential
	if len(creds) == 0 {
		creds = r.Header.From.Credential
	}
	if len(creds) > 0 {
		s.SenderIdentity = strings.TrimSpace(creds[0].Identity)
		s.SharedSecret = strings.TrimSpace(creds[0].SharedSecret)
	}
	if s.Operation == "" {
		s.Operation = "create"
	}
	return s, nil
}

// ---- outbound: PunchOutSetupResponse --------------------------------------

// SetupResponse marshals a success response carrying the start URL the buyer's
// browser is redirected to.
func SetupResponse(startURL string) []byte {
	var b strings.Builder
	b.WriteString(xml.Header)
	b.WriteString("<cXML>")
	b.WriteString(`<Response><Status code="200" text="OK"/>`)
	b.WriteString("<PunchOutSetupResponse><StartPage><URL>")
	b.WriteString(html.EscapeString(startURL))
	b.WriteString("</URL></StartPage></PunchOutSetupResponse></Response></cXML>")
	return []byte(b.String())
}

// ErrorResponse marshals a cXML failure (e.g. bad credentials → 401).
func ErrorResponse(code int, text string) []byte {
	var b strings.Builder
	b.WriteString(xml.Header)
	b.WriteString(fmt.Sprintf(`<cXML><Response><Status code="%d" text="%s"/></Response></cXML>`, code, html.EscapeString(text)))
	return []byte(b.String())
}

// ---- outbound: PunchOutOrderMessage ---------------------------------------

// Line is one cart line returned to the procurement system.
type Line struct {
	SupplierPartID string
	Description    string
	UnitOfMeasure  string
	Quantity       string
	UnitPrice      string // decimal string
	Classification string
}

// OrderMessage builds the PunchOutOrderMessage cXML for a transferred cart.
// buyerCookie is echoed from the setup so the procurement system can correlate.
func OrderMessage(fromIdentity, buyerCookie, currency, total string, lines []Line) []byte {
	var b strings.Builder
	b.WriteString(xml.Header)
	b.WriteString("<cXML>")
	b.WriteString("<Header><From><Credential domain=\"NetworkId\"><Identity>")
	b.WriteString(html.EscapeString(fromIdentity))
	b.WriteString("</Identity></Credential></From></Header>")
	b.WriteString("<Message><PunchOutOrderMessage>")
	b.WriteString("<BuyerCookie>")
	b.WriteString(html.EscapeString(buyerCookie))
	b.WriteString("</BuyerCookie>")
	b.WriteString(`<PunchOutOrderMessageHeader operationAllowed="create"><Total><Money currency="`)
	b.WriteString(html.EscapeString(currency))
	b.WriteString("\">")
	b.WriteString(html.EscapeString(total))
	b.WriteString("</Money></Total></PunchOutOrderMessageHeader>")
	for _, l := range lines {
		b.WriteString(`<ItemIn quantity="`)
		b.WriteString(html.EscapeString(l.Quantity))
		b.WriteString("\"><ItemID><SupplierPartID>")
		b.WriteString(html.EscapeString(l.SupplierPartID))
		b.WriteString("</SupplierPartID></ItemID><ItemDetail><UnitPrice><Money currency=\"")
		b.WriteString(html.EscapeString(currency))
		b.WriteString("\">")
		b.WriteString(html.EscapeString(l.UnitPrice))
		b.WriteString("</Money></UnitPrice><Description xml:lang=\"en\">")
		b.WriteString(html.EscapeString(l.Description))
		b.WriteString("</Description><UnitOfMeasure>")
		b.WriteString(html.EscapeString(defUOM(l.UnitOfMeasure)))
		b.WriteString("</UnitOfMeasure></ItemDetail></ItemIn>")
	}
	b.WriteString("</PunchOutOrderMessage></Message></cXML>")
	return []byte(b.String())
}

func AutoPostForm(returnURL string, orderMessage []byte) []byte {
	enc := base64.StdEncoding.EncodeToString(orderMessage)
	return []byte(`<!doctype html><html><body onload="document.forms[0].submit()">` +
		`<form method="POST" action="` + html.EscapeString(returnURL) + `">` +
		`<input type="hidden" name="cxml-base64" value="` + enc + `"/>` +
		`<noscript><button type="submit">Return cart</button></noscript>` +
		`</form></body></html>`)
}

func defUOM(u string) string {
	if u == "" {
		return "EA"
	}
	return u
}
