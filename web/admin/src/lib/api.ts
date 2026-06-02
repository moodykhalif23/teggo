// Thin typed fetch wrapper for the admin SPA.
//
// It attaches the bearer token, normalizes the backend error envelope
// ({code, message, details} — see internal/server/response), and surfaces 401s
// so the auth layer can force re-authentication. When a generated TypeScript
// client is produced from the OpenAPI contract (Pack 2 §5), this becomes that
// client's request interceptor instead of a bespoke wrapper.

const BASE = import.meta.env.VITE_API_BASE_URL ?? ''

export interface ApiErrorBody {
  code: string
  message: string
  details?: Record<string, unknown>
}

export class ApiError extends Error {
  constructor(
    public status: number,
    public code: string,
    message: string,
    public details?: Record<string, unknown>,
  ) {
    super(message)
    this.name = 'ApiError'
  }
}

// Pluggable hooks so the auth store can provide the token and react to 401s
// without this module importing the store (avoids a circular dependency).
let tokenProvider: () => string | null = () => null
let onUnauthorized: () => void = () => {}

export function configureApi(opts: {
  getToken: () => string | null
  onUnauthorized: () => void
}) {
  tokenProvider = opts.getToken
  onUnauthorized = opts.onUnauthorized
}

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  const headers: Record<string, string> = { Accept: 'application/json' }
  const token = tokenProvider()
  if (token) headers.Authorization = `Bearer ${token}`
  if (body !== undefined) headers['Content-Type'] = 'application/json'

  const res = await fetch(`${BASE}${path}`, {
    method,
    headers,
    body: body !== undefined ? JSON.stringify(body) : undefined,
  })

  if (res.status === 401) {
    onUnauthorized()
  }

  const text = await res.text()
  const data = text ? JSON.parse(text) : null

  if (!res.ok) {
    const err = (data ?? {}) as Partial<ApiErrorBody>
    throw new ApiError(
      res.status,
      err.code ?? 'error',
      err.message ?? res.statusText,
      err.details,
    )
  }
  return data as T
}

export const api = {
  get: <T>(path: string) => request<T>('GET', path),
  post: <T>(path: string, body?: unknown) => request<T>('POST', path, body),
  put: <T>(path: string, body?: unknown) => request<T>('PUT', path, body),
  patch: <T>(path: string, body?: unknown) => request<T>('PATCH', path, body),
  del: <T>(path: string) => request<T>('DELETE', path),
}
