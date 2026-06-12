package email

import (
	"bytes"
	"fmt"
	"html/template"
)

type tmpl struct {
	subject string
	body    string
}

var templates = map[string]tmpl{
	"order_confirmation": {
		subject: "Your Teggo order {{.order_number}} is confirmed",
		body: `<p>Hi {{.name}},</p>
<p>We've received your order <strong>{{.order_number}}</strong> for a total of <strong>{{.total}} {{.currency}}</strong>.</p>
<p>We'll let you know when it ships. Thank you for your business.</p>
<p>— Teggo</p>`,
	},
	"quote_sent": {
		subject: "Quote {{.quote_number}} from Teggo",
		body: `<p>Hi {{.name}},</p>
<p>A new quote <strong>{{.quote_number}}</strong> totalling <strong>{{.total}} {{.currency}}</strong> is ready for your review.</p>
<p>Sign in to your account to accept or decline it.</p>
<p>— Teggo</p>`,
	},
	"invoice_issued": {
		subject: "Invoice {{.invoice_number}} from Teggo",
		body: `<p>Hi {{.name}},</p>
<p>Invoice <strong>{{.invoice_number}}</strong> for <strong>{{.total}} {{.currency}}</strong> has been issued{{if .due_at}}, due {{.due_at}}{{end}}.</p>
<p>You can view and pay it from your account.</p>
<p>— Teggo</p>`,
	},
	"order_status_update": {
		subject: "Order {{.order_number}} is now {{.status}}",
		body: `<p>Hi {{.name}},</p>
<p>Your order <strong>{{.order_number}}</strong> has moved to <strong>{{.status}}</strong>.</p>
<p>Sign in to your account for the full details.</p>
<p>— Teggo</p>`,
	},
	"quote_expired": {
		subject: "Quote {{.quote_number}} has expired",
		body: `<p>Hi {{.name}},</p>
<p>Quote <strong>{{.quote_number}}</strong> has passed its validity date and is now expired.</p>
<p>Need it again? Reply or request a new quote and we'll be glad to help.</p>
<p>— Teggo</p>`,
	},
	"subscription_order_placed": {
		subject: "Your recurring order has been placed",
		body: `<p>Hi,</p>
<p>Your recurring order <strong>{{.order_public_id}}</strong> for <strong>{{.grand_total}} {{.currency}}</strong> has been placed automatically.</p>
<p>Sign in to your account to review it or manage the subscription.</p>
<p>— Teggo</p>`,
	},
	"signup_verify": {
		subject: "Verify your email to activate {{.organization}}",
		body: `<p>Hi {{.name}},</p>
<p>You're one step away from activating <strong>{{.organization}}</strong> on Teggo.</p>
<p><a href="{{.link}}">Verify your email address</a> (the link expires {{.expires_at}}).</p>
<p>If you didn't sign up, you can ignore this email.</p>
<p>— Teggo</p>`,
	},
	"report_ready": {
		subject: "Your scheduled report \"{{.name}}\" is ready",
		body: `<p>Hello,</p>
<p>Your scheduled report <strong>{{.name}}</strong> has run ({{.row_count}} rows).</p>
<p>Download it here: <a href="{{.file_url}}">{{.file_url}}</a></p>
<p>— Teggo</p>`,
	},
}

func Render(to, key string, data map[string]any) (Message, error) {
	t, ok := templates[key]
	if !ok {
		return Message{}, fmt.Errorf("unknown email template %q", key)
	}
	subject, err := execString("subject:"+key, t.subject, data)
	if err != nil {
		return Message{}, err
	}
	html, err := execString("body:"+key, t.body, data)
	if err != nil {
		return Message{}, err
	}
	return Message{To: to, Subject: subject, HTML: html, Text: stripTags(html)}, nil
}

func execString(name, src string, data map[string]any) (string, error) {
	t, err := template.New(name).Parse(src)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// stripTags is a minimal HTML→text fallback for the plain-text MIME part.
func stripTags(html string) string {
	var b bytes.Buffer
	inTag := false
	for _, r := range html {
		switch {
		case r == '<':
			inTag = true
		case r == '>':
			inTag = false
		case !inTag:
			b.WriteRune(r)
		}
	}
	return b.String()
}
