<script setup lang="ts">
import { onMounted, ref } from 'vue'
import DataTable from 'primevue/datatable'
import Column from 'primevue/column'
import Tag from 'primevue/tag'
import Button from 'primevue/button'
import Message from 'primevue/message'
import { api, errMessage } from '@/lib/client'
import type { components } from '@teggo/api/schema'

type Payout = components['schemas']['VendorPayout']

const payouts = ref<Payout[]>([])
const loading = ref(false)
const error = ref('')

function sev(s: string) {
  return s === 'paid' ? 'success' : s === 'cancelled' ? 'danger' : 'warn'
}

async function load() {
  loading.value = true
  error.value = ''
  const { data, error: err } = await api.GET('/vendor/payouts')
  loading.value = false
  if (err || !data) {
    error.value = errMessage(err, 'Failed to load payouts')
    return
  }
  payouts.value = data.items ?? []
}

onMounted(load)
</script>

<template>
  <div class="page">
    <div class="header">
      <h1>Payouts <span class="muted">({{ payouts.length }})</span></h1>
      <Button icon="pi pi-refresh" severity="secondary" text @click="load" />
    </div>
    <p class="muted">Disbursements of your net earnings from delivered orders, settled by the operator.</p>

    <Message v-if="error" severity="error" :closable="false" class="mb">{{ error }}</Message>

    <DataTable :value="payouts" :loading="loading" dataKey="id" stripedRows>
      <template #empty>No payouts yet.</template>
      <Column header="Reference"><template #body="{ data }">{{ data.reference || data.public_id?.slice(0, 8) + '…' }}</template></Column>
      <Column header="Amount"><template #body="{ data }">{{ data.amount }} {{ data.currency }}</template></Column>
      <Column header="Status"><template #body="{ data }"><Tag :value="data.status" :severity="sev(data.status)" /></template></Column>
    </DataTable>
  </div>
</template>

<style scoped>
.header { display: flex; align-items: center; justify-content: space-between; }
.header h1 { margin: 0; }
.muted { color: #64748b; }
.mb { margin-bottom: 1rem; }
</style>
