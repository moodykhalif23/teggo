import Aura from '@primeuix/themes/aura'

// https://nuxt.com/docs/api/configuration/nuxt-config
export default defineNuxtConfig({
  compatibilityDate: '2025-01-01',
  devtools: { enabled: true },

  modules: ['@primevue/nuxt-module'],

  // @oro/api ships as TypeScript source (workspace package) — transpile it.
  build: { transpile: ['@oro/api'] },

  primevue: {
    options: {
      theme: {
        preset: Aura,
        options: {
          darkModeSelector: '.app-dark',
        },
      },
    },
  },

  css: ['primeicons/primeicons.css', '~/assets/css/main.css'],

  runtimeConfig: {
    public: {
      // Base URL of the Go storefront API. Override with NUXT_PUBLIC_API_BASE.
      apiBase: 'http://localhost:8080',
    },
  },

  // SSR is on (SEO). Cache catalog pages with stale-while-revalidate; keep
  // cart/account/checkout dynamic and uncached.
  routeRules: {
    '/': { swr: 600 },
    '/c/**': { swr: 600 },
    '/p/**': { swr: 600 },
  },

  app: {
    head: {
      htmlAttrs: { lang: 'en' },
      title: 'Oro Storefront',
      meta: [{ name: 'viewport', content: 'width=device-width, initial-scale=1' }],
    },
  },
})
