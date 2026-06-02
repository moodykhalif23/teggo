package main

import (
	"context"
	"log"
	"time"

	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"

	"b2bcommerce/internal/config"
	"b2bcommerce/internal/db"
	"b2bcommerce/migrations"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	pool, err := db.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("db: %v", err)
	}
	defer pool.Close()

	// 1) Application schema (embedded *.sql).
	if err := db.Migrate(ctx, pool, migrations.FS); err != nil {
		log.Fatalf("app migrate: %v", err)
	}

	// 2) River's own tables (idempotent).
	rm, err := rivermigrate.New(riverpgxv5.New(pool), nil)
	if err != nil {
		log.Fatalf("river migrator: %v", err)
	}
	if _, err := rm.Migrate(ctx, rivermigrate.DirectionUp, nil); err != nil {
		log.Fatalf("river migrate: %v", err)
	}

	log.Println("migrations complete")
}
