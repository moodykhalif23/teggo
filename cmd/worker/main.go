package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"b2bcommerce/internal/config"
	"b2bcommerce/internal/db"
	"b2bcommerce/internal/queue"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	ctx := context.Background()
	pool, err := db.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("db: %v", err)
	}
	defer pool.Close()

	client, err := queue.NewWorkerClient(pool)
	if err != nil {
		log.Fatalf("queue: %v", err)
	}

	if err := client.Start(ctx); err != nil {
		log.Fatalf("worker start: %v", err)
	}
	log.Println("worker started")

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	if err := client.Stop(ctx); err != nil {
		log.Printf("worker stop: %v", err)
	}
	log.Println("worker stopped")
}
