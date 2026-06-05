<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useToast } from 'primevue/usetoast'
import Button from 'primevue/button'
import Dialog from 'primevue/dialog'
import InputText from 'primevue/inputtext'
import Select from 'primevue/select'
import Checkbox from 'primevue/checkbox'
import DataTable from 'primevue/datatable'
import Column from 'primevue/column'
import Tag from 'primevue/tag'
import Message from 'primevue/message'
import { api, errMessage } from '@/lib/client'
import { useCustomerOptions } from '@/composables/useRecordOptions'
import type { components } from '@teggo/api/schema'

type TradingPartner = components['schemas']['TradingPartner']
type EDIDocument = components['schemas']['EDIDocument']

const toast = useToast()
const partners = ref<TradingPartner[]>([])
const docs = ref<EDIDocument[]>([])
const error = ref('')

const { customers, customersLoaded, loadCustomers } = useCustomerOptions()
const custName = computed(() => Object.fromEntries(customers.value.map((c) => [c.id, c.name])))

const protocols = ['cxml', 'oci', 'edi_x12', 'edifact']
const transports = ['https', 'as2', 'sftp', 'van']

async function load() {
  error.value = ''
  const { data, error: err } = await api.GET('/admin/trading-partners')
  const { data: d } = await api.GET('/admin/edi/documents')
  if (err || !data) {
    error.value = errMessage(err, 'Failed to load trading partners')
    return
  }
  partners.value = data.items ?? []
  docs.value = d?.items ?? []
}

// ---- partner editor ----
const dialogOpen = ref(false)
const editingId = ref<number | null>(null)
const saving = ref(false)
const formError = ref('')
const form = reactive({
  name: '', protocol: 'cxml', transport: 'https',
  customer_id: null as number | null, identity: '', shared_secret: '', is_active: true,
})

function openCreate() {
  editingId.value = null
  Object.assign(form, { name: '', protocol: 'cxml', transport: 'https', customer_id: null, identity: '', shared_secret: '', is_active: true })
  formError.value = ''
  dialogOpen.value = true
}
function openEdit(p: TradingPartner) {
  editingId.value = p.id
  Object.assign(form, {
    name: p.name, protocol: p.protocol, transport: p.transport ?? 'https',
    customer_id: p.customer_id ?? null, identity: p.identity ?? '', shared_secret: '', is_active: p.is_active,
  })
  formError.value = ''
  dialogOpen.value = true
}

async function save() {
  if (!form.name) {
    formError.value = 'Name is required.'
    return
  }
  saving.value = true
  const body: components['schemas']['TradingPartnerInput'] = {
    name: form.name,
    protocol: form.protocol as TradingPartner['protocol'],
    transport: form.transport as NonNullable<TradingPartner['transport']>,
    customer_id: form.customer_id,
    identity: form.identity || null,
    is_active: form.is_active,
  }
  // Only send a secret when one was entered (keeps the existing one otherwise).
  if (form.shared_secret) body.shared_secret = form.shared_secret
  const { error: err } = editingId.value
    ? await api.PUT('/admin/trading-partners/{id}', { params: { path: { id: editingId.value } }, body })
    : await api.POST('/admin/trading-partners', { body })
  saving.value = false
  if (err) {
    formError.value = errMessage(err, 'Save failed (identity may be in use)')
    return
  }
  toast.add({ severity: 'success', summary: 'Partner saved', life: 2000 })
  dialogOpen.value = false
  load()
}

const sevForStatus = (s: string) =>
  s === 'processed' || s === 'sent' || s === 'acknowledged' ? 'success' : s === 'error' ? 'danger' : 'info'

onMounted(() => {
  load()
  loadCustomers()
})
</script>

<template>
  <div class="page">
    <div class="header">
      <h1>Integrations <span class="muted">Punchout &amp; EDI</span></h1>
      <div class="actions">
        <Button icon="pi pi-refresh" severity="secondary" text @click="load" />
        <Button icon="pi pi-plus" label="New partner" @click="openCreate" />
      </div>
    </div>
    <p class="muted">Trading partners connect procurement systems via cXML/OCI punchout or X12 EDI. Inbound 850s become orders (with an 855 ack); 810/856 are emitted back.</p>

    <Message v-if="error" severity="error" :closable="false" class="mb">{{ error }}</Message>

    <h3>Trading partners</h3>
    <DataTable :value="partners" dataKey="id" stripedRows class="mb">
      <template #empty>No trading partners yet.</template>
      <Column field="name" header="Name" />
      <Column field="protocol" header="Protocol" />
      <Column field="transport" header="Transport" />
      <Column field="identity" header="Identity" />
      <Column header="Customer"><template #body="{ data }">{{ data.customer_id ? (custName[data.customer_id] ?? `#${data.customer_id}`) : '—' }}</template></Column>
      <Column header="Secret"><template #body="{ data }"><i :class="data.has_secret ? 'pi pi-check' : 'pi pi-minus'" /></template></Column>
      <Column header="Active"><template #body="{ data }"><Tag :value="data.is_active ? 'active' : 'off'" :severity="data.is_active ? 'success' : 'secondary'" /></template></Column>
      <Column header="" style="width:4rem"><template #body="{ data }"><Button icon="pi pi-pencil" text rounded size="small" @click="openEdit(data)" /></template></Column>
    </DataTable>

    <h3>EDI document log</h3>
    <DataTable :value="docs" dataKey="id" stripedRows scrollable scrollHeight="360px">
      <template #empty>No EDI documents yet.</template>
      <Column field="id" header="#" style="width:4rem" />
      <Column field="direction" header="Dir" />
      <Column field="doc_type" header="Type" />
      <Column header="Status"><template #body="{ data }"><Tag :value="data.status" :severity="sevForStatus(data.status)" /></template></Column>
      <Column field="control_number" header="Control #" />
      <Column header="Related"><template #body="{ data }">{{ data.related_entity_type ? `${data.related_entity_type} ${data.related_entity_id}` : '—' }}</template></Column>
      <Column field="error" header="Error" />
      <Column field="created_at" header="Received" />
    </DataTable>

    <Dialog v-model:visible="dialogOpen" modal :header="editingId ? 'Edit partner' : 'New trading partner'" :style="{ width: '32rem' }">
      <Message v-if="formError" severity="error" :closable="false" class="mb">{{ formError }}</Message>
      <div class="field"><label>Name</label><InputText v-model="form.name" /></div>
      <div class="row">
        <div class="field"><label>Protocol</label><Select v-model="form.protocol" :options="protocols" /></div>
        <div class="field"><label>Transport</label><Select v-model="form.transport" :options="transports" /></div>
      </div>
      <div class="row">
        <div class="field"><label>Identity</label><InputText v-model="form.identity" placeholder="cXML / ISA id" /></div>
        <div class="field">
          <label>Customer <span class="muted">(optional)</span></label>
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
          />
        </div>
      </div>
      <div class="field">
        <label>Shared secret <span class="muted">(punchout; leave blank to keep)</span></label>
        <InputText v-model="form.shared_secret" type="password" />
      </div>
      <div class="field-inline"><Checkbox v-model="form.is_active" :binary="true" inputId="active" /><label for="active">Active</label></div>
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
h3 { margin: 1.25rem 0 0.5rem; }
.row { display: grid; grid-template-columns: 1fr 1fr; gap: 0.75rem; }
.field { display: flex; flex-direction: column; gap: 0.35rem; margin-bottom: 1rem; }
.field label { font-size: 0.85rem; font-weight: 600; }
.field-inline { display: flex; align-items: center; gap: 0.5rem; margin-bottom: 1rem; }
</style>
