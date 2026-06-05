<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useToast } from 'primevue/usetoast'
import Card from 'primevue/card'
import Tag from 'primevue/tag'
import Button from 'primevue/button'
import Dialog from 'primevue/dialog'
import Select from 'primevue/select'
import InputText from 'primevue/inputtext'
import Textarea from 'primevue/textarea'
import Message from 'primevue/message'
import { api, errMessage } from '@/lib/client'
import type { components } from '@teggo/api/schema'

type Workflow = components['schemas']['WorkflowDefinition']
type Transition = components['schemas']['WfTransition']

// Built-in guards the engine registers, with their parameters — drives the
// structured guard editor (replaces hand-written JSON).
interface GuardDef { key: string; label: string; params: { name: string; placeholder?: string; default?: string }[] }
const GUARD_CATALOG: GuardDef[] = [
  {
    key: 'amount_lte_limit',
    label: 'Amount within limit',
    params: [
      { name: 'field', default: 'grand_total' },
      { name: 'limit_field', default: 'spending_limit' },
    ],
  },
  { key: 'has_permission', label: 'Actor has permission', params: [{ name: 'permission', placeholder: 'order.approve' }] },
]
function guardDef(key: string) {
  return GUARD_CATALOG.find((g) => g.key === key)
}

const workflows = ref<Workflow[]>([])
const loading = ref(false)
const error = ref('')
const toast = useToast()

const dialogOpen = ref(false)
const editing = ref<Transition | null>(null)
const saving = ref(false)
const formError = ref('')

interface GuardRow { key: string; params: Record<string, string> }
const guardRows = ref<GuardRow[]>([])
const actionsText = ref('[]')

async function load() {
  loading.value = true
  error.value = ''
  const { data, error: err } = await api.GET('/admin/workflows')
  loading.value = false
  if (err || !data) {
    error.value = errMessage(err, 'Failed to load workflows')
    return
  }
  workflows.value = data.items ?? []
}

function stateSeverity(wf: Workflow, code: string | null | undefined) {
  const s = (wf.states ?? []).find((x) => x.code === code)
  if (!s) return 'secondary'
  return s.is_initial ? 'info' : s.is_final ? 'contrast' : 'secondary'
}
function guardNames(t: Transition) {
  return ((t.guards ?? []) as Record<string, unknown>[]).map((g) => String(g.key))
}

function openEdit(t: Transition) {
  editing.value = t
  guardRows.value = ((t.guards ?? []) as Record<string, unknown>[]).map((g) => ({
    key: String(g.key ?? ''),
    params: Object.fromEntries(Object.entries((g.params as Record<string, unknown>) ?? {}).map(([k, v]) => [k, String(v)])),
  }))
  actionsText.value = JSON.stringify(t.actions ?? [], null, 2)
  formError.value = ''
  dialogOpen.value = true
}

function addGuard() {
  const def = GUARD_CATALOG[0]!
  const params: Record<string, string> = {}
  for (const p of def.params) params[p.name] = p.default ?? ''
  guardRows.value.push({ key: def.key, params })
}
function onGuardKey(row: GuardRow) {
  const def = guardDef(row.key)
  const params: Record<string, string> = {}
  for (const p of def?.params ?? []) params[p.name] = row.params[p.name] ?? p.default ?? ''
  row.params = params
}
function removeGuard(i: number) {
  guardRows.value.splice(i, 1)
}

async function save() {
  if (!editing.value) return
  if (guardRows.value.some((g) => !g.key)) {
    formError.value = 'Every guard needs a type.'
    return
  }
  let actions: unknown
  try {
    actions = JSON.parse(actionsText.value)
  } catch {
    formError.value = 'Actions must be a valid JSON array.'
    return
  }
  formError.value = ''
  saving.value = true
  const { error: err } = await api.PATCH('/admin/workflow-transitions/{id}', {
    params: { path: { id: editing.value.id } },
    body: {
      guards: guardRows.value.map((g) => ({ key: g.key, params: g.params })),
      actions: actions as Record<string, unknown>[],
    },
  })
  saving.value = false
  if (err) {
    formError.value = errMessage(err, 'Update failed')
    return
  }
  toast.add({ severity: 'success', summary: 'Transition updated', life: 2500 })
  dialogOpen.value = false
  load()
}

onMounted(load)
</script>

<template>
  <div class="page">
    <div class="header">
      <h1>Workflows</h1>
      <Button icon="pi pi-refresh" severity="secondary" text @click="load" />
    </div>
    <p class="muted">Entity lifecycles are config-driven. The map shows each state and the transitions between them; edit a transition to change its guards/actions without a deploy.</p>

    <Message v-if="error" severity="error" :closable="false" class="mb">{{ error }}</Message>

    <Card v-for="wf in workflows" :key="wf.id" class="wf">
      <template #title>
        <div class="wf-head">
          <span>{{ wf.name }} <span class="muted">({{ wf.entity_type }})</span></span>
          <Tag :value="wf.is_active ? 'active' : 'inactive'" :severity="wf.is_active ? 'success' : 'secondary'" />
        </div>
      </template>
      <template #content>
        <div class="legend">
          <span class="muted">States:</span>
          <Tag v-for="s in wf.states" :key="s.id" :value="s.label"
               :severity="s.is_initial ? 'info' : s.is_final ? 'contrast' : 'secondary'" />
        </div>

        <div class="map">
          <div v-for="t in wf.transitions" :key="t.id" class="edge">
            <div class="edge-main">
              <Tag :value="t.from || 'any'" :severity="stateSeverity(wf, t.from)" />
              <span class="arrow">→</span>
              <Tag :value="t.to" :severity="stateSeverity(wf, t.to)" />
              <span class="tcode">{{ t.code }}</span>
            </div>
            <div class="edge-meta">
              <Tag v-for="g in guardNames(t)" :key="g" :value="g" severity="warn" class="gtag" />
              <span v-if="!guardNames(t).length" class="muted small">no guards</span>
              <Button icon="pi pi-pencil" size="small" text rounded severity="secondary" @click="openEdit(t)" />
            </div>
          </div>
        </div>
      </template>
    </Card>

    <Dialog v-model:visible="dialogOpen" modal :header="`Edit transition: ${editing?.code}`" :style="{ width: '40rem' }">
      <Message v-if="formError" severity="error" :closable="false" class="mb">{{ formError }}</Message>
      <p v-if="editing" class="muted">
        {{ editing.from || 'any' }} → <strong>{{ editing.to }}</strong>
      </p>

      <div class="builder">
        <div class="bhead"><span>Guards <span class="muted">(all must pass)</span></span><Button label="Add guard" icon="pi pi-plus" size="small" text @click="addGuard" /></div>
        <div v-for="(g, i) in guardRows" :key="i" class="guard">
          <div class="row">
            <Select v-model="g.key" :options="GUARD_CATALOG" optionLabel="label" optionValue="key" class="grow" @change="onGuardKey(g)" />
            <Button icon="pi pi-times" text rounded severity="danger" @click="removeGuard(i)" />
          </div>
          <div v-if="guardDef(g.key)?.params.length" class="params">
            <div v-for="p in guardDef(g.key)!.params" :key="p.name" class="field">
              <label>{{ p.name }}</label>
              <InputText :modelValue="g.params[p.name] ?? ''" :placeholder="p.placeholder" @update:modelValue="g.params[p.name] = $event as string" />
            </div>
          </div>
        </div>
        <p v-if="!guardRows.length" class="muted small">No guards — this transition is always allowed.</p>
      </div>

      <div class="field adv">
        <label>Actions (advanced JSON)</label>
        <Textarea v-model="actionsText" rows="3" class="mono" autoResize />
        <small class="muted">Rarely used; e.g. [{"key":"notify","params":{}}]</small>
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
.muted { color: var(--p-text-muted-color, #64748b); font-weight: 400; }
.small { font-size: 0.8rem; }
.mb { margin-bottom: 1rem; }
.wf { margin: 1rem 0; }
.wf-head { display: flex; align-items: center; justify-content: space-between; }
.legend { display: flex; flex-wrap: wrap; align-items: center; gap: 0.4rem; margin-bottom: 0.9rem; }
.map { display: grid; grid-template-columns: repeat(auto-fill, minmax(18rem, 1fr)); gap: 0.6rem; }
.edge { border: 1px solid var(--p-surface-200, #e2e8f0); border-radius: 8px; padding: 0.6rem 0.7rem; }
.edge-main { display: flex; align-items: center; gap: 0.45rem; }
.arrow { color: var(--p-text-muted-color, #64748b); }
.tcode { margin-left: auto; font-size: 0.78rem; color: var(--p-text-muted-color, #64748b); font-family: ui-monospace, monospace; }
.edge-meta { display: flex; align-items: center; gap: 0.35rem; margin-top: 0.5rem; flex-wrap: wrap; }
.gtag { font-size: 0.7rem; }
.builder { border-top: 1px solid var(--p-surface-200, #e2e8f0); padding-top: 0.8rem; }
.bhead { display: flex; align-items: center; justify-content: space-between; font-weight: 600; font-size: 0.9rem; margin-bottom: 0.5rem; }
.guard { border: 1px solid var(--p-surface-200, #e2e8f0); border-radius: 8px; padding: 0.6rem; margin-bottom: 0.6rem; }
.row { display: flex; align-items: center; gap: 0.5rem; }
.grow { flex: 1; }
.params { padding-left: 0.5rem; margin-top: 0.5rem; }
.field { display: flex; flex-direction: column; gap: 0.35rem; margin-bottom: 0.6rem; }
.field label { font-size: 0.8rem; font-weight: 600; }
.adv { margin-top: 1rem; }
.mono :deep(textarea) { font-family: ui-monospace, monospace; font-size: 0.8rem; }
</style>
