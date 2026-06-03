<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { useToast } from 'primevue/usetoast'
import DataTable from 'primevue/datatable'
import Column from 'primevue/column'
import Tag from 'primevue/tag'
import Button from 'primevue/button'
import Dialog from 'primevue/dialog'
import InputText from 'primevue/inputtext'
import Textarea from 'primevue/textarea'
import Message from 'primevue/message'
import { api, errMessage } from '@/lib/client'
import type { components } from '@teggo/api/schema'

type Rule = components['schemas']['AutomationRule']

const rules = ref<Rule[]>([])
const loading = ref(false)
const error = ref('')
const toast = useToast()

const dialogOpen = ref(false)
const editingId = ref<number | null>(null)
const saving = ref(false)
const formError = ref('')
const form = reactive({ name: '', trigger_event: '', conditions: '[]', actions: '[]' })

async function load() {
  loading.value = true
  error.value = ''
  const { data, error: err } = await api.GET('/admin/automation-rules')
  loading.value = false
  if (err || !data) {
    error.value = errMessage(err, 'Failed to load rules')
    return
  }
  rules.value = data.items ?? []
}

function openCreate() {
  editingId.value = null
  Object.assign(form, { name: '', trigger_event: 'order.status_changed', conditions: '[]', actions: '[]' })
  formError.value = ''
  dialogOpen.value = true
}
function openEdit(r: Rule) {
  editingId.value = r.id
  Object.assign(form, {
    name: r.name,
    trigger_event: r.trigger_event,
    conditions: JSON.stringify(r.conditions ?? [], null, 2),
    actions: JSON.stringify(r.actions ?? [], null, 2),
  })
  formError.value = ''
  dialogOpen.value = true
}

async function save() {
  let conditions: unknown, actions: unknown
  try {
    conditions = JSON.parse(form.conditions)
    actions = JSON.parse(form.actions)
  } catch {
    formError.value = 'Conditions and actions must be valid JSON arrays.'
    return
  }
  if (!form.name || !form.trigger_event) {
    formError.value = 'Name and trigger event are required.'
    return
  }
  saving.value = true
  const body = {
    name: form.name,
    trigger_event: form.trigger_event,
    conditions: conditions as Record<string, unknown>[],
    actions: actions as Record<string, unknown>[],
  }
  const { error: err } = editingId.value
    ? await api.PATCH('/admin/automation-rules/{id}', { params: { path: { id: editingId.value } }, body })
    : await api.POST('/admin/automation-rules', { body })
  saving.value = false
  if (err) {
    formError.value = errMessage(err, 'Save failed')
    return
  }
  toast.add({ severity: 'success', summary: 'Rule saved', life: 2500 })
  dialogOpen.value = false
  load()
}

async function toggle(r: Rule) {
  const { error: err } = await api.PATCH('/admin/automation-rules/{id}', {
    params: { path: { id: r.id } },
    body: { name: r.name, trigger_event: r.trigger_event, is_active: !r.is_active },
  })
  if (err) {
    toast.add({ severity: 'error', summary: 'Toggle failed', detail: errMessage(err), life: 4000 })
    return
  }
  load()
}

function summary(v: unknown) {
  const s = JSON.stringify(v ?? [])
  return s === '[]' ? '—' : s
}

onMounted(load)
</script>

<template>
  <div class="page">
    <div class="header">
      <h1>Automation rules <span class="muted">({{ rules.length }})</span></h1>
      <div class="actions">
        <Button icon="pi pi-refresh" severity="secondary" text @click="load" />
        <Button icon="pi pi-plus" label="New rule" @click="openCreate" />
      </div>
    </div>
    <p class="muted">When an event fires and conditions match, the actions run as background jobs.</p>

    <Message v-if="error" severity="error" :closable="false" class="mb">{{ error }}</Message>

    <DataTable :value="rules" :loading="loading" dataKey="id" stripedRows>
      <template #empty>No automation rules yet.</template>
      <Column field="name" header="Name" />
      <Column field="trigger_event" header="Trigger" />
      <Column header="Conditions"><template #body="{ data }"><code>{{ summary(data.conditions) }}</code></template></Column>
      <Column header="Actions"><template #body="{ data }"><code>{{ summary(data.actions) }}</code></template></Column>
      <Column header="Status">
        <template #body="{ data }"><Tag :value="data.is_active ? 'active' : 'off'" :severity="data.is_active ? 'success' : 'secondary'" /></template>
      </Column>
      <Column header="" style="width: 11rem">
        <template #body="{ data }">
          <Button :label="data.is_active ? 'Disable' : 'Enable'" size="small" severity="secondary" outlined @click="toggle(data)" />
          <Button icon="pi pi-pencil" severity="secondary" text rounded @click="openEdit(data)" />
        </template>
      </Column>
    </DataTable>

    <Dialog v-model:visible="dialogOpen" modal :header="editingId ? 'Edit rule' : 'New rule'" :style="{ width: '38rem' }">
      <Message v-if="formError" severity="error" :closable="false" class="mb">{{ formError }}</Message>
      <div class="field"><label>Name</label><InputText v-model="form.name" /></div>
      <div class="field">
        <label>Trigger event</label>
        <InputText v-model="form.trigger_event" />
        <small class="muted">e.g. order.status_changed, quote.created, schedule.hourly</small>
      </div>
      <div class="field">
        <label>Conditions (JSON array)</label>
        <Textarea v-model="form.conditions" rows="3" class="mono" autoResize />
        <small class="muted">e.g. [{"field":"status","op":"eq","value":"cancelled"}]</small>
      </div>
      <div class="field">
        <label>Actions (JSON array)</label>
        <Textarea v-model="form.actions" rows="3" class="mono" autoResize />
        <small class="muted">e.g. [{"key":"email_customer","params":{"template":"order_status_update"}}]</small>
      </div>
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
.field { display: flex; flex-direction: column; gap: 0.35rem; margin-bottom: 1rem; }
.field label { font-size: 0.85rem; font-weight: 600; }
code { font-size: 0.8rem; }
.mono :deep(textarea) { font-family: ui-monospace, monospace; font-size: 0.8rem; }
</style>
