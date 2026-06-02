<script setup lang="ts">
import DataTable from 'primevue/datatable'
import Column from 'primevue/column'
import Tag from 'primevue/tag'
import Button from 'primevue/button'
import Message from 'primevue/message'
import type { components } from '@teggo/api/schema'

definePageMeta({ middleware: 'auth' })

type Quote = components['schemas']['QuoteDetail']

const route = useRoute()
const router = useRouter()
const client = useClient()
const publicID = route.params.publicID as string

const quote = ref<Quote | null>(null)
const loadError = ref(false)
const busy = ref(false)
const notice = ref('')

async function load() {
  const { data, error } = await client.GET('/storefront/quotes/{publicID}', { params: { path: { publicID } } })
  if (error || !data) {
    loadError.value = true
    return
  }
  quote.value = data
}

const open = computed(() => quote.value?.status === 'sent' || quote.value?.status === 'revised')

async function accept() {
  busy.value = true
  notice.value = ''
  const { data, error } = await client.POST('/storefront/quotes/{publicID}/accept', { params: { path: { publicID } } })
  busy.value = false
  if (error || !data) {
    notice.value = 'This quote could not be accepted (it may have expired).'
    return
  }
  router.push(`/account/orders/${data.public_id}`)
}

async function decline() {
  busy.value = true
  const { error } = await client.POST('/storefront/quotes/{publicID}/decline', { params: { path: { publicID } } })
  busy.value = false
  if (!error) await load()
}

function sev(s?: string) {
  return s === 'accepted' ? 'success' : s === 'declined' || s === 'expired' ? 'danger' : 'info'
}

useSeoMeta({ title: 'Your quote — Teggo Store' })
await load()
</script>

<template>
  <section class="wrap">
    <Message v-if="loadError" severity="error" :closable="false">Quote not found.</Message>

    <template v-if="quote">
      <div class="head">
        <h1>Quote <Tag :value="quote.status" :severity="sev(quote.status)" /></h1>
        <div class="muted">v{{ quote.version }} · {{ quote.currency }}<span v-if="quote.valid_until"> · valid until {{ new Date(quote.valid_until).toLocaleDateString() }}</span></div>
      </div>

      <Message v-if="notice" severity="warn" :closable="true" class="mb">{{ notice }}</Message>

      <DataTable :value="quote.items" dataKey="id" stripedRows>
        <template #empty>No line items.</template>
        <Column field="name" header="Product" />
        <Column field="quantity" header="Qty" />
        <Column field="unit_price" header="Unit price" />
        <Column field="discount" header="Discount" />
        <Column field="row_total" header="Row total" />
      </DataTable>

      <div class="summary">
        <div class="subtotal"><span>Subtotal</span><strong>{{ quote.subtotal }} {{ quote.currency }}</strong></div>
        <div v-if="open" class="cta">
          <Button label="Decline" severity="secondary" outlined :loading="busy" @click="decline" />
          <Button label="Accept &amp; create order" icon="pi pi-check" :loading="busy" @click="accept" />
        </div>
        <Message v-else severity="secondary" :closable="false">This quote is {{ quote.status }}.</Message>
      </div>
    </template>
  </section>
</template>

<style scoped>
.wrap { max-width: 820px; }
.head { display: flex; align-items: baseline; justify-content: space-between; margin-bottom: 1rem; }
.head h1 { margin: 0; display: flex; align-items: center; gap: 0.6rem; }
.muted { color: var(--p-text-muted-color, #64748b); }
.mb { margin-bottom: 1rem; }
.summary { display: flex; align-items: center; justify-content: space-between; margin-top: 1.25rem; gap: 1rem; flex-wrap: wrap; }
.subtotal { display: flex; gap: 0.6rem; align-items: baseline; font-size: 1.2rem; }
.cta { display: flex; gap: 0.75rem; }
</style>
