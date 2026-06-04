import { createApp } from 'vue'
import { createPinia } from 'pinia'
import PrimeVue from 'primevue/config'
import ToastService from 'primevue/toastservice'
import ConfirmationService from 'primevue/confirmationservice'

import 'primeicons/primeicons.css'
import '@/assets/main.css'
import { TeggoPreset } from '@/theme'

import App from '@/App.vue'
import { router } from '@/router'
import { useAuthStore } from '@/stores/auth'
import { configureClient } from '@/lib/client'

const app = createApp(App)

app.use(createPinia())
app.use(PrimeVue, {
  theme: {
    preset: TeggoPreset,
    options: {
      darkModeSelector: '.app-dark',
    },
  },
})
app.use(ToastService)
app.use(ConfirmationService)

// Wire the API client to the auth store: token on every request, and a 401
// clears the session and bounces to login.
const auth = useAuthStore()
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
