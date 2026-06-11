<script setup lang="ts">
import { computed, defineAsyncComponent, onMounted, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import Card from 'primevue/card'
import Tag from 'primevue/tag'
import Button from 'primevue/button'
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
type AccountHealth = components['schemas']['AccountHealth']

const auth = useAuthStore()
const router = useRouter()

const loading = ref(true)
const refreshing = ref(false)
const orgName = ref<string>('')

// Period selector — drives the summary KPIs, deltas and the revenue trend.
// Persisted across sessions.
const PERIOD_KEY = 'teggo.dashboard.period'
const stored = Number(localStorage.getItem(PERIOD_KEY))
const period = ref([7, 30, 90].includes(stored) ? stored : 30)
const periodOptions = [
  { label: '7d', value: 7 },
  { label: '30d', value: 30 },
  { label: '90d', value: 90 },
]
const canReport = computed(() => auth.can('report.view'))

// Period-dependent (refetched on toggle).
const summary = ref<SalesSummary | null>(null)
const salesItems = ref<DailyPoint[]>([])
const prevRevenue = ref<number | null>(null)
const prevOrders = ref<number | null>(null)

// Loaded once.
const arOpen = ref<string | null>(null)
const agingBuckets = ref<{ label: string; amount: string }[]>([])
const customersTotal = ref<number | null>(null)
const productsTotal = ref<number | null>(null)
const top = ref<TopProduct[]>([])
const statusLabels = ref<string[]>([])
const statusValues = ref<number[]>([])
const recentOrders = ref<OrderSummary[]>([])
const atRisk = ref<AccountHealth[]>([])
const pipelineValue = ref<number | null>(null)
const pipelineCount = ref(0)
const pipelineCurrency = ref('')

interface Kpi {
  key: string
  label: string
  sub?: string
  icon: string
  value: number | string
  route: string
  delta?: number | null
}

const pct = (curr: number, prev: number | null) =>
  prev && prev > 0 ? Math.round(((curr - prev) / prev) * 100) : null

const kpis = computed<Kpi[]>(() => {
  const k: Kpi[] = []
  if (summary.value) {
    const curRev = Number(summary.value.revenue)
    const curAov = Number(summary.value.avg_order_value)
    const prevAov = prevOrders.value && prevOrders.value > 0 ? (prevRevenue.value ?? 0) / prevOrders.value : null
    k.push({ key: 'rev', label: 'Revenue', sub: `last ${period.value} days`, icon: 'pi pi-chart-line', value: summary.value.revenue, route: 'analytics', delta: pct(curRev, prevRevenue.value) })
    k.push({ key: 'ord', label: 'Orders', sub: `last ${period.value} days`, icon: 'pi pi-shopping-cart', value: summary.value.order_count, route: 'orders', delta: pct(summary.value.order_count, prevOrders.value) })
    k.push({ key: 'aov', label: 'Avg order value', sub: `last ${period.value} days`, icon: 'pi pi-receipt', value: summary.value.avg_order_value, route: 'analytics', delta: pct(curAov, prevAov) })
  }
  if (arOpen.value !== null) k.push({ key: 'ar', label: 'Open AR', sub: 'outstanding', icon: 'pi pi-wallet', value: arOpen.value, route: 'ar-aging' })
  if (customersTotal.value !== null) k.push({ key: 'cust', label: 'Customers', icon: 'pi pi-building', value: customersTotal.value, route: 'customers' })
  if (productsTotal.value !== null) k.push({ key: 'prod', label: 'Products', icon: 'pi pi-box', value: productsTotal.value, route: 'products' })
  return k
})

const salesLabels = computed(() => salesItems.value.map((d) => d.day.slice(5)))
const salesValues = computed(() => salesItems.value.map((d) => Number(d.revenue)))

interface Task {
  label: string
  icon: string
  count: number
  route: string
  severity: 'info' | 'warn' | 'danger' | 'success'
}
const tasks = ref<Task[]>([])

// Quick-create shortcuts — gated by the matching manage permission. Each lands
// on its list view with ?new=1, which the view reads to open its create dialog.
const quickActions = computed(() =>
  [
    { label: 'New quote', icon: 'pi pi-file-edit', route: 'quotes', perm: 'quote.manage' },
    { label: 'New order', icon: 'pi pi-shopping-cart', route: 'orders', perm: 'order.manage' },
    { label: 'New product', icon: 'pi pi-box', route: 'products', perm: 'product.manage' },
  ].filter((a) => auth.can(a.perm)),
)
function quickCreate(route: string) {
  router.push({ name: route, query: { new: '1' } })
}

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

// Period-dependent reports + previous-window comparison for the deltas.
async function loadReports(days: number) {
  if (!canReport.value) return
  const to = new Date()
  const from = new Date()
  from.setDate(from.getDate() - days)
  const prevFrom = new Date()
  prevFrom.setDate(prevFrom.getDate() - 2 * days)
  const [s, sales, prevSales] = await Promise.all([
    api.GET('/admin/reports/summary', { params: { query: { days } } }),
    api.GET('/admin/reports/sales', { params: { query: { from: ymd(from), to: ymd(to) } } }),
    api.GET('/admin/reports/sales', { params: { query: { from: ymd(prevFrom), to: ymd(from) } } }),
  ])
  summary.value = s.data ?? null
  salesItems.value = sales.data?.items ?? []
  const prev = prevSales.data?.items ?? []
  prevRevenue.value = prev.reduce((a, d) => a + Number(d.revenue), 0)
  prevOrders.value = prev.reduce((a, d) => a + d.order_count, 0)
}

async function load() {
  const can = (p: string) => auth.can(p)

  const [orders, aging, rfqs, quotes, returns, pendingProducts, customers, products, org, topProducts, health, opps] =
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
      can('crm.view') ? api.GET('/admin/accounts/health') : null,
      can('crm.view') ? api.GET('/admin/opportunities') : null,
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

  // At-risk accounts.
  atRisk.value = (health?.data?.items ?? []).filter((a) => a.at_risk).slice(0, 6)

  // Open sales pipeline (opportunities not yet closed).
  if (opps?.data?.items?.length) {
    const open = opps.data.items.filter((o) => !o.closed_at)
    pipelineValue.value = open.reduce((a, o) => a + Number(o.amount), 0)
    pipelineCount.value = open.length
    pipelineCurrency.value = open[0]?.currency ?? ''
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
  if (atRisk.value.length) t.push({ label: 'At-risk accounts', icon: 'pi pi-heart', count: atRisk.value.length, route: 'account-health', severity: 'danger' })
  tasks.value = t

  loading.value = false
}

async function refresh() {
  refreshing.value = true
  await load()
  refreshing.value = false
}

// Re-pull the period-dependent reports when the toggle changes; persist choice.
watch(period, (d) => {
  localStorage.setItem(PERIOD_KEY, String(d))
  loadReports(d)
})

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
      <div class="head-actions">
        <Button
          v-for="a in quickActions"
          :key="a.route"
          :label="a.label"
          :icon="a.icon"
          size="small"
          severity="secondary"
          outlined
          @click="quickCreate(a.route)"
        />
        <Button icon="pi pi-refresh" size="small" severity="secondary" text :loading="refreshing" aria-label="Refresh" @click="refresh" />
        <SelectButton
          v-if="canReport"
          v-model="period"
          :options="periodOptions"
          optionLabel="label"
          optionValue="value"
          :allowEmpty="false"
          aria-label="Reporting period"
        />
      </div>
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
            <span class="stat-lbl">
              {{ k.label }}
              <span
                v-if="k.delta !== undefined && k.delta !== null"
                class="delta"
                :class="k.delta >= 0 ? 'up' : 'down'"
              >
                <i :class="k.delta >= 0 ? 'pi pi-arrow-up' : 'pi pi-arrow-down'" />{{ Math.abs(k.delta) }}%
              </span>
            </span>
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

        <!-- Sales pipeline -->
        <Card v-if="pipelineValue !== null" class="panel">
          <template #title>Sales pipeline</template>
          <template #subtitle>Open opportunities</template>
          <template #content>
            <button type="button" class="pipeline" @click="router.push({ name: 'pipeline' })">
              <span class="pipe-val">{{ pipelineValue.toLocaleString() }} {{ pipelineCurrency }}</span>
              <span class="pipe-lbl">{{ pipelineCount }} open {{ pipelineCount === 1 ? 'opportunity' : 'opportunities' }}</span>
            </button>
          </template>
        </Card>

        <!-- At-risk accounts -->
        <Card v-if="atRisk.length" class="panel">
          <template #title>At-risk accounts</template>
          <template #subtitle>Customers slipping in order cadence</template>
          <template #content>
            <ul class="risklist">
              <li
                v-for="a in atRisk"
                :key="a.customer_id"
                class="risk"
                @click="router.push({ name: 'account-health' })"
              >
                <span class="risk-main">
                  <span class="risk-name">{{ a.name }}</span>
                  <span v-if="a.reason" class="risk-reason">{{ a.reason }}</span>
                </span>
                <Tag :value="`${a.days_since}d`" severity="danger" />
              </li>
            </ul>
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
  flex-wrap: wrap;
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
.head-actions {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  flex-wrap: wrap;
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
  display: inline-flex;
  align-items: center;
  gap: 0.4rem;
}
.delta {
  display: inline-flex;
  align-items: center;
  gap: 0.1rem;
  font-size: 0.72rem;
  font-weight: 700;
}
.delta .pi { font-size: 0.6rem; }
.delta.up { color: var(--p-green-600, #16a34a); }
.delta.down { color: var(--p-red-500, #ef4444); }
.stat-sub {
  font-size: 0.72rem;
  color: var(--p-text-muted-color, #94a3b8);
}

/* Panel grid */
.grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 1rem;
  align-items: stretch;
}
.panel {
  margin: 0;
  height: 100%;
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
.toplist,
.risklist {
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

/* Sales pipeline */
.pipeline {
  display: flex;
  flex-direction: column;
  gap: 0.2rem;
  width: 100%;
  text-align: left;
  border: none;
  background: none;
  cursor: pointer;
  padding: 0.5rem 0;
}
.pipe-val {
  font-size: 1.7rem;
  font-weight: 700;
  color: var(--p-text-color, #0f172a);
}
.pipe-lbl {
  font-size: 0.85rem;
  color: var(--p-text-muted-color, #64748b);
}

/* At-risk accounts */
.risk {
  display: flex;
  align-items: center;
  gap: 0.7rem;
  padding: 0.6rem 0.25rem;
  border-bottom: 1px solid var(--teggo-border, #f1f5f9);
  cursor: pointer;
}
.risk:last-child { border-bottom: none; }
.risk-main { flex: 1; min-width: 0; display: flex; flex-direction: column; }
.risk-name { font-size: 0.9rem; font-weight: 600; }
.risk-reason { font-size: 0.75rem; color: var(--p-text-muted-color, #94a3b8); }

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
