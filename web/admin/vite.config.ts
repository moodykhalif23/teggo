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
    port: 5173,
    proxy: {
      // Dev convenience: proxy API calls to the Go service so the SPA and API
      // share an origin (no CORS). Override the target with VITE_API_BASE_URL
      // for direct calls in other environments.
      '/admin': { target: 'http://localhost:8080', changeOrigin: true },
      '/storefront': { target: 'http://localhost:8080', changeOrigin: true },
    },
  },
})
