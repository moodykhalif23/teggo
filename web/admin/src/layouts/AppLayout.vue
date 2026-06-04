<script setup lang="ts">
import { computed } from 'vue'
import { RouterView, useRouter } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import Menu from 'primevue/menu'
import Button from 'primevue/button'
import type { MenuItem } from 'primevue/menuitem'

const auth = useAuthStore()
const router = useRouter()

// Each nav item declares the permission it needs; items the user can't access
// are filtered out (deny-by-default mirrors the backend RBAC).
interface NavItem extends MenuItem {
  permission?: string
  routeName: string
}

const allItems: NavItem[] = [
  { label: 'Dashboard', icon: 'pi pi-home', routeName: 'dashboard' },
  { label: 'Customers', icon: 'pi pi-building', routeName: 'customers', permission: 'customer.view' },
  { label: 'Customer groups', icon: 'pi pi-users', routeName: 'customer-groups', permission: 'customer.view' },
  { label: 'Products', icon: 'pi pi-box', routeName: 'products', permission: 'product.view' },
  { label: 'Categories', icon: 'pi pi-sitemap', routeName: 'categories', permission: 'category.view' },
  { label: 'Attributes', icon: 'pi pi-tags', routeName: 'attributes', permission: 'attribute.view' },
  { label: 'Configurator', icon: 'pi pi-sliders-h', routeName: 'configurator', permission: 'product.view' },
  { label: 'Pricing', icon: 'pi pi-dollar', routeName: 'pricing', permission: 'price_list.view' },
  { label: 'Tax & Shipping', icon: 'pi pi-percentage', routeName: 'tax-shipping', permission: 'tax.view' },
  { label: 'RFQs', icon: 'pi pi-inbox', routeName: 'rfqs', permission: 'rfq.view' },
  { label: 'Quotes', icon: 'pi pi-file-edit', routeName: 'quotes', permission: 'quote.view' },
  { label: 'Orders', icon: 'pi pi-shopping-cart', routeName: 'orders', permission: 'order.view' },
  { label: 'Invoices', icon: 'pi pi-receipt', routeName: 'invoices', permission: 'invoice.view' },
  { label: 'Inventory', icon: 'pi pi-warehouse', routeName: 'inventory', permission: 'inventory.view' },
  { label: 'Leads', icon: 'pi pi-filter', routeName: 'leads', permission: 'crm.view' },
  { label: 'Pipeline', icon: 'pi pi-chart-bar', routeName: 'pipeline', permission: 'crm.view' },
  { label: 'Opportunities', icon: 'pi pi-briefcase', routeName: 'opportunities', permission: 'crm.view' },
  { label: 'Workflows', icon: 'pi pi-sitemap', routeName: 'workflows', permission: 'workflow.view' },
  { label: 'Automation', icon: 'pi pi-bolt', routeName: 'automation-rules', permission: 'workflow.view' },
  { label: 'Pages', icon: 'pi pi-file', routeName: 'pages', permission: 'cms.view' },
  { label: 'Media', icon: 'pi pi-images', routeName: 'media', permission: 'cms.view' },
  { label: 'Analytics', icon: 'pi pi-chart-line', routeName: 'analytics', permission: 'report.view' },
  { label: 'Report builder', icon: 'pi pi-table', routeName: 'report-builder', permission: 'report.view' },
  { label: 'Websites', icon: 'pi pi-globe', routeName: 'websites', permission: 'tenant.view' },
  { label: 'Integrations', icon: 'pi pi-sync', routeName: 'integrations', permission: 'integration.view' },
  { label: 'ERP sync', icon: 'pi pi-server', routeName: 'erp', permission: 'erp.view' },
  { label: 'SSO providers', icon: 'pi pi-id-card', routeName: 'identity-providers', permission: 'sso.view' },
  { label: 'Field devices', icon: 'pi pi-mobile', routeName: 'field-devices', permission: 'field.sync' },
]

const navItems = computed<MenuItem[]>(() =>
  allItems
    .filter((i) => !i.permission || auth.can(i.permission))
    .map((i) => ({
      label: i.label,
      icon: i.icon,
      command: () => router.push({ name: i.routeName }),
    })),
)

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
        <Menu :model="navItems" class="nav" />
      </div>
    </aside>

    <div class="main">
      <header class="topbar">
        <span class="spacer" />
        <Button
          icon="pi pi-sign-out"
          label="Sign out"
          severity="secondary"
          text
          @click="logout"
        />
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
  background: var(--p-surface-0, #fff);
  border-right: 1px solid var(--p-surface-200, #e2e8f0);
  display: flex;
  flex-direction: column;
}
.brand {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  font-weight: 700;
  font-size: 1.1rem;
  padding: 1.25rem 1.25rem;
  border-bottom: 1px solid var(--p-surface-200, #e2e8f0);
  flex-shrink: 0;
}
.nav-scroll {
  flex: 1;
  min-height: 0;
  overflow-y: auto;
  overscroll-behavior: contain;
  scrollbar-width: thin;
  scrollbar-color: var(--p-surface-300, #cbd5e1) transparent;
}
.nav-scroll::-webkit-scrollbar {
  width: 8px;
}
.nav-scroll::-webkit-scrollbar-thumb {
  background: var(--p-surface-300, #cbd5e1);
  border-radius: 4px;
}
.nav-scroll::-webkit-scrollbar-thumb:hover {
  background: var(--p-surface-400, #94a3b8);
}
.nav {
  border: none;
  width: 100%;
  background: transparent;
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
  background: var(--p-surface-0, #fff);
  border-bottom: 1px solid var(--p-surface-200, #e2e8f0);
}
.spacer {
  flex: 1;
}
.content {
  padding: 1.5rem;
  flex: 1;
  min-height: 0;
  overflow-y: auto;
}
</style>
