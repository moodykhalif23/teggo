// Typed storefront API client (generated from the OpenAPI contract via @teggo/api).
// Reads the customer session token from a cookie and attaches it as a bearer on
// every request, so authenticated calls work in SSR and on the client alike.
// Call this in component/composable setup (it uses useCookie).
import { createApiClient, type ApiClient } from '@teggo/api'

export function useClient(): ApiClient {
  const config = useRuntimeConfig()
  const token = useCookie<string | null>('teggo_token')
  return createApiClient({
    baseUrl: config.public.apiBase,
    getToken: () => token.value ?? null,
  })
}
