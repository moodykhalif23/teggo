---
slug: /
title: Introduction
sidebar_position: 1
---

# Teggo developer documentation

Teggo is a **self-hosted, API-first B2B commerce platform** (an in-house OroCommerce-equivalent) for manufacturers, distributors, and wholesalers. It combines **commerce + CRM + a low-code workflow/automation engine** in one system.

## Stack

- **Backend** — Go: [chi](https://github.com/go-chi/chi) router, [sqlc](https://sqlc.dev) (type-safe queries), [river](https://riverqueue.com) (Postgres-backed job queue), [pgx](https://github.com/jackc/pgx). A **modular monolith** — clear package boundaries, not microservices.
- **Database** — PostgreSQL 16, doing triple duty: data, the job queue (river), and full-text search.
- **Frontends** — Vue 3 admin SPA + Nuxt storefront (SSR), both **pure API consumers** of a generated, typed client.
- **Contract** — a single OpenAPI 3.1 document is the source of truth; the TypeScript client and this API reference are generated from it.
- **Edge / deploy** — Nginx, Gotenberg (invoice PDFs), Docker Compose.

The Go service is the single source of truth. Everything else consumes its API.

## How these docs are organized

- **[Getting started](./getting-started.md)** — run the whole stack locally in minutes.
- **[Architecture](./architecture.md)** — the modular monolith, request lifecycle, package map.
- **[Conventions](./conventions.md)** — the rules every table and query follows. Read before writing code.
- **[Adding a module](./module-pattern.md)** — the repeatable recipe for a new feature module.
- **[Auth & RBAC](./auth-rbac.md)**, **[Data layer](./data-layer.md)**, **[Background jobs](./background-jobs.md)**, **[Integrations](./integrations.md)**, **[Frontend](./frontend.md)**, **[Configuration](./configuration.md)**.
- **[API reference](/api)** — generated from the OpenAPI contract.

:::tip
New here? Read **Getting started → Architecture → Conventions → Adding a module** in order. That's the fast path to being productive.
:::
