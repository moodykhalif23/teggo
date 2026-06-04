<script setup lang="ts">
import { computed, defineAsyncComponent, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import Card from 'primevue/card'
import Tag from 'primevue/tag'
import { useAuthStore } from '@/stores/auth'
import { api } from '@/lib/client'

// Lazy so ECharts lands in its own chunk, off the dashboard's critical path.
const BarChart = defineAsyncComponent(() => import('@/components/BarChart.vue'))

const auth = useAuthStore()
const router = useRouter()

interface Kpi {
  label: string
  icon: string
  value: number | string
  route: string
  permission: string
}

const kpis = ref<Kpi[]>([])

async function load() {
  const out: Kpi[] = []

  if (auth.can('customer.view')) {
    const { data } = await api.GET('/admin/customers', { params: { query: { page: 1, page_size: 1 } } })
    out.push({ label: 'Customers', icon: 'pi pi-building', value: data?.total ?? 0, route: 'customers', permission: 'customer.view' })
  }
  if (auth.can('product.view')) {
    const { data } = await api.GET('/admin/products', { params: { query: { page: 1, page_size: 1 } } })
    out.push({ label: 'Products', icon: 'pi pi-box', value: data?.total ?? 0, route: 'products', permission: 'product.view' })
  }
  if (auth.can('quote.view')) {
    const { data } = await api.GET('/admin/quotes')
    out.push({ label: 'Quotes', icon: 'pi pi-file-edit', value: data?.items?.length ?? 0, route: 'quotes', permission: 'quote.view' })
  }
  if (auth.can('order.view')) {
    const { data } = await api.GET('/admin/orders')
    out.push({ label: 'Orders', icon: 'pi pi-shopping-cart', value: data?.items?.length ?? 0, route: 'orders', permission: 'order.view' })
  }
  if (auth.can('invoice.view')) {
    const { data } = await api.GET('/admin/invoices')
    out.push({ label: 'Invoices', icon: 'pi pi-receipt', value: data?.items?.length ?? 0, route: 'invoices', permission: 'invoice.view' })
  }
  kpis.value = out
}

// A KPI is charted only when its value is numeric (counts, not "—").
const chartKpis = computed(() => kpis.value.filter((k) => typeof k.value === 'number'))
const chartLabels = computed(() => chartKpis.value.map((k) => k.label))
const chartValues = computed(() => chartKpis.value.map((k) => Number(k.value)))

onMounted(load)
</script>

<template>
  <div class="page">
    <h1>Dashboard</h1>
    <p class="muted">Back-office for the Teggo B2B platform.</p>

    <div class="kpis">
      <Card v-for="k in kpis" :key="k.label" class="kpi" @click="router.push({ name: k.route })">
        <template #content>
          <div class="kpi-row">
            <i :class="k.icon" />
            <div>
              <div class="kpi-value">{{ k.value }}</div>
              <div class="kpi-label">{{ k.label }}</div>
            </div>
          </div>
        </template>
      </Card>
    </div>

    <Card v-if="chartKpis.length" class="overview">
      <template #title>At a glance</template>
      <template #content>
        <BarChart :labels="chartLabels" :values="chartValues" name="Records" />
      </template>
    </Card>

    <Card v-if="chartValues.length" class="chartcard">
      <template #title>Overview</template>
      <template #content>
        <BarChart :labels="chartLabels" :values="chartValues" name="Records" />
      </template>
    </Card>

    <Card class="session">
      <template #title>Session</template>
      <template #content>
        <p><strong>Organization:</strong> {{ auth.orgId ?? '—' }}</p>
        <div class="tags">
          <Tag v-for="p in auth.permissions" :key="p" :value="p" severity="secondary" />
          <span v-if="!auth.permissions.length" class="muted">no permissions</span>
        </div>
      </template>
    </Card>
  </div>
</template>

<style scoped>
.page h1 { margin: 0 0 0.25rem; }
.muted { color: var(--p-text-muted-color, #64748b); }
.kpis { display: grid; grid-template-columns: repeat(auto-fit, minmax(180px, 1fr)); gap: 1rem; margin: 1.25rem 0; }
.kpi { cursor: pointer; }
.kpi-row { display: flex; align-items: center; gap: 0.9rem; }
.kpi-row i { font-size: 1.6rem; color: var(--p-primary-color, #16a34a); }
.kpi-value { font-size: 1.7rem; font-weight: 700; line-height: 1; }
.kpi-label { color: var(--p-text-muted-color, #64748b); font-size: 0.85rem; margin-top: 0.2rem; }
.chartcard { margin-bottom: 1rem; }
.session { margin-top: 0.5rem; }
.tags { display: flex; flex-wrap: wrap; gap: 0.4rem; margin-top: 0.5rem; }
</style>
