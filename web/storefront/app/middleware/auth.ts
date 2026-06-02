// Guards customer-only routes (cart, account, checkout). Redirects to /login,
// preserving the intended destination.
export default defineNuxtRouteMiddleware((to) => {
  const token = useCookie<string | null>('teggo_token')
  if (!token.value) {
    return navigateTo({ path: '/login', query: { redirect: to.fullPath } })
  }
})
