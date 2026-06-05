<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import DataTable from 'primevue/datatable'
import Column from 'primevue/column'
import Tag from 'primevue/tag'
import Button from 'primevue/button'
import ToggleSwitch from 'primevue/toggleswitch'
import Message from 'primevue/message'
import { api, errMessage } from '@/lib/client'
import type { components } from '@teggo/api/schema'

type Account = components['schemas']['AccountHealth']

const accounts = ref<Account[]>([])
const loading = ref(false)
const error = ref('')
const atRiskOnly = ref(false)

async function load() {
  loading.value = true
  error.value = ''
  const { data, error: err } = await api.GET('/admin/accounts/health', {
    params: { query: atRiskOnly.value ? { at_risk: true } : {} },
  })
  loading.value = false
  if (err || !data) {
    error.value = errMessage(err, 'Failed to load account health')
    return
  }
  accounts.value = data.items ?? []
}

const atRiskCount = computed(() => accounts.value.filter((a) => a.at_risk).length)

onMounted(load)
</script>

<template>
  <div class="page">
    <div class="header">
      <h1>Account health <span class="muted">({{ atRiskCount }} at risk)</span></h1>
      <div class="actions">
        <span class="filter"><ToggleSwitch v-model="atRiskOnly" @update:modelValue="load" inputId="ar" /><label for="ar">At-risk only</label></span>
        <Button icon="pi pi-refresh" severity="secondary" text @click="load" />
      </div>
    </div>
    <p class="muted">Accounts slipping below their own ordering pattern — overdue to reorder or buying less than the prior quarter.</p>

    <Message v-if="error" severity="error" :closable="false" class="mb">{{ error }}</Message>

    <DataTable :value="accounts" :loading="loading" dataKey="customer_id" stripedRows>
      <template #empty>No accounts with order history yet.</template>
      <Column field="name" header="Account" />
      <Column header="Health">
        <template #body="{ data }">
          <Tag v-if="data.at_risk" value="at risk" severity="danger" />
          <Tag v-else value="healthy" severity="success" />
        </template>
      </Column>
      <Column header="Last order"><template #body="{ data }">{{ data.days_since }} days ago</template></Column>
      <Column header="Cadence"><template #body="{ data }">{{ data.avg_interval_days > 0 ? `~${data.avg_interval_days}d` : '—' }}</template></Column>
      <Column header="Orders (90d / prior)"><template #body="{ data }">{{ data.recent_count }} / {{ data.prior_count }}</template></Column>
      <Column field="lifetime_value" header="Lifetime value" />
      <Column header="Why"><template #body="{ data }"><span class="muted">{{ data.reason }}</span></template></Column>
    </DataTable>
  </div>
</template>

<style scoped>
.header { display: flex; align-items: center; justify-content: space-between; }
.header h1 { margin: 0; }
.actions { display: flex; align-items: center; gap: 1rem; }
.filter { display: flex; align-items: center; gap: 0.5rem; font-size: 0.85rem; }
.muted { color: var(--p-text-muted-color, #64748b); font-weight: 400; }
.mb { margin-bottom: 1rem; }
</style>
