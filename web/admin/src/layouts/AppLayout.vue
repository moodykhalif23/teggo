<script setup lang="ts">
import { computed, onMounted, onBeforeUnmount, ref, watch } from 'vue'
import { RouterView, useRouter, useRoute } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import { useNotificationsStore } from '@/stores/notifications'
import Avatar from 'primevue/avatar'
import Popover from 'primevue/popover'
import Button from 'primevue/button'
import NotificationBell from '@/components/NotificationBell.vue'

const auth = useAuthStore()
const notifications = useNotificationsStore()

// The layout only renders for an authenticated session, so start the feed on
// mount and tear it down on sign-out.
onMounted(() => notifications.start())
onBeforeUnmount(() => notifications.stop())
const router = useRouter()
const route = useRoute()

// Mobile sidebar drawer (small screens only). Closes on navigation.
const mobileOpen = ref(false)
watch(() => route.fullPath, () => { mobileOpen.value = false })

// Each leaf declares the permission it needs; leaves the user can't access are
// filtered out (deny-by-default mirrors the backend RBAC). Leaves are grouped
// into a handful of related, collapsible sections so the sidebar reads as ~9
// areas instead of ~28 flat links — purely presentation; routes are unchanged.
interface NavLeaf {
  label: string
  icon: string
  routeName: string
  permission?: string
}
interface NavGroup {
  label: string
  icon?: string
  items: NavLeaf[]
}

const groups: NavGroup[] = [
  {
    label: '',
    items: [
      { label: 'Dashboard', icon: 'pi pi-home', routeName: 'dashboard' },
      { label: 'Assistant', icon: 'pi pi-sparkles', routeName: 'assistant' },
    ],
  },
  {
    label: 'Catalog',
    icon: 'pi pi-th-large',
    items: [
      { label: 'Products', icon: 'pi pi-box', routeName: 'products', permission: 'product.view' },
      { label: 'Categories', icon: 'pi pi-sitemap', routeName: 'categories', permission: 'category.view' },
      { label: 'Attributes', icon: 'pi pi-tags', routeName: 'attributes', permission: 'attribute.view' },
      { label: 'Configurator', icon: 'pi pi-sliders-h', routeName: 'configurator', permission: 'product.view' },
      { label: 'Search merchandising', icon: 'pi pi-search-plus', routeName: 'merchandising', permission: 'merchandising.view' },
    ],
  },
  {
    label: 'Pricing',
    icon: 'pi pi-dollar',
    items: [
      { label: 'Price lists', icon: 'pi pi-dollar', routeName: 'pricing', permission: 'price_list.view' },
      { label: 'Price rules', icon: 'pi pi-sliders-h', routeName: 'price-rules', permission: 'price_list.view' },
      { label: 'Promotions', icon: 'pi pi-tag', routeName: 'promotions', permission: 'promotion.view' },
      { label: 'Exchange rates', icon: 'pi pi-money-bill', routeName: 'fx-rates', permission: 'fx.view' },
      { label: 'Tax & shipping', icon: 'pi pi-percentage', routeName: 'tax-shipping', permission: 'tax.view' },
    ],
  },
  {
    label: 'Sales',
    icon: 'pi pi-shopping-cart',
    items: [
      { label: 'RFQs', icon: 'pi pi-inbox', routeName: 'rfqs', permission: 'rfq.view' },
      { label: 'Quotes', icon: 'pi pi-file-edit', routeName: 'quotes', permission: 'quote.view' },
      { label: 'Orders', icon: 'pi pi-shopping-cart', routeName: 'orders', permission: 'order.view' },
      { label: 'Subscriptions', icon: 'pi pi-sync', routeName: 'subscriptions', permission: 'subscription.view' },
      { label: 'Rebates', icon: 'pi pi-percentage', routeName: 'rebates', permission: 'rebate.view' },
      { label: 'Invoices', icon: 'pi pi-receipt', routeName: 'invoices', permission: 'invoice.view' },
      { label: 'AR aging', icon: 'pi pi-chart-line', routeName: 'ar-aging', permission: 'invoice.view' },
      { label: 'Returns', icon: 'pi pi-replay', routeName: 'returns', permission: 'return.view' },
    ],
  },
  {
    label: 'Customers',
    icon: 'pi pi-users',
    items: [
      { label: 'Customers', icon: 'pi pi-building', routeName: 'customers', permission: 'customer.view' },
      { label: 'Customer groups', icon: 'pi pi-users', routeName: 'customer-groups', permission: 'customer.view' },
      { label: 'Leads', icon: 'pi pi-filter', routeName: 'leads', permission: 'crm.view' },
      { label: 'Pipeline', icon: 'pi pi-chart-bar', routeName: 'pipeline', permission: 'crm.view' },
      { label: 'Opportunities', icon: 'pi pi-briefcase', routeName: 'opportunities', permission: 'crm.view' },
      { label: 'Account health', icon: 'pi pi-heart', routeName: 'account-health', permission: 'crm.view' },
    ],
  },
  {
    label: 'Marketplace',
    icon: 'pi pi-shop',
    items: [
      { label: 'Vendors', icon: 'pi pi-shop', routeName: 'vendors', permission: 'vendor.view' },
      { label: 'Catalog moderation', icon: 'pi pi-check-square', routeName: 'moderation', permission: 'vendor.view' },
    ],
  },
  {
    label: 'Operations',
    icon: 'pi pi-warehouse',
    items: [
      { label: 'Inventory', icon: 'pi pi-warehouse', routeName: 'inventory', permission: 'inventory.view' },
      { label: 'Field devices', icon: 'pi pi-mobile', routeName: 'field-devices', permission: 'field.sync' },
    ],
  },
  {
    label: 'Automation',
    icon: 'pi pi-bolt',
    items: [
      { label: 'Workflows', icon: 'pi pi-sitemap', routeName: 'workflows', permission: 'workflow.view' },
      { label: 'Automation rules', icon: 'pi pi-bolt', routeName: 'automation-rules', permission: 'workflow.view' },
      { label: 'Approval routing', icon: 'pi pi-check-circle', routeName: 'approval-routing', permission: 'workflow.view' },
    ],
  },
  {
    label: 'Content',
    icon: 'pi pi-folder',
    items: [
      { label: 'Pages', icon: 'pi pi-file', routeName: 'pages', permission: 'cms.view' },
      { label: 'Media', icon: 'pi pi-images', routeName: 'media', permission: 'cms.view' },
    ],
  },
  {
    label: 'Insights',
    icon: 'pi pi-chart-bar',
    items: [
      { label: 'Analytics', icon: 'pi pi-chart-line', routeName: 'analytics', permission: 'report.view' },
      { label: 'Report builder', icon: 'pi pi-table', routeName: 'report-builder', permission: 'report.view' },
    ],
  },
  {
    label: 'Settings',
    icon: 'pi pi-cog',
    items: [
      { label: 'Platform', icon: 'pi pi-building-columns', routeName: 'platform-orgs', permission: 'platform.view' },
      { label: 'Websites', icon: 'pi pi-globe', routeName: 'websites', permission: 'tenant.view' },
      { label: 'Configuration', icon: 'pi pi-cog', routeName: 'settings', permission: 'settings.view' },
      { label: 'Integrations', icon: 'pi pi-sync', routeName: 'integrations', permission: 'integration.view' },
      { label: 'ERP sync', icon: 'pi pi-server', routeName: 'erp', permission: 'erp.view' },
      { label: 'SSO providers', icon: 'pi pi-id-card', routeName: 'identity-providers', permission: 'sso.view' },
    ],
  },
]

// Static menu (Verona-style): always-visible section labels + flat item links.
// Drop leaves the user lacks permission for, then drop any emptied group.
const visibleGroups = computed(() =>
  groups
    .map((g) => ({ ...g, items: g.items.filter((i) => !i.permission || auth.can(i.permission)) }))
    .filter((g) => g.items.length),
)

// Highlight a nav item when its route is active. Exact match for the root
// dashboard ('/'), otherwise prefix-match so detail/editor routes (e.g.
// /customers/123) keep their parent list item highlighted.
function isActive(routeName: string): boolean {
  if (route.name === routeName) return true
  const path = router.resolve({ name: routeName }).path
  if (path === '/') return false
  return route.path === path || route.path.startsWith(path + '/')
}

const account = ref()
function toggleAccount(e: Event) {
  account.value?.toggle(e)
}
function logout() {
  auth.logout()
  router.push({ name: 'login' })
}
</script>

<template>
  <div class="layout" :class="{ 'drawer-open': mobileOpen }">
    <div class="scrim" @click="mobileOpen = false" />
    <aside class="sidebar">
      <div class="brand">
        <span class="brand-badge"><i class="pi pi-bolt" /></span>
        <span class="brand-name">Teggo<span class="brand-sub">Admin</span></span>
      </div>
      <nav class="nav-scroll">
        <ul class="menu">
          <template v-for="g in visibleGroups" :key="g.label || 'root'">
            <li v-if="g.label" class="menu-section">{{ g.label }}</li>
            <li v-for="item in g.items" :key="item.routeName" class="menu-item">
              <RouterLink :to="{ name: item.routeName }" class="menu-link" :class="{ active: isActive(item.routeName) }">
                <i :class="item.icon" class="menu-icon" />
                <span class="menu-text">{{ item.label }}</span>
              </RouterLink>
            </li>
          </template>
        </ul>
      </nav>
    </aside>

    <div class="main">
      <header class="topbar">
        <button type="button" class="hamburger" aria-label="Menu" @click="mobileOpen = !mobileOpen">
          <i class="pi pi-bars" />
        </button>
        <span class="spacer" />
        <NotificationBell class="topbar-bell" />
        <button type="button" class="account-trigger" @click="toggleAccount" aria-label="Account">
          <Avatar :label="auth.initials" shape="square" class="account-avatar" />
        </button>
        <Popover ref="account" class="account-pop">
          <div class="account-card">
            <div class="account-head">
              <Avatar :label="auth.initials" shape="square" size="large" class="account-avatar" />
              <div class="account-id">
                <div class="account-email">{{ auth.email ?? 'Signed in' }}</div>
                <div class="account-org">Organization {{ auth.orgId ?? '—' }}</div>
              </div>
            </div>
            <div class="account-meta">
              <i class="pi pi-shield" />
              <span>{{ auth.permissions.length }} permissions</span>
            </div>
            <Button
              icon="pi pi-sign-out"
              label="Sign out"
              severity="secondary"
              outlined
              class="account-signout"
              @click="logout"
            />
          </div>
        </Popover>
      </header>
      <main class="content">
        <RouterView />
      </main>
    </div>
  </div>
</template>

<style scoped>
.layout {
  display: flex;
  height: 100vh;
  height: 100dvh;
  overflow: hidden;
}
.sidebar {
  width: var(--teggo-sidebar-width);
  flex-shrink: 0;
  height: 100%;
  background: var(--teggo-surface, #fff);
  border-right: 1px solid var(--p-surface-200, #e2e8f0);
  display: flex;
  flex-direction: column;
}
.brand {
  display: flex;
  align-items: center;
  gap: 0.6rem;
  height: 56px; /* match the topbar so the header line is flush, not bumpy */
  padding: 0 1.1rem;
  flex-shrink: 0;
}
.brand-badge {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 30px;
  height: 30px;
  border-radius: 8px;
  background: var(--p-primary-color, #16a34a);
  color: #fff;
  font-size: 0.95rem;
  flex-shrink: 0;
}
.brand-name {
  font-weight: 700;
  font-size: 1.05rem;
  letter-spacing: -0.01em;
  color: var(--p-text-color, #0f172a);
}
.brand-sub {
  margin-left: 0.35rem;
  font-weight: 500;
  font-size: 0.82rem;
  color: var(--p-text-muted-color, #94a3b8);
}
.nav-scroll {
  flex: 1;
  min-height: 0;
  overflow-y: auto;
  overscroll-behavior: contain;
  scrollbar-width: thin;
  scrollbar-color: var(--teggo-border, #cbd5e1) transparent;
  padding: 0.5rem 0 1rem;
}
.nav-scroll::-webkit-scrollbar {
  width: 8px;
}
.nav-scroll::-webkit-scrollbar-thumb {
  background: var(--teggo-border, #cbd5e1);
  border-radius: 8px;
}

/* --- Static menu (Verona-style): section labels + flat item links --- */
.menu {
  list-style: none;
  margin: 0;
  padding: 0 0.75rem;
}
/* Uppercase, muted, bold section labels — always visible. */
.menu-section {
  padding: 1rem 0.6rem 0.4rem;
  font-size: 0.72rem;
  font-weight: 700;
  letter-spacing: 0.06em;
  text-transform: uppercase;
  color: var(--p-text-muted-color, #94a3b8);
}
.menu-item {
  margin-bottom: 1px;
}
.menu-link {
  display: flex;
  align-items: center;
  gap: 0.65rem;
  padding: 0.55rem 0.65rem;
  border-radius: 8px;
  text-decoration: none;
  color: var(--p-text-color, #334155);
  font-size: 0.875rem;
  font-weight: 500;
  line-height: 1.2;
  transition: background-color 0.12s ease, color 0.12s ease;
}
.menu-icon {
  font-size: 1rem;
  width: 1.15rem;
  text-align: center;
  color: var(--p-text-muted-color, #94a3b8);
  flex-shrink: 0;
}
.menu-text {
  flex: 1;
  min-width: 0;
}
.menu-link:hover {
  background: var(--p-surface-100, #f1f5f9);
}
/* Active route: subtle primary-tint fill + bold; icon picks up the brand color. */
.menu-link.active {
  background: var(--p-primary-50, #f0fdf4);
  font-weight: 700;
}
.menu-link.active .menu-icon {
  color: var(--p-primary-color, #16a34a);
}

.main {
  flex: 1;
  display: flex;
  flex-direction: column;
  min-width: 0;
  height: 100%;
  overflow: hidden;
}
.topbar {
  display: flex;
  align-items: center;
  height: 56px;
  flex-shrink: 0;
  padding: 0 1rem;
  background: var(--teggo-surface, #fff);
  border-bottom: 1px solid var(--p-surface-200, #e2e8f0);
}
.spacer {
  flex: 1;
}
.topbar-bell {
  margin-right: 0.75rem;
}
.account-trigger {
  border: none;
  background: transparent;
  padding: 0;
  cursor: pointer;
  display: flex;
  align-items: center;
}
.account-avatar {
  background: var(--p-primary-color, #16a34a);
  color: #fff;
  font-weight: 700;
  font-size: 0.8rem;
}

/* Account dropdown card */
.account-card {
  width: 230px;
  display: flex;
  flex-direction: column;
  gap: 0.85rem;
}
.account-head {
  display: flex;
  align-items: center;
  gap: 0.7rem;
}
.account-id {
  min-width: 0;
}
.account-email {
  font-weight: 600;
  font-size: 0.9rem;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.account-org {
  font-size: 0.78rem;
  color: var(--p-text-muted-color, #64748b);
}
.account-meta {
  display: flex;
  align-items: center;
  gap: 0.45rem;
  font-size: 0.8rem;
  color: var(--p-text-muted-color, #64748b);
  padding-top: 0.6rem;
  border-top: 1px solid var(--teggo-border, #cbd5e1);
}
.account-signout {
  width: 100%;
}
.content {
  padding: 1.5rem;
  flex: 1;
  min-height: 0;
  overflow-y: auto;
  background: var(--p-content-background, #f8fafc);
}

/* Hamburger + scrim are desktop-hidden; the breakpoint turns the fixed sidebar
   into an off-canvas drawer. */
.hamburger {
  display: none;
  align-items: center;
  justify-content: center;
  background: none;
  border: none;
  cursor: pointer;
  font-size: 1.3rem;
  color: var(--p-text-color, #1e293b);
  padding: 0.25rem 0.5rem;
  margin-right: 0.25rem;
}
.scrim {
  display: none;
}

@media (max-width: 1024px) {
  .hamburger {
    display: inline-flex;
  }
  .sidebar {
    position: fixed;
    top: 0;
    left: 0;
    z-index: 60;
    transform: translateX(-100%);
    transition: transform 0.2s ease;
    box-shadow: 0 0 40px rgba(0, 0, 0, 0.15);
  }
  .drawer-open .sidebar {
    transform: translateX(0);
  }
  .drawer-open .scrim {
    display: block;
    position: fixed;
    inset: 0;
    z-index: 55;
    background: rgba(15, 23, 42, 0.45);
  }
}
</style>
