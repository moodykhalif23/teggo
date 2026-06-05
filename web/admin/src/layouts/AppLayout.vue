<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { RouterView, useRouter, useRoute } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import PanelMenu from 'primevue/panelmenu'
import Avatar from 'primevue/avatar'
import Popover from 'primevue/popover'
import Button from 'primevue/button'
import type { MenuItem } from 'primevue/menuitem'

const auth = useAuthStore()
const router = useRouter()
const route = useRoute()

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
    items: [{ label: 'Dashboard', icon: 'pi pi-home', routeName: 'dashboard' }],
  },
  {
    label: 'Catalog',
    icon: 'pi pi-th-large',
    items: [
      { label: 'Products', icon: 'pi pi-box', routeName: 'products', permission: 'product.view' },
      { label: 'Categories', icon: 'pi pi-sitemap', routeName: 'categories', permission: 'category.view' },
      { label: 'Attributes', icon: 'pi pi-tags', routeName: 'attributes', permission: 'attribute.view' },
      { label: 'Configurator', icon: 'pi pi-sliders-h', routeName: 'configurator', permission: 'product.view' },
    ],
  },
  {
    label: 'Pricing',
    icon: 'pi pi-dollar',
    items: [
      { label: 'Price lists', icon: 'pi pi-dollar', routeName: 'pricing', permission: 'price_list.view' },
      { label: 'Price rules', icon: 'pi pi-sliders-h', routeName: 'price-rules', permission: 'price_list.view' },
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
      { label: 'Websites', icon: 'pi pi-globe', routeName: 'websites', permission: 'tenant.view' },
      { label: 'Configuration', icon: 'pi pi-cog', routeName: 'settings', permission: 'settings.view' },
      { label: 'Integrations', icon: 'pi pi-sync', routeName: 'integrations', permission: 'integration.view' },
      { label: 'ERP sync', icon: 'pi pi-server', routeName: 'erp', permission: 'erp.view' },
      { label: 'SSO providers', icon: 'pi pi-id-card', routeName: 'identity-providers', permission: 'sso.view' },
    ],
  },
]

// Build the PanelMenu model: drop leaves the user lacks permission for, drop
// emptied groups, key each node, and mark the active route leaf.
const navModel = computed<MenuItem[]>(() => {
  const model: MenuItem[] = []
  for (const g of groups) {
    const leaves = g.items.filter((i) => !i.permission || auth.can(i.permission))
    if (!leaves.length) continue
    const children: MenuItem[] = leaves.map((i) => ({
      key: i.routeName,
      label: i.label,
      icon: i.icon,
      class: route.name === i.routeName ? 'nav-active' : undefined,
      command: () => router.push({ name: i.routeName }),
    }))
    if (g.label) model.push({ key: g.label, label: g.label, icon: g.icon, items: children })
    else model.push(...children) // Dashboard — top-level leaf, no group
  }
  return model
})

// Breadcrumb shown in the topbar for orientation. Built from the nav groups
// (routeName → { section, title }) plus a small map for detail/editor routes
// that aren't in the sidebar. Presentation only.
const crumbIndex = computed<Record<string, { section: string; title: string }>>(() => {
  const idx: Record<string, { section: string; title: string }> = {}
  for (const g of groups) {
    for (const i of g.items) idx[i.routeName] = { section: g.label, title: i.label }
  }
  Object.assign(idx, {
    'customer-detail': { section: 'Customers', title: 'Customer' },
    'price-list-detail': { section: 'Pricing', title: 'Price list' },
    'rfq-detail': { section: 'Sales', title: 'RFQ' },
    'quote-editor': { section: 'Sales', title: 'Quote' },
    'order-detail': { section: 'Sales', title: 'Order' },
    'invoice-detail': { section: 'Sales', title: 'Invoice' },
  })
  return idx
})
const crumb = computed(() => crumbIndex.value[String(route.name)] ?? { section: '', title: 'Dashboard' })

// Keep the group that owns the current route expanded (without collapsing groups
// the user opened themselves). PanelMenu also toggles this via v-model.
const expandedKeys = ref<Record<string, boolean>>({})
watch(
  () => route.name,
  () => {
    for (const g of groups) {
      if (g.label && g.items.some((i) => i.routeName === route.name)) {
        expandedKeys.value = { ...expandedKeys.value, [g.label]: true }
      }
    }
  },
  { immediate: true },
)

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
  <div class="layout">
    <aside class="sidebar">
      <div class="brand">
        <i class="pi pi-bolt" />
        <span>Teggo Admin</span>
      </div>
      <div class="nav-scroll">
        <PanelMenu :model="navModel" v-model:expandedKeys="expandedKeys" class="nav" multiple />
      </div>
    </aside>

    <div class="main">
      <header class="topbar">
        <span class="spacer" />
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
        <nav class="crumb" aria-label="Breadcrumb">
          <i class="pi pi-home crumb-home" />
          <template v-if="crumb.section">
            <span class="crumb-sec">{{ crumb.section }}</span>
            <i class="pi pi-angle-right crumb-sep" />
          </template>
          <span class="crumb-cur">{{ crumb.title }}</span>
        </nav>
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
  gap: 0.5rem;
  font-weight: 700;
  font-size: 1rem;
  letter-spacing: 0.02em;
  height: 56px; /* match the topbar so the header line is flush, not bumpy */
  padding: 0 1.25rem;
  border-bottom: 1px solid var(--p-surface-200, #e2e8f0);
  flex-shrink: 0;
}
.brand i {
  color: var(--p-primary-color, #16a34a);
}
.nav-scroll {
  flex: 1;
  min-height: 0;
  overflow-y: auto;
  overscroll-behavior: contain;
  scrollbar-width: thin;
  scrollbar-color: var(--teggo-border, #cbd5e1) transparent;
  padding: 0.4rem 0;
}
.nav-scroll::-webkit-scrollbar {
  width: 8px;
}
.nav-scroll::-webkit-scrollbar-thumb {
  background: var(--teggo-border, #cbd5e1);
  border-radius: 0;
}

/* --- PanelMenu: flat, borderless, collapsible sections (:deep for internals) --- */
.nav {
  width: 100%;
}
.nav :deep(.p-panelmenu-panel) {
  border: none;
  background: transparent;
  margin: 0;
}
/* Group headers — clickable, with the collapse chevron; subtle, not bold/dark. */
.nav :deep(.p-panelmenu-header-content) {
  border: none;
  border-radius: 0;
  background: transparent;
}
.nav :deep(.p-panelmenu-header-link) {
  display: flex;
  align-items: center;
  width: 100%;
  gap: 0.65rem;
  padding: 0.55rem 1.1rem;
  color: var(--p-text-muted-color, #64748b);
  font-size: 0.7rem;
  font-weight: 700;
  letter-spacing: 0.07em;
  text-transform: uppercase;
}
/* Group icon on the left of the label (matches the submenu/leaf icons). */
.nav :deep(.p-panelmenu-header-icon) {
  font-size: 0.95rem;
  color: var(--p-text-muted-color, #64748b);
}
/* Collapse chevron sits on the right of the group header. */
.nav :deep(.p-panelmenu-header-link) .p-panelmenu-submenu-icon {
  order: 1;
  margin-left: auto;
  font-size: 0.75rem;
}
.nav :deep(.p-panelmenu-header-content:hover) {
  background: transparent;
}
/* Leaf items — flat, sharp, compact; no hover, active state instead. */
.nav :deep(.p-panelmenu-item-content) {
  border-radius: 0;
  transition: none;
}
.nav :deep(.p-panelmenu-item-link) {
  padding: 0.45rem 1.1rem 0.45rem 1.6rem;
  gap: 0.6rem;
}
.nav :deep(.p-panelmenu-item-icon) {
  font-size: 0.9rem;
  color: var(--p-text-muted-color, #64748b);
}
.nav :deep(.p-panelmenu-item-label) {
  font-size: 0.87rem;
}
.nav :deep(.p-panelmenu-item-content:hover) {
  background: transparent;
}
/* Active route leaf: green left-accent + tint. */
.nav :deep(.p-panelmenu-item.nav-active > .p-panelmenu-item-content) {
  background: var(--p-primary-50, #f0fdf4);
  box-shadow: inset 2px 0 0 0 var(--p-primary-color, #16a34a);
}
.nav :deep(.p-panelmenu-item.nav-active .p-panelmenu-item-label) {
  font-weight: 600;
  color: var(--p-primary-color, #16a34a);
}
.nav :deep(.p-panelmenu-item.nav-active .p-panelmenu-item-icon) {
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
.crumb {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  font-size: 0.8rem;
  margin-bottom: 1rem;
}
.crumb-home {
  font-size: 0.85rem;
  color: var(--p-surface-400, #94a3b8);
}
.crumb-sec {
  color: var(--p-text-muted-color, #64748b);
}
.crumb-sep {
  font-size: 0.7rem;
  color: var(--p-surface-300, #cbd5e1);
}
.crumb-cur {
  font-weight: 600;
  color: var(--p-text-color, #0f172a);
}
.spacer {
  flex: 1;
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
</style>
