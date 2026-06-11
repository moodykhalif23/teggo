import Aura from '@primeuix/themes/aura'

// https://nuxt.com/docs/api/configuration/nuxt-config
export default defineNuxtConfig({
  compatibilityDate: '2025-01-01',
  devtools: { enabled: true },

  modules: ['@primevue/nuxt-module'],

  // @teggo/api and @teggo/blocks ship as source (workspace packages) — transpile them.
  build: { transpile: ['@teggo/api', '@teggo/blocks'] },

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

  $production: {
    routeRules: {
      '/': { swr: 600 },
      '/c/**': { swr: 600 },
      '/p/**': { swr: 600 },
    },
  },

  app: {
    head: {
      htmlAttrs: { lang: 'en' },
      title: 'Teggo Storefront',
      meta: [{ name: 'viewport', content: 'width=device-width, initial-scale=1' }],
      link: [
        { rel: 'preconnect', href: 'https://fonts.googleapis.com' },
        { rel: 'preconnect', href: 'https://fonts.gstatic.com', crossorigin: '' },
        {
          rel: 'stylesheet',
          href: 'https://fonts.googleapis.com/css2?family=Open+Sans:ital,wght@0,300..800;1,300..800&display=swap',
        },
      ],
    },
  },
})
