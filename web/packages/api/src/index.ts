import createClient, { type Client, type Middleware } from 'openapi-fetch'
import type { paths } from './schema'

export type { paths }
export type ApiClient = Client<paths>

export interface CreateApiOptions {
  baseUrl: string
  /** Returns the current bearer token, or null/undefined when unauthenticated. */
  getToken?: () => string | null | undefined
  /** Called when any response is 401, so the app can force re-authentication. */
  onUnauthorized?: () => void
}

export function createApiClient(opts: CreateApiOptions): ApiClient {
  const client = createClient<paths>({ baseUrl: opts.baseUrl })

  const auth: Middleware = {
    onRequest({ request }) {
      const token = opts.getToken?.()
      if (token) request.headers.set('Authorization', `Bearer ${token}`)
      return request
    },
    onResponse({ response }) {
      if (response.status === 401) opts.onUnauthorized?.()
      return response
    },
  }
  client.use(auth)
  return client
}
