<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useToast } from 'primevue/usetoast'
import DataTable from 'primevue/datatable'
import Column from 'primevue/column'
import Tag from 'primevue/tag'
import Button from 'primevue/button'
import Dialog from 'primevue/dialog'
import Message from 'primevue/message'
import { api, errMessage } from '@/lib/client'
import type { components } from '@teggo/api/schema'

type ReturnSummary = components['schemas']['ReturnSummary']
type ReturnDetail = components['schemas']['ReturnDetail']

const returns = ref<ReturnSummary[]>([])
const loading = ref(false)
const error = ref('')
const toast = useToast()

const detail = ref<ReturnDetail | null>(null)
const detailOpen = ref(false)
const busy = ref(false)

function sev(s: string) {
  return s === 'received' || s === 'closed' ? 'success' : s === 'rejected' ? 'danger' : s === 'approved' ? 'info' : 'warn'
}

async function load() {
  loading.value = true
  error.value = ''
  const { data, error: err } = await api.GET('/admin/returns')
  loading.value = false
  if (err || !data) {
    error.value = errMessage(err, 'Failed to load returns')
    return
  }
  returns.value = data.items ?? []
}

async function open(r: ReturnSummary) {
  const { data } = await api.GET('/admin/returns/{id}', { params: { path: { id: r.id } } })
  detail.value = data ?? null
  detailOpen.value = true
}

async function act(verb: 'approve' | 'reject' | 'receive') {
  if (!detail.value) return
  busy.value = true
  const path = `/admin/returns/{id}/${verb}` as '/admin/returns/{id}/approve'
  const { data, error: err } = await api.POST(path, { params: { path: { id: detail.value.id } } })
  busy.value = false
  if (err || !data) {
    toast.add({ severity: 'error', summary: `${verb} failed`, detail: errMessage(err), life: 4000 })
    return
  }
  detail.value = data
  toast.add({ severity: 'success', summary: `Return ${data.status}`, life: 2500 })
  load()
}

onMounted(load)
</script>

<template>
  <div class="page">
    <div class="header">
      <h1>Returns <span class="muted">({{ returns.length }})</span></h1>
      <Button icon="pi pi-refresh" severity="secondary" text @click="load" />
    </div>
    <p class="muted">Return requests move requested → approved → received (restock + credit note), or rejected.</p>

    <Message v-if="error" severity="error" :closable="false" class="mb">{{ error }}</Message>

    <DataTable :value="returns" :loading="loading" dataKey="id" stripedRows @rowClick="open($event.data)" class="clickable">
      <template #empty>No returns.</template>
      <Column header="RMA"><template #body="{ data }">{{ data.public_id.slice(0, 8) }}…</template></Column>
      <Column field="order_id" header="Order" />
      <Column header="Status"><template #body="{ data }"><Tag :value="data.status" :severity="sev(data.status)" /></template></Column>
      <Column field="reason" header="Reason" />
    </DataTable>

    <Dialog v-model:visible="detailOpen" modal :header="detail ? `Return ${detail.public_id.slice(0,8)}…` : 'Return'" :style="{ width: '40rem' }">
      <template v-if="detail">
        <div class="status"><Tag :value="detail.status" :severity="sev(detail.status)" /></div>
        <table class="lines">
          <thead><tr><th>Product</th><th>SKU</th><th>Qty</th><th class="r">Unit</th></tr></thead>
          <tbody>
            <tr v-for="it in detail.items" :key="it.id">
              <td>{{ it.name }}</td><td>{{ it.sku }}</td><td>{{ it.quantity }}</td><td class="r">{{ it.unit_price }}</td>
            </tr>
          </tbody>
        </table>
        <div v-if="detail.credit_notes.length" class="credits">
          <strong>Credit notes</strong>
          <div v-for="cn in detail.credit_notes" :key="cn.id" class="cn">{{ cn.amount }} {{ cn.currency }} <Tag :value="cn.status" severity="success" /></div>
        </div>
      </template>
      <template #footer>
        <template v-if="detail?.status === 'requested'">
          <Button label="Reject" severity="danger" outlined :loading="busy" @click="act('reject')" />
          <Button label="Approve" :loading="busy" @click="act('approve')" />
        </template>
        <Button v-else-if="detail?.status === 'approved'" label="Receive (restock + credit)" icon="pi pi-check" :loading="busy" @click="act('receive')" />
        <Button v-else label="Close" text @click="detailOpen = false" />
      </template>
    </Dialog>
  </div>
</template>

<style scoped>
.header { display: flex; align-items: center; justify-content: space-between; }
.header h1 { margin: 0; }
.muted { color: var(--p-text-muted-color, #64748b); font-weight: 400; }
.mb { margin-bottom: 1rem; }
.clickable :deep(tbody tr) { cursor: pointer; }
.status { margin-bottom: 0.75rem; }
.lines { width: 100%; border-collapse: collapse; }
.lines th { text-align: left; font-size: 0.78rem; color: var(--p-text-muted-color, #64748b); padding: 0.3rem 0.4rem; }
.lines td { padding: 0.3rem 0.4rem; border-top: 1px solid var(--p-surface-100, #f1f5f9); }
.lines .r { text-align: right; }
.credits { margin-top: 1rem; }
.cn { display: flex; gap: 0.5rem; align-items: center; margin-top: 0.3rem; }
</style>
