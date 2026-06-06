// The vendor portal's typed API client (generated from the OpenAPI contract via
// @teggo/api). Token + 401 handling are injected from main.ts to keep this module
// free of any dependency on the auth store (avoids an import cycle).
import { createApiClient, type ApiClient } from '@teggo/api'

let getToken: () => string | null = () => null
let onUnauthorized: () => void = () => {}

export function configureClient(opts: {
  getToken: () => string | null
  onUnauthorized: () => void
}) {
  getToken = opts.getToken
  onUnauthorized = opts.onUnauthorized
}

// Empty baseUrl => same-origin relative requests; the Vite dev server proxies
// /vendor to the Go API. Override with VITE_API_BASE_URL.
export const api: ApiClient = createApiClient({
  baseUrl: import.meta.env.VITE_API_BASE_URL ?? '',
  getToken: () => getToken(),
  onUnauthorized: () => onUnauthorized(),
})

// errMessage extracts a human message from an openapi-fetch error body.
export function errMessage(error: unknown, fallback = 'Request failed'): string {
  if (error && typeof error === 'object' && 'message' in error) {
    return String((error as { message: unknown }).message)
  }
  return fallback
}
