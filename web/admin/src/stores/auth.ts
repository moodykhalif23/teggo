import { defineStore } from 'pinia'
import { api, errMessage } from '@/lib/client'

// JWT payload minted by the Go API (internal/auth/jwt.go): org_id, aud, perms, sub.
interface JwtClaims {
  org_id: number
  aud: string
  perms?: string[]
  sub: string
  exp: number
}

function decodeClaims(token: string): JwtClaims | null {
  try {
    const payload = token.split('.')[1]
    const json = atob(payload.replace(/-/g, '+').replace(/_/g, '/'))
    return JSON.parse(json) as JwtClaims
  } catch {
    return null
  }
}

const TOKEN_KEY = 'teggo.admin.token'
const EMAIL_KEY = 'teggo.admin.email'

export const useAuthStore = defineStore('auth', {
  state: () => ({
    token: localStorage.getItem(TOKEN_KEY) as string | null,
    // The signed-in email, kept only to render the account avatar/menu (the JWT
    // carries no display name). Cleared on logout.
    email: localStorage.getItem(EMAIL_KEY) as string | null,
  }),
  getters: {
    claims: (state): JwtClaims | null => (state.token ? decodeClaims(state.token) : null),
    isAuthenticated(): boolean {
      const c = this.claims
      return !!c && c.exp * 1000 > Date.now()
    },
    permissions(): string[] {
      return this.claims?.perms ?? []
    },
    orgId(): number | null {
      return this.claims?.org_id ?? null
    },
    // Up-to-two-letter initials for the avatar, derived from the email local part
    // (e.g. "ada.lovelace@x.io" → "AL", "admin@x.io" → "AD").
    initials(): string {
      const local = (this.email ?? '').split('@')[0]
      if (!local) return '?'
      const parts = local.split(/[._-]+/).filter(Boolean)
      const letters =
        parts.length >= 2 ? parts[0][0] + parts[1][0] : local.slice(0, 2)
      return letters.toUpperCase()
    },
  },
  actions: {
    can(permission: string): boolean {
      return this.permissions.includes(permission)
    },
    setToken(token: string | null) {
      this.token = token
      if (token) localStorage.setItem(TOKEN_KEY, token)
      else localStorage.removeItem(TOKEN_KEY)
    },
    setEmail(email: string | null) {
      this.email = email
      if (email) localStorage.setItem(EMAIL_KEY, email)
      else localStorage.removeItem(EMAIL_KEY)
    },
    async login(email: string, password: string, orgId = 1) {
      const { data, error } = await api.POST('/admin/auth/login', {
        body: { email, password, org_id: orgId },
      })
      if (error || !data) throw new Error(errMessage(error, 'Invalid credentials'))
      this.setToken(data.token)
      this.setEmail(email)
    },
    logout() {
      this.setToken(null)
      this.setEmail(null)
    },
  },
})
