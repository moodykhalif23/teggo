# oro-folk frontends

Two apps in one pnpm workspace, both consuming the Go API (the single source of truth):

| App | Path | Stack | Role |
|---|---|---|---|
| **Admin SPA** | [`admin/`](admin/) | Vue 3 · Vite · Pinia · Vue Router · **PrimeVue** | Login-gated back-office (data grids, CRUD, dashboards). No SEO. |
| **Storefront** | [`storefront/`](storefront/) | Nuxt · SSR · **PrimeVue** | Customer-facing, crawlable storefront (SEO-critical). |

## Quick start

```bash
corepack enable pnpm        # if pnpm is missing
cd web
pnpm install

# Admin SPA (Vite dev server, default http://localhost:5173)
pnpm dev:admin

# Storefront (Nuxt dev server, default http://localhost:3000)
pnpm dev:storefront
```

Both apps read the API base URL from an env var (`VITE_API_BASE_URL` for admin,
`NUXT_PUBLIC_API_BASE` for the storefront) — default `http://localhost:8080` (the Go API).
Copy each app's `.env.example` to `.env` to override.

## Conventions

- **No business logic in the frontend.** Both apps are pure API consumers; the Go service
  owns all rules. UI is presentation + orchestration only.
- **PrimeVue v4** with the **Aura** theme preset (`@primeuix/themes`). Components are imported
  per-SFC (explicit over magic).
- **OpenAPI is the source of truth** for the API contract (see `../docs`, Pack 2 §5). The plan
  is to generate a shared TypeScript client from it; until then each app has a thin typed
  `fetch`/`$fetch` wrapper that attaches the bearer token and normalizes the error envelope.
- **Two security contexts** mirror the backend: admin uses a bearer JWT (`/admin/*`); the
  storefront uses a customer-user session (`/storefront/*`).
