<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { useToast } from 'primevue/usetoast'
import DataTable from 'primevue/datatable'
import Column from 'primevue/column'
import Button from 'primevue/button'
import Dialog from 'primevue/dialog'
import InputText from 'primevue/inputtext'
import Select from 'primevue/select'
import Checkbox from 'primevue/checkbox'
import Tag from 'primevue/tag'
import Message from 'primevue/message'
import { api, errMessage } from '@/lib/client'
import type { components } from '@oro/api/schema'

type Attribute = components['schemas']['Attribute']
type AttributeInput = components['schemas']['AttributeInput']

const rows = ref<Attribute[]>([])
const loading = ref(false)
const error = ref('')
const dialogOpen = ref(false)
const saving = ref(false)
const toast = useToast()

const dataTypes: AttributeInput['data_type'][] = [
  'text', 'number', 'boolean', 'select', 'multiselect', 'date', 'file', 'price',
]
const form = reactive<AttributeInput>({
  code: '',
  label: '',
  data_type: 'text',
  is_filterable: false,
  is_variant_axis: false,
})

async function load() {
  loading.value = true
  error.value = ''
  const { data, error: err } = await api.GET('/admin/attributes')
  loading.value = false
  if (err || !data) {
    error.value = errMessage(err, 'Failed to load attributes')
    return
  }
  rows.value = data.items ?? []
}

function openCreate() {
  Object.assign(form, { code: '', label: '', data_type: 'text', is_filterable: false, is_variant_axis: false })
  dialogOpen.value = true
}

async function save() {
  saving.value = true
  const { error: err } = await api.POST('/admin/attributes', { body: { ...form } })
  saving.value = false
  if (err) {
    toast.add({ severity: 'error', summary: 'Save failed', detail: errMessage(err), life: 4000 })
    return
  }
  dialogOpen.value = false
  toast.add({ severity: 'success', summary: 'Attribute created', life: 2000 })
  load()
}

onMounted(load)
</script>

<template>
  <div class="page">
    <div class="header">
      <h1>Attributes</h1>
      <Button icon="pi pi-plus" label="New attribute" @click="openCreate" />
    </div>
    <Message v-if="error" severity="error" :closable="false" class="mb">{{ error }}</Message>
    <DataTable :value="rows" :loading="loading" dataKey="id" stripedRows>
      <template #empty>No attributes yet.</template>
      <Column field="code" header="Code" sortable />
      <Column field="label" header="Label" sortable />
      <Column field="data_type" header="Type" />
      <Column header="Flags">
        <template #body="{ data }">
          <Tag v-if="data.is_filterable" value="filterable" severity="info" class="flag" />
          <Tag v-if="data.is_variant_axis" value="variant axis" severity="warn" class="flag" />
        </template>
      </Column>
    </DataTable>

    <Dialog v-model:visible="dialogOpen" header="New attribute" modal :style="{ width: '440px' }">
      <form class="form" @submit.prevent="save">
        <div class="field"><label>Code</label><InputText v-model="form.code" fluid /></div>
        <div class="field"><label>Label</label><InputText v-model="form.label" fluid /></div>
        <div class="field"><label>Data type</label><Select v-model="form.data_type" :options="dataTypes" fluid /></div>
        <div class="check"><Checkbox v-model="form.is_filterable" binary inputId="filt" /><label for="filt">Filterable (facet)</label></div>
        <div class="check"><Checkbox v-model="form.is_variant_axis" binary inputId="var" /><label for="var">Variant axis</label></div>
      </form>
      <template #footer>
        <Button label="Cancel" severity="secondary" text @click="dialogOpen = false" />
        <Button label="Save" :loading="saving" @click="save" />
      </template>
    </Dialog>
  </div>
</template>

<style scoped>
.header { display: flex; align-items: center; justify-content: space-between; margin-bottom: 1rem; }
.header h1 { margin: 0; }
.mb { margin-bottom: 1rem; }
.form { display: flex; flex-direction: column; gap: 0.9rem; }
.field { display: flex; flex-direction: column; gap: 0.3rem; }
.field label { font-size: 0.8rem; font-weight: 600; }
.check { display: flex; align-items: center; gap: 0.5rem; }
.flag { margin-right: 0.35rem; }
</style>
