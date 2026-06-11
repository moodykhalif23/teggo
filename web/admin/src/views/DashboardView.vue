<script setup lang="ts">
import { computed, defineAsyncComponent, onMounted, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import Card from 'primevue/card'
import Tag from 'primevue/tag'
import SelectButton from 'primevue/selectbutton'
import ProgressSpinner from 'primevue/progressspinner'
import { useAuthStore } from '@/stores/auth'
import { api } from '@/lib/client'
import type { components } from '@teggo/api/schema'

// ECharts lazy-loaded into their own chunk, off the dashboard's critical path.
const LineChart = defineAsyncComponent(() => import('@/components/LineChart.vue'))
const BarChart = defineAsyncComponent(() => import('@/components/BarChart.vue'))

type OrderSummary = components['schemas']['OrderSummary']
type SalesSummary = components['schemas']['SalesSummary']
type DailyPoint = components['schemas']['DailySalesPoint']
type TopProduct = components['schemas']['TopProduct']

const auth = useAuthStore()
const router = useRouter()

const loading = ref(true)
const orgName = ref<string>('')

// Period selector — drives the summary KPIs and the revenue trend.
const period = ref(30)
const periodOptions = [
  { label: '7d', value: 7 },
  { label: '30d', value: 30 },
  { label: '90d', value: 90 },
]
const canReport = computed(() => auth.can('report.view'))

// Period-dependent (refetched on toggle).
const summary = ref<SalesSummary | null>(null)
const salesItems = ref<DailyPoint[]>([])

// Loaded once.
const arOpen = ref<string | null>(null)
const agingBuckets = ref<{ label: string; amount: string }[]>([])
const customersTotal = ref<number | null>(null)
const productsTotal = ref<number | null>(null)
const top = ref<TopProduct[]>([])
const statusLabels = ref<string[]>([])
const statusValues = ref<number[]>([])
const recentOrders = ref<OrderSummary[]>([])

interface Kpi {
  key: string
  label: string
  sub?: string
  icon: string
  value: number | string
  route: string
}
const kpis = computed<Kpi[]>(() => {
  const k: Kpi[] = []
  if (summary.value) {
    k.push({ key: 'rev', label: 'Revenue', sub: `last ${period.value} days`, icon: 'pi pi-chart-line', value: summary.value.revenue, route: 'analytics' })
    k.push({ key: 'ord', label: 'Orders', sub: `last ${period.value} days`, icon: 'pi pi-shopping-cart', value: summary.value.order_count, route: 'orders' })
    k.push({ key: 'aov', label: 'Avg order value', sub: `last ${period.value} days`, icon: 'pi pi-receipt', value: summary.value.avg_order_value, route: 'analytics' })
  }
  if (arOpen.value !== null) k.push({ key: 'ar', label: 'Open AR', sub: 'outstanding', icon: 'pi pi-wallet', value: arOpen.value, route: 'ar-aging' })
  if (customersTotal.value !== null) k.push({ key: 'cust', label: 'Customers', icon: 'pi pi-building', value: customersTotal.value, route: 'customers' })
  if (productsTotal.value !== null) k.push({ key: 'prod', label: 'Products', icon: 'pi pi-box', value: productsTotal.value, route: 'products' })
  return k
})

const salesLabels = computed(() => salesItems.value.map((d) => d.day.slice(5)))
const salesValues = computed(() => salesItems.value.map((d) => Number(d.revenue)))
const topLabels = computed(() => top.value.map((t) => t.name))
const topValues = computed(() => top.value.map((t) => Number(t.revenue)))

interface Task {
  label: string
  icon: string
  count: number
  route: string
  severity: 'info' | 'warn' | 'danger' | 'success'
}
const tasks = ref<Task[]>([])

const greeting = computed(() => {
  const h = new Date().getHours()
  return h < 12 ? 'Good morning' : h < 18 ? 'Good afternoon' : 'Good evening'
})
const firstName = computed(() => (auth.email ?? '').split('@')[0] || 'there')

function statusSeverity(s: string) {
  if (['delivered', 'closed', 'paid', 'confirmed'].includes(s)) return 'success'
  if (['cancelled', 'rejected', 'overdue'].includes(s)) return 'danger'
  if (['on_hold', 'pending'].includes(s)) return 'warn'
  return 'info'
}

const ymd = (d: Date) => d.toISOString().slice(0, 10)

// Period-dependent reports — refetched whenever the toggle changes.
async function loadReports(days: number) {
  if (!canReport.value) return
  const to = new Date()
  const from = new Date()
  from.setDate(from.getDate() - days)
  const [s, sales] = await Promise.all([
    api.GET('/admin/reports/summary', { params: { query: { days } } }),
    api.GET('/admin/reports/sales', { params: { query: { from: ymd(from), to: ymd(to) } } }),
  ])
  summary.value = s.data ?? null
  salesItems.value = sales.data?.items ?? []
}

async function load() {
  loading.value = true
  const can = (p: string) => auth.can(p)

  const [orders, aging, rfqs, quotes, returns, pendingProducts, customers, products, org, topProducts] =
    await Promise.all([
      can('order.view') ? api.GET('/admin/orders') : null,
      can('invoice.view') ? api.GET('/admin/invoices/aging') : null,
      can('rfq.view') ? api.GET('/admin/rfqs') : null,
      can('quote.view') ? api.GET('/admin/quotes') : null,
      can('return.view') ? api.GET('/admin/returns') : null,
      can('product.view') ? api.GET('/admin/products/pending') : null,
      can('customer.view') ? api.GET('/admin/customers', { params: { query: { page: 1, page_size: 1 } } }) : null,
      can('product.view') ? api.GET('/admin/products', { params: { query: { page: 1, page_size: 1 } } }) : null,
      can('tenant.view') ? api.GET('/admin/organization') : null,
      can('report.view') ? api.GET('/admin/reports/top-products', { params: { query: { limit: 6 } } }) : null,
      loadReports(period.value),
    ])

  orgName.value = org?.data?.name ?? ''
  customersTotal.value = customers ? customers.data?.total ?? 0 : null
  productsTotal.value = products ? products.data?.total ?? 0 : null
  top.value = topProducts?.data?.items ?? []

  if (aging?.data) {
    arOpen.value = aging.data.open_total
    agingBuckets.value = ['current', '1-30', '31-60', '61-90', '90+'].map((b) => ({
      label: b,
      amount: aging.data!.buckets?.[b] ?? '0',
    }))
  }

  if (orders?.data?.items?.length) {
    const items = orders.data.items
    const counts = new Map<string, number>()
    for (const o of items) counts.set(o.status, (counts.get(o.status) ?? 0) + 1)
    statusLabels.value = [...counts.keys()]
    statusValues.value = [...counts.values()]
    recentOrders.value = [...items]
      .sort((a, b) => (b.created_at ?? '').localeCompare(a.created_at ?? ''))
      .slice(0, 5)
  }

  const t: Task[] = []
  const newRfqs = rfqs?.data?.items?.filter((r) => r.status === 'submitted').length ?? 0
  if (rfqs && newRfqs) t.push({ label: 'RFQs to quote', icon: 'pi pi-inbox', count: newRfqs, route: 'rfqs', severity: 'info' })
  const openQuotes = quotes?.data?.items?.filter((q) => q.status === 'draft' || q.status === 'sent').length ?? 0
  if (quotes && openQuotes) t.push({ label: 'Quotes in progress', icon: 'pi pi-file-edit', count: openQuotes, route: 'quotes', severity: 'info' })
  const overdue = aging?.data?.items?.filter((i) => i.days_overdue > 0).length ?? 0
  if (aging && overdue) t.push({ label: 'Overdue invoices', icon: 'pi pi-exclamation-triangle', count: overdue, route: 'ar-aging', severity: 'danger' })
  const toProcess = returns?.data?.items?.filter((r) => r.status === 'requested').length ?? 0
  if (returns && toProcess) t.push({ label: 'Returns to review', icon: 'pi pi-replay', count: toProcess, route: 'returns', severity: 'warn' })
  const pending = pendingProducts?.data?.items?.length ?? 0
  if (pendingProducts && pending) t.push({ label: 'Products awaiting moderation', icon: 'pi pi-check-square', count: pending, route: 'moderation', severity: 'warn' })
  tasks.value = t

  loading.value = false
}

// Re-pull the period-dependent reports when the toggle changes (no full reload).
watch(period, (d) => loadReports(d))

onMounted(load)
</script>

<template>
  <div class="page dashboard">
    <header class="dash-head">
      <div>
        <h1>{{ greeting }}, {{ firstName }}</h1>
        <p class="sub">
          Here's what's happening
          <template v-if="orgName"> at <strong>{{ orgName }}</strong></template>
          today.
        </p>
      </div>
      <SelectButton
        v-if="canReport"
        v-model="period"
        :options="periodOptions"
        optionLabel="label"
        optionValue="value"
        :allowEmpty="false"
        aria-label="Reporting period"
      />
    </header>

    <div v-if="loading" class="loading"><ProgressSpinner style="width: 2.5rem; height: 2.5rem" /></div>

    <template v-else>
      <!-- Headline KPIs -->
      <section v-if="kpis.length" class="stats">
        <button
          v-for="k in kpis"
          :key="k.key"
          type="button"
          class="stat"
          @click="router.push({ name: k.route })"
        >
          <span class="stat-ic"><i :class="k.icon" /></span>
          <span class="stat-main">
            <span class="stat-val">{{ k.value }}</span>
            <span class="stat-lbl">{{ k.label }}</span>
            <span v-if="k.sub" class="stat-sub">{{ k.sub }}</span>
          </span>
        </button>
      </section>

      <div class="grid">
        <!-- Revenue trend -->
        <Card v-if="canReport" class="panel span2">
          <template #title>Revenue trend</template>
          <template #subtitle>Daily revenue · last {{ period }} days</template>
          <template #content>
            <LineChart v-if="salesValues.length" :labels="salesLabels" :values="salesValues" name="Revenue" />
            <p v-else class="muted empty-line">No sales in this window yet.</p>
          </template>
        </Card>

        <!-- Needs attention -->
        <Card class="panel">
          <template #title>Needs attention</template>
          <template #subtitle>Open items across your workspace</template>
          <template #content>
            <ul v-if="tasks.length" class="tasklist">
              <li v-for="t in tasks" :key="t.label" class="task" @click="router.push({ name: t.route })">
                <span class="task-ic" :class="`sev-${t.severity}`"><i :class="t.icon" /></span>
                <span class="task-label">{{ t.label }}</span>
                <Tag :value="String(t.count)" :severity="t.severity === 'info' ? 'info' : t.severity" />
                <i class="pi pi-angle-right task-go" />
              </li>
            </ul>
            <div v-else class="caught-up">
              <i class="pi pi-check-circle" />
              <span>You're all caught up — nothing needs attention.</span>
            </div>
          </template>
        </Card>

        <!-- Top products -->
        <Card v-if="top.length" class="panel">
          <template #title>Top products</template>
          <template #subtitle>By revenue this month</template>
          <template #content>
            <ol class="toplist">
              <li v-for="(p, i) in top" :key="p.product_id" class="topitem">
                <span class="rank">{{ i + 1 }}</span>
                <span class="topname">
                  {{ p.name }}
                  <span class="topsku">{{ p.sku }}</span>
                </span>
                <span class="toprev">{{ p.revenue }}</span>
              </li>
            </ol>
          </template>
        </Card>

        <!-- Order status mix -->
        <Card v-if="statusValues.length" class="panel">
          <template #title>Orders by status</template>
          <template #subtitle>Current pipeline</template>
          <template #content>
            <BarChart :labels="statusLabels" :values="statusValues" name="Orders" />
          </template>
        </Card>

        <!-- Recent orders -->
        <Card v-if="recentOrders.length" class="panel">
          <template #title>Recent orders</template>
          <template #subtitle>Latest activity</template>
          <template #content>
            <ul class="orderlist">
              <li
                v-for="o in recentOrders"
                :key="o.public_id"
                class="order"
                @click="router.push({ name: 'order-detail', params: { id: o.id } })"
              >
                <span class="order-ref">{{ o.public_id.slice(0, 8) }}…</span>
                <Tag :value="o.status" :severity="statusSeverity(o.status)" />
                <span class="order-total">{{ o.grand_total }} {{ o.currency }}</span>
              </li>
            </ul>
          </template>
        </Card>

        <!-- AR aging -->
        <Card v-if="arOpen !== null" class="panel">
          <template #title>Accounts receivable</template>
          <template #subtitle>Open balance: {{ arOpen }}</template>
          <template #content>
            <ul class="aging">
              <li v-for="b in agingBuckets" :key="b.label" class="aging-row">
                <span class="aging-bucket" :class="{ overdue: b.label !== 'current' }">{{ b.label }}</span>
                <span class="aging-amt">{{ b.amount }}</span>
              </li>
            </ul>
          </template>
        </Card>
      </div>
    </template>
  </div>
</template>

<style scoped>
.dash-head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 1rem;
  margin-bottom: 1.5rem;
}
.dash-head h1 {
  margin: 0;
  font-size: 1.5rem;
  font-weight: 700;
  letter-spacing: -0.01em;
}
.sub {
  margin: 0.3rem 0 0;
  color: var(--p-text-muted-color, #64748b);
  font-size: 0.92rem;
}
.loading {
  display: flex;
  justify-content: center;
  padding: 4rem 0;
}
.muted { color: var(--p-text-muted-color, #64748b); }
.empty-line { padding: 1rem 0; }

/* Stat tiles */
.stats {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(210px, 1fr));
  gap: 1rem;
  margin-bottom: 1.25rem;
}
.stat {
  display: flex;
  align-items: center;
  gap: 0.9rem;
  width: 100%;
  text-align: left;
  cursor: pointer;
  padding: 1.05rem 1.15rem;
  background: var(--teggo-surface, #fff);
  border: 1px solid var(--teggo-border, #e2e8f0);
  border-radius: var(--teggo-radius, 6px);
  transition: border-color 0.15s, box-shadow 0.15s;
}
.stat:hover {
  border-color: var(--p-primary-color, #16a34a);
  box-shadow: 0 1px 6px rgba(15, 23, 42, 0.06);
}
.stat-ic {
  flex-shrink: 0;
  width: 44px;
  height: 44px;
  display: flex;
  align-items: center;
  justify-content: center;
  border-radius: var(--teggo-radius, 6px);
  background: color-mix(in srgb, var(--p-primary-color, #16a34a) 12%, transparent);
  color: var(--p-primary-color, #16a34a);
  font-size: 1.2rem;
}
.stat-main {
  display: flex;
  flex-direction: column;
  min-width: 0;
}
.stat-val {
  font-size: 1.6rem;
  font-weight: 700;
  line-height: 1.15;
  color: var(--p-text-color, #0f172a);
  overflow-wrap: anywhere;
}
.stat-lbl {
  font-size: 0.85rem;
  font-weight: 600;
  color: var(--p-text-color, #334155);
  margin-top: 0.15rem;
}
.stat-sub {
  font-size: 0.72rem;
  color: var(--p-text-muted-color, #94a3b8);
}

/* Panel grid */
.grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 1rem;
  align-items: start;
}
.panel {
  margin: 0;
}
.span2 {
  grid-column: 1 / -1;
}
@media (max-width: 900px) {
  .grid {
    grid-template-columns: 1fr;
  }
  .span2 {
    grid-column: auto;
  }
}

/* Lists */
.tasklist,
.orderlist,
.aging,
.toplist {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
}
.task {
  display: flex;
  align-items: center;
  gap: 0.7rem;
  padding: 0.65rem 0.25rem;
  border-bottom: 1px solid var(--teggo-border, #f1f5f9);
  cursor: pointer;
}
.task:last-child {
  border-bottom: none;
}
.task-ic {
  flex-shrink: 0;
  width: 30px;
  height: 30px;
  display: flex;
  align-items: center;
  justify-content: center;
  border-radius: 6px;
  font-size: 0.9rem;
}
.sev-info { background: color-mix(in srgb, var(--p-blue-500, #3b82f6) 14%, transparent); color: var(--p-blue-500, #3b82f6); }
.sev-warn { background: color-mix(in srgb, var(--p-amber-500, #f59e0b) 16%, transparent); color: var(--p-amber-600, #d97706); }
.sev-danger { background: color-mix(in srgb, var(--p-red-500, #ef4444) 14%, transparent); color: var(--p-red-500, #ef4444); }
.sev-success { background: color-mix(in srgb, var(--p-green-500, #22c55e) 14%, transparent); color: var(--p-green-600, #16a34a); }
.task-label {
  flex: 1;
  min-width: 0;
  font-size: 0.9rem;
}
.task-go {
  color: var(--p-text-muted-color, #cbd5e1);
  font-size: 0.85rem;
}
.caught-up {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 0.5rem;
  padding: 1.75rem 1rem;
  text-align: center;
  color: var(--p-text-muted-color, #64748b);
  font-size: 0.88rem;
}
.caught-up .pi {
  font-size: 1.5rem;
  color: var(--p-green-500, #22c55e);
}

/* Top products */
.topitem {
  display: flex;
  align-items: center;
  gap: 0.7rem;
  padding: 0.55rem 0.25rem;
  border-bottom: 1px solid var(--teggo-border, #f1f5f9);
}
.topitem:last-child {
  border-bottom: none;
}
.rank {
  flex-shrink: 0;
  width: 22px;
  height: 22px;
  display: flex;
  align-items: center;
  justify-content: center;
  border-radius: 50%;
  background: var(--teggo-surface-muted, #f1f5f9);
  font-size: 0.75rem;
  font-weight: 700;
  color: var(--p-text-muted-color, #64748b);
}
.topname {
  flex: 1;
  min-width: 0;
  font-size: 0.88rem;
  display: flex;
  flex-direction: column;
}
.topsku {
  font-size: 0.72rem;
  color: var(--p-text-muted-color, #94a3b8);
  font-family: ui-monospace, monospace;
}
.toprev {
  font-weight: 600;
  font-size: 0.88rem;
}

/* Recent orders */
.order {
  display: flex;
  align-items: center;
  gap: 0.7rem;
  padding: 0.6rem 0.25rem;
  border-bottom: 1px solid var(--teggo-border, #f1f5f9);
  cursor: pointer;
}
.order:last-child {
  border-bottom: none;
}
.order-ref {
  font-family: ui-monospace, monospace;
  font-size: 0.82rem;
}
.order-total {
  margin-left: auto;
  font-weight: 600;
  font-size: 0.88rem;
}

/* AR aging */
.aging-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 0.5rem 0.25rem;
  border-bottom: 1px solid var(--teggo-border, #f1f5f9);
}
.aging-row:last-child {
  border-bottom: none;
}
.aging-bucket {
  font-size: 0.85rem;
  color: var(--p-text-muted-color, #64748b);
  text-transform: capitalize;
}
.aging-bucket.overdue {
  color: var(--p-red-500, #ef4444);
  font-weight: 600;
}
.aging-amt {
  font-weight: 600;
  font-size: 0.88rem;
}
</style>
