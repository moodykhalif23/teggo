<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useToast } from 'primevue/usetoast'
import { useConfirm } from 'primevue/useconfirm'
import DataTable from 'primevue/datatable'
import Column from 'primevue/column'
import Tag from 'primevue/tag'
import Button from 'primevue/button'
import Dialog from 'primevue/dialog'
import Select from 'primevue/select'
import InputText from 'primevue/inputtext'
import Message from 'primevue/message'
import { reactive } from 'vue'
import { api, errMessage } from '@/lib/client'
import { useProductOptions } from '@/composables/useRecordOptions'
import type { components } from '@teggo/api/schema'
import PageHeader from '@/components/PageHeader.vue'
import EmptyState from '@/components/EmptyState.vue'

type Subscription = components['schemas']['Subscription']

const cadenceOptions = [
  { label: 'Weekly', value: 'weekly' },
  { label: 'Every 2 weeks', value: 'biweekly' },
  { label: 'Monthly', value: 'monthly' },
  { label: 'Quarterly', value: 'quarterly' },
]

const rows = ref<Subscription[]>([])
const loading = ref(false)
const error = ref('')
const toast = useToast()
const confirm = useConfirm()

const detail = ref<Subscription | null>(null)
const detailOpen = ref(false)

// ---- edit ----
const { productOptions, productsLoaded, loadProducts } = useProductOptions()
const editOpen = ref(false)
const editSaving = ref(false)
const editErr = ref('')
interface EditLine { product_id: number | null; quantity: string; unit: string }
const editForm = reactive<{ id: number; name: string; cadence: string; lines: EditLine[] }>({
  id: 0, name: '', cadence: 'monthly', lines: [],
})

async function openEdit(s: Subscription) {
  loadProducts()
  editErr.value = ''
  const { data } = await api.GET('/admin/subscriptions/{id}', { params: { path: { id: s.id } } })
  const full = data ?? s
  editForm.id = full.id
  editForm.name = full.name ?? ''
  editForm.cadence = full.cadence
  editForm.lines = (full.items ?? []).map((it) => ({ product_id: it.product_id, quantity: it.quantity, unit: it.unit }))
  if (!editForm.lines.length) editForm.lines.push({ product_id: null, quantity: '1', unit: 'each' })
  editOpen.value = true
}
function addEditLine() {
  editForm.lines.push({ product_id: null, quantity: '1', unit: 'each' })
}
function removeEditLine(i: number) {
  editForm.lines.splice(i, 1)
  if (!editForm.lines.length) addEditLine()
}
async function saveEdit() {
  const items = editForm.lines
    .filter((l) => l.product_id)
    .map((l) => ({ product_id: l.product_id as number, quantity: l.quantity || '1', unit: l.unit || 'each' }))
  if (!items.length) {
    editErr.value = 'At least one product is required.'
    return
  }
  editSaving.value = true
  const { error: err } = await api.PUT('/admin/subscriptions/{id}', {
    params: { path: { id: editForm.id } },
    body: { name: editForm.name || null, cadence: editForm.cadence as 'weekly' | 'biweekly' | 'monthly' | 'quarterly', items },
  })
  editSaving.value = false
  if (err) {
    editErr.value = errMessage(err, 'Save failed')
    return
  }
  editOpen.value = false
  load()
}

async function load() {
  loading.value = true
  error.value = ''
  const { data, error: err } = await api.GET('/admin/subscriptions')
  loading.value = false
  if (err || !data) {
    error.value = errMessage(err, 'Failed to load subscriptions')
    return
  }
  rows.value = data.items ?? []
}

function statusSeverity(s: string) {
  return s === 'active' ? 'success' : s === 'paused' ? 'warn' : 'secondary'
}

async function openDetail(s: Subscription) {
  const { data } = await api.GET('/admin/subscriptions/{id}', { params: { path: { id: s.id } } })
  detail.value = data ?? s
  detailOpen.value = true
}

async function setStatus(s: Subscription, status: 'active' | 'paused' | 'cancelled') {
  const { error: err } = await api.POST('/admin/subscriptions/{id}/status', {
    params: { path: { id: s.id } },
    body: { status },
  })
  if (err) {
    toast.add({ severity: 'error', summary: errMessage(err, 'Update failed'), life: 4000 })
    return
  }
  load()
}

function confirmCancel(s: Subscription) {
  confirm.require({
    message: 'Cancel this subscription? It will stop creating orders.',
    header: 'Cancel subscription',
    icon: 'pi pi-exclamation-triangle',
    rejectProps: { label: 'Keep', severity: 'secondary', outlined: true },
    acceptProps: { label: 'Cancel subscription', severity: 'danger' },
    accept: () => setStatus(s, 'cancelled'),
  })
}

async function runNow(s: Subscription) {
  const { data, error: err } = await api.POST('/admin/subscriptions/{id}/run', { params: { path: { id: s.id } } })
  if (err) {
    toast.add({ severity: 'error', summary: errMessage(err, 'Run failed'), life: 4000 })
    return
  }
  toast.add({
    severity: data?.order_created ? 'success' : 'warn',
    summary: data?.order_created ? 'Order created' : 'No order created',
    detail: data?.order_created ? undefined : 'No current price for the subscription items.',
    life: 3500,
  })
  load()
}

onMounted(load)
</script>

<template>
  <div class="page">
    <PageHeader title="Subscriptions" :meta="rows.length">
      <template #actions>
        <Button icon="pi pi-refresh" severity="secondary" text @click="load" />
      </template>
    </PageHeader>
    <p class="muted mb">
      Recurring &amp; standing orders. A daily job turns due subscriptions into orders, priced from
      the customer's current price list. Buyers can pause, skip, or cancel from their account.
    </p>

    <Message v-if="error" severity="error" :closable="false" class="mb">{{ error }}</Message>

    <DataTable :value="rows" :loading="loading" dataKey="id" stripedRows paginator :rows="15">
      <template #empty>
        <EmptyState icon="pi pi-sync" title="No subscriptions yet" message="When a customer sets up a recurring order, it appears here." />
      </template>
      <Column field="id" header="ID" style="width: 4rem" />
      <Column header="Name"><template #body="{ data }">{{ data.name || '—' }}</template></Column>
      <Column field="customer_id" header="Customer" />
      <Column field="cadence" header="Cadence" />
      <Column field="next_run_date" header="Next run" />
      <Column header="Status">
        <template #body="{ data }"><Tag :value="data.status" :severity="statusSeverity(data.status)" /></template>
      </Column>
      <Column header="" style="width: 12rem">
        <template #body="{ data }">
          <Button icon="pi pi-eye" severity="secondary" text rounded @click="openDetail(data)" />
          <Button v-if="data.status !== 'cancelled'" icon="pi pi-pencil" severity="secondary" text rounded title="Edit" @click="openEdit(data)" />
          <Button v-if="data.status === 'active'" icon="pi pi-play" severity="secondary" text rounded title="Run now" @click="runNow(data)" />
          <Button v-if="data.status === 'active'" icon="pi pi-pause" severity="secondary" text rounded title="Pause" @click="setStatus(data, 'paused')" />
          <Button v-if="data.status === 'paused'" icon="pi pi-play-circle" severity="secondary" text rounded title="Resume" @click="setStatus(data, 'active')" />
          <Button v-if="data.status !== 'cancelled'" icon="pi pi-times" severity="danger" text rounded title="Cancel" @click="confirmCancel(data)" />
        </template>
      </Column>
    </DataTable>

    <Dialog v-model:visible="detailOpen" modal header="Subscription" :style="{ width: '42rem' }">
      <template v-if="detail">
        <div class="meta mb">
          <span><strong>{{ detail.name || `Subscription #${detail.id}` }}</strong></span>
          <Tag :value="detail.status" :severity="statusSeverity(detail.status)" />
          <span class="muted">{{ detail.cadence }} · next {{ detail.next_run_date }} · {{ detail.currency }}</span>
        </div>

        <h4>Items</h4>
        <DataTable :value="detail.items ?? []" dataKey="id" class="mb" stripedRows>
          <template #empty>No items.</template>
          <Column field="sku" header="SKU" />
          <Column field="name" header="Product" />
          <Column field="quantity" header="Qty" />
          <Column field="unit" header="Unit" />
        </DataTable>

        <h4>Recent runs</h4>
        <DataTable :value="detail.runs ?? []" dataKey="id" stripedRows>
          <template #empty>No runs yet.</template>
          <Column field="run_date" header="Date" />
          <Column header="Result">
            <template #body="{ data }">
              <Tag :value="data.status" :severity="data.status === 'success' ? 'success' : data.status === 'failed' ? 'danger' : 'secondary'" />
            </template>
          </Column>
          <Column field="order_id" header="Order">
            <template #body="{ data }">{{ data.order_id ?? '—' }}</template>
          </Column>
          <Column field="note" header="Note"><template #body="{ data }">{{ data.note ?? '' }}</template></Column>
        </DataTable>
      </template>
      <template #footer>
        <Button label="Close" severity="secondary" text @click="detailOpen = false" />
      </template>
    </Dialog>

    <!-- Edit cadence + items -->
    <Dialog v-model:visible="editOpen" modal header="Edit subscription" :style="{ width: '40rem' }">
      <Message v-if="editErr" severity="error" :closable="false" class="mb">{{ editErr }}</Message>
      <div class="grid2 mb">
        <div class="field"><label>Name</label><InputText v-model="editForm.name" fluid /></div>
        <div class="field"><label>Cadence</label><Select v-model="editForm.cadence" :options="cadenceOptions" optionLabel="label" optionValue="value" fluid /></div>
      </div>
      <div class="lines-head">
        <label>Items</label>
        <Button label="Add line" icon="pi pi-plus" size="small" text @click="addEditLine" />
      </div>
      <table class="lines">
        <thead><tr><th>Product</th><th class="num">Qty</th><th></th></tr></thead>
        <tbody>
          <tr v-for="(l, i) in editForm.lines" :key="i">
            <td>
              <Select v-model="l.product_id" :options="productOptions" optionLabel="label" optionValue="id" filter
                filterPlaceholder="Search…" placeholder="Select a product"
                :emptyMessage="productsLoaded ? 'No products' : 'Loading…'" showClear fluid />
            </td>
            <td class="num"><InputText v-model="l.quantity" class="sm" /></td>
            <td><Button icon="pi pi-times" text rounded severity="danger" :disabled="editForm.lines.length === 1" @click="removeEditLine(i)" /></td>
          </tr>
        </tbody>
      </table>
      <template #footer>
        <Button label="Cancel" severity="secondary" text @click="editOpen = false" />
        <Button label="Save" :loading="editSaving" @click="saveEdit" />
      </template>
    </Dialog>
  </div>
</template>

<style scoped>
.mb { margin-bottom: 1rem; }
.muted { color: var(--p-text-muted-color, #64748b); font-weight: 400; }
.meta { display: flex; align-items: center; gap: 0.75rem; flex-wrap: wrap; }
h4 { margin: 0.5rem 0 0.5rem; font-size: 0.9rem; }
.grid2 { display: grid; grid-template-columns: 1fr 1fr; gap: 1rem; }
.field { display: flex; flex-direction: column; gap: 0.35rem; }
.field label { font-size: 0.85rem; font-weight: 600; }
.lines-head { display: flex; align-items: center; justify-content: space-between; }
.lines-head label { font-size: 0.85rem; font-weight: 600; }
.lines { width: 100%; border-collapse: collapse; }
.lines th { text-align: left; font-size: 0.72rem; font-weight: 600; text-transform: uppercase; color: var(--p-text-muted-color, #64748b); padding: 0.25rem 0.4rem; }
.lines th.num, .lines td.num { text-align: right; }
.lines td { padding: 0.25rem 0.4rem; vertical-align: middle; }
.lines :deep(.sm) { width: 6rem; text-align: right; }
</style>
