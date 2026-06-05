<script setup lang="ts">
import Button from 'primevue/button'
import InputText from 'primevue/inputtext'

const { isAuthenticated, logout } = useAuth()
const router = useRouter()
const route = useRoute()

const term = ref((route.query.q as string) ?? '')

function search() {
  const q = term.value.trim()
  if (q) router.push({ path: '/search', query: { q } })
}

function signOut() {
  logout()
  router.push('/')
}
</script>

<template>
  <div class="shell">
    <header class="header">
      <NuxtLink to="/" class="brand"><i class="pi pi-shopping-bag" /> Teggo Store</NuxtLink>
      <nav class="nav">
        <NuxtLink to="/">Home</NuxtLink>
        <NuxtLink to="/c/all">Catalog</NuxtLink>
        <NuxtLink to="/contact">Contact</NuxtLink>
        <NuxtLink v-if="isAuthenticated" to="/quick-order">Quick order</NuxtLink>
        <NuxtLink v-if="isAuthenticated" to="/account/reorder">Reorder</NuxtLink>
        <NuxtLink v-if="isAuthenticated" to="/account/lists">Lists</NuxtLink>
        <NuxtLink v-if="isAuthenticated" to="/account/rfqs">RFQs</NuxtLink>
        <NuxtLink v-if="isAuthenticated" to="/account/quotes">Quotes</NuxtLink>
        <NuxtLink v-if="isAuthenticated" to="/account/orders">Orders</NuxtLink>
        <NuxtLink v-if="isAuthenticated" to="/account/invoices">Invoices</NuxtLink>
        <NuxtLink v-if="isAuthenticated" to="/account/settings">Account</NuxtLink>
      </nav>
      <span class="spacer" />
      <span class="search">
        <i class="pi pi-search" />
        <InputText
          v-model="term"
          placeholder="Search products…"
          class="search-input"
          @keyup.enter="search"
        />
      </span>
      <NuxtLink to="/cart">
        <Button icon="pi pi-shopping-cart" label="Cart" severity="secondary" outlined />
      </NuxtLink>
      <NuxtLink v-if="!isAuthenticated" to="/login">
        <Button icon="pi pi-user" label="Sign in" text />
      </NuxtLink>
      <Button v-else icon="pi pi-sign-out" label="Sign out" text @click="signOut" />
    </header>

    <main class="content">
      <slot />
    </main>

    <footer class="footer">
      <p>Teggo storefront — server-rendered for SEO. © {{ new Date().getFullYear() }}</p>
    </footer>
  </div>
</template>

<style scoped>
.shell {
  min-height: 100vh;
  display: flex;
  flex-direction: column;
}
.header {
  display: flex;
  align-items: center;
  gap: 1.5rem;
  padding: 0.9rem 1.5rem;
  border-bottom: 1px solid var(--p-surface-200, #e2e8f0);
  background: var(--p-surface-0, #fff);
}
.brand {
  font-weight: 700;
  font-size: 1.15rem;
  text-decoration: none;
  color: inherit;
  display: flex;
  align-items: center;
  gap: 0.4rem;
}
.nav {
  display: flex;
  gap: 1rem;
}
.nav a {
  text-decoration: none;
  color: var(--p-text-muted-color, #64748b);
}
.nav a.router-link-active {
  color: var(--p-primary-color, #0ea5e9);
  font-weight: 600;
}
.spacer {
  flex: 1;
}
.search {
  display: flex;
  align-items: center;
  gap: 0.4rem;
  color: var(--p-text-muted-color, #64748b);
}
.search-input {
  width: 16rem;
  max-width: 30vw;
}
.content {
  flex: 1;
  padding: 1.5rem;
  max-width: 1200px;
  margin: 0 auto;
  width: 100%;
}
.footer {
  padding: 1.5rem;
  text-align: center;
  color: var(--p-text-muted-color, #64748b);
  border-top: 1px solid var(--p-surface-200, #e2e8f0);
}
</style>
