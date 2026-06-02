<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useToast } from 'primevue/usetoast'
import { useConfirm } from 'primevue/useconfirm'
import DataTable from 'primevue/datatable'
import Column from 'primevue/column'
import Tag from 'primevue/tag'
import Button from 'primevue/button'
import Message from 'primevue/message'
import { api, errMessage } from '@/lib/client'
import type { components } from '@oro/api/schema'
import ProductFormDialog from './ProductFormDialog.vue'

type AdminProduct = components['schemas']['AdminProduct']

const products = ref<AdminProduct[]>([])
const total = ref(0)
const loading = ref(false)
const error = ref('')

const dialogOpen = ref(false)
const editing = ref<AdminProduct | null>(null)

const toast = useToast()
const confirm = useConfirm()

async function load() {
  loading.value = true
  error.value = ''
  const { data, error: err } = await api.GET('/admin/products', {
    params: { query: { page: 1, page_size: 100 } },
  })
  loading.value = false
  if (err || !data) {
    error.value = errMessage(err, 'Failed to load products')
    return
  }
  products.value = data.items
  total.value = data.total ?? data.items.length
}

function openCreate() {
  editing.value = null
  dialogOpen.value = true
}
function openEdit(p: AdminProduct) {
  editing.value = p
  dialogOpen.value = true
}

function confirmDelete(p: AdminProduct) {
  confirm.require({
    message: `Delete product "${p.name}"? It will be soft-deleted.`,
    header: 'Confirm delete',
    icon: 'pi pi-exclamation-triangle',
    rejectProps: { label: 'Cancel', severity: 'secondary', outlined: true },
    acceptProps: { label: 'Delete', severity: 'danger' },
    accept: async () => {
      const { error: err } = await api.DELETE('/admin/products/{id}', {
        params: { path: { id: p.id } },
      })
      if (err) {
        toast.add({ severity: 'error', summary: 'Delete failed', detail: errMessage(err), life: 4000 })
        return
      }
      toast.add({ severity: 'success', summary: 'Deleted', detail: p.name, life: 2500 })
      load()
    },
  })
}

function onSaved() {
  dialogOpen.value = false
  load()
}

function statusSeverity(s: string) {
  return s === 'active' ? 'success' : s === 'draft' ? 'warn' : 'danger'
}

onMounted(load)
</script>

<template>
  <div class="page">
    <div class="header">
      <h1>Products <span class="muted">({{ total }})</span></h1>
      <div class="actions">
        <Button icon="pi pi-refresh" severity="secondary" text @click="load" />
        <Button icon="pi pi-plus" label="New product" @click="openCreate" />
      </div>
    </div>

    <Message v-if="error" severity="error" :closable="false" class="mb">{{ error }}</Message>

    <DataTable
      :value="products"
      :loading="loading"
      paginator
      :rows="10"
      :rowsPerPageOptions="[10, 25, 50]"
      dataKey="id"
      stripedRows
      removableSort
    >
      <template #empty>No products yet — create one.</template>
      <Column field="sku" header="SKU" sortable />
      <Column field="name" header="Name" sortable />
      <Column field="type" header="Type" sortable />
      <Column field="unit" header="Unit" />
      <Column header="Status" sortable field="status">
        <template #body="{ data }">
          <Tag :value="data.status" :severity="statusSeverity(data.status)" />
        </template>
      </Column>
      <Column header="" style="width: 7rem">
        <template #body="{ data }">
          <Button icon="pi pi-pencil" severity="secondary" text rounded @click="openEdit(data)" />
          <Button icon="pi pi-trash" severity="danger" text rounded @click="confirmDelete(data)" />
        </template>
      </Column>
    </DataTable>

    <ProductFormDialog v-model:open="dialogOpen" :product="editing" @saved="onSaved" />
  </div>
</template>

<style scoped>
.header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 1rem;
}
.header h1 {
  margin: 0;
}
.actions {
  display: flex;
  gap: 0.5rem;
}
.muted {
  color: var(--p-text-muted-color, #64748b);
  font-weight: 400;
  font-size: 1rem;
}
.mb {
  margin-bottom: 1rem;
}
</style>
