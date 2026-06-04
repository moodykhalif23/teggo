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
  <div class="page dashboard">
    <header class="dash-head">
      <div>
        <h1>Dashboard</h1>
        <p class="sub">Overview of your B2B operations</p>
      </div>
    </header>

    <section class="stats">
      <button
        v-for="k in kpis"
        :key="k.label"
        type="button"
        class="stat"
        @click="router.push({ name: k.route })"
      >
        <span class="stat-ic"><i :class="k.icon" /></span>
        <span class="stat-main">
          <span class="stat-val">{{ k.value }}</span>
          <span class="stat-lbl">{{ k.label }}</span>
        </span>
        <i class="pi pi-angle-right stat-go" />
      </button>
    </section>

    <Card v-if="chartKpis.length" class="panel">
      <template #title>Records by area</template>
      <template #subtitle>Live counts across your workspace</template>
      <template #content>
        <BarChart :labels="chartLabels" :values="chartValues" name="Records" />
      </template>
    </Card>

    <Card class="panel">
      <template #title>Session</template>
      <template #subtitle>Your access in this organization</template>
      <template #content>
        <div class="sess">
          <div class="sess-org">
            <span class="sess-k">Organization</span>
            <span class="sess-v">{{ auth.orgId ?? '—' }}</span>
          </div>
          <div class="sess-perm">
            <div class="sess-perm-head">
              <i class="pi pi-shield" />
              <span>{{ auth.permissions.length }} permissions</span>
            </div>
            <div class="tags">
              <Tag v-for="p in auth.permissions" :key="p" :value="p" severity="secondary" />
              <span v-if="!auth.permissions.length" class="muted">no permissions</span>
            </div>
          </div>
        </div>
      </template>
    </Card>
  </div>
</template>

<style scoped>
.dash-head {
  display: flex;
  align-items: flex-end;
  justify-content: space-between;
  margin-bottom: 1.25rem;
}
.sub {
  margin: 0.25rem 0 0;
  color: var(--p-text-muted-color, #64748b);
  font-size: 0.9rem;
}
.muted { color: var(--p-text-muted-color, #64748b); }

/* Stat tiles */
.stats {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(214px, 1fr));
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
  background: #fff;
  border: 1px solid var(--teggo-border, #e2e8f0);
  border-radius: var(--teggo-radius, 6px);
}
.stat-ic {
  flex-shrink: 0;
  width: 44px;
  height: 44px;
  display: flex;
  align-items: center;
  justify-content: center;
  border-radius: var(--teggo-radius, 6px);
  background: var(--p-primary-50, #f0fdf4);
  color: var(--p-primary-color, #16a34a);
  font-size: 1.2rem;
}
.stat-main {
  display: flex;
  flex-direction: column;
  min-width: 0;
}
.stat-val {
  font-size: 1.7rem;
  font-weight: 700;
  line-height: 1.1;
  color: var(--p-text-color, #0f172a);
}
.stat-lbl {
  font-size: 0.8rem;
  color: var(--p-text-muted-color, #64748b);
  margin-top: 0.1rem;
}
.stat-go {
  margin-left: auto;
  color: var(--p-surface-300, #cbd5e1);
  font-size: 0.85rem;
}

/* Panels (cards) */
.panel { margin-bottom: 1rem; }

/* Session */
.sess { display: flex; flex-direction: column; gap: 1rem; }
.sess-org {
  display: flex;
  align-items: baseline;
  gap: 0.6rem;
}
.sess-k {
  font-size: 0.72rem;
  text-transform: uppercase;
  letter-spacing: 0.05em;
  color: var(--p-text-muted-color, #64748b);
  font-weight: 600;
}
.sess-v { font-weight: 700; font-size: 1.05rem; }
.sess-perm-head {
  display: flex;
  align-items: center;
  gap: 0.45rem;
  font-size: 0.8rem;
  color: var(--p-text-muted-color, #64748b);
  margin-bottom: 0.55rem;
}
.tags { display: flex; flex-wrap: wrap; gap: 0.4rem; }
</style>
