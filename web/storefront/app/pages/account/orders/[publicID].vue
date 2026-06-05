<script setup lang="ts">
import DataTable from 'primevue/datatable'
import Column from 'primevue/column'
import Tag from 'primevue/tag'
import Button from 'primevue/button'
import Dialog from 'primevue/dialog'
import InputNumber from 'primevue/inputnumber'
import InputText from 'primevue/inputtext'
import Message from 'primevue/message'
import type { components } from '@teggo/api/schema'

definePageMeta({ middleware: 'auth' })

type Order = components['schemas']['OrderDetail']

const route = useRoute()
const router = useRouter()
const client = useClient()
const publicID = route.params.publicID as string

const { data: order, error } = await useAsyncData(`order-${publicID}`, async () => {
  const { data, error } = await client.GET('/storefront/orders/{publicID}', { params: { path: { publicID } } })
  if (error || !data) throw createError({ statusCode: 404, statusMessage: 'Order not found' })
  return data
})

useSeoMeta({ title: () => (order.value ? `Order ${order.value.public_id.slice(0, 8)} — Teggo` : 'Order') })

const reordering = ref(false)
const reorderNotice = ref('')

async function reorder() {
  reordering.value = true
  reorderNotice.value = ''
  const { data, error: err } = await client.POST('/storefront/cart/reorder', {
    body: { order_public_id: publicID },
  })
  reordering.value = false
  if (err || !data) {
    reorderNotice.value = 'Could not add these items to your cart.'
    return
  }
  const skipped = data.skipped_skus ?? []
  if (skipped.length) {
    reorderNotice.value = `Added to cart. ${skipped.length} item${skipped.length > 1 ? 's' : ''} skipped (price on request): ${skipped.join(', ')}.`
  } else {
    router.push('/cart')
  }
}

function sev(s?: string) {
  if (s === 'cancelled') return 'danger'
  if (s === 'delivered' || s === 'closed') return 'success'
  if (s === 'pending' || s === 'on_hold') return 'warn'
  return 'info'
}

// ---- request a return ----
const returnOpen = ref(false)
const returnQty = reactive<Record<number, number>>({})
const returnReason = ref('')
const returning = ref(false)
const returnNotice = ref('')

function openReturn() {
  returnReason.value = ''
  for (const it of order.value?.items ?? []) returnQty[it.id] = 0
  returnOpen.value = true
}

async function submitReturn() {
  const items = (order.value?.items ?? [])
    .filter((it) => (returnQty[it.id] ?? 0) > 0)
    .map((it) => ({ order_item_id: it.id, quantity: String(returnQty[it.id]) }))
  if (!items.length) {
    returnNotice.value = 'Choose a quantity to return.'
    return
  }
  returning.value = true
  const { error: err } = await client.POST('/storefront/orders/{publicID}/returns', {
    params: { path: { publicID } },
    body: { reason: returnReason.value || null, items },
  })
  returning.value = false
  if (err) {
    returnNotice.value = 'Could not submit your return request.'
    return
  }
  returnOpen.value = false
  returnNotice.value = 'Return requested — we’ll review it shortly.'
}
</script>

<template>
  <section class="wrap">
    <Button icon="pi pi-arrow-left" label="My orders" text severity="secondary" @click="router.push('/account/orders')" />
    <Message v-if="error" severity="error" :closable="false">Order not found.</Message>
    <Message v-if="reorderNotice" severity="info" :closable="true" class="mb">{{ reorderNotice }}</Message>

    <template v-if="order">
      <div class="head">
        <h1>Order <span class="muted">{{ order.public_id.slice(0, 8) }}…</span> <Tag :value="order.status" :severity="sev(order.status)" /></h1>
        <div class="actions">
          <Button label="Request return" icon="pi pi-undo" outlined severity="secondary" @click="openReturn" />
          <Button label="Reorder" icon="pi pi-replay" outlined :loading="reordering" @click="reorder" />
          <div class="total">{{ order.grand_total }} {{ order.currency }}</div>
        </div>
      </div>

      <Message v-if="returnNotice" severity="info" :closable="true" class="mb">{{ returnNotice }}</Message>

      <DataTable :value="order.items" dataKey="id" stripedRows>
        <template #empty>No items.</template>
        <Column field="name" header="Product" />
        <Column field="sku" header="SKU" />
        <Column field="quantity" header="Qty" />
        <Column field="unit_price" header="Unit price" />
        <Column field="row_total" header="Row total" />
      </DataTable>

      <Dialog v-model:visible="returnOpen" modal header="Request a return" :style="{ width: '34rem' }">
        <p class="muted small">Choose how many of each item to return.</p>
        <div v-for="it in order.items" :key="it.id" class="rrow">
          <span class="rname">{{ it.name }} <span class="muted">(of {{ it.quantity }})</span></span>
          <InputNumber v-model="returnQty[it.id]" :min="0" :max="Number(it.quantity)" showButtons buttonLayout="horizontal" class="rqty">
            <template #incrementbuttonicon><i class="pi pi-plus" /></template>
            <template #decrementbuttonicon><i class="pi pi-minus" /></template>
          </InputNumber>
        </div>
        <div class="field"><label>Reason (optional)</label><InputText v-model="returnReason" placeholder="e.g. damaged, wrong item" /></div>
        <template #footer>
          <Button label="Cancel" text :disabled="returning" @click="returnOpen = false" />
          <Button label="Submit request" :loading="returning" @click="submitReturn" />
        </template>
      </Dialog>
    </template>
  </section>
</template>

<style scoped>
.wrap { max-width: 820px; }
.head { display: flex; align-items: center; justify-content: space-between; margin: 0.5rem 0 1rem; }
.head h1 { margin: 0; display: flex; align-items: center; gap: 0.6rem; }
.actions { display: flex; align-items: center; gap: 1.25rem; }
.muted { color: var(--p-text-muted-color, #64748b); font-weight: 400; font-size: 1rem; }
.total { font-size: 1.3rem; font-weight: 700; }
.mb { margin-bottom: 1rem; }
.small { font-size: 0.85rem; }
.rrow { display: flex; align-items: center; justify-content: space-between; gap: 1rem; margin-bottom: 0.5rem; }
.rqty { width: 9rem; }
.field { display: flex; flex-direction: column; gap: 0.35rem; margin-top: 0.75rem; }
.field label { font-size: 0.85rem; font-weight: 600; }
</style>
