package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"b2bcommerce/internal/config"
	"b2bcommerce/internal/db"
	"b2bcommerce/internal/email"
	"b2bcommerce/internal/pdf"
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

	var renderer pdf.Renderer
	if cfg.GotenbergURL != "" {
		renderer = pdf.NewGotenberg(cfg.GotenbergURL)
		log.Printf("invoice PDFs: Gotenberg at %s", cfg.GotenbergURL)
	} else {
		renderer = pdf.Stub{}
		log.Println("invoice PDFs: GOTENBERG_URL unset, using stub renderer")
	}

	var sender email.Sender
	if cfg.SMTPHost != "" {
		sender = email.NewSMTP(email.SMTPConfig{
			Host: cfg.SMTPHost, Port: cfg.SMTPPort, Username: cfg.SMTPUsername,
			Password: cfg.SMTPPassword, From: cfg.EmailFrom,
		})
		log.Printf("email: SMTP at %s:%s", cfg.SMTPHost, cfg.SMTPPort)
	} else {
		sender = email.LogSender{From: cfg.EmailFrom}
		log.Println("email: SMTP_HOST unset, using log transport")
	}

	client, err := queue.NewWorkerClient(pool, renderer, sender)
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
