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
