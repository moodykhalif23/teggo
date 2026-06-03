package email_test

import (
	"context"
	"strings"
	"testing"

	"b2bcommerce/internal/email"
	"b2bcommerce/internal/queue/jobs"
)

func TestRenderTemplates(t *testing.T) {
	cases := []struct {
		key  string
		data map[string]any
		want string // a substring that must appear in the rendered HTML
	}{
		{"order_confirmation", map[string]any{"name": "Ada", "order_number": "ORD-12345678", "total": "100.0000", "currency": "USD"}, "ORD-12345678"},
		{"quote_sent", map[string]any{"name": "Ada", "quote_number": "Q-abcd1234", "total": "50.0000", "currency": "USD"}, "Q-abcd1234"},
		{"invoice_issued", map[string]any{"name": "Ada", "invoice_number": "INV-9999", "total": "75.0000", "currency": "USD", "due_at": "2026-07-01"}, "INV-9999"},
	}
	for _, c := range cases {
		msg, err := email.Render("buyer@acme.test", c.key, c.data)
		if err != nil {
			t.Fatalf("render %s: %v", c.key, err)
		}
		if msg.To != "buyer@acme.test" || msg.Subject == "" || msg.HTML == "" {
			t.Errorf("%s: incomplete message %+v", c.key, msg)
		}
		if !strings.Contains(msg.HTML, c.want) {
			t.Errorf("%s: HTML missing %q:\n%s", c.key, c.want, msg.HTML)
		}
		// The plain-text part is tag-stripped, so it must not contain '<'.
		if strings.Contains(msg.Text, "<") {
			t.Errorf("%s: text part still has HTML tags: %q", c.key, msg.Text)
		}
	}

	if _, err := email.Render("x@y.z", "no_such_template", nil); err == nil {
		t.Error("unknown template should error")
	}
}

// captureSender records the last message instead of sending it.
type captureSender struct{ last email.Message }

func (c *captureSender) Send(_ context.Context, m email.Message) error {
	c.last = m
	return nil
}

func TestSendEmailJobRendersAndSends(t *testing.T) {
	cap := &captureSender{}
	err := jobs.SendEmail(context.Background(), cap, jobs.SendEmailArgs{
		To:       "buyer@acme.test",
		Template: "order_confirmation",
		Data:     []byte(`{"name":"Ada","order_number":"ORD-abcd1234","total":"12.0000","currency":"USD"}`),
	})
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if cap.last.To != "buyer@acme.test" || !strings.Contains(cap.last.HTML, "ORD-abcd1234") {
		t.Errorf("job did not render+send expected message: %+v", cap.last)
	}
}
