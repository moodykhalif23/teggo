import { createRouter, createWebHistory, type RouteRecordRaw } from 'vue-router'
import { auth } from '@/lib/auth'
import VendorLayout from '@/layouts/VendorLayout.vue'

const routes: RouteRecordRaw[] = [
  { path: '/login', name: 'login', component: () => import('@/views/LoginView.vue'), meta: { public: true } },
  {
    path: '/',
    component: VendorLayout,
    children: [
      { path: '', name: 'dashboard', component: () => import('@/views/DashboardView.vue') },
      { path: 'orders', name: 'orders', component: () => import('@/views/OrdersView.vue') },
      { path: 'products', name: 'products', component: () => import('@/views/ProductsView.vue') },
      { path: 'payouts', name: 'payouts', component: () => import('@/views/PayoutsView.vue') },
    ],
  },
]

export const router = createRouter({
  history: createWebHistory(),
  routes,
})

// Auth guard: every non-public route requires a vendor token.
router.beforeEach((to) => {
  if (to.meta.public) return true
  if (!auth.isAuthenticated) return { name: 'login', query: { redirect: to.fullPath } }
  return true
})
