import { defineStore } from 'pinia'
import { api } from '@/lib/client'
import type { components } from '@teggo/api/schema'

type BillingView = components['schemas']['BillingView']

// The org's plan entitlements, loaded once per session. The sidebar hides
// premium modules the plan lacks (the API enforces regardless — this is just
// honest navigation). Until loaded (or with no plan row) everything shows.
export const useBillingStore = defineStore('billing', {
  state: () => ({
    loaded: false,
    view: null as BillingView | null,
  }),
  getters: {
    features(): string[] {
      return this.view?.features ?? []
    },
  },
  actions: {
    // allows() is permissive before load and for plan-less orgs — the server
    // gate is the authority; the nav must never hide more than it does.
    allows(feature: string): boolean {
      if (!this.loaded || !this.view?.plan?.code) return true
      return this.features.includes(feature)
    },
    async load() {
      const { data } = await api.GET('/admin/billing')
      if (data) {
        this.view = data
        this.loaded = true
      }
    },
    reset() {
      this.loaded = false
      this.view = null
    },
  },
})
