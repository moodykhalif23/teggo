#!/usr/bin/env bash
# Spin the whole app for local development:
#   - backend (postgres + migrate + api + worker + gotenberg) via Docker Compose, detached
#   - admin SPA + storefront dev servers in the foreground (concurrent, with cleanup)
# Docs are served separately (heavier build): `make docs`.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

echo "▶ Backend: docker compose up -d --build"
docker compose up -d --build

echo "▶ Web deps: pnpm install"
( cd web && pnpm install )

pids=()
cleanup() {
  echo
  echo "■ Stopping frontends (backend keeps running — 'make down' to stop it)…"
  kill "${pids[@]}" 2>/dev/null || true
}
trap cleanup EXIT INT TERM

( cd "$ROOT/web" && pnpm --filter @teggo/admin dev )      & pids+=($!)
( cd "$ROOT/web" && pnpm --filter @teggo/storefront dev ) & pids+=($!)

cat <<'EOF'
──────────────────────────────────────────────
  Admin SPA   → http://localhost:5173
  Storefront  → http://localhost:3000
  API         → http://localhost:8080
  Docs        → run `make docs`  (http://localhost:3001)

  Ctrl-C stops the frontends. The backend stays up; `make down` to stop it.
──────────────────────────────────────────────
EOF

wait
