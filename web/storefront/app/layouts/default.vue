<script setup lang="ts">
import Button from 'primevue/button'

const { isAuthenticated, logout } = useAuth()
const router = useRouter()

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
        <NuxtLink v-if="isAuthenticated" to="/account/rfqs">Quotes</NuxtLink>
        <NuxtLink v-if="isAuthenticated" to="/account/orders">Orders</NuxtLink>
      </nav>
      <span class="spacer" />
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
