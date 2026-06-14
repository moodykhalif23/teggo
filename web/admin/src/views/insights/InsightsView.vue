<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import Card from 'primevue/card'
import Button from 'primevue/button'
import Message from 'primevue/message'
import Tag from 'primevue/tag'
import { useToast } from 'primevue/usetoast'
import { api, errMessage } from '@/lib/client'
import type { components } from '@teggo/api/schema'
import PageHeader from '@/components/PageHeader.vue'

type Metrics = components['schemas']['InsightMetrics']
type Digest = components['schemas']['InsightDigest']
type Anomaly = components['schemas']['InsightAnomaly']

const router = useRouter()
const toast = useToast()

const metrics = ref<Metrics | null>(null)
const latest = ref<Digest | null>(null)
const loading = ref(false)
const generating = ref(false)
const error = ref('')

const kpis = computed(() => metrics.value?.kpis)
const anomalies = computed<Anomaly[]>(() => metrics.value?.anomalies ?? [])
// The narrative comes from the last persisted (AI-authored) digest; the KPIs and
// signals are recomputed live so the numbers are never stale to the last run.
const narrative = computed(() => latest.value?.narrative ?? '')
const aiAuthored = computed(() => !!latest.value && latest.value.source !== 'deterministic')

async function loadMetrics() {
  const { data, error: e } = await api.GET('/admin/insights/metrics')
  if (e) {
    error.value = errMessage(e, 'Failed to load metrics')
    return
  }
  metrics.value = data ?? null
}

async function loadLatest() {
  const { data, error: e } = await api.GET('/admin/insights/latest')
  if (e) return // a missing briefing is not an error; the empty state handles it
  latest.value = data?.digest ?? null
}

async function load() {
  loading.value = true
  error.value = ''
  await Promise.all([loadMetrics(), loadLatest()])
  loading.value = false
}

async function generate() {
  generating.value = true
  const { data, error: e } = await api.POST('/admin/insights/generate')
  if (e) {
    toast.add({ severity: 'error', summary: 'Could not generate', detail: errMessage(e), life: 4000 })
    generating.value = false
    return
  }
  if (data?.scheduled) {
    toast.add({ severity: 'info', summary: 'Briefing is generating', detail: 'It will appear here in a moment.', life: 3500 })
    // The worker writes it shortly; pull the fresh briefing once it has run.
    window.setTimeout(async () => {
      await Promise.all([loadLatest(), loadMetrics()])
      generating.value = false
    }, 5000)
    return
  }
  // Inline path (no queue): the digest came back with the response.
  latest.value = data?.digest ?? latest.value
  await loadMetrics()
  generating.value = false
  toast.add({ severity: 'success', summary: 'Briefing updated', life: 2500 })
}

onMounted(load)

// ---- presentation helpers -------------------------------------------------

function sevTag(sev: string): 'danger' | 'warn' | 'info' {
  if (sev === 'critical') return 'danger'
  if (sev === 'warn') return 'warn'
  return 'info'
}
function sevLabel(sev: string): string {
  if (sev === 'critical') return 'Act now'
  if (sev === 'warn') return 'Watch'
  return 'Note'
}

interface Delta { text: string; dir: 'up' | 'down' | 'flat' }
function delta(pct: number | undefined): Delta {
  const p = pct ?? 0
  const text = `${p > 0 ? '+' : ''}${p.toFixed(1)}%`
  return { text, dir: p > 0.05 ? 'up' : p < -0.05 ? 'down' : 'flat' }
}

const marginDir = computed<'up' | 'down' | 'flat'>(() => {
  const d = kpis.value?.margin_delta_pts ?? 0
  return d > 0.05 ? 'up' : d < -0.05 ? 'down' : 'flat'
})

const arNote = computed(() => {
  const over = kpis.value?.ar_90_plus
  if (over && Number(over) > 0) return `${over} over 90 days`
  return 'all current'
})

function goAction(href: string | undefined) {
  if (href) router.push(href)
}
</script>

<template>
  <div class="page">
    <PageHeader title="Briefings" :meta="metrics?.period_label ?? 'this week'">
      <template #actions>
        <Button
          icon="pi pi-sparkles"
          label="Generate now"
          :loading="generating"
          severity="secondary"
          outlined
          @click="generate"
        />
      </template>
    </PageHeader>

    <Message v-if="error" severity="error" :closable="false" class="mb">{{ error }}</Message>

    <!-- The written briefing — the soul of the page. -->
    <Card class="briefing">
      <template #content>
        <div class="briefing-head">
          <span class="briefing-kicker">
            <i class="pi pi-sparkles" /> Executive briefing
          </span>
          <Tag v-if="aiAuthored" value="AI-written" severity="contrast" rounded />
          <Tag v-else-if="latest" value="Auto-summarised" severity="secondary" rounded />
          <span v-if="latest" class="briefing-when">
            {{ new Date(latest.generated_at).toLocaleDateString() }}
          </span>
        </div>

        <p v-if="narrative" class="briefing-body">{{ narrative }}</p>

        <div v-else class="briefing-empty">
          <i class="pi pi-sparkles empty-spark" />
          <p class="empty-title">No briefing yet</p>
          <p class="empty-sub">
            Generate your first executive briefing — a written read on the week's revenue,
            receivables, churn risk and what to do about it.
          </p>
          <Button label="Generate the first briefing" icon="pi pi-sparkles" :loading="generating" @click="generate" />
        </div>
      </template>
    </Card>

    <!-- Live KPIs (recomputed on load, never stale to the last digest). -->
    <div class="kpis">
      <Card class="kpi"><template #content>
        <div class="kpi-label">Revenue</div>
        <div class="kpi-value">{{ kpis?.revenue ?? '—' }}</div>
        <span class="kpi-delta" :class="delta(kpis?.revenue_delta_pct).dir">
          <i :class="delta(kpis?.revenue_delta_pct).dir === 'down' ? 'pi pi-arrow-down-right' : 'pi pi-arrow-up-right'" />
          {{ delta(kpis?.revenue_delta_pct).text }} vs prior
        </span>
      </template></Card>

      <Card class="kpi"><template #content>
        <div class="kpi-label">Orders</div>
        <div class="kpi-value">{{ kpis?.orders ?? '—' }}</div>
        <span class="kpi-delta" :class="delta(kpis?.orders_delta_pct).dir">
          <i :class="delta(kpis?.orders_delta_pct).dir === 'down' ? 'pi pi-arrow-down-right' : 'pi pi-arrow-up-right'" />
          {{ delta(kpis?.orders_delta_pct).text }} vs prior
        </span>
      </template></Card>

      <Card class="kpi"><template #content>
        <div class="kpi-label">Avg order value</div>
        <div class="kpi-value">{{ kpis?.avg_order_value ?? '—' }}</div>
        <span class="kpi-sub">per order this period</span>
      </template></Card>

      <Card v-if="kpis?.has_cost" class="kpi"><template #content>
        <div class="kpi-label">Gross margin</div>
        <div class="kpi-value">{{ (kpis?.margin_pct ?? 0).toFixed(1) }}%</div>
        <span class="kpi-delta" :class="marginDir">
          <i :class="marginDir === 'down' ? 'pi pi-arrow-down-right' : 'pi pi-arrow-up-right'" />
          {{ (kpis?.margin_delta_pts ?? 0) > 0 ? '+' : '' }}{{ (kpis?.margin_delta_pts ?? 0).toFixed(1) }} pts vs prior
        </span>
      </template></Card>

      <Card class="kpi"><template #content>
        <div class="kpi-label">Open receivables</div>
        <div class="kpi-value">{{ kpis?.open_ar ?? '—' }}</div>
        <span class="kpi-sub" :class="{ warnish: Number(kpis?.ar_90_plus ?? 0) > 0 }">{{ arNote }}</span>
      </template></Card>

      <Card class="kpi"><template #content>
        <div class="kpi-label">New accounts</div>
        <div class="kpi-value">{{ kpis?.new_customers ?? '—' }}</div>
        <span class="kpi-sub">won this period</span>
      </template></Card>
    </div>

    <!-- Signals: what changed and what to do about it. -->
    <section class="signals">
      <h2 class="section-title">Signals</h2>
      <p v-if="!loading && anomalies.length === 0" class="muted no-signals">
        <i class="pi pi-check-circle" /> Nothing needs your attention this period.
      </p>
      <div v-else class="signal-list">
        <Card v-for="a in anomalies" :key="a.key" class="signal" :class="a.severity">
          <template #content>
            <div class="signal-row">
              <div class="signal-main">
                <div class="signal-head">
                  <Tag :value="sevLabel(a.severity)" :severity="sevTag(a.severity)" />
                  <span class="signal-title">{{ a.title }}</span>
                </div>
                <p class="signal-detail">{{ a.detail }}</p>
                <p v-if="a.recommendation" class="signal-rec">
                  <i class="pi pi-arrow-right" /> {{ a.recommendation }}
                </p>
              </div>
              <Button
                v-if="a.action"
                :label="a.action.label"
                icon="pi pi-external-link"
                size="small"
                text
                @click="goAction(a.action.href)"
              />
            </div>
          </template>
        </Card>
      </div>
    </section>
  </div>
</template>

<style scoped>
.mb { margin-bottom: 1rem; }

/* Briefing hero */
.briefing { margin: 1.25rem 0; border: 1px solid var(--p-surface-200, #e2e8f0); }
.briefing-head {
  display: flex; align-items: center; gap: 0.6rem; margin-bottom: 0.75rem;
}
.briefing-kicker {
  display: inline-flex; align-items: center; gap: 0.4rem;
  font-size: 0.78rem; font-weight: 700; letter-spacing: 0.04em; text-transform: uppercase;
  color: var(--p-primary-color, #16a34a);
}
.briefing-when { margin-left: auto; font-size: 0.8rem; color: var(--p-text-muted-color, #94a3b8); }
.briefing-body {
  white-space: pre-line;
  font-size: 1.02rem;
  line-height: 1.7;
  color: var(--p-text-color, #1e293b);
  margin: 0;
}
.briefing-empty { text-align: center; padding: 1.5rem 1rem; }
.empty-spark { font-size: 1.8rem; color: var(--p-primary-color, #16a34a); }
.empty-title { font-weight: 700; font-size: 1.05rem; margin: 0.6rem 0 0.25rem; }
.empty-sub { color: var(--p-text-muted-color, #64748b); max-width: 38rem; margin: 0 auto 1rem; line-height: 1.6; }

/* KPI strip */
.kpis { display: grid; grid-template-columns: repeat(auto-fit, minmax(170px, 1fr)); gap: 1rem; margin-bottom: 1.5rem; }
.kpi-label { color: var(--p-text-muted-color, #64748b); font-size: 0.8rem; }
.kpi-value { font-size: 1.55rem; font-weight: 700; margin: 0.2rem 0; line-height: 1.2; }
.kpi-delta { display: inline-flex; align-items: center; gap: 0.25rem; font-size: 0.8rem; font-weight: 600; }
.kpi-delta.up { color: #16a34a; }
.kpi-delta.down { color: #dc2626; }
.kpi-delta.flat { color: var(--p-text-muted-color, #94a3b8); }
.kpi-sub { font-size: 0.8rem; color: var(--p-text-muted-color, #94a3b8); }
.kpi-sub.warnish { color: #d97706; font-weight: 600; }

/* Signals */
.section-title { font-size: 1rem; font-weight: 700; margin: 0 0 0.75rem; }
.no-signals { display: inline-flex; align-items: center; gap: 0.4rem; }
.signal-list { display: flex; flex-direction: column; gap: 0.75rem; }
.signal { border-left: 3px solid var(--p-surface-300, #cbd5e1); }
.signal.critical { border-left-color: #dc2626; }
.signal.warn { border-left-color: #d97706; }
.signal.info { border-left-color: #2563eb; }
.signal-row { display: flex; align-items: flex-start; gap: 1rem; }
.signal-main { flex: 1; min-width: 0; }
.signal-head { display: flex; align-items: center; gap: 0.6rem; margin-bottom: 0.35rem; }
.signal-title { font-weight: 600; }
.signal-detail { margin: 0; color: var(--p-text-color, #334155); font-size: 0.9rem; }
.signal-rec { margin: 0.4rem 0 0; color: var(--p-text-muted-color, #64748b); font-size: 0.86rem; display: flex; align-items: center; gap: 0.35rem; }
.muted { color: var(--p-text-muted-color, #94a3b8); }
</style>
