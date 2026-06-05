<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
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
import type { components } from '@teggo/api/schema'

type Order = components['schemas']['OrderSummary']

const router = useRouter()
const toast = useToast()
const rows = ref<Order[]>([])
const loading = ref(false)
const error = ref('')

const dialogOpen = ref(false)
const saving = ref(false)
const form = reactive({ customer_id: null as number | null, currency: 'USD', product_id: null as number | null, quantity: '1', unit_price: '' })

const { customers, customersLoaded, loadCustomers } = useCustomerOptions()
const { productOptions, productsLoaded, loadProducts } = useProductOptions()

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
  Object.assign(form, { customer_id: null, currency: 'USD', product_id: null, quantity: '1', unit_price: '' })
  dialogOpen.value = true
  loadCustomers()
  loadProducts()
}

async function create() {
  if (!form.customer_id || !form.product_id || !form.unit_price) {
    toast.add({ severity: 'warn', summary: 'customer, product, and unit price required', life: 3000 })
    return
  }
  saving.value = true
  const { data, error: err } = await api.POST('/admin/orders', {
    body: {
      customer_id: form.customer_id,
      currency: form.currency,
      items: [{ product_id: form.product_id, quantity: form.quantity, unit_price: form.unit_price }],
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
</script>

<template>
  <div class="page">
    <div class="header">
      <h1>Orders</h1>
      <Button icon="pi pi-plus" label="New order (on behalf)" @click="openCreate" />
    </div>
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
      <template #empty>No orders yet.</template>
      <Column field="id" header="ID" style="width: 5rem" />
      <Column header="Reference"><template #body="{ data }">{{ data.public_id.slice(0, 8) }}…</template></Column>
      <Column header="Status"><template #body="{ data }"><Tag :value="data.status" :severity="sev(data.status)" /></template></Column>
      <Column field="currency" header="Ccy" />
      <Column field="grand_total" header="Total" />
    </DataTable>

    <Dialog v-model:visible="dialogOpen" header="New order (on behalf of customer)" modal :style="{ width: '460px' }">
      <form class="form" @submit.prevent="create">
        <div class="grid2">
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
          <div class="field"><label>Currency</label><InputText v-model="form.currency" maxlength="3" fluid /></div>
        </div>
        <div class="grid3">
          <div class="field span2">
            <label>Product</label>
            <Select
              v-model="form.product_id"
              :options="productOptions"
              optionLabel="label"
              optionValue="id"
              filter
              filterPlaceholder="Search products…"
              placeholder="Select a product"
              :emptyMessage="productsLoaded ? 'No products' : 'Loading…'"
              showClear
              fluid
            />
          </div>
          <div class="field"><label>Qty</label><InputText v-model="form.quantity" fluid /></div>
        </div>
        <div class="field"><label>Unit price</label><InputText v-model="form.unit_price" fluid /></div>
      </form>
      <template #footer>
        <Button label="Cancel" severity="secondary" text @click="dialogOpen = false" />
        <Button label="Create" :loading="saving" @click="create" />
      </template>
    </Dialog>
  </div>
</template>

<style scoped>
.header { display: flex; align-items: center; justify-content: space-between; margin-bottom: 1rem; }
.header h1 { margin: 0; }
.mb { margin-bottom: 1rem; }
.clickable :deep(tbody tr) { cursor: pointer; }
.form { display: flex; flex-direction: column; gap: 0.9rem; }
.grid2 { display: grid; grid-template-columns: 1fr 1fr; gap: 0.9rem; }
.grid3 { display: grid; grid-template-columns: 1fr 1fr 1fr; gap: 0.9rem; }
.span2 { grid-column: span 2; }
.field { display: flex; flex-direction: column; gap: 0.3rem; }
.field label { font-size: 0.8rem; font-weight: 600; }
</style>
