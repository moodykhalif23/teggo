package queue

import (
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"

	"b2bcommerce/internal/queue/jobs"
)

// NewWorkerClient builds a river client that processes jobs (used by cmd/worker).
func NewWorkerClient(pool *pgxpool.Pool) (*river.Client[*pgxpool.Pool], error) {
	workers := river.NewWorkers()
	river.AddWorker(workers, &jobs.SendEmailWorker{})
	// Register additional workers here as modules add jobs.

	return river.NewClient(riverpgxv5.New(pool), &river.Config{
		Queues: map[string]river.QueueConfig{
			river.QueueDefault: {MaxWorkers: 50},
		},
		Workers: workers,
	})
}

// NewInsertClient builds an insert-only river client (used by the API to enqueue).
func NewInsertClient(pool *pgxpool.Pool) (*river.Client[*pgxpool.Pool], error) {
	return river.NewClient(riverpgxv5.New(pool), &river.Config{})
}
