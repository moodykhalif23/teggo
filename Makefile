.PHONY: help tidy generate build run-api run-worker migrate up down logs psql fmt vet

help:
	@echo "Targets:"
	@echo "  tidy        go mod tidy (pin deps)"
	@echo "  generate    sqlc generate (typed query layer -> internal/store/gen)"
	@echo "  build       build all three binaries"
	@echo "  up          docker compose up (postgres + migrate + api + worker)"
	@echo "  down        docker compose down"
	@echo "  logs        tail compose logs"
	@echo "  migrate     run migrations locally (needs DATABASE_URL)"
	@echo "  run-api     run the API locally"
	@echo "  run-worker  run the worker locally"
	@echo "  psql        open psql against the compose database"

tidy:
	go mod tidy

generate:
	sqlc generate

build:
	go build ./...

run-api:
	go run ./cmd/api

run-worker:
	go run ./cmd/worker

migrate:
	go run ./cmd/migrate

up:
	docker compose up --build

down:
	docker compose down

logs:
	docker compose logs -f

psql:
	docker compose exec postgres psql -U b2b -d b2b

fmt:
	gofmt -w .

vet:
	go vet ./...
