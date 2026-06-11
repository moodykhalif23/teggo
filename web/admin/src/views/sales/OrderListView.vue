<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { useToast } from 'primevue/usetoast'
import DataTable from 'primevue/datatable'
import Column from 'primevue/column'
import Tag from 'primevue/tag'
import Button from 'primevue/button'
import Dialog from 'primevue/dialog'
import InputText from 'primevue/inputtext'
import Select from 'primevue/select'
import Message from 'primevue/message'
import { api, errMessage } from '@/lib/client'
import { useCustomerOptions, useProductOptions } from '@/composables/useRecordOptions'
import { useCurrency } from '@/composables/useCurrency'
import type { components } from '@teggo/api/schema'
import PageHeader from '@/components/PageHeader.vue'
import EmptyState from '@/components/EmptyState.vue'

type Order = components['schemas']['OrderSummary']

const router = useRouter()
const route = useRoute()
const toast = useToast()
const rows = ref<Order[]>([])
const loading = ref(false)
const error = ref('')

const dialogOpen = ref(false)
const saving = ref(false)

interface OrderLine {
  product_id: number | null
  quantity: string
  unit_price: string
}
const blankLine = (): OrderLine => ({ product_id: null, quantity: '1', unit_price: '' })
const form = reactive<{ customer_id: number | null; lines: OrderLine[] }>({
  customer_id: null,
  lines: [blankLine()],
})
function addLine() {
  form.lines.push(blankLine())
}
function removeLine(i: number) {
  form.lines.splice(i, 1)
  if (form.lines.length === 0) form.lines.push(blankLine())
}
function lineTotal(l: OrderLine): string {
  const q = Number(l.quantity)
  const p = Number(l.unit_price)
  if (!isFinite(q) || !isFinite(p)) return '—'
  return (q * p).toFixed(2)
}

const { customers, customersLoaded, loadCustomers } = useCustomerOptions()
const { productOptions, productsLoaded, loadProducts } = useProductOptions()
// Currency follows the org default set in Settings; the server stamps it on create.
const { currency } = useCurrency()

async function load() {
  loading.value = true
  error.value = ''
  const { data, error: err } = await api.GET('/admin/orders')
  loading.value = false
  if (err || !data) {
    error.value = errMessage(err, 'Failed to load orders')
    return
  }
  rows.value = data.items ?? []
}

function openCreate() {
  Object.assign(form, { customer_id: null, lines: [blankLine()] })
  dialogOpen.value = true
  loadCustomers()
  loadProducts()
}

async function create() {
  const items = form.lines
    .filter((l) => l.product_id && l.unit_price !== '')
    .map((l) => ({ product_id: l.product_id as number, quantity: l.quantity || '1', unit_price: l.unit_price }))
  if (!form.customer_id || items.length === 0) {
    toast.add({ severity: 'warn', summary: 'A customer and at least one complete line are required', life: 3500 })
    return
  }
  saving.value = true
  const { data, error: err } = await api.POST('/admin/orders', {
    body: {
      // Currency intentionally omitted — the server applies the org default.
      customer_id: form.customer_id,
      items,
    },
  })
  saving.value = false
  if (err || !data) {
    toast.add({ severity: 'error', summary: 'Create failed', detail: errMessage(err), life: 4000 })
    return
  }
  dialogOpen.value = false
  router.push({ name: 'order-detail', params: { id: data.id } })
}

function sev(s: string) {
  if (s === 'cancelled') return 'danger'
  if (s === 'delivered' || s === 'closed') return 'success'
  if (s === 'pending' || s === 'on_hold') return 'warn'
  return 'info'
}

onMounted(load)
// Opened from the dashboard "New order" quick action.
onMounted(() => { if (route.query.new) openCreate() })
</script>

<template>
  <div class="page">
    <PageHeader title="Orders">
      <template #actions>
        <Button icon="pi pi-plus" label="New order (on behalf)" @click="openCreate" />
      </template>
    </PageHeader>
    <Message v-if="error" severity="error" :closable="false" class="mb">{{ error }}</Message>
    <DataTable
      :value="rows"
      :loading="loading"
      dataKey="id"
      stripedRows
      paginator
      :rows="15"
      @rowClick="router.push({ name: 'order-detail', params: { id: $event.data.id } })"
      class="clickable"
    >
      <template #empty>
        <EmptyState icon="pi pi-shopping-cart" title="No orders yet" message="Orders placed by customers appear here. You can also create one on a customer's behalf.">
          <Button icon="pi pi-plus" label="New order (on behalf)" @click="openCreate" />
        </EmptyState>
      </template>
      <Column field="id" header="ID" style="width: 5rem" />
      <Column header="Reference">
        <template #body="{ data }">
          {{ data.public_id.slice(0, 8) }}…
          <Tag v-if="data.placed_by_sales_rep_id" value="rep-placed" severity="info" class="rep-tag" />
        </template>
      </Column>
      <Column header="Status"><template #body="{ data }"><Tag :value="data.status" :severity="sev(data.status)" /></template></Column>
      <Column field="currency" header="Ccy" />
      <Column field="grand_total" header="Total" />
    </DataTable>

    <Dialog v-model:visible="dialogOpen" header="New order (on behalf of customer)" modal :style="{ width: '640px' }">
      <form class="form" @submit.prevent="create">
        <div class="field">
          <label>Customer</label>
          <Select
            v-model="form.customer_id"
            :options="customers"
            optionLabel="name"
            optionValue="id"
            filter
            filterPlaceholder="Search customers…"
            placeholder="Select a customer"
            :emptyMessage="customersLoaded ? 'No customers' : 'Loading…'"
            showClear
            fluid
          />
        </div>

        <div class="lines-head">
          <label>Line items <span v-if="currency" class="ccy">(prices in {{ currency }})</span></label>
          <Button label="Add line" icon="pi pi-plus" size="small" text @click="addLine" />
        </div>
        <table class="lines">
          <thead>
            <tr><th>Product</th><th class="num">Qty</th><th class="num">Unit price</th><th class="num">Total</th><th></th></tr>
          </thead>
          <tbody>
            <tr v-for="(l, i) in form.lines" :key="i">
              <td>
                <Select
                  v-model="l.product_id"
                  :options="productOptions"
                  optionLabel="label"
                  optionValue="id"
                  filter
                  filterPlaceholder="Search…"
                  placeholder="Select a product"
                  :emptyMessage="productsLoaded ? 'No products' : 'Loading…'"
                  showClear
                  fluid
                />
              </td>
              <td class="num"><InputText v-model="l.quantity" class="sm" /></td>
              <td class="num"><InputText v-model="l.unit_price" class="sm" /></td>
              <td class="num total">{{ lineTotal(l) }}</td>
              <td><Button icon="pi pi-times" text rounded severity="danger" :disabled="form.lines.length === 1" @click="removeLine(i)" /></td>
            </tr>
          </tbody>
        </table>
      </form>
      <template #footer>
        <Button label="Cancel" severity="secondary" text @click="dialogOpen = false" />
        <Button label="Create" :loading="saving" @click="create" />
      </template>
    </Dialog>
  </div>
</template>

<style scoped>
.mb { margin-bottom: 1rem; }
.clickable :deep(tbody tr) { cursor: pointer; }
.form { display: flex; flex-direction: column; gap: 0.9rem; }
.field { display: flex; flex-direction: column; gap: 0.3rem; }
.field label { font-size: 0.8rem; font-weight: 600; }
.ccy { font-weight: 400; color: var(--p-text-muted-color, #64748b); }
.lines-head { display: flex; align-items: center; justify-content: space-between; }
.lines-head label { font-size: 0.8rem; font-weight: 600; }
.lines { width: 100%; border-collapse: collapse; }
.lines th { text-align: left; font-size: 0.72rem; font-weight: 600; text-transform: uppercase; color: var(--p-text-muted-color, #64748b); padding: 0.25rem 0.4rem; }
.lines th.num, .lines td.num { text-align: right; }
.lines td { padding: 0.25rem 0.4rem; vertical-align: middle; }
.lines :deep(.sm) { width: 6rem; text-align: right; }
.lines .total { font-variant-numeric: tabular-nums; white-space: nowrap; }
</style>
