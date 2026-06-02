<script setup lang="ts">
import Card from 'primevue/card'
import Select from 'primevue/select'
import InputNumber from 'primevue/inputnumber'
import InputText from 'primevue/inputtext'
import Textarea from 'primevue/textarea'
import Button from 'primevue/button'
import Message from 'primevue/message'
import type { components } from '@teggo/api/schema'

definePageMeta({ middleware: 'auth' })
useSeoMeta({ title: 'Request a quote — Teggo Store' })

type Product = components['schemas']['StorefrontProduct']

const client = useClient()
const router = useRouter()

interface Line {
  product_public_id: string | null
  quantity: number
  target_price: string
}
const lines = ref<Line[]>([{ product_public_id: null, quantity: 1, target_price: '' }])
const notes = ref('')
const submitting = ref(false)
const error = ref('')

// Public catalog for the product picker.
const { data: products } = await useAsyncData('rfq-products', async () => {
  const { data } = await client.GET('/storefront/products', { params: { query: { page: 1, page_size: 100 } } })
  return (data?.items ?? []) as Product[]
})

function addLine() {
  lines.value.push({ product_public_id: null, quantity: 1, target_price: '' })
}
function removeLine(i: number) {
  lines.value.splice(i, 1)
}

async function submit() {
  error.value = ''
  const items = lines.value
    .filter((l) => l.product_public_id)
    .map((l) => ({
      product_public_id: l.product_public_id as string,
      quantity: String(l.quantity || 1),
      target_price: l.target_price ? l.target_price : null,
    }))
  if (!items.length) {
    error.value = 'Add at least one product.'
    return
  }
  submitting.value = true
  // Create the RFQ (draft) then submit it for the seller to quote.
  const created = await client.POST('/storefront/rfqs', { body: { notes: notes.value, items } })
  if (created.error || !created.data) {
    submitting.value = false
    error.value = 'Could not create the RFQ.'
    return
  }
  const submitted = await client.POST('/storefront/rfqs/{publicID}/submit', {
    params: { path: { publicID: created.data.public_id } },
  })
  submitting.value = false
  if (submitted.error) {
    error.value = 'RFQ created but could not be submitted.'
    return
  }
  router.push('/account/rfqs')
}
</script>

<template>
  <section class="wrap">
    <h1>Request a quote</h1>
    <p class="muted">Add the products and quantities you need. Our team will send back a quote.</p>

    <Message v-if="error" severity="error" :closable="false" class="mb">{{ error }}</Message>

    <Card>
      <template #content>
        <div class="lines">
          <div class="lhead">
            <span>Product</span><span>Qty</span><span>Target price (optional)</span><span></span>
          </div>
          <div v-for="(l, i) in lines" :key="i" class="line">
            <Select
              v-model="l.product_public_id"
              :options="products ?? []"
              optionLabel="name"
              optionValue="public_id"
              filter
              placeholder="Select a product"
              fluid
            />
            <InputNumber v-model="l.quantity" :min="1" :useGrouping="false" />
            <InputText v-model="l.target_price" placeholder="e.g. 9.50" />
            <Button icon="pi pi-times" text rounded severity="danger" :disabled="lines.length === 1" @click="removeLine(i)" />
          </div>
          <Button label="Add product" icon="pi pi-plus" text size="small" @click="addLine" />
        </div>

        <div class="field">
          <label>Notes (optional)</label>
          <Textarea v-model="notes" rows="2" fluid />
        </div>
      </template>
    </Card>

    <div class="actions">
      <Button label="Submit request" icon="pi pi-send" :loading="submitting" @click="submit" />
    </div>
  </section>
</template>

<style scoped>
.wrap { max-width: 760px; }
.muted { color: var(--p-text-muted-color, #64748b); }
.mb { margin-bottom: 1rem; }
.lines { display: flex; flex-direction: column; gap: 0.6rem; margin-bottom: 1rem; }
.lhead, .line { display: grid; grid-template-columns: 1fr 7rem 1fr auto; gap: 0.75rem; align-items: center; }
.lhead { font-size: 0.78rem; color: var(--p-text-muted-color, #64748b); }
.field { display: flex; flex-direction: column; gap: 0.35rem; margin-top: 0.5rem; }
.field label { font-size: 0.85rem; font-weight: 600; }
.actions { margin-top: 1.25rem; }
</style>
