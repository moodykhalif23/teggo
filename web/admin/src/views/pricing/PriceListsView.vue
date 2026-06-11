<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import { useToast } from 'primevue/usetoast'
import DataTable from 'primevue/datatable'
import Column from 'primevue/column'
import Button from 'primevue/button'
import Tag from 'primevue/tag'
import Dialog from 'primevue/dialog'
import InputText from 'primevue/inputtext'
import Checkbox from 'primevue/checkbox'
import Message from 'primevue/message'
import { api, errMessage } from '@/lib/client'
import { useCurrency } from '@/composables/useCurrency'
import type { components } from '@teggo/api/schema'
import PricingTools from './PricingTools.vue'
import PageHeader from '@/components/PageHeader.vue'
import EmptyState from '@/components/EmptyState.vue'

type PriceList = components['schemas']['PriceList']

const router = useRouter()
const toast = useToast()
const rows = ref<PriceList[]>([])
const loading = ref(false)
const error = ref('')
const dialogOpen = ref(false)
const saving = ref(false)
const editing = ref<PriceList | null>(null)

const form = reactive({ name: '', currency: 'USD', is_default: false, is_active: true })
// New price lists default to the org's configured currency (Settings).
const { currency } = useCurrency()

async function load() {
  loading.value = true
  error.value = ''
  const { data, error: err } = await api.GET('/admin/price-lists')
  loading.value = false
  if (err || !data) {
    error.value = errMessage(err, 'Failed to load price lists')
    return
  }
  rows.value = data.items ?? []
}

function openCreate() {
  editing.value = null
  Object.assign(form, { name: '', currency: currency.value || 'USD', is_default: false, is_active: true })
  dialogOpen.value = true
}
function openEdit(pl: PriceList) {
  editing.value = pl
  Object.assign(form, { name: pl.name, currency: pl.currency, is_default: pl.is_default, is_active: pl.is_active })
  dialogOpen.value = true
}

async function save() {
  saving.value = true
  const body = { ...form }
  const editingNow = editing.value
  const err = editingNow
    ? (await api.PUT('/admin/price-lists/{id}', { params: { path: { id: editingNow.id } }, body })).error
    : (await api.POST('/admin/price-lists', { body })).error
  saving.value = false
  if (err) {
    toast.add({ severity: 'error', summary: 'Save failed', detail: errMessage(err), life: 4000 })
    return
  }
  dialogOpen.value = false
  toast.add({ severity: 'success', summary: editingNow ? 'Updated' : 'Created', life: 2000 })
  load()
}

onMounted(load)
</script>

<template>
  <div class="page">
    <PageHeader title="Price lists">
      <template #actions>
        <Button icon="pi pi-plus" label="New price list" @click="openCreate" />
      </template>
    </PageHeader>

    <Message v-if="error" severity="error" :closable="false" class="mb">{{ error }}</Message>

    <DataTable
      :value="rows"
      :loading="loading"
      dataKey="id"
      stripedRows
      @rowClick="router.push({ name: 'price-list-detail', params: { id: $event.data.id } })"
      class="clickable"
    >
      <template #empty>
        <EmptyState icon="pi pi-dollar" title="No price lists yet" message="Create a price list to set per-customer or per-group pricing for your catalog.">
          <Button icon="pi pi-plus" label="New price list" @click="openCreate" />
        </EmptyState>
      </template>
      <Column field="name" header="Name" sortable />
      <Column field="currency" header="Currency" />
      <Column header="Default"><template #body="{ data }"><Tag v-if="data.is_default" value="default" severity="info" /></template></Column>
      <Column header="Active">
        <template #body="{ data }"><Tag :value="data.is_active ? 'active' : 'inactive'" :severity="data.is_active ? 'success' : 'secondary'" /></template>
      </Column>
      <Column header="" style="width: 4rem">
        <template #body="{ data }"><Button icon="pi pi-pencil" text rounded severity="secondary" @click.stop="openEdit(data)" /></template>
      </Column>
    </DataTable>

    <PricingTools class="tools" />

    <Dialog v-model:visible="dialogOpen" :header="editing ? 'Edit price list' : 'New price list'" modal :style="{ width: '440px' }">
      <form class="form" @submit.prevent="save">
        <div class="field"><label>Name</label><InputText v-model="form.name" fluid /></div>
        <div class="field"><label>Currency (3-letter)</label><InputText v-model="form.currency" maxlength="3" fluid /></div>
        <div class="check"><Checkbox v-model="form.is_default" binary inputId="def" /><label for="def">Default list</label></div>
        <div class="check"><Checkbox v-model="form.is_active" binary inputId="act" /><label for="act">Active</label></div>
      </form>
      <template #footer>
        <Button label="Cancel" severity="secondary" text @click="dialogOpen = false" />
        <Button label="Save" :loading="saving" @click="save" />
      </template>
    </Dialog>
  </div>
</template>

<style scoped>
.mb { margin-bottom: 1rem; }
.tools { margin-top: 1.5rem; }
.clickable :deep(tbody tr) { cursor: pointer; }
.form { display: flex; flex-direction: column; gap: 0.9rem; }
.field { display: flex; flex-direction: column; gap: 0.3rem; }
.field label { font-size: 0.8rem; font-weight: 600; }
.check { display: flex; align-items: center; gap: 0.5rem; }
</style>
