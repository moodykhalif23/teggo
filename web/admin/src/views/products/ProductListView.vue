<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { api, ApiError } from '@/lib/api'
import DataTable from 'primevue/datatable'
import Column from 'primevue/column'
import Tag from 'primevue/tag'
import Button from 'primevue/button'
import Message from 'primevue/message'

// Mirrors store.Product (internal/store/store.go).
interface Product {
  public_id: string
  sku: string
  name: string
  slug: string
  status: 'draft' | 'active' | 'disabled'
  unit: string
}

interface ProductList {
  items: Product[]
  page: number
}

const products = ref<Product[]>([])
const loading = ref(false)
const error = ref('')

async function load() {
  loading.value = true
  error.value = ''
  try {
    const res = await api.get<ProductList>('/admin/products?page=1&page_size=50')
    products.value = res.items ?? []
  } catch (e) {
    error.value = e instanceof ApiError ? e.message : 'Failed to load products'
  } finally {
    loading.value = false
  }
}

function statusSeverity(s: Product['status']) {
  return s === 'active' ? 'success' : s === 'draft' ? 'warn' : 'danger'
}

onMounted(load)
</script>

<template>
  <div class="page">
    <div class="header">
      <h1>Products</h1>
      <Button icon="pi pi-refresh" label="Refresh" severity="secondary" @click="load" />
    </div>

    <Message v-if="error" severity="error" :closable="false" class="mb">{{ error }}</Message>

    <DataTable
      :value="products"
      :loading="loading"
      paginator
      :rows="10"
      :rowsPerPageOptions="[10, 25, 50]"
      dataKey="public_id"
      stripedRows
      removableSort
    >
      <template #empty>No products found.</template>
      <Column field="sku" header="SKU" sortable />
      <Column field="name" header="Name" sortable />
      <Column field="slug" header="Slug" />
      <Column field="unit" header="Unit" />
      <Column header="Status" sortable field="status">
        <template #body="{ data }">
          <Tag :value="data.status" :severity="statusSeverity(data.status)" />
        </template>
      </Column>
    </DataTable>
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
.mb {
  margin-bottom: 1rem;
}
</style>
