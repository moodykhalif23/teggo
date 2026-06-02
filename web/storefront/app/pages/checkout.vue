<script setup lang="ts">
import Button from 'primevue/button'
import InputText from 'primevue/inputtext'
import DatePicker from 'primevue/datepicker'
import Checkbox from 'primevue/checkbox'
import Message from 'primevue/message'
import Card from 'primevue/card'
import type { components } from '@teggo/api/schema'

definePageMeta({ middleware: 'auth' })
useSeoMeta({ title: 'Checkout — Teggo Store' })

type Cart = components['schemas']['Cart']
type AddressInput = components['schemas']['OrderAddressInput']

const client = useClient()
const router = useRouter()

const cart = ref<Cart | null>(null)
const error = ref('')
const placing = ref(false)

const poNumber = ref('')
const deliveryDate = ref<Date | null>(null)
const customAddress = ref(false)
const shipping = reactive<AddressInput>({ line1: '', city: '', country: '' })
const billing = reactive<AddressInput>({ line1: '', city: '', country: '' })
const billingSameAsShipping = ref(true)

async function load() {
  error.value = ''
  const { data, error: err } = await client.GET('/storefront/cart')
  if (err || !data) {
    error.value = 'Could not load your cart.'
    return
  }
  cart.value = data
}

async function placeOrder() {
  if (!cart.value?.items.length) return
  placing.value = true
  error.value = ''

  const body: components['schemas']['PlaceOrderRequest'] = {}
  if (poNumber.value.trim()) body.po_number = poNumber.value.trim()
  if (deliveryDate.value) body.requested_delivery_date = deliveryDate.value.toISOString()
  if (customAddress.value) {
    body.shipping_address = { ...shipping }
    body.billing_address = billingSameAsShipping.value ? { ...shipping } : { ...billing }
  }

  const { data, error: err } = await client.POST('/storefront/orders', { body })
  placing.value = false
  if (err || !data) {
    error.value = errStorefront(err) || 'Could not place your order. Please check your cart and try again.'
    return
  }
  await router.push(`/account/orders/${data.public_id}`)
}

function errStorefront(err: unknown): string {
  const e = err as { error?: { message?: string } } | undefined
  return e?.error?.message ?? ''
}

await load()
</script>

<template>
  <section class="wrap">
    <Button icon="pi pi-arrow-left" label="Back to cart" text severity="secondary" @click="router.push('/cart')" />
    <h1 class="title">Checkout</h1>

    <Message v-if="error" severity="error" :closable="false" class="mb">{{ error }}</Message>

    <template v-if="cart && cart.items.length">
      <div class="grid">
        <div class="form">
          <Card>
            <template #title>Order details</template>
            <template #content>
              <div class="field">
                <label for="po">PO number <span class="muted">(optional)</span></label>
                <InputText id="po" v-model="poNumber" placeholder="e.g. PO-1234" />
              </div>
              <div class="field">
                <label for="dd">Requested delivery date <span class="muted">(optional)</span></label>
                <DatePicker id="dd" v-model="deliveryDate" dateFormat="yy-mm-dd" showIcon :minDate="new Date()" />
              </div>
            </template>
          </Card>

          <Card>
            <template #title>Shipping</template>
            <template #content>
              <div class="check">
                <Checkbox v-model="customAddress" inputId="custom" binary />
                <label for="custom">Ship to a different address than my default</label>
              </div>

              <template v-if="customAddress">
                <div class="addr">
                  <div class="field"><label>Address line 1</label><InputText v-model="shipping.line1" /></div>
                  <div class="field"><label>Address line 2 <span class="muted">(optional)</span></label><InputText :modelValue="shipping.line2 ?? ''" @update:modelValue="shipping.line2 = ($event as string) || null" /></div>
                  <div class="row">
                    <div class="field"><label>City</label><InputText v-model="shipping.city" /></div>
                    <div class="field"><label>Region <span class="muted">(optional)</span></label><InputText :modelValue="shipping.region ?? ''" @update:modelValue="shipping.region = ($event as string) || null" /></div>
                  </div>
                  <div class="row">
                    <div class="field"><label>Postal code <span class="muted">(optional)</span></label><InputText :modelValue="shipping.postal_code ?? ''" @update:modelValue="shipping.postal_code = ($event as string) || null" /></div>
                    <div class="field"><label>Country</label><InputText v-model="shipping.country" maxlength="2" placeholder="KE" /></div>
                  </div>
                </div>

                <div class="check">
                  <Checkbox v-model="billingSameAsShipping" inputId="samebill" binary />
                  <label for="samebill">Billing address is the same as shipping</label>
                </div>

                <div v-if="!billingSameAsShipping" class="addr">
                  <h4>Billing address</h4>
                  <div class="field"><label>Address line 1</label><InputText v-model="billing.line1" /></div>
                  <div class="field"><label>Address line 2 <span class="muted">(optional)</span></label><InputText :modelValue="billing.line2 ?? ''" @update:modelValue="billing.line2 = ($event as string) || null" /></div>
                  <div class="row">
                    <div class="field"><label>City</label><InputText v-model="billing.city" /></div>
                    <div class="field"><label>Region <span class="muted">(optional)</span></label><InputText :modelValue="billing.region ?? ''" @update:modelValue="billing.region = ($event as string) || null" /></div>
                  </div>
                  <div class="row">
                    <div class="field"><label>Postal code <span class="muted">(optional)</span></label><InputText :modelValue="billing.postal_code ?? ''" @update:modelValue="billing.postal_code = ($event as string) || null" /></div>
                    <div class="field"><label>Country</label><InputText v-model="billing.country" maxlength="2" placeholder="KE" /></div>
                  </div>
                </div>
              </template>
              <p v-else class="muted small">Your order will ship to your default address on file.</p>
            </template>
          </Card>
        </div>

        <Card class="summary">
          <template #title>Order summary</template>
          <template #content>
            <div v-for="it in cart.items" :key="it.id" class="sumline">
              <span class="sn">{{ it.name }} <span class="muted">× {{ it.quantity }}</span></span>
              <span class="sr">{{ it.row_total }} {{ cart.currency }}</span>
            </div>
            <div class="total">
              <span>Total</span>
              <strong>{{ cart.subtotal }} {{ cart.currency }}</strong>
            </div>
            <Button
              label="Place order"
              icon="pi pi-check"
              class="place"
              :loading="placing"
              @click="placeOrder"
            />
            <p class="muted small">Taxes and shipping are calculated by your sales rep after the order is placed.</p>
          </template>
        </Card>
      </div>
    </template>

    <div v-else-if="cart" class="empty">
      <p class="muted">Your cart is empty.</p>
      <NuxtLink to="/c/all"><Button label="Browse catalog" icon="pi pi-shopping-bag" /></NuxtLink>
    </div>
  </section>
</template>

<style scoped>
.wrap { max-width: 980px; }
.title { margin: 0.5rem 0 1.25rem; }
.mb { margin-bottom: 1rem; }
.grid { display: grid; grid-template-columns: 1fr 22rem; gap: 1.25rem; align-items: start; }
.form { display: flex; flex-direction: column; gap: 1.25rem; }
.field { display: flex; flex-direction: column; gap: 0.35rem; margin-bottom: 0.9rem; }
.field label { font-size: 0.85rem; font-weight: 600; }
.row { display: grid; grid-template-columns: 1fr 1fr; gap: 0.75rem; }
.addr { margin: 0.5rem 0 1rem; }
.addr h4 { margin: 0.5rem 0; }
.check { display: flex; align-items: center; gap: 0.5rem; margin: 0.5rem 0; }
.muted { color: var(--p-text-muted-color, #64748b); font-weight: 400; }
.small { font-size: 0.8rem; }
.summary { position: sticky; top: 1rem; }
.sumline { display: flex; justify-content: space-between; gap: 1rem; padding: 0.35rem 0; font-size: 0.9rem; }
.sn { min-width: 0; }
.sr { white-space: nowrap; font-variant-numeric: tabular-nums; }
.total { display: flex; justify-content: space-between; align-items: baseline; margin: 0.75rem 0; padding-top: 0.75rem; border-top: 1px solid var(--p-surface-200, #e2e8f0); font-size: 1.15rem; }
.place { width: 100%; margin-top: 0.5rem; }
.empty { text-align: center; padding: 3rem 0; }
</style>
