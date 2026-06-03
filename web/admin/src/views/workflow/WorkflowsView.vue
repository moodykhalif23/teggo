<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useToast } from 'primevue/usetoast'
import Card from 'primevue/card'
import DataTable from 'primevue/datatable'
import Column from 'primevue/column'
import Tag from 'primevue/tag'
import Button from 'primevue/button'
import Dialog from 'primevue/dialog'
import Textarea from 'primevue/textarea'
import Message from 'primevue/message'
import { api, errMessage } from '@/lib/client'
import type { components } from '@teggo/api/schema'

type Workflow = components['schemas']['WorkflowDefinition']
type Transition = components['schemas']['WfTransition']

const workflows = ref<Workflow[]>([])
const loading = ref(false)
const error = ref('')
const toast = useToast()

const dialogOpen = ref(false)
const editing = ref<Transition | null>(null)
const guardsText = ref('[]')
const actionsText = ref('[]')
const saving = ref(false)
const formError = ref('')

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

function openEdit(t: Transition) {
  editing.value = t
  guardsText.value = JSON.stringify(t.guards ?? [], null, 2)
  actionsText.value = JSON.stringify(t.actions ?? [], null, 2)
  formError.value = ''
  dialogOpen.value = true
}

async function save() {
  if (!editing.value) return
  let guards: unknown, actions: unknown
  try {
    guards = JSON.parse(guardsText.value)
    actions = JSON.parse(actionsText.value)
  } catch {
    formError.value = 'Guards and actions must be valid JSON arrays.'
    return
  }
  saving.value = true
  const { error: err } = await api.PATCH('/admin/workflow-transitions/{id}', {
    params: { path: { id: editing.value.id } },
    body: { guards: guards as Record<string, unknown>[], actions: actions as Record<string, unknown>[] },
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

function preview(v: unknown) {
  const s = JSON.stringify(v ?? [])
  return s === '[]' ? '—' : s
}

onMounted(load)
</script>

<template>
  <div class="page">
    <div class="header">
      <h1>Workflows</h1>
      <Button icon="pi pi-refresh" severity="secondary" text @click="load" />
    </div>
    <p class="muted">Entity lifecycles are config-driven — edit a transition's guards/actions without a deploy.</p>

    <Message v-if="error" severity="error" :closable="false" class="mb">{{ error }}</Message>

    <Card v-for="wf in workflows" :key="wf.id" class="wf">
      <template #title>
        <div class="wf-head">
          <span>{{ wf.name }} <span class="muted">({{ wf.entity_type }})</span></span>
          <Tag :value="wf.is_active ? 'active' : 'inactive'" :severity="wf.is_active ? 'success' : 'secondary'" />
        </div>
      </template>
      <template #content>
        <div class="states">
          <Tag v-for="s in wf.states" :key="s.id" :value="s.label"
               :severity="s.is_initial ? 'info' : s.is_final ? 'contrast' : 'secondary'" />
        </div>
        <DataTable :value="wf.transitions" dataKey="id" stripedRows class="mt">
          <Column field="code" header="Transition" />
          <Column header="Move">
            <template #body="{ data }">
              <span class="muted">{{ data.from || 'any' }}</span> → <strong>{{ data.to }}</strong>
            </template>
          </Column>
          <Column header="Guards"><template #body="{ data }"><code>{{ preview(data.guards) }}</code></template></Column>
          <Column header="Actions"><template #body="{ data }"><code>{{ preview(data.actions) }}</code></template></Column>
          <Column header="" style="width: 5rem">
            <template #body="{ data }">
              <Button icon="pi pi-pencil" severity="secondary" text rounded @click="openEdit(data)" />
            </template>
          </Column>
        </DataTable>
      </template>
    </Card>

    <Dialog v-model:visible="dialogOpen" modal :header="`Edit transition: ${editing?.code}`" :style="{ width: '38rem' }">
      <Message v-if="formError" severity="error" :closable="false" class="mb">{{ formError }}</Message>
      <div class="field">
        <label>Guards (JSON array)</label>
        <Textarea v-model="guardsText" rows="5" class="mono" autoResize />
        <small class="muted">e.g. [{"key":"amount_lte_limit","params":{"field":"grand_total","limit_field":"spending_limit"}}]</small>
      </div>
      <div class="field">
        <label>Actions (JSON array)</label>
        <Textarea v-model="actionsText" rows="4" class="mono" autoResize />
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
.mb { margin-bottom: 1rem; }
.mt { margin-top: 0.75rem; }
.wf { margin: 1rem 0; }
.wf-head { display: flex; align-items: center; justify-content: space-between; }
.states { display: flex; flex-wrap: wrap; gap: 0.4rem; }
.field { display: flex; flex-direction: column; gap: 0.35rem; margin-bottom: 1rem; }
.field label { font-size: 0.85rem; font-weight: 600; }
code { font-size: 0.8rem; }
.mono :deep(textarea) { font-family: ui-monospace, monospace; font-size: 0.8rem; }
</style>
