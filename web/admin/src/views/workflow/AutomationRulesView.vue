<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { useToast } from 'primevue/usetoast'
import DataTable from 'primevue/datatable'
import Column from 'primevue/column'
import Tag from 'primevue/tag'
import Button from 'primevue/button'
import Dialog from 'primevue/dialog'
import InputText from 'primevue/inputtext'
import Select from 'primevue/select'
import Message from 'primevue/message'
import { api, errMessage } from '@/lib/client'
import type { components } from '@teggo/api/schema'

type Rule = components['schemas']['AutomationRule']

// Catalogs that drive the builder. Trigger events + per-event payload fields are
// what the backend dispatcher emits; action keys are the registered actions.
const TRIGGER_EVENTS = ['order.status_changed', 'quote.expired', 'schedule.hourly', 'schedule.daily']
const FIELDS_BY_EVENT: Record<string, string[]> = {
  'order.status_changed': ['status', 'from', 'to', 'grand_total', 'customer_id', 'order_number'],
  'quote.expired': ['quote_number', 'customer_id'],
}
const OPS = [
  { label: '= equals', value: 'eq' },
  { label: '≠ not equals', value: 'ne' },
  { label: '> greater than', value: 'gt' },
  { label: '≥ at least', value: 'gte' },
  { label: '< less than', value: 'lt' },
  { label: '≤ at most', value: 'lte' },
]
interface ActionDef { key: string; label: string; params: { name: string; label: string; placeholder?: string }[] }
const ACTION_CATALOG: ActionDef[] = [
  { key: 'email_customer', label: 'Email the customer', params: [{ name: 'template', label: 'Email template', placeholder: 'order_status_update' }] },
  { key: 'expire_quotes', label: 'Expire stale quotes', params: [] },
  { key: 'mark_overdue', label: 'Mark invoices overdue + dun', params: [] },
  { key: 'quote_followup', label: 'Follow up on expiring quotes', params: [{ name: 'within_days', label: 'Days before expiry', placeholder: '3' }] },
  { key: 'cart_recovery', label: 'Recover abandoned carts', params: [{ name: 'idle_hours', label: 'Idle hours before nudge', placeholder: '24' }] },
]
function actionDef(key: string) {
  return ACTION_CATALOG.find((a) => a.key === key)
}

const rules = ref<Rule[]>([])
const loading = ref(false)
const error = ref('')
const toast = useToast()

const dialogOpen = ref(false)
const editingId = ref<number | null>(null)
const saving = ref(false)
const formError = ref('')
const form = reactive({ name: '', trigger_event: 'order.status_changed' })

interface Cond { field: string; op: string; value: string }
interface Act { key: string; params: Record<string, string> }
const conds = ref<Cond[]>([])
const acts = ref<Act[]>([])

function fieldOptions() {
  return FIELDS_BY_EVENT[form.trigger_event] ?? []
}

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
  Object.assign(form, { name: '', trigger_event: 'order.status_changed' })
  conds.value = []
  acts.value = []
  formError.value = ''
  dialogOpen.value = true
}

function openEdit(r: Rule) {
  editingId.value = r.id
  Object.assign(form, { name: r.name, trigger_event: r.trigger_event })
  conds.value = ((r.conditions ?? []) as Record<string, unknown>[]).map((c) => ({
    field: String(c.field ?? ''),
    op: String(c.op ?? 'eq'),
    value: c.value === undefined || c.value === null ? '' : String(c.value),
  }))
  acts.value = ((r.actions ?? []) as Record<string, unknown>[]).map((a) => ({
    key: String(a.key ?? ''),
    params: Object.fromEntries(Object.entries((a.params as Record<string, unknown>) ?? {}).map(([k, v]) => [k, String(v)])),
  }))
  formError.value = ''
  dialogOpen.value = true
}

function addCond() {
  conds.value.push({ field: fieldOptions()[0] ?? '', op: 'eq', value: '' })
}
function removeCond(i: number) {
  conds.value.splice(i, 1)
}
function addAct() {
  acts.value.push({ key: ACTION_CATALOG[0]!.key, params: {} })
}
function removeAct(i: number) {
  acts.value.splice(i, 1)
}

// Preserve JSON types: numeric strings -> number, true/false -> boolean.
function coerce(v: string): unknown {
  const t = v.trim()
  if (t === '') return ''
  if (/^-?\d+(\.\d+)?$/.test(t)) return Number(t)
  if (t === 'true') return true
  if (t === 'false') return false
  return t
}

async function save() {
  if (!form.name || !form.trigger_event) {
    formError.value = 'Name and trigger event are required.'
    return
  }
  if (conds.value.some((c) => !c.field)) {
    formError.value = 'Every condition needs a field.'
    return
  }
  if (acts.value.some((a) => !a.key)) {
    formError.value = 'Every action needs a type.'
    return
  }
  formError.value = ''
  saving.value = true
  const body = {
    name: form.name,
    trigger_event: form.trigger_event,
    conditions: conds.value.map((c) => ({ field: c.field, op: c.op, value: coerce(c.value) })),
    actions: acts.value.map((a) => ({ key: a.key, params: a.params })),
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

function condSummary(v: unknown) {
  const arr = (v ?? []) as Record<string, unknown>[]
  if (!arr.length) return 'always'
  return arr.map((c) => `${c.field} ${c.op} ${c.value}`).join(', ')
}
function actSummary(v: unknown) {
  const arr = (v ?? []) as Record<string, unknown>[]
  if (!arr.length) return '—'
  return arr.map((a) => String(a.key)).join(', ')
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
    <p class="muted">When an event fires and all conditions match, the actions run as background jobs.</p>

    <Message v-if="error" severity="error" :closable="false" class="mb">{{ error }}</Message>

    <DataTable :value="rules" :loading="loading" dataKey="id" stripedRows>
      <template #empty>No automation rules yet.</template>
      <Column field="name" header="Name" />
      <Column field="trigger_event" header="Trigger" />
      <Column header="When"><template #body="{ data }"><span class="muted">{{ condSummary(data.conditions) }}</span></template></Column>
      <Column header="Then"><template #body="{ data }">{{ actSummary(data.actions) }}</template></Column>
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

    <Dialog v-model:visible="dialogOpen" modal :header="editingId ? 'Edit rule' : 'New rule'" :style="{ width: '44rem' }">
      <Message v-if="formError" severity="error" :closable="false" class="mb">{{ formError }}</Message>
      <div class="grid2">
        <div class="field"><label>Name</label><InputText v-model="form.name" /></div>
        <div class="field">
          <label>When this happens</label>
          <Select v-model="form.trigger_event" :options="TRIGGER_EVENTS" editable placeholder="Select or type an event" />
        </div>
      </div>

      <div class="builder">
        <div class="bhead"><span>Only if <span class="muted">(all must match; none = always)</span></span><Button label="Add condition" icon="pi pi-plus" size="small" text @click="addCond" /></div>
        <div v-for="(c, i) in conds" :key="i" class="row">
          <Select v-model="c.field" :options="fieldOptions()" editable placeholder="field" class="grow" />
          <Select v-model="c.op" :options="OPS" optionLabel="label" optionValue="value" class="op" />
          <InputText v-model="c.value" placeholder="value" class="grow" />
          <Button icon="pi pi-times" text rounded severity="danger" @click="removeCond(i)" />
        </div>
        <p v-if="!conds.length" class="muted empty">No conditions — the rule runs on every {{ form.trigger_event }} event.</p>
      </div>

      <div class="builder">
        <div class="bhead"><span>Then do</span><Button label="Add action" icon="pi pi-plus" size="small" text @click="addAct" /></div>
        <div v-for="(a, i) in acts" :key="i" class="actrow">
          <div class="row">
            <Select
              v-model="a.key"
              :options="ACTION_CATALOG"
              optionLabel="label"
              optionValue="key"
              placeholder="action"
              class="grow"
            />
            <Button icon="pi pi-times" text rounded severity="danger" @click="removeAct(i)" />
          </div>
          <div v-if="actionDef(a.key)?.params.length" class="params">
            <div v-for="p in actionDef(a.key)!.params" :key="p.name" class="field">
              <label>{{ p.label }}</label>
              <InputText :modelValue="a.params[p.name] ?? ''" :placeholder="p.placeholder" @update:modelValue="a.params[p.name] = $event as string" />
            </div>
          </div>
        </div>
        <p v-if="!acts.length" class="muted empty">No actions yet — add at least one.</p>
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
.grid2 { display: grid; grid-template-columns: 1fr 1fr; gap: 1rem; }
.field { display: flex; flex-direction: column; gap: 0.35rem; margin-bottom: 1rem; }
.field label { font-size: 0.85rem; font-weight: 600; }
.builder { border-top: 1px solid var(--p-surface-200, #e2e8f0); padding-top: 0.8rem; margin-top: 0.4rem; }
.bhead { display: flex; align-items: center; justify-content: space-between; font-weight: 600; font-size: 0.9rem; margin-bottom: 0.5rem; }
.row { display: flex; align-items: center; gap: 0.5rem; margin-bottom: 0.5rem; }
.grow { flex: 1; }
.op { width: 11rem; }
.actrow { border: 1px solid var(--p-surface-200, #e2e8f0); border-radius: 8px; padding: 0.6rem; margin-bottom: 0.6rem; }
.params { padding-left: 0.5rem; }
.empty { font-size: 0.85rem; }
</style>
