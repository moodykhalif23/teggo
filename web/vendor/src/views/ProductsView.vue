<script setup lang="ts">
import { onMounted, ref } from 'vue'
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

type Product = components['schemas']['VendorProduct']

const products = ref<Product[]>([])
const loading = ref(false)
const error = ref('')
const toast = useToast()

function sev(s: string) {
  return s === 'approved' ? 'success' : s === 'rejected' ? 'danger' : s === 'pending' ? 'info' : 'warn'
}

async function load() {
  loading.value = true
  error.value = ''
  const { data, error: err } = await api.GET('/vendor/products')
  loading.value = false
  if (err || !data) {
    error.value = errMessage(err, 'Failed to load products')
    return
  }
  products.value = data.items ?? []
}

// ---- create ----
const createOpen = ref(false)
const form = ref({ sku: '', name: '', description: '', unit: 'each' })
const saving = ref(false)

function openCreate() {
  form.value = { sku: '', name: '', description: '', unit: 'each' }
  createOpen.value = true
}

async function create() {
  saving.value = true
  const { error: err } = await api.POST('/vendor/products', {
    body: { sku: form.value.sku, name: form.value.name, description: form.value.description || null, unit: form.value.unit },
  })
  saving.value = false
  if (err) {
    toast.add({ severity: 'error', summary: 'Create failed', detail: errMessage(err), life: 4000 })
    return
  }
  createOpen.value = false
  toast.add({ severity: 'success', summary: 'Product submitted for approval', life: 2500 })
  load()
}

async function submit(p: Product) {
  const { error: err } = await api.POST('/vendor/products/{id}/submit', { params: { path: { id: p.id } } })
  if (err) {
    toast.add({ severity: 'error', summary: 'Submit failed', detail: errMessage(err), life: 4000 })
    return
  }
  toast.add({ severity: 'success', summary: 'Submitted for approval', life: 2000 })
  load()
}

onMounted(load)
</script>

<template>
  <div class="page">
    <div class="header">
      <h1>Products <span class="muted">({{ products.length }})</span></h1>
      <div class="actions">
        <Button icon="pi pi-refresh" severity="secondary" text @click="load" />
        <Button label="List a product" icon="pi pi-plus" @click="openCreate" />
      </div>
    </div>
    <p class="muted">New and edited listings are reviewed by the marketplace operator before going live.</p>

    <Message v-if="error" severity="error" :closable="false" class="mb">{{ error }}</Message>

    <DataTable :value="products" :loading="loading" dataKey="id" stripedRows>
      <template #empty>No products yet. List your first one.</template>
      <Column field="sku" header="SKU" />
      <Column field="name" header="Name" />
      <Column field="status" header="Status" />
      <Column header="Approval"><template #body="{ data }"><Tag :value="data.approval_status" :severity="sev(data.approval_status)" /></template></Column>
      <Column header="">
        <template #body="{ data }">
          <Button
            v-if="data.approval_status === 'rejected' || data.approval_status === 'draft'"
            label="Submit for approval"
            size="small"
            text
            @click="submit(data)"
          />
        </template>
      </Column>
    </DataTable>

    <Dialog v-model:visible="createOpen" modal header="List a product" :style="{ width: '32rem' }">
      <div class="field"><label>SKU</label><InputText v-model="form.sku" /></div>
      <div class="field"><label>Name</label><InputText v-model="form.name" /></div>
      <div class="field"><label>Description</label><Textarea v-model="form.description" rows="3" autoResize /></div>
      <div class="field"><label>Unit</label><InputText v-model="form.unit" /></div>
      <template #footer>
        <Button label="Cancel" text @click="createOpen = false" />
        <Button label="Submit" :loading="saving" :disabled="!form.sku || !form.name" @click="create" />
      </template>
    </Dialog>
  </div>
</template>

<style scoped>
.header { display: flex; align-items: center; justify-content: space-between; }
.header h1 { margin: 0; }
.actions { display: flex; gap: 0.5rem; align-items: center; }
.muted { color: #64748b; }
.mb { margin-bottom: 1rem; }
.field { display: flex; flex-direction: column; gap: 0.35rem; margin-bottom: 0.85rem; }
.field label { font-size: 0.82rem; font-weight: 600; }
</style>
