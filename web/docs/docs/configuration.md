---
title: Configuration
sidebar_position: 11
---

# Configuration

All runtime configuration comes from **environment variables** (`internal/config`). Docker Compose reads them from a local `.env` file.

:::danger Never commit secrets
`.env` is git-ignored and must stay that way — it holds `JWT_SECRET`, SMTP and payment credentials. API responses never echo secrets back (`client_secret` / `shared_secret` / `idp_certificate` collapse to a `has_secret` boolean).
:::

## Required

| Var | Notes |
|---|---|
| `DATABASE_URL` | Postgres DSN. Required — boot fails without it. |
| `JWT_SECRET` | HMAC signing key for all tokens **and** capability URLs. Required. |

## Core

| Var | Default | Notes |
|---|---|---|
| `HTTP_PORT` | `8080` | API listen port. |
| `ENV` | `development` | `production` switches `slog` to JSON. |
| `LOG_LEVEL` | `info` | `debug` / `info` / `warn` / `error`. |
| `JWT_TTL` | `24h` | Token lifetime (Go duration). |

## Database pool

| Var | Default |
|---|---|
| `DB_MAX_CONNS` | `20` |
| `DB_MAX_CONN_IDLE_TIME` | `5m` |

## PDF & email

| Var | Default | Notes |
|---|---|---|
| `GOTENBERG_URL` | _(empty)_ | Empty → stub PDF renderer. |
| `SMTP_HOST` | _(empty)_ | Empty → log transport (emails printed, not sent). |
| `SMTP_PORT` / `SMTP_USERNAME` / `SMTP_PASSWORD` | `587` / — / — | |
| `EMAIL_FROM` | `Teggo <no-reply@teggo.local>` | |

## Storage, payments, integrations

| Var | Default | Notes |
|---|---|---|
| `MEDIA_ROOT` | `/data/media` | DAM blob dir; **shared volume** in multi-node deploys. |
| `PAYMENTS_GATEWAY` | `mock` | `mock` or a real provider once wired. |
| `PUNCHOUT_STOREFRONT_URL` | `/` | Landing URL after punchout start. |
| `EDI_SENDER_ID` | `TEGGO` | Our identity on outbound cXML/EDI. |
| `PUNCHOUT_TTL` | `1h` | Punchout session lifetime. |

## Observability (OpenTelemetry)

Metrics are **opt-in**: set `OTEL_EXPORTER_OTLP_ENDPOINT` and the API exports via OTLP (DB-pool gauges + HTTP metrics via `otelhttp`). Unset → no exporter, zero overhead. See `internal/telemetry`.

## Adding a setting

1. Add the field + `getenv(...)` default in `internal/config/config.go` (validate required ones in `Load`).
2. Document it here.
3. Add it to `.env.example` if one is present — never to `.env` in git.
