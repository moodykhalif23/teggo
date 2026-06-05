<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useToast } from 'primevue/usetoast'
import Card from 'primevue/card'
import Tabs from 'primevue/tabs'
import TabList from 'primevue/tablist'
import Tab from 'primevue/tab'
import TabPanels from 'primevue/tabpanels'
import TabPanel from 'primevue/tabpanel'
import DataTable from 'primevue/datatable'
import Column from 'primevue/column'
import Tag from 'primevue/tag'
import Button from 'primevue/button'
import Select from 'primevue/select'
import InputText from 'primevue/inputtext'
import Dialog from 'primevue/dialog'
import Message from 'primevue/message'
import { api, errMessage } from '@/lib/client'
import type { components } from '@teggo/api/schema'

type Order = components['schemas']['OrderDetail']
type Shipment = components['schemas']['Shipment']
type Invoice = components['schemas']['InvoiceSummary']
type ShipmentStatus = components['schemas']['ShipmentStatusPatch']['status']

const ORDER_TRANSITIONS: Record<string, string[]> = {
  pending: ['confirmed', 'on_hold', 'cancelled'],
  confirmed: ['processing', 'on_hold', 'cancelled'],
  processing: ['shipped', 'on_hold', 'cancelled'],
  shipped: ['delivered'],
  delivered: ['closed'],
  on_hold: ['confirmed', 'cancelled'],
}
const SHIPMENT_TRANSITIONS: Record<string, ShipmentStatus[]> = {
  pending: ['shipped', 'returned'],
  shipped: ['delivered', 'returned'],
  delivered: ['returned'],
}

const route = useRoute()
const router = useRouter()
const toast = useToast()
const id = Number(route.params.id)

const order = ref<Order | null>(null)
const shipments = ref<Shipment[]>([])
const invoices = ref<Invoice[]>([])
const error = ref('')

const nextStatus = ref<string | null>(null)
const note = ref('')
const applying = ref(false)
const nextOptions = computed(() => (order.value ? ORDER_TRANSITIONS[order.value.status] ?? [] : []))

async function load() {
  error.value = ''
  const [o, s, i] = await Promise.all([
    api.GET('/admin/orders/{id}', { params: { path: { id } } }),
    api.GET('/admin/orders/{id}/shipments', { params: { path: { id } } }),
    api.GET('/admin/orders/{id}/invoices', { params: { path: { id } } }),
  ])
  if (o.error || !o.data) {
    error.value = errMessage(o.error, 'Order not found')
    return
  }
  order.value = o.data
  shipments.value = s.data?.items ?? []
  invoices.value = i.data?.items ?? []
}

async function applyStatus() {
  if (!nextStatus.value) return
  applying.value = true
  const { data, error: err } = await api.PATCH('/admin/orders/{id}/status', {
    params: { path: { id } },
    body: { status: nextStatus.value, note: note.value || null },
  })
  applying.value = false
  if (err || !data) {
    toast.add({ severity: 'error', summary: 'Transition failed', detail: errMessage(err), life: 4000 })
    return
  }
  order.value = data
  nextStatus.value = null
  note.value = ''
  toast.add({ severity: 'success', summary: `Status → ${data.status}`, life: 2500 })
}

const issuing = ref(false)
async function issueInvoice() {
  issuing.value = true
  const { data, error: err } = await api.POST('/admin/orders/{id}/invoices', { params: { path: { id } } })
  issuing.value = false
  if (err || !data) {
    toast.add({ severity: 'error', summary: 'Issue failed', detail: errMessage(err), life: 4000 })
    return
  }
  toast.add({ severity: 'success', summary: 'Invoice issued', life: 2500 })
  load()
}

const shipDialog = ref(false)
const savingShip = ref(false)
const warehouses = ref<{ id: number; name: string }[]>([])
const shipForm = reactive({ carrier: '', tracking_number: '', warehouse_id: null as number | null, items: [] as { order_item_id: number; quantity: string }[] })
async function loadWarehouses() {
  const { data } = await api.GET('/admin/warehouses')
  warehouses.value = (data?.items ?? []).map((w) => ({ id: w.id, name: w.name }))
}
function openShip() {
  shipForm.carrier = ''
  shipForm.tracking_number = ''
  shipForm.warehouse_id = null
  shipForm.items = (order.value?.items ?? []).map((it) => ({ order_item_id: it.id, quantity: it.quantity }))
  loadWarehouses()
  shipDialog.value = true
}
async function saveShip() {
  savingShip.value = true
  const { error: err } = await api.POST('/admin/orders/{id}/shipments', {
    params: { path: { id } },
    body: {
      carrier: shipForm.carrier || null,
      tracking_number: shipForm.tracking_number || null,
      warehouse_id: shipForm.warehouse_id,
      items: shipForm.items.filter((i) => Number(i.quantity) > 0),
    },
  })
  savingShip.value = false
  if (err) {
    toast.add({ severity: 'error', summary: 'Shipment failed', detail: errMessage(err), life: 4000 })
    return
  }
  shipDialog.value = false
  toast.add({ severity: 'success', summary: 'Shipment created', life: 2500 })
  load()
}
async function shipStatus(s: Shipment, status: ShipmentStatus) {
  const { error: err } = await api.PATCH('/admin/shipments/{id}/status', { params: { path: { id: s.id } }, body: { status } })
  if (err) {
    toast.add({ severity: 'error', summary: 'Failed', detail: errMessage(err), life: 4000 })
    return
  }
  load()
}
function shipNext(s: Shipment): ShipmentStatus[] {
  return SHIPMENT_TRANSITIONS[s.status] ?? []
}

function orderSev(s: string) {
  if (s === 'cancelled') return 'danger'
  if (s === 'delivered' || s === 'closed') return 'success'
  if (s === 'pending' || s === 'on_hold') return 'warn'
  return 'info'
}
function invSev(s: string) {
  return s === 'paid' ? 'success' : s === 'void' ? 'secondary' : s === 'overdue' ? 'danger' : 'info'
}

onMounted(load)
</script>

<template>
  <div class="page">
    <Button icon="pi pi-arrow-left" label="Orders" text severity="secondary" @click="router.push({ name: 'orders' })" />
    <Message v-if="error" severity="error" :closable="false" class="mb">{{ error }}</Message>

    <template v-if="order">
      <div class="head">
        <h1>Order <span class="muted">{{ order.public_id.slice(0, 8) }}…</span> <Tag :value="order.status" :severity="orderSev(order.status)" /></h1>
        <div class="total">{{ order.grand_total }} {{ order.currency }}</div>
      </div>

      <Card v-if="nextOptions.length" class="statuscard">
        <template #content>
          <div class="statusform">
            <span class="lbl">Change status:</span>
            <Select v-model="nextStatus" :options="nextOptions" placeholder="Next status" />
            <InputText v-model="note" placeholder="Note (optional)" class="grow" />
            <Button label="Apply" icon="pi pi-check" :disabled="!nextStatus" :loading="applying" @click="applyStatus" />
          </div>
        </template>
      </Card>

      <Tabs value="items" class="tabs">
        <TabList>
          <Tab value="items">Items</Tab>
          <Tab value="shipments">Shipments ({{ shipments.length }})</Tab>
          <Tab value="invoices">Invoices ({{ invoices.length }})</Tab>
        </TabList>
        <TabPanels>
          <TabPanel value="items">
            <DataTable :value="order.items" dataKey="id" stripedRows>
              <Column field="sku" header="SKU" />
              <Column field="name" header="Product" />
              <Column field="quantity" header="Qty" />
              <Column field="unit_price" header="Unit price" />
              <Column field="row_total" header="Row total" />
            </DataTable>
          </TabPanel>

          <TabPanel value="shipments">
            <div class="tabhead"><Button icon="pi pi-plus" label="Create shipment" size="small" @click="openShip" /></div>
            <DataTable :value="shipments" dataKey="id" stripedRows>
              <template #empty>No shipments.</template>
              <Column header="Status"><template #body="{ data }"><Tag :value="data.status" /></template></Column>
              <Column field="carrier" header="Carrier" />
              <Column field="tracking_number" header="Tracking" />
              <Column header="">
                <template #body="{ data }">
                  <Button v-for="ns in shipNext(data)" :key="ns" :label="ns" size="small" text @click="shipStatus(data, ns)" />
                </template>
              </Column>
            </DataTable>
          </TabPanel>

          <TabPanel value="invoices">
            <div class="tabhead"><Button icon="pi pi-file" label="Issue invoice" size="small" :loading="issuing" @click="issueInvoice" /></div>
            <DataTable
              :value="invoices"
              dataKey="id"
              stripedRows
              @rowClick="router.push({ name: 'invoice-detail', params: { id: $event.data.id } })"
              class="clickable"
            >
              <template #empty>No invoices issued.</template>
              <Column header="Status"><template #body="{ data }"><Tag :value="data.status" :severity="invSev(data.status)" /></template></Column>
              <Column field="grand_total" header="Total" />
              <Column field="currency" header="Ccy" />
              <Column header="PDF">
                <template #body="{ data }"><i v-if="data.pdf_url" class="pi pi-file-pdf" /><span v-else class="muted">—</span></template>
              </Column>
            </DataTable>
          </TabPanel>
        </TabPanels>
      </Tabs>
    </template>

    <Dialog v-model:visible="shipDialog" header="Create shipment" modal :style="{ width: '520px' }">
      <div class="form">
        <div class="grid2">
          <div class="field"><label>Carrier</label><InputText v-model="shipForm.carrier" fluid /></div>
          <div class="field"><label>Tracking #</label><InputText v-model="shipForm.tracking_number" fluid /></div>
          <div class="field">
            <label>Ship from warehouse</label>
            <Select v-model="shipForm.warehouse_id" :options="warehouses" optionLabel="name" optionValue="id" placeholder="Default warehouse" showClear fluid />
          </div>
        </div>
        <p class="hint">Quantities to ship (capped at ordered minus already shipped):</p>
        <div v-for="(it, idx) in shipForm.items" :key="it.order_item_id" class="shipline">
          <span class="sline-name">{{ order?.items[idx]?.name }}</span>
          <InputText v-model="it.quantity" class="sm" />
        </div>
      </div>
      <template #footer>
        <Button label="Cancel" severity="secondary" text @click="shipDialog = false" />
        <Button label="Create" :loading="savingShip" @click="saveShip" />
      </template>
    </Dialog>
  </div>
</template>

<style scoped>
.head { display: flex; align-items: center; justify-content: space-between; margin: 0.5rem 0 1rem; }
.head h1 { margin: 0; display: flex; align-items: center; gap: 0.6rem; font-size: 1.4rem; }
.muted { color: var(--p-text-muted-color, #64748b); font-weight: 400; font-size: 1rem; }
.total { font-size: 1.3rem; font-weight: 700; font-variant-numeric: tabular-nums; }
.mb { margin-bottom: 1rem; }
.statuscard { margin-bottom: 1rem; }
.statusform { display: flex; align-items: center; gap: 0.75rem; flex-wrap: wrap; }
.lbl { font-weight: 600; }
.grow { flex: 1; min-width: 12rem; }
.tabhead { display: flex; justify-content: flex-end; margin-bottom: 0.75rem; }
.clickable :deep(tbody tr) { cursor: pointer; }
.form { display: flex; flex-direction: column; gap: 0.9rem; }
.grid2 { display: grid; grid-template-columns: 1fr 1fr; gap: 0.9rem; }
.field { display: flex; flex-direction: column; gap: 0.3rem; }
.field label { font-size: 0.8rem; font-weight: 600; }
.hint { margin: 0.25rem 0 0; font-size: 0.85rem; color: var(--p-text-muted-color, #64748b); }
.shipline { display: flex; align-items: center; justify-content: space-between; gap: 1rem; }
.sm :deep(input), .sm { width: 7rem; }
</style>
