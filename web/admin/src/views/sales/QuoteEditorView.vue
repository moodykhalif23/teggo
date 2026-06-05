<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useToast } from 'primevue/usetoast'
import Card from 'primevue/card'
import Button from 'primevue/button'
import Tag from 'primevue/tag'
import InputText from 'primevue/inputtext'
import Select from 'primevue/select'
import Message from 'primevue/message'
import { api, errMessage } from '@/lib/client'
import { useProductOptions } from '@/composables/useRecordOptions'
import type { components } from '@teggo/api/schema'

type Quote = components['schemas']['QuoteDetail']

interface Line {
  product_id: number
  quantity: string
  unit: string
  unit_price: string
  discount: string
}

const route = useRoute()
const router = useRouter()
const toast = useToast()
const id = Number(route.params.id)

const { productOptions, productsLoaded, loadProducts } = useProductOptions()

const quote = ref<Quote | null>(null)
const lines = ref<Line[]>([])
const error = ref('')
const saving = ref(false)
const sending = ref(false)

const isFinal = computed(() => {
  const s = quote.value?.status
  return s === 'accepted' || s === 'declined' || s === 'expired'
})

function num(s: string) {
  const n = Number(s)
  return Number.isFinite(n) ? n : 0
}
function rowTotal(l: Line) {
  return (num(l.quantity) * num(l.unit_price) - num(l.discount)).toFixed(4)
}
const previewSubtotal = computed(() => lines.value.reduce((acc, l) => acc + Number(rowTotal(l)), 0).toFixed(4))

async function load() {
  error.value = ''
  const { data, error: err } = await api.GET('/admin/quotes/{id}', { params: { path: { id } } })
  if (err || !data) {
    error.value = errMessage(err, 'Quote not found')
    return
  }
  quote.value = data
  lines.value = data.items.map((i) => ({
    product_id: i.product_id,
    quantity: i.quantity,
    unit: i.unit,
    unit_price: i.unit_price,
    discount: i.discount,
  }))
}

function addLine() {
  lines.value.push({ product_id: 0, quantity: '1', unit: 'each', unit_price: '0', discount: '0' })
}
function removeLine(idx: number) {
  lines.value.splice(idx, 1)
}

async function saveLines() {
  saving.value = true
  const { data, error: err } = await api.PUT('/admin/quotes/{id}', {
    params: { path: { id } },
    body: { items: lines.value.map((l) => ({ ...l })) },
  })
  saving.value = false
  if (err || !data) {
    toast.add({ severity: 'error', summary: 'Save failed', detail: errMessage(err), life: 4000 })
    return
  }
  quote.value = data
  toast.add({ severity: 'success', summary: 'Lines saved', detail: `Subtotal ${data.subtotal}`, life: 2500 })
}

async function send() {
  sending.value = true
  const { data, error: err } = await api.POST('/admin/quotes/{id}/send', { params: { path: { id } }, body: {} })
  sending.value = false
  if (err || !data) {
    toast.add({ severity: 'error', summary: 'Send failed', detail: errMessage(err), life: 4000 })
    return
  }
  quote.value = data
  toast.add({ severity: 'success', summary: `Sent (v${data.version})`, life: 2500 })
}

function sev(s?: string) {
  return s === 'sent' || s === 'revised' ? 'info' : s === 'accepted' ? 'success' : s === 'declined' || s === 'expired' ? 'danger' : 'secondary'
}

onMounted(() => {
  load()
  loadProducts()
})
</script>

<template>
  <div class="page">
    <Button icon="pi pi-arrow-left" label="Quotes" text severity="secondary" @click="router.push({ name: 'quotes' })" />
    <Message v-if="error" severity="error" :closable="false" class="mb">{{ error }}</Message>

    <template v-if="quote">
      <div class="head">
        <h1>Quote #{{ quote.id }} <Tag :value="quote.status" :severity="sev(quote.status)" /> <span class="muted">v{{ quote.version }} · {{ quote.currency }}</span></h1>
        <div class="actions">
          <Button label="Save lines" icon="pi pi-save" severity="secondary" :loading="saving" :disabled="isFinal" @click="saveLines" />
          <Button label="Send" icon="pi pi-send" :loading="sending" :disabled="isFinal" @click="send" />
        </div>
      </div>

      <Message v-if="isFinal" severity="warn" :closable="false" class="mb">
        This quote is {{ quote.status }} and can no longer be edited.
      </Message>

      <Card>
        <template #title>
          <div class="linehead">
            <span>Line items</span>
            <Button label="Add line" icon="pi pi-plus" size="small" text :disabled="isFinal" @click="addLine" />
          </div>
        </template>
        <template #content>
          <table class="lines">
            <thead>
              <tr><th>Product</th><th>Qty</th><th>Unit</th><th>Unit price</th><th>Discount</th><th class="r">Row total</th><th></th></tr>
            </thead>
            <tbody>
              <tr v-for="(l, idx) in lines" :key="idx">
                <td>
                  <Select
                    v-model="l.product_id"
                    :options="productOptions"
                    optionLabel="label"
                    optionValue="id"
                    filter
                    filterPlaceholder="Search products…"
                    placeholder="Select a product"
                    :emptyMessage="productsLoaded ? 'No products' : 'Loading…'"
                    :disabled="isFinal"
                    class="prodsel"
                  />
                </td>
                <td><InputText v-model="l.quantity" :disabled="isFinal" class="sm" /></td>
                <td><InputText v-model="l.unit" :disabled="isFinal" class="sm" /></td>
                <td><InputText v-model="l.unit_price" :disabled="isFinal" class="sm" /></td>
                <td><InputText v-model="l.discount" :disabled="isFinal" class="sm" /></td>
                <td class="r tabular">{{ rowTotal(l) }}</td>
                <td><Button icon="pi pi-times" text rounded severity="danger" :disabled="isFinal" @click="removeLine(idx)" /></td>
              </tr>
              <tr v-if="!lines.length"><td colspan="7" class="empty">No lines — add one.</td></tr>
            </tbody>
          </table>

          <div class="totals">
            <span class="muted">Preview subtotal (saved server-side on Save):</span>
            <strong class="tabular">{{ previewSubtotal }}</strong>
            <span class="muted">· stored: {{ quote.subtotal }}</span>
          </div>
        </template>
      </Card>
    </template>
  </div>
</template>

<style scoped>
.head { display: flex; align-items: center; justify-content: space-between; margin: 0.5rem 0 1rem; gap: 1rem; }
.head h1 { margin: 0; display: flex; align-items: center; gap: 0.6rem; font-size: 1.4rem; }
.actions { display: flex; gap: 0.5rem; }
.muted { color: var(--p-text-muted-color, #64748b); font-weight: 400; font-size: 0.95rem; }
.mb { margin-bottom: 1rem; }
.linehead { display: flex; align-items: center; justify-content: space-between; }
.lines { width: 100%; border-collapse: collapse; }
.lines th { text-align: left; font-size: 0.78rem; color: var(--p-text-muted-color, #64748b); padding: 0.3rem 0.5rem; }
.lines td { padding: 0.3rem 0.5rem; }
.lines .r { text-align: right; }
.sm :deep(input), .sm { width: 7rem; }
.prodsel { width: 16rem; }
.tabular { font-variant-numeric: tabular-nums; }
.empty { text-align: center; color: var(--p-text-muted-color, #64748b); padding: 1rem; }
.totals { display: flex; align-items: baseline; gap: 0.6rem; justify-content: flex-end; margin-top: 1rem; font-size: 1.05rem; }
</style>
