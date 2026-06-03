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
	"b2bcommerce/internal/logging"
	"b2bcommerce/internal/pdf"
	"b2bcommerce/internal/queue"
	"b2bcommerce/internal/telemetry"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	logger := logging.Setup(cfg.Env, cfg.LogLevel)

	ctx := context.Background()
	pool, err := db.NewPoolWithConfig(ctx, cfg.DatabaseURL, db.PoolConfig{
		MaxConns: cfg.DBMaxConns, MaxConnIdleTime: cfg.DBMaxConnIdleTime,
	})
	if err != nil {
		logger.Error("db connect failed", "err", err)
		os.Exit(1)
	}
	defer pool.Close()

	shutdownTel, err := telemetry.Setup(ctx, "teggo-worker", "dev")
	if err != nil {
		logger.Error("telemetry init failed", "err", err)
		os.Exit(1)
	}
	defer func() { _ = shutdownTel(context.Background()) }()
	if err := telemetry.RegisterPoolMetrics(pool); err != nil {
		logger.Warn("pool metrics registration failed", "err", err)
	}

	var renderer pdf.Renderer
	if cfg.GotenbergURL != "" {
		renderer = pdf.NewGotenberg(cfg.GotenbergURL)
		logger.Info("invoice PDFs: Gotenberg", "url", cfg.GotenbergURL)
	} else {
		renderer = pdf.Stub{}
		logger.Warn("invoice PDFs: GOTENBERG_URL unset, using stub renderer")
	}

	var sender email.Sender
	if cfg.SMTPHost != "" {
		sender = email.NewSMTP(email.SMTPConfig{
			Host: cfg.SMTPHost, Port: cfg.SMTPPort, Username: cfg.SMTPUsername,
			Password: cfg.SMTPPassword, From: cfg.EmailFrom,
		})
		logger.Info("email: SMTP transport", "host", cfg.SMTPHost, "port", cfg.SMTPPort)
	} else {
		sender = email.LogSender{From: cfg.EmailFrom}
		logger.Warn("email: SMTP_HOST unset, using log transport")
	}

	client, err := queue.NewWorkerClient(pool, renderer, sender)
	if err != nil {
		logger.Error("queue init failed", "err", err)
		os.Exit(1)
	}

	if err := client.Start(ctx); err != nil {
		logger.Error("worker start failed", "err", err)
		os.Exit(1)
	}
	logger.Info("worker started")

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	if err := client.Stop(ctx); err != nil {
		logger.Error("worker stop error", "err", err)
	}
	logger.Info("worker stopped")
}
