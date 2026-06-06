import { fileURLToPath, URL } from 'node:url'
import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

// https://vite.dev/config/
export default defineConfig({
  plugins: [vue()],
  resolve: {
    alias: {
      '@': fileURLToPath(new URL('./src', import.meta.url)),
    },
  },
  server: {
    port: 5174,
    proxy: {
      // Proxy the vendor-portal API to the Go service so the SPA and API share
      // an origin (no CORS). Override the target with VITE_API_BASE_URL.
      '/vendor': { target: 'http://localhost:8080', changeOrigin: true },
    },
  },
})
