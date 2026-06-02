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

const TOKEN_KEY = 'oro.admin.token'

export const useAuthStore = defineStore('auth', {
  state: () => ({
    token: localStorage.getItem(TOKEN_KEY) as string | null,
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
    async login(email: string, password: string, orgId = 1) {
      const { data, error } = await api.POST('/admin/auth/login', {
        body: { email, password, org_id: orgId },
      })
      if (error || !data) throw new Error(errMessage(error, 'Invalid credentials'))
      this.setToken(data.token)
    },
    logout() {
      this.setToken(null)
    },
  },
})
