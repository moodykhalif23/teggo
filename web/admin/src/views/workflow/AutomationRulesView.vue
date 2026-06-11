<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { useToast } from 'primevue/usetoast'
import DataTable from 'primevue/datatable'
import Column from 'primevue/column'
import Tag from 'primevue/tag'
import Button from 'primevue/button'
import InputText from 'primevue/inputtext'
import Message from 'primevue/message'
import { VueFlow } from '@vue-flow/core'
import { Background } from '@vue-flow/background'
import { Controls } from '@vue-flow/controls'
import '@vue-flow/core/dist/style.css'
import '@vue-flow/core/dist/theme-default.css'
import '@vue-flow/controls/dist/style.css'
import { api, errMessage } from '@/lib/client'
import type { components } from '@teggo/api/schema'
import TriggerNode from './flow/TriggerNode.vue'
import ConditionsNode from './flow/ConditionsNode.vue'
import ActionNode from './flow/ActionNode.vue'
import {
  TRIGGER_EVENTS, ACTION_CATALOG, fieldsFor, coerce, uid,
  type Cond, type Act,
} from './flow/catalog'

type Rule = components['schemas']['AutomationRule']

const rules = ref<Rule[]>([])
const loading = ref(false)
const error = ref('')
const toast = useToast()

const mode = ref<'list' | 'edit'>('list')
const editingId = ref<number | null>(null)
const saving = ref(false)
const formError = ref('')

const name = ref('')
const triggerState = reactive({ event: 'order.status_changed' })
const conds = reactive<Cond[]>([])
const acts = reactive<Act[]>([])

const nodes = ref<any[]>([])
const edges = ref<any[]>([])

// ---- list ----------------------------------------------------------------
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

// ---- builder state -------------------------------------------------------
function addCond() {
  conds.push({ field: fieldsFor(triggerState.event)[0] ?? '', op: 'eq', value: '' })
}
function removeCond(i: number) {
  conds.splice(i, 1)
}
function addAction() {
  acts.push({ uid: uid(), key: ACTION_CATALOG[0]!.key, params: {} })
  rebuildGraph()
}
function removeAction(u: string) {
  const i = acts.findIndex((a) => a.uid === u)
  if (i >= 0) acts.splice(i, 1)
  rebuildGraph()
}

// Build the Vue Flow graph from the rule model. Trigger → Conditions → Actions.
// Conditions live in one node (they are AND-ed); actions fan out (all run).
function rebuildGraph() {
  const ns: any[] = [
    { id: 'trigger', type: 'trigger', position: { x: 0, y: 170 }, data: { state: triggerState, options: TRIGGER_EVENTS } },
    {
      id: 'conditions', type: 'conditions', position: { x: 320, y: 100 },
      data: { conds, add: addCond, remove: removeCond, fields: () => fieldsFor(triggerState.event), trigger: () => triggerState.event },
    },
  ]
  const es: any[] = [{ id: 'e-tc', source: 'trigger', target: 'conditions', animated: true }]
  acts.forEach((a, i) => {
    ns.push({ id: 'act-' + a.uid, type: 'action', position: { x: 760, y: i * 200 }, data: { act: a, onRemove: () => removeAction(a.uid) } })
    es.push({ id: 'e-c-' + a.uid, source: 'conditions', target: 'act-' + a.uid, animated: true })
  })
  nodes.value = ns
  edges.value = es
}

function openCreate() {
  editingId.value = null
  name.value = ''
  triggerState.event = 'order.status_changed'
  conds.splice(0, conds.length)
  acts.splice(0, acts.length, { uid: uid(), key: ACTION_CATALOG[0]!.key, params: {} })
  formError.value = ''
  rebuildGraph()
  mode.value = 'edit'
}

function openEdit(r: Rule) {
  editingId.value = r.id
  name.value = r.name
  triggerState.event = r.trigger_event
  conds.splice(0, conds.length, ...((r.conditions ?? []) as Record<string, unknown>[]).map((c) => ({
    field: String(c.field ?? ''),
    op: String(c.op ?? 'eq'),
    value: c.value === undefined || c.value === null ? '' : String(c.value),
  })))
  acts.splice(0, acts.length, ...((r.actions ?? []) as Record<string, unknown>[]).map((a) => ({
    uid: uid(),
    key: String(a.key ?? ''),
    params: Object.fromEntries(Object.entries((a.params as Record<string, unknown>) ?? {}).map(([k, v]) => [k, String(v)])),
  })))
  formError.value = ''
  rebuildGraph()
  mode.value = 'edit'
}

function backToList() {
  mode.value = 'list'
}

async function save() {
  if (!name.value || !triggerState.event) {
    formError.value = 'Name and trigger event are required.'
    return
  }
  if (conds.some((c) => !c.field)) {
    formError.value = 'Every condition needs a field.'
    return
  }
  if (!acts.length || acts.some((a) => !a.key)) {
    formError.value = 'Add at least one action, and every action needs a type.'
    return
  }
  formError.value = ''
  saving.value = true
  const body = {
    name: name.value,
    trigger_event: triggerState.event,
    conditions: conds.map((c) => ({ field: c.field, op: c.op, value: coerce(c.value) })),
    actions: acts.map((a) => ({ key: a.key, params: a.params })),
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
  mode.value = 'list'
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
    <!-- ───────────── LIST ───────────── -->
    <template v-if="mode === 'list'">
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
    </template>

    <!-- ───────────── FLOW EDITOR ───────────── -->
    <template v-else>
      <div class="header">
        <div class="title-line">
          <Button icon="pi pi-arrow-left" severity="secondary" text rounded @click="backToList" />
          <h1>{{ editingId ? 'Edit rule' : 'New rule' }}</h1>
        </div>
        <div class="actions">
          <Button label="Add action" icon="pi pi-plus" severity="secondary" outlined @click="addAction" />
          <Button label="Cancel" severity="secondary" text @click="backToList" />
          <Button label="Save" icon="pi pi-check" :loading="saving" @click="save" />
        </div>
      </div>

      <Message v-if="formError" severity="error" :closable="false" class="mb">{{ formError }}</Message>

      <div class="field name-field"><label>Rule name</label><InputText v-model="name" placeholder="e.g. Email buyer when order ships" /></div>

      <div class="flow-wrap">
        <VueFlow
          v-model:nodes="nodes"
          v-model:edges="edges"
          :nodes-connectable="false"
          :min-zoom="0.4"
          :max-zoom="1.5"
          fit-view-on-init
          class="flow"
        >
          <Background :gap="18" pattern-color="#dbe2ea" />
          <Controls :show-interactive="false" />
          <template #node-trigger="p"><TriggerNode :data="p.data" /></template>
          <template #node-conditions="p"><ConditionsNode :data="p.data" /></template>
          <template #node-action="p"><ActionNode :data="p.data" /></template>
        </VueFlow>
      </div>
    </template>
  </div>
</template>

<style scoped>
.header { display: flex; align-items: center; justify-content: space-between; }
.header h1 { margin: 0; }
.title-line { display: flex; align-items: center; gap: 0.5rem; }
.actions { display: flex; gap: 0.5rem; }
.muted { color: var(--p-text-muted-color, #64748b); font-weight: 400; }
.mb { margin-bottom: 1rem; }
.field { display: flex; flex-direction: column; gap: 0.35rem; }
.field label { font-size: 0.85rem; font-weight: 600; }
.name-field { max-width: 28rem; margin: 0.75rem 0 1rem; }

.flow-wrap { height: 70vh; border: 1px solid var(--p-surface-200, #e2e8f0); border-radius: 10px; overflow: hidden; background: var(--p-surface-50, #f8fafc); }
.flow { width: 100%; height: 100%; }
</style>
