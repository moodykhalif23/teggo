<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useToast } from 'primevue/usetoast'
import Card from 'primevue/card'
import DataTable from 'primevue/datatable'
import Column from 'primevue/column'
import Tag from 'primevue/tag'
import Button from 'primevue/button'
import Dialog from 'primevue/dialog'
import Select from 'primevue/select'
import InputText from 'primevue/inputtext'
import InputNumber from 'primevue/inputnumber'
import Message from 'primevue/message'
import { api, errMessage } from '@/lib/client'
import type { components } from '@teggo/api/schema'

type Invoice = components['schemas']['InvoiceDetail']
type Payment = components['schemas']['Payment']

const route = useRoute()
const router = useRouter()
const toast = useToast()
const id = Number(route.params.id)

const invoice = ref<Invoice | null>(null)
const payments = ref<Payment[]>([])
const error = ref('')

async function load() {
  error.value = ''
  const [inv, pays] = await Promise.all([
    api.GET('/admin/invoices/{id}', { params: { path: { id } } }),
    api.GET('/admin/invoices/{id}/payments', { params: { path: { id } } }),
  ])
  if (inv.error || !inv.data) {
    error.value = errMessage(inv.error, 'Invoice not found')
    return
  }
  invoice.value = inv.data
  payments.value = pays.data?.items ?? []
}

// --- record payment ---
const payDialog = ref(false)
const savingPay = ref(false)
const methods = ['card', 'ach', 'invoice', 'po', 'mpesa']
const payForm = reactive({ customer_id: null as number | null, method: 'card', amount: '', currency: '' })
function openPay() {
  Object.assign(payForm, { customer_id: null, method: 'card', amount: invoice.value?.grand_total ?? '', currency: invoice.value?.currency ?? '' })
  payDialog.value = true
}
async function savePay() {
  if (!payForm.customer_id || !payForm.amount) {
    toast.add({ severity: 'warn', summary: 'customer_id and amount required', life: 3000 })
    return
  }
  savingPay.value = true
  const { error: err } = await api.POST('/admin/payments', {
    body: {
      invoice_id: id,
      customer_id: payForm.customer_id,
      method: payForm.method as Payment['method'],
      amount: payForm.amount,
      currency: payForm.currency,
    },
  })
  savingPay.value = false
  if (err) {
    toast.add({ severity: 'error', summary: 'Payment failed', detail: errMessage(err), life: 4000 })
    return
  }
  payDialog.value = false
  toast.add({ severity: 'success', summary: 'Payment recorded', life: 2500 })
  load()
}

async function refund(p: Payment) {
  const { error: err } = await api.POST('/admin/payments/{id}/refund', { params: { path: { id: p.id } } })
  if (err) {
    toast.add({ severity: 'error', summary: 'Refund failed', detail: errMessage(err), life: 4000 })
    return
  }
  load()
}

const regenerating = ref(false)
async function regeneratePdf() {
  regenerating.value = true
  const { error: err } = await api.POST('/admin/invoices/{id}/pdf', { params: { path: { id } } })
  regenerating.value = false
  toast.add(
    err
      ? { severity: 'error', summary: 'PDF failed', detail: errMessage(err), life: 4000 }
      : { severity: 'success', summary: 'PDF generation enqueued', life: 2500 },
  )
}

function invSev(s?: string) {
  return s === 'paid' ? 'success' : s === 'void' ? 'secondary' : s === 'overdue' ? 'danger' : 'info'
}
function paySev(s: string) {
  return s === 'captured' ? 'success' : s === 'refunded' ? 'secondary' : s === 'failed' ? 'danger' : 'warn'
}
</script>

<template>
  <div class="page">
    <Button icon="pi pi-arrow-left" label="Back" text severity="secondary" @click="router.back()" />
    <Message v-if="error" severity="error" :closable="false" class="mb">{{ error }}</Message>

    <template v-if="invoice">
      <div class="head">
        <h1>Invoice <span class="muted">{{ invoice.public_id.slice(0, 8) }}…</span> <Tag :value="invoice.status" :severity="invSev(invoice.status)" /></h1>
        <div class="total">{{ invoice.grand_total }} {{ invoice.currency }}</div>
      </div>

      <div class="meta">
        <span v-if="invoice.due_at">Due {{ new Date(invoice.due_at).toLocaleDateString() }}</span>
        <a v-if="invoice.pdf_url" :href="invoice.pdf_url" target="_blank" rel="noopener" class="pdf"><i class="pi pi-file-pdf" /> Download PDF</a>
        <Button label="Regenerate PDF" icon="pi pi-refresh" size="small" text :loading="regenerating" @click="regeneratePdf" />
      </div>

      <Card class="block">
        <template #title>Lines</template>
        <template #content>
          <DataTable :value="invoice.items" dataKey="id" stripedRows>
            <Column field="description" header="Description" />
            <Column field="quantity" header="Qty" />
            <Column field="unit_price" header="Unit price" />
            <Column field="row_total" header="Row total" />
          </DataTable>
          <div class="totals">
            <span>Subtotal {{ invoice.subtotal }}</span>
            <span>Tax {{ invoice.tax_total }}</span>
            <strong>Total {{ invoice.grand_total }}</strong>
          </div>
        </template>
      </Card>

      <Card class="block">
        <template #title>
          <div class="cardhead"><span>Payments</span><Button icon="pi pi-plus" label="Record payment" size="small" @click="openPay" /></div>
        </template>
        <template #content>
          <DataTable :value="payments" dataKey="id" stripedRows>
            <template #empty>No payments yet.</template>
            <Column field="method" header="Method" />
            <Column field="amount" header="Amount" />
            <Column header="Status"><template #body="{ data }"><Tag :value="data.status" :severity="paySev(data.status)" /></template></Column>
            <Column header="">
              <template #body="{ data }">
                <Button
                  v-if="data.status === 'captured' || data.status === 'authorized'"
                  label="Refund"
                  size="small"
                  text
                  severity="danger"
                  @click="refund(data)"
                />
              </template>
            </Column>
          </DataTable>
        </template>
      </Card>
    </template>

    <Dialog v-model:visible="payDialog" header="Record payment" modal :style="{ width: '440px' }">
      <form class="form" @submit.prevent="savePay">
        <div class="field"><label>Customer ID</label><InputNumber v-model="payForm.customer_id" :useGrouping="false" fluid /></div>
        <div class="grid2">
          <div class="field"><label>Method</label><Select v-model="payForm.method" :options="methods" fluid /></div>
          <div class="field"><label>Currency</label><InputText v-model="payForm.currency" maxlength="3" fluid /></div>
        </div>
        <div class="field"><label>Amount</label><InputText v-model="payForm.amount" fluid /></div>
      </form>
      <template #footer>
        <Button label="Cancel" severity="secondary" text @click="payDialog = false" />
        <Button label="Save" :loading="savingPay" @click="savePay" />
      </template>
    </Dialog>
  </div>
</template>

<style scoped>
.head { display: flex; align-items: center; justify-content: space-between; margin: 0.5rem 0 0.75rem; }
.head h1 { margin: 0; display: flex; align-items: center; gap: 0.6rem; font-size: 1.4rem; }
.muted { color: var(--p-text-muted-color, #64748b); font-weight: 400; font-size: 1rem; }
.total { font-size: 1.3rem; font-weight: 700; }
.meta { display: flex; align-items: center; gap: 1.25rem; color: var(--p-text-muted-color, #64748b); margin-bottom: 1rem; flex-wrap: wrap; }
.mb { margin-bottom: 1rem; }
.block { margin-bottom: 1rem; }
.cardhead { display: flex; align-items: center; justify-content: space-between; }
.totals { display: flex; gap: 1.5rem; justify-content: flex-end; margin-top: 0.75rem; }
.form { display: flex; flex-direction: column; gap: 0.9rem; }
.grid2 { display: grid; grid-template-columns: 1fr 1fr; gap: 0.9rem; }
.field { display: flex; flex-direction: column; gap: 0.3rem; }
.field label { font-size: 0.8rem; font-weight: 600; }
</style>
