import { createApp } from 'vue'
import PrimeVue from 'primevue/config'
import Aura from '@primeuix/themes/aura'
import ToastService from 'primevue/toastservice'

import 'primeicons/primeicons.css'
import '@/assets/main.css'

import App from '@/App.vue'
import { router } from '@/router'
import { auth } from '@/lib/auth'
import { configureClient } from '@/lib/client'

const app = createApp(App)

app.use(PrimeVue, { theme: { preset: Aura } })
app.use(ToastService)

// Wire the API client to the auth store: token on every request, and a 401
// clears the session and bounces to login.
configureClient({
  getToken: () => auth.token,
  onUnauthorized: () => {
    auth.logout()
    if (router.currentRoute.value.name !== 'login') {
      router.push({ name: 'login' })
    }
  },
})

app.use(router)
app.mount('#app')
