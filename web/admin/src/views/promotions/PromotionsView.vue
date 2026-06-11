<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { useToast } from 'primevue/usetoast'
import { useConfirm } from 'primevue/useconfirm'
import DataTable from 'primevue/datatable'
import Column from 'primevue/column'
import Tag from 'primevue/tag'
import Button from 'primevue/button'
import Dialog from 'primevue/dialog'
import InputText from 'primevue/inputtext'
import InputNumber from 'primevue/inputnumber'
import Textarea from 'primevue/textarea'
import Select from 'primevue/select'
import DatePicker from 'primevue/datepicker'
import ToggleSwitch from 'primevue/toggleswitch'
import Message from 'primevue/message'
import { api, errMessage } from '@/lib/client'
import { useCurrency } from '@/composables/useCurrency'
import type { components } from '@teggo/api/schema'
import PageHeader from '@/components/PageHeader.vue'
import EmptyState from '@/components/EmptyState.vue'

type Promotion = components['schemas']['Promotion']

const rows = ref<Promotion[]>([])
const loading = ref(false)
const error = ref('')
const toast = useToast()
const confirm = useConfirm()
const { currency } = useCurrency()

const dialogOpen = ref(false)
const saving = ref(false)
const formError = ref('')
const editingId = ref<number | null>(null)

interface Form {
  name: string
  code: string
  description: string
  discount_type: 'percent' | 'amount'
  discount_value: string
  min_subtotal: string
  max_redemptions: number | null
  priority: number
  starts_at: Date | null
  ends_at: Date | null
  is_active: boolean
}
const blank = (): Form => ({
  name: '', code: '', description: '', discount_type: 'percent', discount_value: '',
  min_subtotal: '', max_redemptions: null, priority: 0, starts_at: null, ends_at: null, is_active: true,
})
const form = reactive<Form>(blank())

const types = [
  { label: 'Percent (% off)', value: 'percent' },
  { label: 'Fixed amount off', value: 'amount' },
]

async function load() {
  loading.value = true
  error.value = ''
  const { data, error: err } = await api.GET('/admin/promotions')
  loading.value = false
  if (err || !data) {
    error.value = errMessage(err, 'Failed to load promotions')
    return
  }
  rows.value = data.items ?? []
}

function effect(p: Promotion) {
  return p.discount_type === 'percent' ? `${p.discount_value}% off` : `${p.discount_value} off`
}
function usage(p: Promotion) {
  return p.max_redemptions ? `${p.times_redeemed} / ${p.max_redemptions}` : String(p.times_redeemed)
}

function openCreate() {
  editingId.value = null
  Object.assign(form, blank())
  formError.value = ''
  dialogOpen.value = true
}
function openEdit(p: Promotion) {
  editingId.value = p.id
  Object.assign(form, {
    name: p.name,
    code: p.code ?? '',
    description: p.description ?? '',
    discount_type: p.discount_type,
    discount_value: p.discount_value,
    min_subtotal: p.min_subtotal ?? '',
    max_redemptions: p.max_redemptions ?? null,
    priority: p.priority,
    starts_at: p.starts_at ? new Date(p.starts_at) : null,
    ends_at: p.ends_at ? new Date(p.ends_at) : null,
    is_active: p.is_active,
  })
  formError.value = ''
  dialogOpen.value = true
}

async function save() {
  if (!form.name.trim() || form.discount_value === '') {
    formError.value = 'Name and discount value are required.'
    return
  }
  const body = {
    name: form.name.trim(),
    code: form.code.trim() || null,
    description: form.description.trim() || null,
    discount_type: form.discount_type,
    discount_value: String(form.discount_value),
    min_subtotal: form.min_subtotal.trim() || null,
    max_redemptions: form.max_redemptions,
    priority: form.priority,
    starts_at: form.starts_at ? form.starts_at.toISOString() : null,
    ends_at: form.ends_at ? form.ends_at.toISOString() : null,
    is_active: form.is_active,
  }
  saving.value = true
  const res = editingId.value
    ? await api.PUT('/admin/promotions/{id}', { params: { path: { id: editingId.value } }, body })
    : await api.POST('/admin/promotions', { body })
  saving.value = false
  if (res.error) {
    formError.value = errMessage(res.error, 'Save failed')
    return
  }
  toast.add({ severity: 'success', summary: editingId.value ? 'Updated' : 'Created', life: 2500 })
  dialogOpen.value = false
  load()
}

function confirmDelete(p: Promotion) {
  confirm.require({
    message: `Delete promotion "${p.name}"?`,
    header: 'Confirm delete',
    icon: 'pi pi-exclamation-triangle',
    rejectProps: { label: 'Cancel', severity: 'secondary', outlined: true },
    acceptProps: { label: 'Delete', severity: 'danger' },
    accept: async () => {
      const { error: err } = await api.DELETE('/admin/promotions/{id}', { params: { path: { id: p.id } } })
      if (err) {
        toast.add({ severity: 'error', summary: 'Delete failed', detail: errMessage(err), life: 4000 })
        return
      }
      toast.add({ severity: 'success', summary: 'Deleted', life: 2000 })
      load()
    },
  })
}

onMounted(load)
</script>

<template>
  <div class="page">
    <PageHeader title="Promotions" :meta="rows.length">
      <template #actions>
        <Button icon="pi pi-refresh" severity="secondary" text @click="load" />
        <Button icon="pi pi-plus" label="New promotion" @click="openCreate" />
      </template>
    </PageHeader>
    <p class="muted mb">
      Cart-level discounts — a percent or fixed amount, optionally gated by a coupon code, a
      minimum subtotal, a schedule, or a usage cap. The single best-value promotion applies at
      checkout.
    </p>

    <Message v-if="error" severity="error" :closable="false" class="mb">{{ error }}</Message>

    <DataTable :value="rows" :loading="loading" dataKey="id" stripedRows paginator :rows="15">
      <template #empty>
        <EmptyState icon="pi pi-tag" title="No promotions yet" message="Create a coupon or an automatic discount to drive repeat orders.">
          <Button icon="pi pi-plus" label="New promotion" @click="openCreate" />
        </EmptyState>
      </template>
      <Column field="name" header="Name" />
      <Column header="Code">
        <template #body="{ data }">
          <Tag v-if="data.code" :value="data.code" severity="info" />
          <span v-else class="muted">automatic</span>
        </template>
      </Column>
      <Column header="Discount"><template #body="{ data }">{{ effect(data) }}</template></Column>
      <Column header="Used"><template #body="{ data }">{{ usage(data) }}</template></Column>
      <Column header="Status">
        <template #body="{ data }">
          <Tag :value="data.is_active ? 'active' : 'inactive'" :severity="data.is_active ? 'success' : 'secondary'" />
        </template>
      </Column>
      <Column header="" style="width: 7rem">
        <template #body="{ data }">
          <Button icon="pi pi-pencil" severity="secondary" text rounded @click="openEdit(data)" />
          <Button icon="pi pi-trash" severity="danger" text rounded @click="confirmDelete(data)" />
        </template>
      </Column>
    </DataTable>

    <Dialog v-model:visible="dialogOpen" modal :header="editingId ? 'Edit promotion' : 'New promotion'" :style="{ width: '36rem' }">
      <Message v-if="formError" severity="error" :closable="false" class="mb">{{ formError }}</Message>
      <div class="field"><label>Name</label><InputText v-model="form.name" fluid /></div>
      <div class="field">
        <label>Coupon code <span class="muted">(blank = automatic)</span></label>
        <InputText v-model="form.code" placeholder="e.g. SPRING10" fluid />
      </div>
      <div class="field"><label>Description <span class="muted">(optional)</span></label><Textarea v-model="form.description" rows="2" fluid /></div>
      <div class="grid2">
        <div class="field"><label>Type</label><Select v-model="form.discount_type" :options="types" optionLabel="label" optionValue="value" fluid /></div>
        <div class="field">
          <label>Value <span class="muted">{{ form.discount_type === 'percent' ? '(%)' : (currency ? `(${currency})` : '') }}</span></label>
          <InputText v-model="form.discount_value" :placeholder="form.discount_type === 'percent' ? '10' : '5.00'" fluid />
        </div>
      </div>
      <div class="grid2">
        <div class="field"><label>Min subtotal <span class="muted">(optional)</span></label><InputText v-model="form.min_subtotal" placeholder="0.00" fluid /></div>
        <div class="field"><label>Max redemptions <span class="muted">(optional)</span></label><InputNumber v-model="form.max_redemptions" :min="0" :useGrouping="false" showButtons fluid /></div>
      </div>
      <div class="grid2">
        <div class="field"><label>Starts <span class="muted">(optional)</span></label><DatePicker v-model="form.starts_at" showTime hourFormat="24" showButtonBar fluid /></div>
        <div class="field"><label>Ends <span class="muted">(optional)</span></label><DatePicker v-model="form.ends_at" showTime hourFormat="24" showButtonBar fluid /></div>
      </div>
      <div class="grid2">
        <div class="field"><label>Priority <span class="muted">(higher wins ties)</span></label><InputNumber v-model="form.priority" :min="0" showButtons fluid /></div>
        <div class="field switch"><label>Active</label><ToggleSwitch v-model="form.is_active" /></div>
      </div>
      <template #footer>
        <Button label="Cancel" severity="secondary" text @click="dialogOpen = false" />
        <Button :label="editingId ? 'Save' : 'Create'" :loading="saving" @click="save" />
      </template>
    </Dialog>
  </div>
</template>

<style scoped>
.mb { margin-bottom: 1rem; }
.muted { color: var(--p-text-muted-color, #64748b); font-weight: 400; }
.grid2 { display: grid; grid-template-columns: 1fr 1fr; gap: 1rem; }
.field { display: flex; flex-direction: column; gap: 0.35rem; margin-bottom: 1rem; }
.field label { font-size: 0.85rem; font-weight: 600; }
.switch { justify-content: flex-start; }
</style>
