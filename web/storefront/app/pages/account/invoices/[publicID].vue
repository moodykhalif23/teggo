<script setup lang="ts">
import DataTable from 'primevue/datatable'
import Column from 'primevue/column'
import Tag from 'primevue/tag'
import Button from 'primevue/button'
import Message from 'primevue/message'
import { ref } from 'vue'
import type { components } from '@teggo/api/schema'

definePageMeta({ middleware: 'auth' })

type Invoice = components['schemas']['InvoiceDetail']

const route = useRoute()
const router = useRouter()
const client = useClient()
const publicID = route.params.publicID as string

const { data: invoice, error } = await useAsyncData(`invoice-${publicID}`, async () => {
  const { data, error } = await client.GET('/storefront/invoices/{publicID}', { params: { path: { publicID } } })
  if (error || !data) throw createError({ statusCode: 404, statusMessage: 'Invoice not found' })
  return data
})

useSeoMeta({ title: () => (invoice.value ? `Invoice ${invoice.value.public_id.slice(0, 8)} — Teggo` : 'Invoice') })

// pdf_url is an API-relative capability URL; resolve it against the API base so
// the browser download reaches the Go service (not the Nuxt origin).
const apiBase = useRuntimeConfig().public.apiBase
const pdfHref = computed(() => (invoice.value?.pdf_url ? `${apiBase}${invoice.value.pdf_url}` : ''))

function sev(s?: string) {
  return s === 'paid' ? 'success' : s === 'overdue' ? 'danger' : s === 'void' ? 'secondary' : 'info'
}

const paying = ref(false)
const payError = ref('')

async function payByCard() {
  if (!invoice.value) return
  paying.value = true
  payError.value = ''
  // A real integration collects a gateway token client-side (Stripe.js etc.);
  // the demo gateway accepts any token (use one containing "decline" to test it).
  const { data, error: err } = await client.POST('/storefront/invoices/{publicID}/pay', {
    params: { path: { publicID } },
    body: { token: 'tok_demo' },
  })
  paying.value = false
  if (err || !data) {
    payError.value = (err as { error?: { message?: string } })?.error?.message ?? 'Payment failed. Please try again.'
    return
  }
  invoice.value = data
}
</script>

<template>
  <section class="wrap">
    <Button icon="pi pi-arrow-left" label="My invoices" text severity="secondary" @click="router.push('/account/invoices')" />
    <Message v-if="error" severity="error" :closable="false">Invoice not found.</Message>

    <template v-if="invoice">
      <div class="head">
        <h1>Invoice <span class="muted">{{ invoice.public_id.slice(0, 8) }}…</span> <Tag :value="invoice.status" :severity="sev(invoice.status)" /></h1>
        <div class="total">{{ invoice.grand_total }} {{ invoice.currency }}</div>
      </div>
      <div class="meta">
        <span v-if="invoice.due_at">Due {{ new Date(invoice.due_at).toLocaleDateString() }}</span>
        <a v-if="pdfHref" :href="pdfHref" target="_blank" rel="noopener"><i class="pi pi-file-pdf" /> Download PDF</a>
      </div>

      <Message v-if="payError" severity="error" :closable="true" class="mb">{{ payError }}</Message>
      <div v-if="invoice.status !== 'paid' && invoice.status !== 'void'" class="paybar">
        <Button label="Pay by card" icon="pi pi-credit-card" :loading="paying" @click="payByCard" />
      </div>
      <Message v-else-if="invoice.status === 'paid'" severity="success" :closable="false" class="mb">
        This invoice is paid. Thank you.
      </Message>

      <DataTable :value="invoice.items" dataKey="id" stripedRows>
        <Column field="description" header="Description" />
        <Column field="quantity" header="Qty" />
        <Column field="unit_price" header="Unit price" />
        <Column field="row_total" header="Row total" />
      </DataTable>

      <div class="totals">
        <span>Subtotal {{ invoice.subtotal }}</span>
        <span>Tax {{ invoice.tax_total }}</span>
        <strong>Total {{ invoice.grand_total }} {{ invoice.currency }}</strong>
      </div>
    </template>
  </section>
</template>

<style scoped>
.wrap { max-width: 820px; }
.head { display: flex; align-items: center; justify-content: space-between; margin: 0.5rem 0 0.75rem; }
.head h1 { margin: 0; display: flex; align-items: center; gap: 0.6rem; }
.muted { color: var(--p-text-muted-color, #64748b); font-weight: 400; font-size: 1rem; }
.total { font-size: 1.3rem; font-weight: 700; }
.meta { display: flex; gap: 1.5rem; align-items: center; color: var(--p-text-muted-color, #64748b); margin-bottom: 1rem; }
.mb { margin-bottom: 1rem; }
.paybar { margin-bottom: 1rem; }
.totals { display: flex; gap: 1.5rem; justify-content: flex-end; margin-top: 1rem; }
</style>
