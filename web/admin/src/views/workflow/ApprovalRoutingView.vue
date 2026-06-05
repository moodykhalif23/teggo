<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { useToast } from 'primevue/usetoast'
import DataTable from 'primevue/datatable'
import Column from 'primevue/column'
import Tag from 'primevue/tag'
import Button from 'primevue/button'
import Dialog from 'primevue/dialog'
import InputText from 'primevue/inputtext'
import InputNumber from 'primevue/inputnumber'
import Select from 'primevue/select'
import Message from 'primevue/message'
import { api, errMessage } from '@/lib/client'
import type { components } from '@teggo/api/schema'

type Rule = components['schemas']['ApprovalRoutingRule']

const rules = ref<Rule[]>([])
const loading = ref(false)
const error = ref('')
const toast = useToast()

const dialogOpen = ref(false)
const saving = ref(false)
const formError = ref('')
const form = reactive({ min_amount: '0', max_amount: '', required_role: 'approver', sort_order: 0 })
const roles = [
  { label: 'Approver (or higher)', value: 'approver' },
  { label: 'Admin only', value: 'admin' },
]

async function load() {
  loading.value = true
  error.value = ''
  const { data, error: err } = await api.GET('/admin/approval-routing-rules')
  loading.value = false
  if (err || !data) {
    error.value = errMessage(err, 'Failed to load approval routing')
    return
  }
  rules.value = data.items ?? []
}

function openCreate() {
  Object.assign(form, { min_amount: '0', max_amount: '', required_role: 'approver', sort_order: rules.value.length })
  formError.value = ''
  dialogOpen.value = true
}

async function save() {
  if (form.min_amount.trim() === '') form.min_amount = '0'
  saving.value = true
  const { error: err } = await api.POST('/admin/approval-routing-rules', {
    body: {
      min_amount: form.min_amount,
      max_amount: form.max_amount.trim() === '' ? null : form.max_amount,
      required_role: form.required_role as Rule['required_role'],
      sort_order: form.sort_order,
    },
  })
  saving.value = false
  if (err) {
    formError.value = errMessage(err, 'Save failed')
    return
  }
  toast.add({ severity: 'success', summary: 'Tier added', life: 2500 })
  dialogOpen.value = false
  load()
}

async function remove(r: Rule) {
  const { error: err } = await api.DELETE('/admin/approval-routing-rules/{id}', { params: { path: { id: r.id } } })
  if (err) {
    toast.add({ severity: 'error', summary: 'Delete failed', detail: errMessage(err), life: 4000 })
    return
  }
  load()
}

function band(r: Rule) {
  const lo = r.min_amount
  return r.max_amount ? `${lo} – ${r.max_amount}` : `${lo} and up`
}

onMounted(load)
</script>

<template>
  <div class="page">
    <div class="header">
      <h1>Approval routing <span class="muted">({{ rules.length }})</span></h1>
      <div class="actions">
        <Button icon="pi pi-refresh" severity="secondary" text @click="load" />
        <Button icon="pi pi-plus" label="Add tier" @click="openCreate" />
      </div>
    </div>
    <p class="muted">
      When a held order is approved by a buyer's company, the order amount must fall to an approver whose role
      meets the tier below. With no tiers, any approver or admin may release a held order.
    </p>

    <Message v-if="error" severity="error" :closable="false" class="mb">{{ error }}</Message>

    <DataTable :value="rules" :loading="loading" dataKey="id" stripedRows>
      <template #empty>No approval tiers — any approver/admin can release held orders.</template>
      <Column header="Amount band"><template #body="{ data }">{{ band(data) }}</template></Column>
      <Column header="Requires">
        <template #body="{ data }"><Tag :value="data.required_role" :severity="data.required_role === 'admin' ? 'warn' : 'info'" /></template>
      </Column>
      <Column field="sort_order" header="Order" />
      <Column header="" style="width: 5rem">
        <template #body="{ data }">
          <Button icon="pi pi-trash" text rounded severity="danger" @click="remove(data)" />
        </template>
      </Column>
    </DataTable>

    <Dialog v-model:visible="dialogOpen" modal header="Add approval tier" :style="{ width: '30rem' }">
      <Message v-if="formError" severity="error" :closable="false" class="mb">{{ formError }}</Message>
      <div class="grid2">
        <div class="field"><label>Min amount</label><InputText v-model="form.min_amount" /></div>
        <div class="field"><label>Max amount <span class="muted">(blank = no ceiling)</span></label><InputText v-model="form.max_amount" /></div>
      </div>
      <div class="field"><label>Required role</label><Select v-model="form.required_role" :options="roles" optionLabel="label" optionValue="value" /></div>
      <div class="field"><label>Sort order</label><InputNumber v-model="form.sort_order" :min="0" showButtons /></div>
      <template #footer>
        <Button label="Cancel" severity="secondary" text @click="dialogOpen = false" />
        <Button label="Save" :loading="saving" @click="save" />
      </template>
    </Dialog>
  </div>
</template>

<style scoped>
.header { display: flex; align-items: center; justify-content: space-between; }
.header h1 { margin: 0; }
.actions { display: flex; gap: 0.5rem; }
.muted { color: var(--p-text-muted-color, #64748b); font-weight: 400; }
.mb { margin-bottom: 1rem; }
.grid2 { display: grid; grid-template-columns: 1fr 1fr; gap: 1rem; }
.field { display: flex; flex-direction: column; gap: 0.35rem; margin-bottom: 1rem; }
.field label { font-size: 0.85rem; font-weight: 600; }
</style>
