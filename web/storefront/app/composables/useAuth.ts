// Customer-session auth for the storefront. The JWT is kept in a cookie
// (readable in SSR + client). NOTE: this is a regular cookie for now; hardening
// to an httpOnly cookie behind a Nuxt server route (BFF) is a tracked follow-up.
export function useAuth() {
  const token = useCookie<string | null>('teggo_token', {
    sameSite: 'lax',
    secure: !import.meta.dev,
    maxAge: 60 * 60 * 24,
    path: '/',
  })
  const client = useClient()

  const isAuthenticated = computed(() => !!token.value)

  async function login(email: string, password: string, orgId = 1) {
    const { data, error } = await client.POST('/storefront/auth/login', {
      body: { email, password, org_id: orgId },
    })
    if (error || !data) throw new Error('Invalid credentials')
    token.value = data.token
  }

  // Redeem a shareable invite link (/join/<token>): registers the buyer under
  // the invite's company and signs them straight in.
  async function acceptInvite(inviteToken: string, body: { email: string; full_name: string; password: string }) {
    const { data, error } = await client.POST('/storefront/invites/{token}/accept', {
      params: { path: { token: inviteToken } },
      body,
    })
    if (error || !data) {
      throw new Error((error as { message?: string } | undefined)?.message || 'Could not create your account')
    }
    token.value = data.token
  }

  function logout() {
    token.value = null
  }

  return { token, isAuthenticated, login, acceptInvite, logout }
}
