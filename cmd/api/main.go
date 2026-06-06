package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"b2bcommerce/internal/ai"
	"b2bcommerce/internal/auth"
	"b2bcommerce/internal/blob"
	"b2bcommerce/internal/config"
	"b2bcommerce/internal/db"
	"b2bcommerce/internal/imageproc"
	"b2bcommerce/internal/logging"
	"b2bcommerce/internal/payments/gateway"
	"b2bcommerce/internal/queue"
	"b2bcommerce/internal/server"
	"b2bcommerce/internal/store"
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

	// OpenTelemetry metrics (opt-in via OTEL_EXPORTER_OTLP_ENDPOINT).
	shutdownTel, err := telemetry.Setup(ctx, "teggo-api", "dev")
	if err != nil {
		logger.Error("telemetry init failed", "err", err)
		os.Exit(1)
	}
	defer func() {
		sdctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := shutdownTel(sdctx); err != nil {
			logger.Error("telemetry shutdown error", "err", err)
		}
	}()
	if err := telemetry.RegisterPoolMetrics(pool); err != nil {
		logger.Warn("pool metrics registration failed", "err", err)
	}

	st := store.New(pool)
	issuer := auth.NewIssuer(cfg.JWTSecret, cfg.JWTTTL)

	// Insert-only river client so the API can enqueue background work
	// (e.g. combined_prices recompute). The worker process runs the jobs.
	enq, err := queue.NewEnqueuer(pool)
	if err != nil {
		logger.Error("queue init failed", "err", err)
		os.Exit(1)
	}
	// Card processor. Only the deterministic mock is built in; selecting another
	// provider falls back to mock with a warning until its adapter lands.
	var gw gateway.Gateway = gateway.Mock{}
	if cfg.PaymentsGateway != "" && cfg.PaymentsGateway != "mock" {
		logger.Warn("payment gateway not implemented, using mock", "requested", cfg.PaymentsGateway)
	}

	// DAM blob store (local FS; swap for object storage in multi-node deploys).
	mediaStore, err := blob.NewFSStore(cfg.MediaRoot)
	if err != nil {
		logger.Error("media store init failed", "err", err)
		os.Exit(1)
	}

	// AI assistant decision engine. The deterministic local engine is the default;
	// the Claude adapter is used only when explicitly selected AND an API key is
	// present (otherwise we fall back to deterministic with a warning).
	var aiProvider ai.Provider = ai.NewDeterministicProvider()
	if cfg.AIProvider == "claude" {
		if cfg.AnthropicAPIKey != "" {
			aiProvider = ai.NewClaudeProvider(cfg.AnthropicAPIKey, cfg.AIModel)
		} else {
			logger.Warn("AI_PROVIDER=claude but ANTHROPIC_API_KEY is empty; using deterministic engine")
		}
	}

	handler := server.New(st, issuer,
		server.WithRecompute(enq),
		server.WithInvoicePDF(enq),
		server.WithNotifier(enq),
		server.WithPaymentGateway(gw),
		server.WithLogger(logger),
		server.WithMedia(mediaStore, imageproc.GoProcessor{}),
		server.WithRendition(enq),
		server.WithIntegration(cfg.PunchoutStorefrontURL, cfg.EDISenderID, cfg.PunchoutTTL),
		server.WithAIProvider(aiProvider),
	)

	srv := &http.Server{
		Addr:              ":" + cfg.HTTPPort,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		logger.Info("api listening", "port", cfg.HTTPPort, "env", cfg.Env)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("listen failed", "err", err)
			os.Exit(1)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("shutdown error", "err", err)
	}
	logger.Info("api stopped")
}
