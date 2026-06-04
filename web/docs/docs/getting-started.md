---
title: Getting started
sidebar_position: 2
---

# Getting started

Run the full stack — Postgres, migrations, API, worker, Gotenberg — with Docker Compose.

## Prerequisites

- Docker + Docker Compose
- Go ≥ 1.25 and Node ≥ 18 + pnpm (only for local dev outside containers)

## 1. Configure the environment

```bash
cp .env.example .env
# Generate a 32-byte JWT secret:
openssl rand -base64 32   # paste into JWT_SECRET in .env
```

`.env` is git-ignored and must never be committed. See **[Configuration](./configuration.md)** for every variable.

## 2. Bring up the stack

```bash
docker compose up -d --build
```

This starts `postgres`, runs `migrate` (idempotent, embedded SQL migrations), then `api` (`:8080`), `worker`, and `gotenberg`. The API is healthy when:

```bash
curl -s localhost:8080/readyz   # checks DB connectivity
```

The seed migration creates a demo admin: **`admin@demo.test` / `admin1234`** (org 1). Change it for anything real.

```bash
curl -s -X POST localhost:8080/admin/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"email":"admin@demo.test","password":"admin1234"}'
# → {"token":"<JWT>"}
```

## 3. Run the frontends

```bash
cd web && pnpm install
pnpm --filter admin dev        # Vue admin SPA (Vite proxies /admin, /storefront, /media, /files to :8080)
pnpm --filter storefront dev   # Nuxt storefront
```

## 4. Tests

The Go test suite uses **testcontainers** — a throwaway Postgres per package, all migrations applied:

```bash
make test         # or: go test ./...
```

Frontend:

```bash
pnpm --filter @teggo/api typecheck   # the generated client compiles against the contract
pnpm --filter admin build
```

## Useful endpoints

| Endpoint | Purpose |
|---|---|
| `GET /healthz` | liveness |
| `GET /readyz` | readiness (pings the DB) |
| `POST /admin/auth/login` | admin login → admin JWT |
| `POST /storefront/auth/login` | buyer login → storefront JWT |

Next: **[Architecture](./architecture.md)**.
