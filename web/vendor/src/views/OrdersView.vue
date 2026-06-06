<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useToast } from 'primevue/usetoast'
import DataTable from 'primevue/datatable'
import Column from 'primevue/column'
import Tag from 'primevue/tag'
import Button from 'primevue/button'
import Dialog from 'primevue/dialog'
import Message from 'primevue/message'
import { api, errMessage } from '@/lib/client'
import type { components } from '@teggo/api/schema'

type OrderSummary = components['schemas']['VendorOrderSummary']
type OrderDetail = components['schemas']['VendorOrderDetail']

const orders = ref<OrderSummary[]>([])
const loading = ref(false)
const error = ref('')
const toast = useToast()

const detail = ref<OrderDetail | null>(null)
const detailOpen = ref(false)
const busy = ref(false)

// The next fulfilment action available from each status.
const nextStatus: Record<string, { label: string; value: string }> = {
  pending: { label: 'Accept', value: 'accepted' },
  accepted: { label: 'Mark shipped', value: 'shipped' },
  shipped: { label: 'Mark delivered', value: 'delivered' },
}

function sev(s: string) {
  return s === 'delivered' ? 'success' : s === 'cancelled' ? 'danger' : s === 'shipped' ? 'info' : 'warn'
}

async function load() {
  loading.value = true
  error.value = ''
  const { data, error: err } = await api.GET('/vendor/orders')
  loading.value = false
  if (err || !data) {
    error.value = errMessage(err, 'Failed to load orders')
    return
  }
  orders.value = data.items ?? []
}

async function open(o: OrderSummary) {
  const { data } = await api.GET('/vendor/orders/{id}', { params: { path: { id: o.id } } })
  detail.value = data ?? null
  detailOpen.value = true
}

async function advance(o: OrderSummary | OrderDetail, status: string) {
  busy.value = true
  const { error: err } = await api.PATCH('/vendor/orders/{id}/status', {
    params: { path: { id: o.id } },
    body: { status: status as components['schemas']['VendorOrderStatusInput']['status'] },
  })
  busy.value = false
  if (err) {
    toast.add({ severity: 'error', summary: 'Update failed', detail: errMessage(err), life: 4000 })
    return
  }
  toast.add({ severity: 'success', summary: `Marked ${status}`, life: 2000 })
  detailOpen.value = false
  load()
}

onMounted(load)
</script>

<template>
  <div class="page">
    <div class="header">
      <h1>Orders <span class="muted">({{ orders.length }})</span></h1>
      <Button icon="pi pi-refresh" severity="secondary" text @click="load" />
    </div>
    <p class="muted">Your share of each buyer order. Advance fulfilment as you accept, ship and deliver.</p>

    <Message v-if="error" severity="error" :closable="false" class="mb">{{ error }}</Message>

    <DataTable :value="orders" :loading="loading" dataKey="id" stripedRows @rowClick="open($event.data)" class="clickable">
      <template #empty>No orders yet.</template>
      <Column header="Order"><template #body="{ data }">{{ data.order_public_id.slice(0, 8) }}…</template></Column>
      <Column header="Fulfilment"><template #body="{ data }"><Tag :value="data.status" :severity="sev(data.status)" /></template></Column>
      <Column header="Gross"><template #body="{ data }">{{ data.gross_total }} {{ data.currency }}</template></Column>
      <Column header="Net"><template #body="{ data }">{{ data.net_total }} {{ data.currency }}</template></Column>
      <Column header="">
        <template #body="{ data }">
          <Button
            v-if="nextStatus[data.status]"
            :label="nextStatus[data.status].label"
            size="small"
            :loading="busy"
            @click.stop="advance(data, nextStatus[data.status].value)"
          />
        </template>
      </Column>
    </DataTable>

    <Dialog v-model:visible="detailOpen" modal :header="detail ? `Order ${detail.order_public_id.slice(0,8)}…` : 'Order'" :style="{ width: '40rem' }">
      <template v-if="detail">
        <div class="status"><Tag :value="detail.status" :severity="sev(detail.status)" /></div>
        <table class="lines">
          <thead><tr><th>Product</th><th>SKU</th><th>Qty</th><th class="r">Unit</th><th class="r">Total</th></tr></thead>
          <tbody>
            <tr v-for="it in detail.items" :key="it.id">
              <td>{{ it.name }}</td><td>{{ it.sku }}</td><td>{{ it.quantity }}</td>
              <td class="r">{{ it.unit_price }}</td><td class="r">{{ it.row_total }}</td>
            </tr>
          </tbody>
        </table>
        <div class="totals">
          <div>Gross <strong>{{ detail.gross_total }} {{ detail.currency }}</strong></div>
          <div>Commission ({{ detail.commission_rate }}%) <strong>−{{ detail.commission_total }}</strong></div>
          <div class="net">Net payable <strong>{{ detail.net_total }} {{ detail.currency }}</strong></div>
        </div>
      </template>
      <template #footer>
        <Button
          v-if="detail && nextStatus[detail.status]"
          :label="nextStatus[detail.status].label"
          :loading="busy"
          @click="advance(detail, nextStatus[detail.status].value)"
        />
        <Button v-else label="Close" text @click="detailOpen = false" />
      </template>
    </Dialog>
  </div>
</template>

<style scoped>
.header { display: flex; align-items: center; justify-content: space-between; }
.header h1 { margin: 0; }
.muted { color: #64748b; }
.mb { margin-bottom: 1rem; }
.clickable :deep(tbody tr) { cursor: pointer; }
.status { margin-bottom: 0.75rem; }
.lines { width: 100%; border-collapse: collapse; }
.lines th { text-align: left; font-size: 0.78rem; color: #64748b; padding: 0.3rem 0.4rem; }
.lines td { padding: 0.3rem 0.4rem; border-top: 1px solid #f1f5f9; }
.lines .r { text-align: right; }
.totals { margin-top: 1rem; display: flex; flex-direction: column; gap: 0.25rem; align-items: flex-end; }
.totals .net { color: #15803d; font-size: 1.05rem; }
</style>
