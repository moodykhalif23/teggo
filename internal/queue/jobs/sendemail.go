package jobs

import (
	"context"
	"fmt"

	"github.com/riverqueue/river"
)

// SendEmailArgs is a sample job demonstrating the river pattern. Real actions
// (invoice PDF, ERP sync, price recompute) follow the same shape.
type SendEmailArgs struct {
	To       string `json:"to"`
	Template string `json:"template"`
}

func (SendEmailArgs) Kind() string { return "send_email" }

// SendEmailWorker processes SendEmailArgs jobs.
type SendEmailWorker struct {
	river.WorkerDefaults[SendEmailArgs]
}

func (w *SendEmailWorker) Work(ctx context.Context, job *river.Job[SendEmailArgs]) error {
	// Replace with a real EmailAdapter call (see Pack 2 §4.5).
	fmt.Printf("[worker] send_email to=%s template=%s\n", job.Args.To, job.Args.Template)
	return nil
}
