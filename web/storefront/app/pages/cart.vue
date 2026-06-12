<script setup lang="ts">
import Button from 'primevue/button'
import InputNumber from 'primevue/inputnumber'
import InputText from 'primevue/inputtext'
import Select from 'primevue/select'
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

// Display currency (indicative). Empty = the store's base currency.
const displayCurrency = ref('')
const currencyOptions = ref<{ label: string; value: string }[]>([])

async function loadCurrencies() {
  const { data } = await client.GET('/storefront/currencies')
  const list = data?.currencies ?? []
  currencyOptions.value = [
    { label: `${data?.base ?? 'Base'} (base)`, value: '' },
    ...list.map((c) => ({ label: c, value: c })),
  ]
}

async function load() {
  error.value = ''
  const query = displayCurrency.value ? { currency: displayCurrency.value } : {}
  const { data, error: err } = await client.GET('/storefront/cart', { params: { query } })
  if (err || !data) {
    error.value = 'Could not load your cart.'
    return
  }
  cart.value = data
}

function onCurrencyChange() {
  load()
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

const couponInput = ref('')
const couponError = ref('')

async function applyCoupon() {
  couponError.value = ''
  if (!couponInput.value.trim()) return
  busy.value = true
  const { data, error: err } = await client.POST('/storefront/cart/coupon', {
    body: { code: couponInput.value.trim() },
  })
  busy.value = false
  if (err || !data) {
    couponError.value = "That coupon code isn't valid."
    return
  }
  cart.value = data
  couponInput.value = ''
}

async function removeCoupon() {
  busy.value = true
  const { data, error: err } = await client.DELETE('/storefront/cart/coupon')
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
await loadCurrencies()
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

          <div class="totals">
            <!-- Coupon -->
            <div v-if="cart.coupon_code" class="coupon-applied">
              <span><i class="pi pi-tag" /> {{ cart.coupon_code }}<template v-if="cart.discount_label"> — {{ cart.discount_label }}</template></span>
              <Button icon="pi pi-times" text rounded size="small" :disabled="busy" aria-label="Remove coupon" @click="removeCoupon" />
            </div>
            <div v-else class="coupon-entry">
              <InputText v-model="couponInput" placeholder="Coupon code" :disabled="busy" @keyup.enter="applyCoupon" />
              <Button label="Apply" severity="secondary" outlined :disabled="busy || !couponInput.trim()" @click="applyCoupon" />
            </div>
            <small v-if="couponError" class="coupon-error">{{ couponError }}</small>

            <div class="row">
              <span>Subtotal</span>
              <span>{{ cart.subtotal }} {{ cart.currency }}</span>
            </div>
            <div v-if="cart.discount_amount && Number(cart.discount_amount) > 0" class="row discount">
              <span>Discount</span>
              <span>−{{ cart.discount_amount }} {{ cart.currency }}</span>
            </div>
            <div class="row grand">
              <span>Total</span>
              <strong>{{ cart.grand_total ?? cart.subtotal }} {{ cart.currency }}</strong>
            </div>

            <!-- Display-currency conversion (indicative) -->
            <div v-if="currencyOptions.length > 1" class="ccy-row">
              <span>Show in</span>
              <Select
                v-model="displayCurrency"
                :options="currencyOptions"
                optionLabel="label"
                optionValue="value"
                :disabled="busy"
                @change="onCurrencyChange"
              />
            </div>
            <div v-if="cart.display" class="row display">
              <span>≈ {{ cart.display.currency }}</span>
              <span>{{ cart.display.grand_total }} {{ cart.display.currency }}</span>
            </div>

            <NuxtLink to="/checkout" class="checkout-link">
              <Button label="Checkout" icon="pi pi-arrow-right" iconPos="right" />
            </NuxtLink>
          </div>
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
@media (max-width: 640px) {
  .line { grid-template-columns: 1fr auto; gap: 0.5rem 1rem; }
  .qty { width: auto; }
  .rowtotal { grid-column: 1 / -1; text-align: left; font-weight: 600; }
}
.summary {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 1.5rem;
  margin-top: 1rem;
  padding: 1rem;
  flex-wrap: wrap;
}
.totals {
  display: flex;
  flex-direction: column;
  gap: 0.5rem;
  min-width: 18rem;
  margin-left: auto;
}
.coupon-entry { display: flex; gap: 0.5rem; }
.coupon-entry :deep(input) { flex: 1; }
.coupon-applied {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 0.5rem;
  padding: 0.4rem 0.2rem 0.4rem 0.6rem;
  border: 1px dashed var(--p-primary-color, #16a34a);
  border-radius: 8px;
  color: var(--p-primary-color, #16a34a);
  font-weight: 600;
  font-size: 0.9rem;
}
.coupon-error { color: var(--p-red-500, #ef4444); font-size: 0.8rem; }
.totals .row { display: flex; justify-content: space-between; font-variant-numeric: tabular-nums; }
.totals .discount { color: var(--p-primary-color, #16a34a); }
.totals .grand { font-size: 1.15rem; padding-top: 0.4rem; border-top: 1px solid var(--p-surface-200, #e2e8f0); }
.ccy-row { display: flex; align-items: center; justify-content: space-between; gap: 0.6rem; margin-top: 0.5rem; }
.ccy-row span { font-size: 0.85rem; color: var(--p-text-muted-color, #64748b); }
.totals .display { color: var(--p-text-muted-color, #64748b); font-size: 0.9rem; }
.checkout-link { margin-top: 0.5rem; display: block; }
.checkout-link :deep(button) { width: 100%; }
.empty { text-align: center; padding: 3rem 0; }
.muted { color: var(--p-text-muted-color, #64748b); margin-bottom: 1rem; }
</style>
