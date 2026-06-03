<script setup lang="ts">
import { computed, defineAsyncComponent, onMounted, ref } from 'vue'
import { useToast } from 'primevue/usetoast'
import Card from 'primevue/card'
import Button from 'primevue/button'
import Message from 'primevue/message'
import { api, errMessage } from '@/lib/client'
import type { components } from '@teggo/api/schema'

// ECharts lazy-loaded (own chunk) per the established pattern.
const LineChart = defineAsyncComponent(() => import('@/components/LineChart.vue'))
const BarChart = defineAsyncComponent(() => import('@/components/BarChart.vue'))

type Summary = components['schemas']['SalesSummary']
type DailyPoint = components['schemas']['DailySalesPoint']
type TopProduct = components['schemas']['TopProduct']

const summary = ref<Summary | null>(null)
const sales = ref<DailyPoint[]>([])
const top = ref<TopProduct[]>([])
const loading = ref(false)
const error = ref('')
const refreshing = ref(false)
const toast = useToast()

const salesLabels = computed(() => sales.value.map((d) => d.day.slice(5))) // MM-DD
const salesValues = computed(() => sales.value.map((d) => Number(d.revenue)))
const topLabels = computed(() => top.value.map((t) => t.name))
const topValues = computed(() => top.value.map((t) => Number(t.revenue)))

async function load() {
  loading.value = true
  error.value = ''
  const [s, ds, tp] = await Promise.all([
    api.GET('/admin/reports/summary', { params: { query: { days: 30 } } }),
    api.GET('/admin/reports/sales'),
    api.GET('/admin/reports/top-products', { params: { query: { limit: 8 } } }),
  ])
  loading.value = false
  if (s.error || ds.error || tp.error) {
    error.value = errMessage(s.error || ds.error || tp.error, 'Failed to load reports')
    return
  }
  summary.value = s.data ?? null
  sales.value = ds.data?.items ?? []
  top.value = tp.data?.items ?? []
}

async function refresh() {
  refreshing.value = true
  const { error: err } = await api.POST('/admin/reports/refresh')
  refreshing.value = false
  if (err) {
    toast.add({ severity: 'error', summary: 'Refresh failed', detail: errMessage(err), life: 4000 })
    return
  }
  toast.add({ severity: 'success', summary: 'Reports refreshed', life: 2000 })
  load()
}

onMounted(load)
</script>

<template>
  <div class="page">
    <div class="header">
      <h1>Analytics <span class="muted">last 30 days</span></h1>
      <Button icon="pi pi-sync" label="Refresh data" :loading="refreshing" severity="secondary" outlined @click="refresh" />
    </div>

    <Message v-if="error" severity="error" :closable="false" class="mb">{{ error }}</Message>

    <div class="kpis">
      <Card class="kpi"><template #content>
        <div class="kpi-value">{{ summary?.revenue ?? '—' }}</div>
        <div class="kpi-label">Revenue (30d)</div>
      </template></Card>
      <Card class="kpi"><template #content>
        <div class="kpi-value">{{ summary?.order_count ?? '—' }}</div>
        <div class="kpi-label">Orders (30d)</div>
      </template></Card>
      <Card class="kpi"><template #content>
        <div class="kpi-value">{{ summary?.avg_order_value ?? '—' }}</div>
        <div class="kpi-label">Avg order value</div>
      </template></Card>
    </div>

    <Card class="block">
      <template #title>Daily revenue</template>
      <template #content>
        <LineChart v-if="salesValues.length" :labels="salesLabels" :values="salesValues" name="Revenue" />
        <p v-else class="muted">No sales in this window yet.</p>
      </template>
    </Card>

    <Card class="block">
      <template #title>Top products (this month)</template>
      <template #content>
        <BarChart v-if="topValues.length" :labels="topLabels" :values="topValues" name="Revenue" />
        <p v-else class="muted">No product sales this month yet.</p>
      </template>
    </Card>
  </div>
</template>

<style scoped>
.header { display: flex; align-items: center; justify-content: space-between; }
.header h1 { margin: 0; }
.muted { color: var(--p-text-muted-color, #64748b); font-weight: 400; font-size: 1rem; }
.mb { margin-bottom: 1rem; }
.kpis { display: grid; grid-template-columns: repeat(auto-fit, minmax(180px, 1fr)); gap: 1rem; margin: 1.25rem 0; }
.kpi-value { font-size: 1.7rem; font-weight: 700; }
.kpi-label { color: var(--p-text-muted-color, #64748b); font-size: 0.85rem; margin-top: 0.2rem; }
.block { margin-bottom: 1rem; }
</style>
