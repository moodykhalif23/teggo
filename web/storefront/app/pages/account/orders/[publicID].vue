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

// ---- pay this order ----
const paying = ref(false)
const payNotice = ref('')
// Show "Pay now" for live, unpaid orders (not cancelled, not awaiting approval).
const canPay = computed(
  () => !!order.value && !order.value.paid && !['cancelled', 'on_hold'].includes(order.value.status),
)

async function payNow() {
  paying.value = true
  payNotice.value = ''
  // Demo token; the mock gateway accepts any token that doesn't contain "decline".
  const { data, error: err, response } = await client.POST('/storefront/orders/{publicID}/pay', {
    params: { path: { publicID } },
    body: { token: 'tok_demo' },
  })
  paying.value = false
  if (!err && data) {
    // Land on the (now paid) invoice with its receipt + PDF.
    router.push(`/account/invoices/${data.public_id}`)
    return
  }
  if (response?.status === 402) payNotice.value = 'The card was declined. Please try another card.'
  else if (response?.status === 409 && order.value?.invoice_public_id)
    router.push(`/account/invoices/${order.value.invoice_public_id}`)
  else payNotice.value = 'Payment could not be processed. Please try again.'
}

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
          <Button v-if="canPay" label="Pay now" icon="pi pi-credit-card" :loading="paying" @click="payNow" />
          <Tag v-else-if="order.paid" value="Paid" severity="success" icon="pi pi-check" />
          <div class="total">{{ order.grand_total }} {{ order.currency }}</div>
        </div>
      </div>

      <Message v-if="payNotice" severity="error" :closable="true" class="mb">{{ payNotice }}</Message>
      <Message v-if="order.paid" severity="success" :closable="false" class="mb">
        This order has been paid.
        <NuxtLink v-if="order.invoice_public_id" :to="`/account/invoices/${order.invoice_public_id}`">View invoice</NuxtLink>
      </Message>
      <Message v-if="returnNotice" severity="info" :closable="true" class="mb">{{ returnNotice }}</Message>

      <DataTable :value="order.items" dataKey="id" stripedRows>
        <template #empty>No items.</template>
        <Column field="name" header="Product" />
        <Column field="sku" header="SKU" />
        <Column field="quantity" header="Qty" />
        <Column field="unit_price" header="Unit price" />
        <Column field="row_total" header="Row total" />
      </DataTable>

      <!-- Cost breakdown so the VAT-inclusive total is transparent (subtotal is
           pre-tax; VAT is added at order creation). -->
      <div class="summary">
        <div class="srow"><span>Subtotal</span><span>{{ order.subtotal }} {{ order.currency }}</span></div>
        <div v-if="Number(order.tax_total) > 0" class="srow">
          <span>VAT</span><span>{{ order.tax_total }} {{ order.currency }}</span>
        </div>
        <div v-if="Number(order.shipping_total) > 0" class="srow">
          <span>Shipping</span><span>{{ order.shipping_total }} {{ order.currency }}</span>
        </div>
        <div class="srow grand"><span>Total</span><span>{{ order.grand_total }} {{ order.currency }}</span></div>
      </div>

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
.summary { max-width: 22rem; margin-left: auto; margin-top: 1rem; display: flex; flex-direction: column; gap: 0.4rem; }
.srow { display: flex; justify-content: space-between; font-size: 0.95rem; color: var(--p-text-color, #334155); }
.srow.grand { border-top: 1px solid var(--p-surface-200, #e2e8f0); padding-top: 0.5rem; margin-top: 0.25rem; font-size: 1.1rem; font-weight: 700; }
.mb { margin-bottom: 1rem; }
.small { font-size: 0.85rem; }
.rrow { display: flex; align-items: center; justify-content: space-between; gap: 1rem; margin-bottom: 0.5rem; }
.rqty { width: 9rem; }
.field { display: flex; flex-direction: column; gap: 0.35rem; margin-top: 0.75rem; }
.field label { font-size: 0.85rem; font-weight: 600; }
</style>
