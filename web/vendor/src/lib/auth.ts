import { reactive } from 'vue'
import { api, errMessage } from './client'

// A small reactive auth store for the vendor portal (no Pinia needed). The token
// is a vendor-audience JWT minted by POST /vendor/auth/login.
const TOKEN_KEY = 'teggo.vendor.token'
const EMAIL_KEY = 'teggo.vendor.email'

export const auth = reactive({
  token: localStorage.getItem(TOKEN_KEY) as string | null,
  email: localStorage.getItem(EMAIL_KEY) as string | null,

  get isAuthenticated(): boolean {
    return !!this.token
  },

  async login(email: string, password: string, orgId = 1): Promise<void> {
    const { data, error } = await api.POST('/vendor/auth/login', {
      body: { email, password, org_id: orgId },
    })
    if (error || !data) throw new Error(errMessage(error, 'Invalid credentials'))
    this.token = data.token
    this.email = email
    localStorage.setItem(TOKEN_KEY, data.token)
    localStorage.setItem(EMAIL_KEY, email)
  },

  logout(): void {
    this.token = null
    this.email = null
    localStorage.removeItem(TOKEN_KEY)
    localStorage.removeItem(EMAIL_KEY)
  },
})
