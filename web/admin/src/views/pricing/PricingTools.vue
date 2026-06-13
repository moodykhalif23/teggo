<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { useToast } from 'primevue/usetoast'
import Card from 'primevue/card'
import InputText from 'primevue/inputtext'
import Select from 'primevue/select'
import Button from 'primevue/button'
import Tag from 'primevue/tag'
import { api, errMessage } from '@/lib/client'
import type { components } from '@teggo/api/schema'
import { useCustomerOptions, useProductOptions } from '@/composables/useRecordOptions'

type ResolvedPrice = components['schemas']['ResolvedPrice']

const toast = useToast()

const { customers, customersLoaded, loadCustomers } = useCustomerOptions()
const { productOptions, productsLoaded, loadProducts } = useProductOptions()
onMounted(() => {
  loadCustomers()
  loadProducts()
})

// Resolve preview
const resolve = reactive({ customer_id: null as number | null, product_id: null as number | null, quantity: '1', currency: 'USD' })
const resolving = ref(false)
const result = ref<{ price_on_request: boolean; value?: string; source_price_list_id?: number } | null>(null)

async function doResolve() {
  if (!resolve.customer_id || !resolve.product_id) {
    toast.add({ severity: 'warn', summary: 'customer_id and product_id required', life: 3000 })
    return
  }
  resolving.value = true
  result.value = null
  const { data, error } = await api.GET('/admin/pricing/resolve', {
    params: {
      query: {
        customer_id: resolve.customer_id,
        product_id: resolve.product_id,
        quantity: resolve.quantity || '1',
        currency: resolve.currency,
      },
    },
  })
  resolving.value = false
  if (error || !data) {
    toast.add({ severity: 'error', summary: 'Resolve failed', detail: errMessage(error), life: 4000 })
    return
  }
  result.value = data
}

// Customer price book — resolved live , keyset-paginated.
const book = reactive({ customer_id: null as number | null, currency: 'USD' })
const bookRows = ref<ResolvedPrice[]>([])
const bookLoading = ref(false)
async function loadBook() {
  if (!book.customer_id) {
    toast.add({ severity: 'warn', summary: 'customer required', life: 3000 })
    return
  }
  bookLoading.value = true
  bookRows.value = []
  const { data, error } = await api.GET('/admin/customers/{id}/resolved-prices', {
    params: { path: { id: book.customer_id }, query: { currency: book.currency || 'USD', limit: 100 } },
  })
  bookLoading.value = false
  if (error || !data) {
    toast.add({ severity: 'error', summary: 'Load failed', detail: errMessage(error), life: 4000 })
    return
  }
  bookRows.value = data.items ?? []
}
</script>

<template>
  <div class="grid">
    <Card>
      <template #title>Resolve price</template>
      <template #subtitle>Deterministic resolution (customer &gt; group &gt; website, tiered).</template>
      <template #content>
        <div class="row">
          <Select v-model="resolve.customer_id" :options="customers" optionLabel="name" optionValue="id" filter filterPlaceholder="Search…" placeholder="Customer" :emptyMessage="customersLoaded ? 'No customers' : 'Loading…'" showClear />
          <Select v-model="resolve.product_id" :options="productOptions" optionLabel="label" optionValue="id" filter filterPlaceholder="Search…" placeholder="Product" :emptyMessage="productsLoaded ? 'No products' : 'Loading…'" showClear />
          <InputText v-model="resolve.quantity" placeholder="qty" class="sm" />
          <InputText v-model="resolve.currency" placeholder="ccy" class="sm" maxlength="3" />
          <Button label="Resolve" :loading="resolving" @click="doResolve" />
        </div>
        <div v-if="result" class="result">
          <template v-if="result.price_on_request">
            <Tag value="price on request" severity="warn" /> — no price resolved (RFQ path)
          </template>
          <template v-else>
            <Tag :value="result.value" severity="success" />
            <span class="muted">from price list #{{ result.source_price_list_id }}</span>
          </template>
        </div>
      </template>
    </Card>

    <Card>
      <template #title>Customer price book</template>
      <template #subtitle>Contract prices resolved live — no cache, always current.</template>
      <template #content>
        <div class="row">
          <Select v-model="book.customer_id" :options="customers" optionLabel="name" optionValue="id" filter filterPlaceholder="Search…" placeholder="Customer" :emptyMessage="customersLoaded ? 'No customers' : 'Loading…'" showClear />
          <InputText v-model="book.currency" placeholder="ccy" class="sm" maxlength="3" />
          <Button label="Load" icon="pi pi-book" :loading="bookLoading" @click="loadBook" />
        </div>
        <div v-if="bookRows.length" class="result book">
          <div v-for="r in bookRows.slice(0, 50)" :key="r.product_id + '-' + r.min_quantity" class="book-row">
            <span class="muted">#{{ r.product_id }}</span>
            <span class="muted">{{ r.min_quantity }}+</span>
            <Tag :value="r.value" severity="success" />
          </div>
        </div>
      </template>
    </Card>
  </div>
</template>

<style scoped>
.grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(320px, 1fr)); gap: 1rem; }
.row { display: flex; flex-wrap: wrap; gap: 0.5rem; align-items: center; }
.row :deep(.p-inputnumber), .row :deep(.p-inputtext) { width: 9rem; }
.row :deep(.p-select) { min-width: 13rem; }
.row .sm :deep(input), .row .sm { width: 6rem; }
.result { margin-top: 0.9rem; display: flex; align-items: center; gap: 0.5rem; }
.book { flex-direction: column; align-items: stretch; gap: 0.3rem; max-height: 18rem; overflow-y: auto; }
.book-row { display: flex; align-items: center; gap: 0.6rem; }
.muted { color: var(--p-text-muted-color, #64748b); }
</style>
