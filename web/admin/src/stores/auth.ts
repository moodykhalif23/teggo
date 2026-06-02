import { defineStore } from 'pinia'
import { api } from '@/lib/api'

interface LoginResponse {
  token: string
}

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
      const res = await api.post<LoginResponse>('/admin/auth/login', {
        email,
        password,
        org_id: orgId,
      })
      this.setToken(res.token)
    },
    logout() {
      this.setToken(null)
    },
  },
})
