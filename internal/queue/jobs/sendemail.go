package jobs

import (
	"context"
	"encoding/json"

	"github.com/riverqueue/river"

	"b2bcommerce/internal/email"
)

// SendEmailArgs renders a transactional template and sends it (Pack 2 §4.5).
// Data is the template payload (e.g. name, order_number, total); it is rendered
// with html/template by the email package.
type SendEmailArgs struct {
	To       string          `json:"to"`
	Template string          `json:"template"`
	Data     json.RawMessage `json:"data"`
}

func (SendEmailArgs) Kind() string { return "send_email" }

// SendEmail renders + sends one message; exposed so tests can drive it directly.
func SendEmail(ctx context.Context, sender email.Sender, args SendEmailArgs) error {
	var data map[string]any
	if len(args.Data) > 0 {
		if err := json.Unmarshal(args.Data, &data); err != nil {
			return err
		}
	}
	msg, err := email.Render(args.To, args.Template, data)
	if err != nil {
		return err
	}
	return sender.Send(ctx, msg)
}

// SendEmailWorker processes SendEmailArgs jobs using the configured Sender.
type SendEmailWorker struct {
	river.WorkerDefaults[SendEmailArgs]
	Sender email.Sender
}

func (w *SendEmailWorker) Work(ctx context.Context, job *river.Job[SendEmailArgs]) error {
	return SendEmail(ctx, w.Sender, job.Args)
}
