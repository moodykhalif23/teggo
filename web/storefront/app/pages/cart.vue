<script setup lang="ts">
import Button from 'primevue/button'
import InputNumber from 'primevue/inputnumber'
import Message from 'primevue/message'
import type { components } from '@teggo/api/schema'

definePageMeta({ middleware: 'auth' })
useSeoMeta({ title: 'Your cart — Teggo Store' })

type Cart = components['schemas']['Cart']

const client = useClient()
const cart = ref<Cart | null>(null)
const error = ref('')
const notice = ref('')
const busy = ref(false)

async function load() {
  error.value = ''
  const { data, error: err } = await client.GET('/storefront/cart')
  if (err || !data) {
    error.value = 'Could not load your cart.'
    return
  }
  cart.value = data
}

async function updateQty(itemId: number, quantity: number) {
  busy.value = true
  const { data, error: err } = await client.PATCH('/storefront/cart/items/{id}', {
    params: { path: { id: itemId } },
    body: { quantity: String(quantity) },
  })
  busy.value = false
  if (!err && data) cart.value = data
}

async function removeItem(itemId: number) {
  busy.value = true
  const { data, error: err } = await client.DELETE('/storefront/cart/items/{id}', {
    params: { path: { id: itemId } },
  })
  busy.value = false
  if (!err && data) cart.value = data
}

async function revalidate() {
  notice.value = ''
  busy.value = true
  const { data, error: err } = await client.POST('/storefront/cart/revalidate')
  busy.value = false
  if (err || !data) return
  const n = data.changed?.length ?? 0
  notice.value = n ? `${n} price${n > 1 ? 's' : ''} updated to current pricing.` : 'All prices are current.'
  await load()
}

await load()
</script>

<template>
  <section>
    <h1 class="title">Your cart</h1>

    <Message v-if="error" severity="error" :closable="false">{{ error }}</Message>
    <Message v-if="notice" severity="info" :closable="true" class="mb">{{ notice }}</Message>

    <template v-if="cart">
      <div v-if="cart.items.length" class="lines">
        <div v-for="it in cart.items" :key="it.id" class="line">
          <div class="info">
            <div class="name">{{ it.name }}</div>
            <div class="sku">{{ it.sku }} · {{ it.unit_price }} {{ cart.currency }} / {{ it.unit }}</div>
          </div>
          <InputNumber
            :modelValue="Number(it.quantity)"
            :min="1"
            :disabled="busy"
            showButtons
            buttonLayout="horizontal"
            :step="1"
            class="qty"
            @update:modelValue="updateQty(it.id, $event as number)"
          >
            <template #incrementbuttonicon><i class="pi pi-plus" /></template>
            <template #decrementbuttonicon><i class="pi pi-minus" /></template>
          </InputNumber>
          <div class="rowtotal">{{ it.row_total }} {{ cart.currency }}</div>
          <Button icon="pi pi-trash" text rounded severity="danger" :disabled="busy" @click="removeItem(it.id)" />
        </div>

        <div class="summary">
          <Button label="Re-check prices" icon="pi pi-refresh" severity="secondary" outlined :loading="busy" @click="revalidate" />
          <div class="subtotal">
            <span>Subtotal</span>
            <strong>{{ cart.subtotal }} {{ cart.currency }}</strong>
          </div>
          <Button label="Checkout (soon)" icon="pi pi-arrow-right" iconPos="right" disabled />
        </div>
      </div>

      <div v-else class="empty">
        <p class="muted">Your cart is empty.</p>
        <NuxtLink to="/c/all"><Button label="Browse catalog" icon="pi pi-shopping-bag" /></NuxtLink>
      </div>
    </template>
  </section>
</template>

<style scoped>
.title { margin: 0 0 1.25rem; }
.mb { margin-bottom: 1rem; }
.lines { display: flex; flex-direction: column; gap: 0.5rem; }
.line {
  display: grid;
  grid-template-columns: 1fr 9rem 8rem auto;
  align-items: center;
  gap: 1rem;
  padding: 0.75rem 1rem;
  background: var(--p-surface-0, #fff);
  border: 1px solid var(--p-surface-200, #e2e8f0);
  border-radius: 10px;
}
.name { font-weight: 600; }
.sku { font-size: 0.8rem; color: var(--p-text-muted-color, #64748b); }
.qty { width: 9rem; }
.rowtotal { text-align: right; font-variant-numeric: tabular-nums; }
.summary {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 1.5rem;
  margin-top: 1rem;
  padding: 1rem;
}
.subtotal { display: flex; gap: 0.6rem; align-items: baseline; font-size: 1.1rem; }
.empty { text-align: center; padding: 3rem 0; }
.muted { color: var(--p-text-muted-color, #64748b); margin-bottom: 1rem; }
</style>
