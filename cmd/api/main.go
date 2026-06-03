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

	"b2bcommerce/internal/auth"
	"b2bcommerce/internal/config"
	"b2bcommerce/internal/db"
	"b2bcommerce/internal/payments/gateway"
	"b2bcommerce/internal/queue"
	"b2bcommerce/internal/server"
	"b2bcommerce/internal/store"
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

	st := store.New(pool)
	issuer := auth.NewIssuer(cfg.JWTSecret, cfg.JWTTTL)

	// Insert-only river client so the API can enqueue background work
	// (e.g. combined_prices recompute). The worker process runs the jobs.
	enq, err := queue.NewEnqueuer(pool)
	if err != nil {
		log.Fatalf("queue: %v", err)
	}
	// Card processor. Only the deterministic mock is built in; selecting another
	// provider falls back to mock with a warning until its adapter lands.
	var gw gateway.Gateway = gateway.Mock{}
	if cfg.PaymentsGateway != "" && cfg.PaymentsGateway != "mock" {
		log.Printf("payments: gateway %q not implemented, using mock", cfg.PaymentsGateway)
	}

	handler := server.New(st, issuer,
		server.WithRecompute(enq),
		server.WithInvoicePDF(enq),
		server.WithNotifier(enq),
		server.WithPaymentGateway(gw),
	)

	srv := &http.Server{
		Addr:              ":" + cfg.HTTPPort,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		log.Printf("api listening on :%s (env=%s)", cfg.HTTPPort, cfg.Env)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("listen: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("shutdown: %v", err)
	}
	log.Println("api stopped")
}
