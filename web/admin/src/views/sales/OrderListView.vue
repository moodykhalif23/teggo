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
const form = reactive({ customer_id: null as number | null, product_id: null as number | null, quantity: '1', unit_price: '' })

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
  Object.assign(form, { customer_id: null, product_id: null, quantity: '1', unit_price: '' })
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
      // Currency intentionally omitted — the server applies the org default.
      customer_id: form.customer_id,
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
      <Column header="Reference"><template #body="{ data }">{{ data.public_id.slice(0, 8) }}…</template></Column>
      <Column header="Status"><template #body="{ data }"><Tag :value="data.status" :severity="sev(data.status)" /></template></Column>
      <Column field="currency" header="Ccy" />
      <Column field="grand_total" header="Total" />
    </DataTable>

    <Dialog v-model:visible="dialogOpen" header="New order (on behalf of customer)" modal :style="{ width: '460px' }">
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
        <div class="field">
          <label>Unit price <span v-if="currency" class="ccy">({{ currency }})</span></label>
          <InputText v-model="form.unit_price" fluid />
        </div>
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
.grid2 { display: grid; grid-template-columns: 1fr 1fr; gap: 0.9rem; }
.grid3 { display: grid; grid-template-columns: 1fr 1fr 1fr; gap: 0.9rem; }
.span2 { grid-column: span 2; }
.field { display: flex; flex-direction: column; gap: 0.3rem; }
.field label { font-size: 0.8rem; font-weight: 600; }
.ccy { font-weight: 400; color: var(--p-text-muted-color, #64748b); }
</style>
