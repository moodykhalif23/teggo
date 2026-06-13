<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import DataTable from 'primevue/datatable'
import Column from 'primevue/column'
import Tag from 'primevue/tag'
import Button from 'primevue/button'
import InputText from 'primevue/inputtext'
import Select from 'primevue/select'
import DatePicker from 'primevue/datepicker'
import Message from 'primevue/message'
import { useAuthStore } from '@/stores/auth'
import { api, errMessage } from '@/lib/client'
import type { components } from '@teggo/api/schema'
import PageHeader from '@/components/PageHeader.vue'

type Entry = components['schemas']['AuditEntry']

const auth = useAuthStore()
const apiBase = import.meta.env.VITE_API_BASE_URL ?? ''

const rows = ref<Entry[]>([])
const total = ref(0)
const loading = ref(false)
const exporting = ref(false)
const error = ref('')
const expanded = ref<Entry[]>([])

const limit = 50
const offset = ref(0)

const action = ref('')
const entityType = ref('')
const audience = ref('')
const from = ref<Date | null>(null)
const to = ref<Date | null>(null)

const audienceOptions = [
  { label: 'All staff', value: '' },
  { label: 'Admin', value: 'admin' },
  { label: 'Vendor', value: 'vendor' },
]

function ymd(d: Date | null): string | undefined {
  if (!d) return undefined
  return d.toISOString().slice(0, 10)
}

// Only set filters are sent; the server treats absent params as "no filter".
function queryParams(): Record<string, string | number> {
  const q: Record<string, string | number> = { limit, offset: offset.value }
  if (action.value) q.action = action.value
  if (entityType.value) q.entity_type = entityType.value
  if (audience.value) q.audience = audience.value
  const f = ymd(from.value)
  const t = ymd(to.value)
  if (f) q.from = f
  if (t) q.to = t
  return q
}

async function load() {
  loading.value = true
  error.value = ''
  const { data, error: e } = await api.GET('/admin/audit', { params: { query: queryParams() } })
  loading.value = false
  if (e) {
    error.value = errMessage(e, 'Failed to load audit log')
    return
  }
  rows.value = data?.items ?? []
  // total is -1 on non-first pages; keep the prior count then.
  if ((data?.total ?? -1) >= 0) total.value = data!.total as number
}

function applyFilters() {
  offset.value = 0
  load()
}

function clearFilters() {
  action.value = ''
  entityType.value = ''
  audience.value = ''
  from.value = null
  to.value = null
  applyFilters()
}

function nextPage() {
  if (offset.value + limit < total.value) {
    offset.value += limit
    load()
  }
}
function prevPage() {
  if (offset.value > 0) {
    offset.value = Math.max(0, offset.value - limit)
    load()
  }
}

const rangeLabel = computed(() => {
  if (total.value === 0) return 'No entries'
  const start = offset.value + 1
  const end = Math.min(offset.value + rows.value.length, offset.value + limit)
  return `${start}–${end} of ${total.value}`
})

async function exportCsv() {
  exporting.value = true
  try {
    const q = new URLSearchParams()
    if (action.value) q.set('action', action.value)
    if (entityType.value) q.set('entity_type', entityType.value)
    if (audience.value) q.set('audience', audience.value)
    const f = ymd(from.value)
    const t = ymd(to.value)
    if (f) q.set('from', f)
    if (t) q.set('to', t)
    const res = await fetch(`${apiBase}/admin/audit/export?${q.toString()}`, {
      headers: { Authorization: `Bearer ${auth.token ?? ''}` },
    })
    if (!res.ok) throw new Error(`Export failed (${res.status})`)
    const blob = await res.blob()
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = 'audit-log.csv'
    document.body.appendChild(a)
    a.click()
    a.remove()
    URL.revokeObjectURL(url)
  } catch (err) {
    error.value = err instanceof Error ? err.message : 'Export failed'
  } finally {
    exporting.value = false
  }
}

onMounted(load)

// ---- presentation ---------------------------------------------------------

function statusSeverity(code: number): 'success' | 'warn' | 'danger' | 'secondary' {
  if (code >= 500) return 'danger'
  if (code >= 400) return 'warn'
  if (code >= 200 && code < 300) return 'success'
  return 'secondary'
}
function fmtTime(s: string): string {
  return new Date(s).toLocaleString()
}
function entityLabel(e: Entry): string {
  if (!e.entity_type) return '—'
  return e.entity_id ? `${e.entity_type} #${e.entity_id}` : e.entity_type
}
function changeOf(e: Entry): { before: unknown; after: unknown } | null {
  const m = e.metadata as Record<string, unknown> | undefined
  const c = m?.change as { before: unknown; after: unknown } | undefined
  return c ?? null
}
function pretty(v: unknown): string {
  if (v === null || v === undefined) return '—'
  return JSON.stringify(v, null, 2)
}
</script>

<template>
  <div class="page">
    <PageHeader title="Audit log" meta="who changed what, when">
      <template #actions>
        <Button icon="pi pi-download" label="Export CSV" severity="secondary" outlined :loading="exporting" @click="exportCsv" />
      </template>
    </PageHeader>

    <Message v-if="error" severity="error" :closable="false" class="mb">{{ error }}</Message>

    <div class="filters">
      <InputText v-model="action" placeholder="Action (e.g. customers.update)" class="f-grow" @keyup.enter="applyFilters" />
      <InputText v-model="entityType" placeholder="Entity type" @keyup.enter="applyFilters" />
      <Select v-model="audience" :options="audienceOptions" option-label="label" option-value="value" placeholder="Audience" />
      <DatePicker v-model="from" placeholder="From" date-format="yy-mm-dd" show-icon />
      <DatePicker v-model="to" placeholder="To" date-format="yy-mm-dd" show-icon />
      <Button label="Apply" icon="pi pi-filter" @click="applyFilters" />
      <Button label="Clear" icon="pi pi-times" severity="secondary" text @click="clearFilters" />
    </div>

    <DataTable
      :value="rows"
      v-model:expandedRows="expanded"
      data-key="id"
      :loading="loading"
      size="small"
      class="audit-table"
    >
      <template #empty>
        <p class="muted">No audit entries match these filters.</p>
      </template>

      <Column expander style="width: 2.5rem" />
      <Column header="When">
        <template #body="{ data }">{{ fmtTime(data.created_at) }}</template>
      </Column>
      <Column header="Actor">
        <template #body="{ data }">
          <span v-if="data.actor_user_id">user #{{ data.actor_user_id }}</span>
          <span v-else class="muted">—</span>
          <span class="aud">{{ data.actor_audience }}</span>
        </template>
      </Column>
      <Column field="action" header="Action">
        <template #body="{ data }"><code class="action">{{ data.action }}</code></template>
      </Column>
      <Column header="Entity">
        <template #body="{ data }">{{ entityLabel(data) }}</template>
      </Column>
      <Column header="Result">
        <template #body="{ data }">
          <span class="method">{{ data.method }}</span>
          <Tag :value="String(data.status_code)" :severity="statusSeverity(data.status_code)" />
        </template>
      </Column>
      <Column field="ip" header="IP">
        <template #body="{ data }"><span class="muted">{{ data.ip || '—' }}</span></template>
      </Column>

      <template #expansion="{ data }">
        <div class="detail">
          <div class="detail-meta">
            <span><strong>Path:</strong> <code>{{ data.path }}</code></span>
            <span v-if="data.summary"><strong>Summary:</strong> {{ data.summary }}</span>
            <span v-if="data.request_id"><strong>Request:</strong> {{ data.request_id }}</span>
            <span v-if="data.user_agent"><strong>Agent:</strong> {{ data.user_agent }}</span>
          </div>
          <div v-if="changeOf(data)" class="diff">
            <div class="diff-col">
              <div class="diff-head">Before</div>
              <pre>{{ pretty(changeOf(data)!.before) }}</pre>
            </div>
            <div class="diff-col">
              <div class="diff-head">After</div>
              <pre>{{ pretty(changeOf(data)!.after) }}</pre>
            </div>
          </div>
          <p v-else class="muted">No field-level change was recorded for this action.</p>
        </div>
      </template>
    </DataTable>

    <div class="pager">
      <span class="muted">{{ rangeLabel }}</span>
      <span class="spacer" />
      <Button icon="pi pi-chevron-left" severity="secondary" text :disabled="offset === 0" @click="prevPage" />
      <Button icon="pi pi-chevron-right" severity="secondary" text :disabled="offset + limit >= total" @click="nextPage" />
    </div>
  </div>
</template>

<style scoped>
.mb { margin-bottom: 1rem; }
.filters {
  display: flex; flex-wrap: wrap; gap: 0.6rem; align-items: center; margin: 1rem 0;
}
.f-grow { min-width: 16rem; flex: 1; }
.audit-table { margin-top: 0.5rem; }
.action { font-size: 0.82rem; background: var(--p-surface-100, #f1f5f9); padding: 0.1rem 0.4rem; border-radius: 5px; }
.aud { margin-left: 0.4rem; font-size: 0.72rem; color: var(--p-text-muted-color, #94a3b8); text-transform: uppercase; }
.method { font-weight: 600; margin-right: 0.5rem; font-size: 0.8rem; }
.muted { color: var(--p-text-muted-color, #94a3b8); }
.detail { padding: 0.5rem 0.25rem; }
.detail-meta { display: flex; flex-wrap: wrap; gap: 1.2rem; font-size: 0.85rem; margin-bottom: 0.75rem; }
.detail-meta code { font-size: 0.82rem; }
.diff { display: grid; grid-template-columns: 1fr 1fr; gap: 1rem; }
.diff-head { font-size: 0.75rem; font-weight: 700; text-transform: uppercase; color: var(--p-text-muted-color, #64748b); margin-bottom: 0.3rem; }
.diff pre {
  margin: 0; padding: 0.6rem; background: var(--p-surface-50, #f8fafc); border: 1px solid var(--p-surface-200, #e2e8f0);
  border-radius: 6px; font-size: 0.78rem; line-height: 1.5; overflow-x: auto; white-space: pre-wrap; word-break: break-word;
}
.pager { display: flex; align-items: center; gap: 0.3rem; margin-top: 0.75rem; }
.spacer { flex: 1; }
</style>
