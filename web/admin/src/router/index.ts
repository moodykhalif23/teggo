import { createRouter, createWebHistory, type RouteRecordRaw } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import AppLayout from '@/layouts/AppLayout.vue'

// Routes carry meta.permission; the global guard enforces auth + permission,
// and the sidebar (AppLayout) filters its items by the same permission keys.
const routes: RouteRecordRaw[] = [
  {
    path: '/login',
    name: 'login',
    component: () => import('@/views/LoginView.vue'),
    meta: { public: true },
  },
  {
    path: '/',
    component: AppLayout,
    children: [
      {
        path: '',
        name: 'dashboard',
        component: () => import('@/views/DashboardView.vue'),
      },
      {
        path: 'customers',
        name: 'customers',
        component: () => import('@/views/customers/CustomerListView.vue'),
        meta: { permission: 'customer.view' },
      },
      {
        path: 'customers/:id',
        name: 'customer-detail',
        component: () => import('@/views/customers/CustomerDetailView.vue'),
        meta: { permission: 'customer.view' },
      },
      {
        path: 'customer-groups',
        name: 'customer-groups',
        component: () => import('@/views/customers/CustomerGroupsView.vue'),
        meta: { permission: 'customer.view' },
      },
      {
        path: 'products',
        name: 'products',
        component: () => import('@/views/products/ProductListView.vue'),
        meta: { permission: 'product.view' },
      },
      {
        path: 'categories',
        name: 'categories',
        component: () => import('@/views/catalog/CategoriesView.vue'),
        meta: { permission: 'category.view' },
      },
      {
        path: 'attributes',
        name: 'attributes',
        component: () => import('@/views/catalog/AttributesView.vue'),
        meta: { permission: 'attribute.view' },
      },
      {
        path: 'pricing',
        name: 'pricing',
        component: () => import('@/views/pricing/PriceListsView.vue'),
        meta: { permission: 'price_list.view' },
      },
      {
        path: 'pricing/:id',
        name: 'price-list-detail',
        component: () => import('@/views/pricing/PriceListDetailView.vue'),
        meta: { permission: 'price_list.view' },
      },
      {
        path: 'price-rules',
        name: 'price-rules',
        component: () => import('@/views/pricing/PriceRulesView.vue'),
        meta: { permission: 'price_list.view' },
      },
      {
        path: 'settings',
        name: 'settings',
        component: () => import('@/views/settings/SettingsView.vue'),
        meta: { permission: 'settings.view' },
      },
      {
        path: 'rfqs',
        name: 'rfqs',
        component: () => import('@/views/sales/RfqListView.vue'),
        meta: { permission: 'rfq.view' },
      },
      {
        path: 'rfqs/:id',
        name: 'rfq-detail',
        component: () => import('@/views/sales/RfqDetailView.vue'),
        meta: { permission: 'rfq.view' },
      },
      {
        path: 'quotes',
        name: 'quotes',
        component: () => import('@/views/sales/QuoteListView.vue'),
        meta: { permission: 'quote.view' },
      },
      {
        path: 'quotes/:id',
        name: 'quote-editor',
        component: () => import('@/views/sales/QuoteEditorView.vue'),
        meta: { permission: 'quote.view' },
      },
      {
        path: 'orders',
        name: 'orders',
        component: () => import('@/views/sales/OrderListView.vue'),
        meta: { permission: 'order.view' },
      },
      {
        path: 'orders/:id',
        name: 'order-detail',
        component: () => import('@/views/sales/OrderDetailView.vue'),
        meta: { permission: 'order.view' },
      },
      {
        path: 'invoices',
        name: 'invoices',
        component: () => import('@/views/sales/InvoiceListView.vue'),
        meta: { permission: 'invoice.view' },
      },
      {
        path: 'ar-aging',
        name: 'ar-aging',
        component: () => import('@/views/sales/ArAgingView.vue'),
        meta: { permission: 'invoice.view' },
      },
      {
        path: 'returns',
        name: 'returns',
        component: () => import('@/views/sales/ReturnsView.vue'),
        meta: { permission: 'return.view' },
      },
      {
        path: 'invoices/:id',
        name: 'invoice-detail',
        component: () => import('@/views/sales/InvoiceDetailView.vue'),
        meta: { permission: 'invoice.view' },
      },
      {
        path: 'inventory',
        name: 'inventory',
        component: () => import('@/views/inventory/InventoryView.vue'),
        meta: { permission: 'inventory.view' },
      },
      {
        path: 'leads',
        name: 'leads',
        component: () => import('@/views/crm/LeadListView.vue'),
        meta: { permission: 'crm.view' },
      },
      {
        path: 'pipeline',
        name: 'pipeline',
        component: () => import('@/views/crm/PipelineBoardView.vue'),
        meta: { permission: 'crm.view' },
      },
      {
        path: 'opportunities',
        name: 'opportunities',
        component: () => import('@/views/crm/OpportunityListView.vue'),
        meta: { permission: 'crm.view' },
      },
      {
        path: 'account-health',
        name: 'account-health',
        component: () => import('@/views/crm/AccountHealthView.vue'),
        meta: { permission: 'crm.view' },
      },
      {
        path: 'workflows',
        name: 'workflows',
        component: () => import('@/views/workflow/WorkflowsView.vue'),
        meta: { permission: 'workflow.view' },
      },
      {
        path: 'automation-rules',
        name: 'automation-rules',
        component: () => import('@/views/workflow/AutomationRulesView.vue'),
        meta: { permission: 'workflow.view' },
      },
      {
        path: 'approval-routing',
        name: 'approval-routing',
        component: () => import('@/views/workflow/ApprovalRoutingView.vue'),
        meta: { permission: 'workflow.view' },
      },
      {
        path: 'pages',
        name: 'pages',
        component: () => import('@/views/cms/PagesView.vue'),
        meta: { permission: 'cms.view' },
      },
      {
        path: 'media',
        name: 'media',
        component: () => import('@/views/dam/MediaView.vue'),
        meta: { permission: 'cms.view' },
      },
      {
        path: 'analytics',
        name: 'analytics',
        component: () => import('@/views/reporting/ReportsView.vue'),
        meta: { permission: 'report.view' },
      },
      {
        path: 'report-builder',
        name: 'report-builder',
        component: () => import('@/views/reporting/ReportBuilderView.vue'),
        meta: { permission: 'report.view' },
      },
      {
        path: 'websites',
        name: 'websites',
        component: () => import('@/views/tenancy/WebsitesView.vue'),
        meta: { permission: 'tenant.view' },
      },
      {
        path: 'integrations',
        name: 'integrations',
        component: () => import('@/views/integration/IntegrationView.vue'),
        meta: { permission: 'integration.view' },
      },
      {
        path: 'field-devices',
        name: 'field-devices',
        component: () => import('@/views/field/FieldDevicesView.vue'),
        meta: { permission: 'field.sync' },
      },
      {
        path: 'erp',
        name: 'erp',
        component: () => import('@/views/erp/ErpView.vue'),
        meta: { permission: 'erp.view' },
      },
      {
        path: 'identity-providers',
        name: 'identity-providers',
        component: () => import('@/views/sso/IdentityProvidersView.vue'),
        meta: { permission: 'sso.view' },
      },
      {
        path: 'configurator',
        name: 'configurator',
        component: () => import('@/views/cpq/ConfiguratorView.vue'),
        meta: { permission: 'product.view' },
      },
      {
        path: 'tax-shipping',
        name: 'tax-shipping',
        component: () => import('@/views/settings/TaxShippingView.vue'),
        meta: { permission: 'tax.view' },
      },
    ],
  },
  { path: '/:pathMatch(.*)*', redirect: '/' },
]

export const router = createRouter({
  history: createWebHistory(),
  routes,
})

router.beforeEach((to) => {
  const auth = useAuthStore()

  if (to.meta.public) {
    return auth.isAuthenticated && to.name === 'login' ? { name: 'dashboard' } : true
  }

  if (!auth.isAuthenticated) {
    return { name: 'login', query: { redirect: to.fullPath } }
  }

  const required = to.meta.permission as string | undefined
  if (required && !auth.can(required)) {
    return { name: 'dashboard' }
  }

  return true
})
