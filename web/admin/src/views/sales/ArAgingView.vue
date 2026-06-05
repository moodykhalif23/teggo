<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useToast } from 'primevue/usetoast'
import DataTable from 'primevue/datatable'
import Column from 'primevue/column'
import Tag from 'primevue/tag'
import Button from 'primevue/button'
import Message from 'primevue/message'
import { api, errMessage } from '@/lib/client'
import { useCustomerOptions } from '@/composables/useRecordOptions'
import type { components } from '@teggo/api/schema'

type Report = components['schemas']['ARAgingReport']

const report = ref<Report | null>(null)
const loading = ref(false)
const sweeping = ref(false)
const error = ref('')
const toast = useToast()
const { customers, loadCustomers } = useCustomerOptions()

const order = ['current', '1-30', '31-60', '61-90', '90+']

async function load() {
  loading.value = true
  error.value = ''
  const { data, error: err } = await api.GET('/admin/invoices/aging')
  loading.value = false
  if (err || !data) {
    error.value = errMessage(err, 'Failed to load aging report')
    return
  }
  report.value = data
}

async function sweep() {
  sweeping.value = true
  const { data, error: err } = await api.POST('/admin/invoices/overdue-sweep')
  sweeping.value = false
  if (err || !data) {
    toast.add({ severity: 'error', summary: 'Sweep failed', detail: errMessage(err), life: 4000 })
    return
  }
  toast.add({ severity: 'success', summary: `Marked ${data.marked_overdue} overdue`, detail: 'Dunning notices queued', life: 3000 })
  load()
}

const buckets = computed(() => order.map((b) => ({ label: b, total: report.value?.buckets?.[b] ?? '0' })))
function custName(id: number) {
  return customers.value.find((c) => c.id === id)?.name ?? `#${id}`
}
function sev(bucket: string) {
  return bucket === 'current' ? 'secondary' : bucket === '90+' || bucket === '61-90' ? 'danger' : 'warn'
}

onMounted(() => {
  load()
  loadCustomers()
})
</script>

<template>
  <div class="page">
    <div class="header">
      <h1>AR aging</h1>
      <div class="actions">
        <Button icon="pi pi-refresh" severity="secondary" text @click="load" />
        <Button label="Run overdue sweep" icon="pi pi-flag" :loading="sweeping" @click="sweep" />
      </div>
    </div>
    <p class="muted">Open (issued + overdue) invoices bucketed by days past due. The sweep flips past-due invoices to overdue and queues a dunning notice to each customer.</p>

    <Message v-if="error" severity="error" :closable="false" class="mb">{{ error }}</Message>

    <div v-if="report" class="cards">
      <div v-for="b in buckets" :key="b.label" class="card" :class="{ danger: b.label === '90+' }">
        <div class="card-label">{{ b.label === 'current' ? 'Current' : b.label + ' days' }}</div>
        <div class="card-total">{{ b.total }}</div>
      </div>
      <div class="card total">
        <div class="card-label">Open total</div>
        <div class="card-total">{{ report.open_total }}</div>
      </div>
    </div>

    <DataTable :value="report?.items ?? []" :loading="loading" dataKey="public_id" stripedRows class="mt">
      <template #empty>No open invoices.</template>
      <Column header="Invoice"><template #body="{ data }">{{ data.public_id.slice(0, 8) }}…</template></Column>
      <Column header="Customer"><template #body="{ data }">{{ custName(data.customer_id) }}</template></Column>
      <Column header="Status"><template #body="{ data }"><Tag :value="data.status" :severity="data.status === 'overdue' ? 'danger' : 'info'" /></template></Column>
      <Column header="Amount"><template #body="{ data }">{{ data.grand_total }} {{ data.currency }}</template></Column>
      <Column header="Days overdue"><template #body="{ data }">{{ data.days_overdue > 0 ? data.days_overdue : '—' }}</template></Column>
      <Column header="Bucket"><template #body="{ data }"><Tag :value="data.bucket" :severity="sev(data.bucket)" /></template></Column>
    </DataTable>
  </div>
</template>

<style scoped>
.header { display: flex; align-items: center; justify-content: space-between; }
.header h1 { margin: 0; }
.actions { display: flex; gap: 0.5rem; }
.muted { color: var(--p-text-muted-color, #64748b); font-weight: 400; }
.mb { margin-bottom: 1rem; }
.mt { margin-top: 1rem; }
.cards { display: grid; grid-template-columns: repeat(auto-fit, minmax(8rem, 1fr)); gap: 0.75rem; margin: 1rem 0; }
.card { border: 1px solid var(--p-surface-200, #e2e8f0); border-radius: 8px; padding: 0.75rem 1rem; }
.card.danger { border-color: var(--p-red-300, #fca5a5); }
.card.total { background: var(--p-surface-50, #f8fafc); }
.card-label { font-size: 0.75rem; color: var(--p-text-muted-color, #64748b); }
.card-total { font-size: 1.25rem; font-weight: 700; font-variant-numeric: tabular-nums; }
</style>
