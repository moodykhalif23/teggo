<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { useToast } from 'primevue/usetoast'
import DataTable from 'primevue/datatable'
import Column from 'primevue/column'
import Button from 'primevue/button'
import Dialog from 'primevue/dialog'
import InputText from 'primevue/inputtext'
import InputNumber from 'primevue/inputnumber'
import Select from 'primevue/select'
import Message from 'primevue/message'
import { api, errMessage } from '@/lib/client'
import { useCustomerOptions } from '@/composables/useRecordOptions'
import { useCurrency } from '@/composables/useCurrency'
import type { components } from '@teggo/api/schema'
import PageHeader from '@/components/PageHeader.vue'
import EmptyState from '@/components/EmptyState.vue'

type Opportunity = components['schemas']['Opportunity']
type PipelineStage = components['schemas']['PipelineStage']

const opps = ref<Opportunity[]>([])
const stages = ref<PipelineStage[]>([])
const loading = ref(false)
const error = ref('')
const dialogOpen = ref(false)
const saving = ref(false)
const toast = useToast()

const form = reactive<{ customer_id: number | null; name: string; amount: number }>({
  customer_id: null,
  name: '',
  amount: 0,
})

const { customers, customersLoaded, loadCustomers } = useCustomerOptions()
// Currency follows the org's configured default (Settings) — the server stamps
// it on create, and we show it here so the amount reads in the right units.
const { currency, money } = useCurrency()

async function loadStages() {
  const { data } = await api.GET('/admin/pipelines')
  if (data?.items?.length) stages.value = data.items[0].stages ?? []
}

async function load() {
  loading.value = true
  error.value = ''
  await loadStages()
  const { data, error: err } = await api.GET('/admin/opportunities')
  loading.value = false
  if (err || !data) {
    error.value = errMessage(err, 'Failed to load opportunities')
    return
  }
  opps.value = data.items ?? []
}

function stageLabel(id: number) {
  return stages.value.find((s) => s.id === id)?.label ?? String(id)
}

function openCreate() {
  Object.assign(form, { customer_id: null, name: '', amount: 0 })
  dialogOpen.value = true
  loadCustomers()
}

async function save() {
  if (!form.customer_id || !form.name) {
    toast.add({ severity: 'warn', summary: 'Missing fields', detail: 'Customer ID and name are required', life: 3000 })
    return
  }
  saving.value = true
  const { error: err } = await api.POST('/admin/opportunities', {
    // Currency intentionally omitted — the server applies the org default.
    body: { customer_id: form.customer_id, name: form.name, amount: form.amount.toFixed(4) },
  })
  saving.value = false
  if (err) {
    toast.add({ severity: 'error', summary: 'Save failed', detail: errMessage(err), life: 4000 })
    return
  }
  dialogOpen.value = false
  load()
}

async function moveStage(opp: Opportunity, stageId: number) {
  const { data, error: err } = await api.PATCH('/admin/opportunities/{id}/stage', {
    params: { path: { id: opp.id } },
    body: { stage_id: stageId },
  })
  if (err || !data) {
    toast.add({ severity: 'error', summary: 'Move failed', detail: errMessage(err), life: 4000 })
    load()
    return
  }
  opp.stage_id = data.stage_id
  toast.add({ severity: 'success', summary: 'Stage updated', detail: stageLabel(data.stage_id), life: 2000 })
}

onMounted(load)
</script>

<template>
  <div class="page">
    <PageHeader title="Opportunities" :meta="opps.length">
      <template #actions>
        <Button icon="pi pi-refresh" severity="secondary" text @click="load" />
        <Button icon="pi pi-plus" label="New opportunity" @click="openCreate" />
      </template>
    </PageHeader>

    <Message v-if="error" severity="error" :closable="false" class="mb">{{ error }}</Message>

    <DataTable :value="opps" :loading="loading" paginator :rows="10" dataKey="id" stripedRows>
      <template #empty>
        <EmptyState icon="pi pi-briefcase" title="No opportunities yet" message="Promote a qualified lead, or add an opportunity to track a deal toward close.">
          <Button icon="pi pi-plus" label="New opportunity" @click="openCreate" />
        </EmptyState>
      </template>
      <Column field="name" header="Name" />
      <Column field="customer_id" header="Customer" />
      <Column header="Amount">
        <template #body="{ data }">{{ money(data.amount, data.currency) }}</template>
      </Column>
      <Column header="Stage" style="width: 14rem">
        <template #body="{ data }">
          <Select
            :modelValue="data.stage_id"
            :options="stages"
            optionLabel="label"
            optionValue="id"
            @update:modelValue="moveStage(data, $event as number)"
          />
        </template>
      </Column>
      <Column header="Closed">
        <template #body="{ data }">
          <span v-if="data.closed_at" class="muted">{{ new Date(data.closed_at).toLocaleDateString() }}</span>
          <span v-else>—</span>
        </template>
      </Column>
    </DataTable>

    <Dialog v-model:visible="dialogOpen" modal header="New opportunity" :style="{ width: '30rem' }">
      <div class="field">
        <label>Customer</label>
        <Select
          v-model="form.customer_id"
          :options="customers"
          optionLabel="name"
          optionValue="id"
          filter
          filterPlaceholder="Search customers…"
          placeholder="Select a customer"
          :emptyMessage="customersLoaded ? 'No customers' : 'Loading…'"
          showClear
          fluid
        />
      </div>
      <div class="field"><label>Name</label><InputText v-model="form.name" /></div>
      <div class="field">
        <label>Amount <span v-if="currency" class="ccy">({{ currency }})</span></label>
        <InputNumber v-model="form.amount" :minFractionDigits="2" :maxFractionDigits="4" />
      </div>
      <template #footer>
        <Button label="Cancel" severity="secondary" text @click="dialogOpen = false" />
        <Button label="Create" :loading="saving" @click="save" />
      </template>
    </Dialog>
  </div>
</template>

<style scoped>
.mb { margin-bottom: 1rem; }
.field { display: flex; flex-direction: column; gap: 0.35rem; margin-bottom: 0.9rem; }
.field label { font-size: 0.85rem; font-weight: 600; }
.ccy { font-weight: 400; color: var(--p-text-muted-color, #64748b); }
</style>
