.PHONY: help tidy generate build test run-api run-worker migrate up down logs psql fmt vet \
        dev web admin storefront vendor docs docs-build web-install web-build api-client

help:
	@echo "Backend:"
	@echo "  tidy        go mod tidy (pin deps)"
	@echo "  generate    sqlc generate (typed query layer -> internal/store/gen)"
	@echo "  build       build all three binaries"
	@echo "  test        go test ./... (integration tests start Postgres via testcontainers; needs Docker)"
	@echo "  up          docker compose up (postgres + migrate + api + worker)"
	@echo "  down        docker compose down"
	@echo "  logs        tail compose logs"
	@echo "  migrate     run migrations locally (needs DATABASE_URL)"
	@echo "  run-api     run the API locally"
	@echo "  run-worker  run the worker locally"
	@echo "  psql        open psql against the compose database"
	@echo ""
	@echo "Frontend / docs (Node + pnpm):"
	@echo "  dev         backend (detached) + admin, storefront & vendor dev servers — one-command spin"
	@echo "  web         admin + storefront + vendor dev servers (backend assumed up)"
	@echo "  admin       admin SPA dev server         -> http://localhost:5173"
	@echo "  storefront  storefront dev server        -> http://localhost:3000"
	@echo "  vendor      vendor portal dev server     -> http://localhost:5174"
	@echo "  docs        generate API reference + serve docs -> http://localhost:3001"
	@echo "  docs-build  generate API reference + static build (web/docs/build)"
	@echo "  api-client  regenerate the typed client from the OpenAPI spec"
	@echo "  web-install pnpm install (the web workspace)"
	@echo "  web-build   production build of admin + storefront + docs"

tidy:
	go mod tidy

generate:
	sqlc generate

build:
	go build ./...

test:
	go test ./... $(TESTFLAGS)

run-api:
	go run ./cmd/api

run-worker:
	go run ./cmd/worker

migrate:
	go run ./cmd/migrate

up:
	docker compose up -d

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

# ---- Frontend / docs -------------------------------------------------------

dev:
	./scripts/dev.sh

web-install:
	cd web && pnpm install

web:
	cd web && pnpm --parallel --filter @teggo/admin --filter @teggo/storefront --filter @teggo/vendor run dev

admin:
	cd web && pnpm --filter @teggo/admin dev

storefront:
	cd web && pnpm --filter @teggo/storefront dev

vendor:
	cd web && pnpm --filter @teggo/vendor dev

docs:
	./scripts/docs.sh

docs-build:
	./scripts/docs.sh build

api-client:
	cd web && pnpm --filter @teggo/api generate

web-build:
	cd web && pnpm --filter @teggo/admin build \
	  && pnpm --filter @teggo/storefront build \
	  && pnpm --filter @teggo/vendor build \
	  && pnpm --filter @teggo/docs build
