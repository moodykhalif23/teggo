<script setup lang="ts">
import DataTable from 'primevue/datatable'
import Column from 'primevue/column'
import Tag from 'primevue/tag'
import Button from 'primevue/button'
import Dialog from 'primevue/dialog'
import Select from 'primevue/select'
import InputText from 'primevue/inputtext'
import Message from 'primevue/message'
import type { components } from '@teggo/api/schema'

definePageMeta({ middleware: 'auth' })
useSeoMeta({ title: 'Recurring orders — Teggo Store' })

type Subscription = components['schemas']['Subscription']

const client = useClient()
const subs = ref<Subscription[]>([])
const error = ref('')
const busy = ref(false)

async function load() {
  const { data, error: err } = await client.GET('/storefront/subscriptions')
  if (err || !data) {
    error.value = 'Could not load your recurring orders.'
    return
  }
  subs.value = data.items ?? []
}

async function setStatus(s: Subscription, status: 'active' | 'paused' | 'cancelled') {
  busy.value = true
  const { error: err } = await client.POST('/storefront/subscriptions/{id}/status', {
    params: { path: { id: s.id } },
    body: { status },
  })
  busy.value = false
  if (!err) load()
}

async function skip(s: Subscription) {
  busy.value = true
  const { error: err } = await client.POST('/storefront/subscriptions/{id}/skip', { params: { path: { id: s.id } } })
  busy.value = false
  if (!err) load()
}

// ---- edit (cadence + quantities) ----
const editOpen = ref(false)
const editSaving = ref(false)
const editErr = ref('')
type EditItem = { product_id: number; name: string; unit: string; quantity: string }
const editForm = reactive<{ id: number; cadence: string; items: EditItem[] }>({ id: 0, cadence: 'monthly', items: [] })
const cadenceOptions = [
  { label: 'Every week', value: 'weekly' },
  { label: 'Every 2 weeks', value: 'biweekly' },
  { label: 'Every month', value: 'monthly' },
  { label: 'Every quarter', value: 'quarterly' },
]

async function openEdit(s: Subscription) {
  editErr.value = ''
  const { data } = await client.GET('/storefront/subscriptions/{id}', { params: { path: { id: s.id } } })
  const full = data ?? s
  editForm.id = full.id
  editForm.cadence = full.cadence
  editForm.items = (full.items ?? []).map((it) => ({ product_id: it.product_id, name: it.name, unit: it.unit, quantity: it.quantity }))
  editOpen.value = true
}
async function saveEdit() {
  editSaving.value = true
  editErr.value = ''
  const items = editForm.items.map((it) => ({ product_id: it.product_id, quantity: it.quantity || '1', unit: it.unit }))
  const { error: err } = await client.PUT('/storefront/subscriptions/{id}', {
    params: { path: { id: editForm.id } },
    body: { cadence: editForm.cadence as 'weekly' | 'biweekly' | 'monthly' | 'quarterly', items },
  })
  editSaving.value = false
  if (err) {
    editErr.value = 'Could not save your changes.'
    return
  }
  editOpen.value = false
  load()
}

function sev(s: string) {
  return s === 'active' ? 'success' : s === 'paused' ? 'warn' : 'secondary'
}
const cadenceLabel: Record<string, string> = {
  weekly: 'Weekly', biweekly: 'Every 2 weeks', monthly: 'Monthly', quarterly: 'Quarterly',
}

await load()
</script>

<template>
  <section>
    <h1 class="title">Recurring orders</h1>
    <p class="muted mb">Standing orders we place for you automatically. Pause, skip the next delivery, or cancel anytime.</p>
    <Message v-if="error" severity="error" :closable="false">{{ error }}</Message>

    <DataTable :value="subs" dataKey="id" stripedRows>
      <template #empty>
        <EmptyState
          icon="pi pi-sync"
          title="No recurring orders"
          message="Open a past order and choose “Set up recurring” to have us reorder it on a schedule."
        />
      </template>
      <Column header="Name"><template #body="{ data }">{{ data.name || `#${data.id}` }}</template></Column>
      <Column header="Cadence"><template #body="{ data }">{{ cadenceLabel[data.cadence] ?? data.cadence }}</template></Column>
      <Column header="Next delivery"><template #body="{ data }">{{ data.status === 'cancelled' ? '—' : data.next_run_date }}</template></Column>
      <Column header="Status"><template #body="{ data }"><Tag :value="data.status" :severity="sev(data.status)" /></template></Column>
      <Column header="" style="width: 18rem">
        <template #body="{ data }">
          <template v-if="data.status !== 'cancelled'">
            <Button label="Edit" icon="pi pi-pencil" text size="small" :disabled="busy" @click="openEdit(data)" />
            <Button v-if="data.status === 'active'" label="Skip next" icon="pi pi-forward" text size="small" :disabled="busy" @click="skip(data)" />
            <Button v-if="data.status === 'active'" label="Pause" icon="pi pi-pause" text size="small" :disabled="busy" @click="setStatus(data, 'paused')" />
            <Button v-if="data.status === 'paused'" label="Resume" icon="pi pi-play" text size="small" :disabled="busy" @click="setStatus(data, 'active')" />
            <Button label="Cancel" icon="pi pi-times" text size="small" severity="danger" :disabled="busy" @click="setStatus(data, 'cancelled')" />
          </template>
        </template>
      </Column>
    </DataTable>

    <Dialog v-model:visible="editOpen" modal header="Edit recurring order" :style="{ width: '32rem' }">
      <Message v-if="editErr" severity="error" :closable="false" class="mb">{{ editErr }}</Message>
      <div class="field">
        <label>How often?</label>
        <Select v-model="editForm.cadence" :options="cadenceOptions" optionLabel="label" optionValue="value" fluid />
      </div>
      <p class="muted small">Quantities</p>
      <div v-for="it in editForm.items" :key="it.product_id" class="erow">
        <span class="ename">{{ it.name }}</span>
        <InputText v-model="it.quantity" class="eqty" />
      </div>
      <template #footer>
        <Button label="Cancel" text :disabled="editSaving" @click="editOpen = false" />
        <Button label="Save" :loading="editSaving" @click="saveEdit" />
      </template>
    </Dialog>
  </section>
</template>

<style scoped>
.title { margin: 0 0 0.5rem; }
.muted { color: var(--p-text-muted-color, #64748b); }
.mb { margin-bottom: 1rem; }
.small { font-size: 0.85rem; }
.field { display: flex; flex-direction: column; gap: 0.35rem; margin-bottom: 1rem; }
.field label { font-size: 0.85rem; font-weight: 600; }
.erow { display: flex; align-items: center; justify-content: space-between; gap: 1rem; margin-bottom: 0.5rem; }
.ename { flex: 1; }
.eqty { width: 7rem; }
</style>
