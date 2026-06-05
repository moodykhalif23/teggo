<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { useToast } from 'primevue/usetoast'
import Card from 'primevue/card'
import InputText from 'primevue/inputtext'
import Select from 'primevue/select'
import Button from 'primevue/button'
import Tag from 'primevue/tag'
import { api, errMessage } from '@/lib/client'
import { useCustomerOptions, useProductOptions } from '@/composables/useRecordOptions'

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

// Recompute combined_prices for a customer
const recompute = reactive({ customer_id: null as number | null, currency: '' })
const recomputing = ref(false)
async function doRecompute() {
  if (!recompute.customer_id) {
    toast.add({ severity: 'warn', summary: 'customer_id required', life: 3000 })
    return
  }
  recomputing.value = true
  const { error } = await api.POST('/admin/pricing/recompute', {
    body: { customer_id: recompute.customer_id, currency: recompute.currency || undefined },
  })
  recomputing.value = false
  if (error) {
    toast.add({ severity: 'error', summary: 'Recompute failed', detail: errMessage(error), life: 4000 })
    return
  }
  toast.add({ severity: 'success', summary: 'Recompute enqueued', life: 2500 })
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
      <template #title>Recompute combined prices</template>
      <template #subtitle>Rebuild the cached prices for a customer (async job).</template>
      <template #content>
        <div class="row">
          <Select v-model="recompute.customer_id" :options="customers" optionLabel="name" optionValue="id" filter filterPlaceholder="Search…" placeholder="Customer" :emptyMessage="customersLoaded ? 'No customers' : 'Loading…'" showClear />
          <InputText v-model="recompute.currency" placeholder="ccy (default website)" class="sm" maxlength="3" />
          <Button label="Recompute" icon="pi pi-bolt" :loading="recomputing" @click="doRecompute" />
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
.muted { color: var(--p-text-muted-color, #64748b); }
</style>
