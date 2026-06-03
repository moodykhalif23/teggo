// Package email renders and sends transactional email (PRD §11/§14, Pack 2 §4.5).
// All sends go through the send_email river job with a template key + data. The
// real transport is SMTP; a Log transport (the default when SMTP is unconfigured)
// prints the message so the flow works end-to-end in dev/tests without a server.
package email

import (
	"context"
	"fmt"
	"log"
	"net/smtp"
	"strings"
)

// Message is a rendered email ready to send.
type Message struct {
	To      string
	Subject string
	HTML    string
	Text    string
}

// Sender delivers a Message. Implemented by SMTPSender (real) and LogSender (dev).
type Sender interface {
	Send(ctx context.Context, msg Message) error
}

// SMTPConfig configures the SMTP transport.
type SMTPConfig struct {
	Host     string
	Port     string
	Username string
	Password string
	From     string
}

// SMTPSender sends via a standard SMTP server (SES/Mailgun/Postfix all speak it).
type SMTPSender struct{ cfg SMTPConfig }

// NewSMTP builds an SMTP sender.
func NewSMTP(cfg SMTPConfig) *SMTPSender { return &SMTPSender{cfg: cfg} }

func (s *SMTPSender) Send(_ context.Context, msg Message) error {
	addr := s.cfg.Host + ":" + s.cfg.Port
	var auth smtp.Auth
	if s.cfg.Username != "" {
		auth = smtp.PlainAuth("", s.cfg.Username, s.cfg.Password, s.cfg.Host)
	}
	body := buildMIME(s.cfg.From, msg)
	return smtp.SendMail(addr, auth, s.cfg.From, []string{msg.To}, body)
}

// buildMIME assembles a minimal multipart/alternative message (text + HTML).
func buildMIME(from string, msg Message) []byte {
	const boundary = "teggo-boundary-9f1c"
	var b strings.Builder
	fmt.Fprintf(&b, "From: %s\r\n", from)
	fmt.Fprintf(&b, "To: %s\r\n", msg.To)
	fmt.Fprintf(&b, "Subject: %s\r\n", msg.Subject)
	b.WriteString("MIME-Version: 1.0\r\n")
	fmt.Fprintf(&b, "Content-Type: multipart/alternative; boundary=%q\r\n\r\n", boundary)
	fmt.Fprintf(&b, "--%s\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n%s\r\n", boundary, msg.Text)
	fmt.Fprintf(&b, "--%s\r\nContent-Type: text/html; charset=UTF-8\r\n\r\n%s\r\n", boundary, msg.HTML)
	fmt.Fprintf(&b, "--%s--\r\n", boundary)
	return []byte(b.String())
}

// LogSender logs the message instead of sending it — the default transport when
// no SMTP server is configured. Keeps the order/quote/invoice flows runnable.
type LogSender struct{ From string }

func (l LogSender) Send(_ context.Context, msg Message) error {
	log.Printf("[email] (log transport) from=%s to=%s subject=%q\n%s", l.From, msg.To, msg.Subject, msg.Text)
	return nil
}
