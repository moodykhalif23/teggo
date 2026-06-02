// Storefront API helper. Wraps $fetch with the configured base URL so SSR and
// client calls hit the Go storefront API consistently. A customer-user session
// (httpOnly cookie) is injected by server middleware later; for now these are
// public catalog reads.
export function useApi() {
  const config = useRuntimeConfig()
  const base = config.public.apiBase

  return $fetch.create({
    baseURL: base,
    headers: { Accept: 'application/json' },
  })
}
