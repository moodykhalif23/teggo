---
title: Background jobs
sidebar_position: 8
---

# Background jobs (river)

Durable, retryable work runs on [river](https://riverqueue.com), a Postgres-backed queue — no Redis. The API **enqueues**; the worker **executes**. A slow or failing integration never blocks a request.

## Two clients

- **API** (`cmd/api`) builds an *insert-only* river client wrapped by `queue.Enqueuer` — typed enqueue methods (`EnqueueEmail`, `EnqueueInvoicePDF`, `EnqueueRecompute`, `EnqueueRendition`, `EmitEvent`, …).
- **Worker** (`cmd/worker`) builds the full client in `queue.NewWorkerClient`, registering every worker and the periodic jobs.

## Job kinds (`internal/queue/jobs/`)

One file per kind. Each defines an `Args` (with `Kind()`) and a `Worker`:

- `send_email` — SMTP or log transport, rendered from a template key.
- `generate_invoice_pdf` — Gotenberg (or stub) → stored bytes → capability URL.
- `recompute_combined_prices` — rebuilds the price cache for a customer.
- `generate_rendition` — DAM image renditions.
- `run_automation_action` / `dispatch_event` / `emit_scheduled` — the workflow/automation engine.
- `refresh_reporting` — materialized-view refresh.
- `run_report_schedules` — due custom-report exports.
- `erp_sync_sweep` — pushes confirmed orders + issued invoices to ERP connections.

## Periodic jobs

Registered in `queue.NewWorkerClient` via `river.PeriodicJob`: hourly `schedule.hourly` (drives quote-expiry/overdue automation), hourly reporting refresh (run-on-start), hourly report-schedule sweep, hourly ERP sweep.

## Adding a job

1. New file in `internal/queue/jobs/` with `XArgs{}` + `XWorker{...}`.
2. Register it in `NewWorkerClient` (`river.AddWorker`), plus a periodic entry if scheduled.
3. Add an `Enqueuer` method if the API triggers it.
4. Keep work **idempotent** — jobs retry. Use stable keys / `external_refs`-style guards so a replay applies nothing new.

Enqueue failures from handlers are logged (`slog`) and never block the user.
