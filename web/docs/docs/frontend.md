---
title: Frontend
sidebar_position: 10
---

# Frontend

A pnpm workspace under `web/` holds four apps + a shared API package.

```
web/
  packages/api/   @teggo/api  — OpenAPI 3.1 spec → typed client
  admin/          Vue 3 + Vite SPA (seller back-office)
  storefront/     Nuxt (B2B buyer storefront)
  vendor/         Vue 3 + Vite SPA (marketplace vendor portal — audience 'vendor')
  docs/           Docusaurus (this site)
```

## The typed API package (`@teggo/api`)

`web/packages/api/openapi.yaml` is the **single source of truth** for the HTTP contract. It generates:

- `src/schema.d.ts` via [openapi-typescript](https://github.com/openapi-ts/openapi-typescript)
- a thin [openapi-fetch](https://openapi-ts.dev/openapi-fetch/) client

```bash
pnpm --filter @teggo/api generate
```

Every request and response is then **type-checked against the spec** — a contract change that breaks a caller fails the build, not production.

## Admin SPA

Vue 3 `<script setup>` + Vite + Pinia, **PrimeVue v4** components, **Apache ECharts** (`echarts` + `vue-echarts`) for all charts. Routes in `router/index.ts` carry `meta.permission`; the nav in `AppLayout.vue` shows an item only if the token grants that permission. Data flows through the `@teggo/api` client (bearer from the auth store); file uploads use a plain `fetch` with the bearer header.

```
views/<area>/SomeView.vue   →  router/index.ts (meta.permission)  →  AppLayout.vue nav
```

## Storefront

Nuxt, consuming the **`storefront`**-audience API. Buyer session, catalog browse, cart, checkout, order history.

## Conventions

- Keep components thin; push logic into stores/composables.
- Use the generated types — never hand-type a response shape.
- Match the surrounding code's style; PrimeVue first before a custom component.

```bash
pnpm install
pnpm --filter @teggo/admin dev      # admin on Vite
pnpm --filter @teggo/admin build    # part of the "done" bar
```
