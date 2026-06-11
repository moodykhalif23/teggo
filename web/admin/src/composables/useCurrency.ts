import { ref } from 'vue'
import { api } from '@/lib/client'

// The organisation's currency — sourced from the active website's configured
// `default_currency`, fetched once and shared app-wide so every form pre-fills
// and every figure renders in the same currency the user set in Settings.
//
// `currency` stays '' until known — we never assume a hardcoded code. Reading
// /admin/websites needs `tenant.view`; for users without it the value stays ''
// and the create handlers fall back to the org default server-side, so records
// are still stamped correctly. Display falls back gracefully to a plain number.
const currency = ref('')
const loaded = ref(false)
let inflight: Promise<void> | null = null

function ensureCurrency(): Promise<void> {
  if (loaded.value) return Promise.resolve()
  if (inflight) return inflight
  inflight = api
    .GET('/admin/websites')
    .then(({ data }) => {
      const sites = data?.items ?? []
      const code = (sites.find((w) => w.is_active) ?? sites[0])?.default_currency
      if (code) currency.value = code
    })
    .catch(() => {
      /* no permission / offline — leave currency unknown, server fills defaults */
    })
    .finally(() => {
      loaded.value = true
      inflight = null
    })
  return inflight
}

export function useCurrency() {
  ensureCurrency()

  // Format an amount in a currency (the record's own, or the org default).
  // Uses Intl for a localized symbol; falls back to "1,234 KES" if the code is
  // unknown to Intl, or a bare number while the org currency is still loading.
  function money(amount: number | string, code?: string): string {
    const c = code || currency.value
    const n = typeof amount === 'string' ? Number(amount) : amount
    if (!isFinite(n)) return c ? `${amount} ${c}` : String(amount)
    if (!c) return n.toLocaleString()
    try {
      return new Intl.NumberFormat(undefined, {
        style: 'currency',
        currency: c,
        currencyDisplay: 'narrowSymbol',
        minimumFractionDigits: 2,
        maximumFractionDigits: 2,
      }).format(n)
    } catch {
      return `${n.toLocaleString(undefined, { minimumFractionDigits: 2, maximumFractionDigits: 2 })} ${c}`
    }
  }

  return { currency, currencyLoaded: loaded, ensureCurrency, money }
}
