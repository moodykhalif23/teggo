<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useToast } from 'primevue/usetoast'
import { useConfirm } from 'primevue/useconfirm'
import Card from 'primevue/card'
import Tabs from 'primevue/tabs'
import TabList from 'primevue/tablist'
import Tab from 'primevue/tab'
import TabPanels from 'primevue/tabpanels'
import TabPanel from 'primevue/tabpanel'
import DataTable from 'primevue/datatable'
import Column from 'primevue/column'
import Button from 'primevue/button'
import Tag from 'primevue/tag'
import Dialog from 'primevue/dialog'
import InputText from 'primevue/inputtext'
import InputNumber from 'primevue/inputnumber'
import Select from 'primevue/select'
import Message from 'primevue/message'
import { api, errMessage } from '@/lib/client'
import { useCustomerOptions, useProductOptions } from '@/composables/useRecordOptions'
import type { components } from '@teggo/api/schema'

type PriceList = components['schemas']['PriceList']
type Price = components['schemas']['Price']
type Assignment = components['schemas']['PriceListAssignment']

const route = useRoute()
const router = useRouter()
const toast = useToast()
const confirm = useConfirm()
const id = Number(route.params.id)

const list = ref<PriceList | null>(null)
const prices = ref<Price[]>([])
const assignments = ref<Assignment[]>([])
const error = ref('')

const { customers, customersLoaded, loadCustomers } = useCustomerOptions()
const { productOptions, productsLoaded, loadProducts } = useProductOptions()
const prodLabel = computed(() => Object.fromEntries(productOptions.value.map((o) => [o.id, o.label])))

async function load() {
  error.value = ''
  const [l, p, a] = await Promise.all([
    api.GET('/admin/price-lists/{id}', { params: { path: { id } } }),
    api.GET('/admin/price-lists/{id}/prices', { params: { path: { id } } }),
    api.GET('/admin/price-lists/{id}/assignments', { params: { path: { id } } }),
  ])
  if (l.error || !l.data) {
    error.value = errMessage(l.error, 'Price list not found')
    return
  }
  list.value = l.data
  prices.value = p.data?.items ?? []
  assignments.value = a.data?.items ?? []
}

// --- add tier ---
const priceDialog = ref(false)
const savingPrice = ref(false)
const priceForm = reactive({ product_id: null as number | null, unit: 'each', min_quantity: '1', value: '' })
function openPrice() {
  Object.assign(priceForm, { product_id: null, unit: 'each', min_quantity: '1', value: '' })
  priceDialog.value = true
}
async function savePrice() {
  if (!priceForm.product_id || !priceForm.value) {
    toast.add({ severity: 'warn', summary: 'product_id and value required', life: 3000 })
    return
  }
  savingPrice.value = true
  const { error: err } = await api.POST('/admin/price-lists/{id}/prices', {
    params: { path: { id } },
    body: { product_id: priceForm.product_id, unit: priceForm.unit, min_quantity: priceForm.min_quantity, value: priceForm.value },
  })
  savingPrice.value = false
  if (err) {
    toast.add({ severity: 'error', summary: 'Failed', detail: errMessage(err), life: 4000 })
    return
  }
  priceDialog.value = false
  toast.add({ severity: 'success', summary: 'Tier saved', life: 2000 })
  load()
}

// --- add assignment ---
const asgDialog = ref(false)
const savingAsg = ref(false)
const asgForm = reactive({ target: 'customer' as 'customer' | 'customer_group' | 'website', targetId: null as number | null, priority: 0 })
function openAsg() {
  Object.assign(asgForm, { target: 'customer', targetId: null, priority: 0 })
  asgDialog.value = true
}
async function saveAsg() {
  if (!asgForm.targetId) {
    toast.add({ severity: 'warn', summary: 'target id required', life: 3000 })
    return
  }
  const body: components['schemas']['AssignmentInput'] = { price_list_id: id, priority: asgForm.priority }
  if (asgForm.target === 'customer') body.customer_id = asgForm.targetId
  else if (asgForm.target === 'customer_group') body.customer_group_id = asgForm.targetId
  else body.website_id = asgForm.targetId
  savingAsg.value = true
  const { error: err } = await api.POST('/admin/price-list-assignments', { body })
  savingAsg.value = false
  if (err) {
    toast.add({ severity: 'error', summary: 'Failed', detail: errMessage(err), life: 4000 })
    return
  }
  asgDialog.value = false
  toast.add({ severity: 'success', summary: 'Assignment added', life: 2000 })
  load()
}
function deleteAsg(a: Assignment) {
  confirm.require({
    message: 'Remove this assignment?',
    header: 'Confirm',
    icon: 'pi pi-exclamation-triangle',
    rejectProps: { label: 'Cancel', severity: 'secondary', outlined: true },
    acceptProps: { label: 'Remove', severity: 'danger' },
    accept: async () => {
      const { error: err } = await api.DELETE('/admin/price-list-assignments/{id}', { params: { path: { id: a.id } } })
      if (err) {
        toast.add({ severity: 'error', summary: 'Failed', detail: errMessage(err), life: 4000 })
        return
      }
      load()
    },
  })
}

function targetLabel(a: Assignment) {
  if (a.customer_id) return `customer #${a.customer_id}`
  if (a.customer_group_id) return `group #${a.customer_group_id}`
  if (a.website_id) return `website #${a.website_id}`
  return '—'
}

onMounted(() => {
  load()
  loadProducts()
  loadCustomers()
})
</script>

<template>
  <div class="page">
    <Button icon="pi pi-arrow-left" label="Price lists" text severity="secondary" @click="router.push({ name: 'pricing' })" />
    <Message v-if="error" severity="error" :closable="false" class="mb">{{ error }}</Message>

    <template v-if="list">
      <h1 class="title">{{ list.name }} <Tag :value="list.currency" severity="secondary" /></h1>

      <Card class="overview">
        <template #content>
          <span class="meta"><strong>Default:</strong> {{ list.is_default ? 'yes' : 'no' }}</span>
          <span class="meta"><strong>Active:</strong> {{ list.is_active ? 'yes' : 'no' }}</span>
        </template>
      </Card>

      <Tabs value="prices">
        <TabList>
          <Tab value="prices">Prices ({{ prices.length }})</Tab>
          <Tab value="assignments">Assignments ({{ assignments.length }})</Tab>
        </TabList>
        <TabPanels>
          <TabPanel value="prices">
            <div class="tabhead"><Button icon="pi pi-plus" label="Add tier" size="small" @click="openPrice" /></div>
            <DataTable :value="prices" dataKey="id" stripedRows>
              <template #empty>No prices.</template>
              <Column header="Product"><template #body="{ data }">{{ prodLabel[data.product_id] ?? `#${data.product_id}` }}</template></Column>
              <Column field="unit" header="Unit" />
              <Column field="min_quantity" header="Min qty" />
              <Column field="value" header="Unit price" />
            </DataTable>
          </TabPanel>
          <TabPanel value="assignments">
            <div class="tabhead"><Button icon="pi pi-plus" label="Add assignment" size="small" @click="openAsg" /></div>
            <DataTable :value="assignments" dataKey="id" stripedRows>
              <template #empty>No assignments — this list resolves to nobody yet.</template>
              <Column header="Target"><template #body="{ data }">{{ targetLabel(data) }}</template></Column>
              <Column field="priority" header="Priority" />
              <Column header="" style="width: 4rem">
                <template #body="{ data }"><Button icon="pi pi-trash" text rounded severity="danger" @click="deleteAsg(data)" /></template>
              </Column>
            </DataTable>
          </TabPanel>
        </TabPanels>
      </Tabs>
    </template>

    <Dialog v-model:visible="priceDialog" header="Add price tier" modal :style="{ width: '420px' }">
      <form class="form" @submit.prevent="savePrice">
        <div class="field">
          <label>Product</label>
          <Select
            v-model="priceForm.product_id"
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
        <div class="field"><label>Unit</label><InputText v-model="priceForm.unit" fluid /></div>
        <div class="field"><label>Min quantity</label><InputText v-model="priceForm.min_quantity" fluid /></div>
        <div class="field"><label>Unit price</label><InputText v-model="priceForm.value" fluid /></div>
      </form>
      <template #footer>
        <Button label="Cancel" severity="secondary" text @click="priceDialog = false" />
        <Button label="Save" :loading="savingPrice" @click="savePrice" />
      </template>
    </Dialog>

    <Dialog v-model:visible="asgDialog" header="Add assignment" modal :style="{ width: '420px' }">
      <form class="form" @submit.prevent="saveAsg">
        <div class="field">
          <label>Target</label>
          <Select v-model="asgForm.target" :options="['customer', 'customer_group', 'website']" fluid />
        </div>
        <div class="field" v-if="asgForm.target === 'customer'">
          <label>Customer</label>
          <Select
            v-model="asgForm.targetId"
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
        <div class="field" v-else><label>{{ asgForm.target === 'customer_group' ? 'Customer group ID' : 'Website ID' }}</label><InputNumber v-model="asgForm.targetId" :useGrouping="false" fluid /></div>
        <div class="field"><label>Priority (higher wins within a level)</label><InputNumber v-model="asgForm.priority" fluid /></div>
      </form>
      <template #footer>
        <Button label="Cancel" severity="secondary" text @click="asgDialog = false" />
        <Button label="Save" :loading="savingAsg" @click="saveAsg" />
      </template>
    </Dialog>
  </div>
</template>

<style scoped>
.title { margin: 0.5rem 0 1rem; display: flex; align-items: center; gap: 0.6rem; }
.mb { margin-bottom: 1rem; }
.overview { margin-bottom: 1.25rem; }
.meta { margin-right: 1.5rem; }
.tabhead { display: flex; justify-content: flex-end; margin-bottom: 0.75rem; }
.form { display: flex; flex-direction: column; gap: 0.9rem; }
.field { display: flex; flex-direction: column; gap: 0.3rem; }
.field label { font-size: 0.8rem; font-weight: 600; }
</style>
