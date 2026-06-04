#!/usr/bin/env bash
# Generate the API reference from the OpenAPI spec and serve the developer docs.
# Served on :3001 to avoid clashing with the storefront dev server (:3000).
# Pass "build" as the first arg to produce a static build instead of serving.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT/web"

echo "▶ Web deps: pnpm install"
pnpm install >/dev/null

cd docs
DOCU="./node_modules/@docusaurus/core/bin/docusaurus.mjs"

echo "▶ Copying OpenAPI spec → static/openapi.yaml"
cp ../packages/api/openapi.yaml static/openapi.yaml

echo "▶ Generating API reference from the OpenAPI contract"
node "$DOCU" clean-api-docs all >/dev/null 2>&1 || true
node "$DOCU" gen-api-docs all

if [[ "${1:-}" == "build" ]]; then
  echo "▶ Building static site → web/docs/build"
  node "$DOCU" build
else
  echo "▶ Serving docs → http://localhost:3001"
  node "$DOCU" start --port 3001
fi
