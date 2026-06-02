<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { useToast } from 'primevue/usetoast'
import Card from 'primevue/card'
import DataTable from 'primevue/datatable'
import Column from 'primevue/column'
import Button from 'primevue/button'
import Tag from 'primevue/tag'
import Dialog from 'primevue/dialog'
import InputText from 'primevue/inputtext'
import InputNumber from 'primevue/inputnumber'
import Select from 'primevue/select'
import Checkbox from 'primevue/checkbox'
import Message from 'primevue/message'
import { api, errMessage } from '@/lib/client'
import type { components } from '@teggo/api/schema'

type Warehouse = components['schemas']['Warehouse']
type LevelRow = components['schemas']['InventoryLevelRow']
type Movement = components['schemas']['InventoryMovement']

const toast = useToast()

// --- warehouses ---
const warehouses = ref<Warehouse[]>([])
const whDialog = ref(false)
const whName = ref('')
const savingWh = ref(false)

async function loadWarehouses() {
  const { data } = await api.GET('/admin/warehouses')
  warehouses.value = data?.items ?? []
}
async function createWarehouse() {
  if (!whName.value) return
  savingWh.value = true
  const { error } = await api.POST('/admin/warehouses', { body: { name: whName.value } })
  savingWh.value = false
  if (error) {
    toast.add({ severity: 'error', summary: 'Failed', detail: errMessage(error), life: 4000 })
    return
  }
  whDialog.value = false
  whName.value = ''
  loadWarehouses()
}

// --- product stock lookup ---
const productId = ref<number | null>(null)
const levels = ref<LevelRow[]>([])
const lookupError = ref('')
const loadedProduct = ref<number | null>(null)

async function loadLevels() {
  if (!productId.value) return
  lookupError.value = ''
  const { data, error } = await api.GET('/admin/inventory/{productId}', { params: { path: { productId: productId.value } } })
  if (error || !data) {
    lookupError.value = errMessage(error, 'Product not found')
    levels.value = []
    loadedProduct.value = null
    return
  }
  levels.value = data.levels ?? []
  loadedProduct.value = data.product_id
  movements.value = []
}

// --- adjustment ---
const adjDialog = ref(false)
const savingAdj = ref(false)
const adjForm = reactive({ warehouse_id: null as number | null, type: 'receipt' as 'receipt' | 'return' | 'adjustment', quantity: '' })
function openAdj(warehouseId?: number) {
  adjForm.warehouse_id = warehouseId ?? warehouses.value[0]?.id ?? null
  adjForm.type = 'receipt'
  adjForm.quantity = ''
  adjDialog.value = true
}
async function saveAdj() {
  if (!loadedProduct.value || !adjForm.warehouse_id || !adjForm.quantity) {
    toast.add({ severity: 'warn', summary: 'warehouse and quantity required', life: 3000 })
    return
  }
  savingAdj.value = true
  const { error } = await api.POST('/admin/inventory/adjustments', {
    body: { product_id: loadedProduct.value, warehouse_id: adjForm.warehouse_id, type: adjForm.type, quantity: adjForm.quantity },
  })
  savingAdj.value = false
  if (error) {
    toast.add({ severity: 'error', summary: 'Failed', detail: errMessage(error), life: 4000 })
    return
  }
  adjDialog.value = false
  toast.add({ severity: 'success', summary: 'Movement recorded', life: 2000 })
  loadLevels()
}

// --- level config ---
const cfgDialog = ref(false)
const savingCfg = ref(false)
const cfgForm = reactive({ warehouse_id: null as number | null, reorder_threshold: '', allow_backorder: false })
function openCfg(row?: LevelRow) {
  cfgForm.warehouse_id = row?.warehouse_id ?? warehouses.value[0]?.id ?? null
  cfgForm.reorder_threshold = row?.reorder_threshold ?? ''
  cfgForm.allow_backorder = row?.allow_backorder ?? false
  cfgDialog.value = true
}
async function saveCfg() {
  if (!loadedProduct.value || !cfgForm.warehouse_id) return
  savingCfg.value = true
  const { error } = await api.PUT('/admin/inventory/{productId}', {
    params: { path: { productId: loadedProduct.value } },
    body: {
      warehouse_id: cfgForm.warehouse_id,
      reorder_threshold: cfgForm.reorder_threshold || null,
      allow_backorder: cfgForm.allow_backorder,
    },
  })
  savingCfg.value = false
  if (error) {
    toast.add({ severity: 'error', summary: 'Failed', detail: errMessage(error), life: 4000 })
    return
  }
  cfgDialog.value = false
  toast.add({ severity: 'success', summary: 'Config saved', life: 2000 })
  loadLevels()
}

// --- movement ledger ---
const movements = ref<Movement[]>([])
async function loadMovements(row: LevelRow) {
  const { data } = await api.GET('/admin/inventory/movements', {
    params: { query: { product_id: row.product_id, warehouse_id: row.warehouse_id } },
  })
  movements.value = data?.items ?? []
}

onMounted(loadWarehouses)
</script>

<template>
  <div class="page">
    <h1>Inventory</h1>

    <div class="grid">
      <Card>
        <template #title>
          <div class="ch"><span>Warehouses</span><Button icon="pi pi-plus" label="Add" size="small" @click="whDialog = true" /></div>
        </template>
        <template #content>
          <DataTable :value="warehouses" dataKey="id" stripedRows>
            <template #empty>No warehouses — add one to track stock.</template>
            <Column field="id" header="ID" style="width: 4rem" />
            <Column field="name" header="Name" />
            <Column header="Active"><template #body="{ data }"><Tag :value="data.is_active ? 'active' : 'inactive'" :severity="data.is_active ? 'success' : 'secondary'" /></template></Column>
          </DataTable>
        </template>
      </Card>

      <Card>
        <template #title>Product stock</template>
        <template #content>
          <div class="lookup">
            <InputNumber v-model="productId" placeholder="Product ID" :useGrouping="false" />
            <Button label="Load" icon="pi pi-search" @click="loadLevels" />
            <span class="spacer" />
            <Button label="Set config" severity="secondary" outlined size="small" :disabled="!loadedProduct" @click="openCfg()" />
            <Button label="Adjust stock" icon="pi pi-sliders-h" size="small" :disabled="!loadedProduct" @click="openAdj()" />
          </div>
          <Message v-if="lookupError" severity="error" :closable="false" class="mt">{{ lookupError }}</Message>

          <DataTable v-if="loadedProduct" :value="levels" dataKey="id" stripedRows class="mt">
            <template #empty>No stock levels for this product yet (untracked). Use “Adjust stock”.</template>
            <Column field="warehouse_id" header="WH" />
            <Column field="quantity_on_hand" header="On hand" />
            <Column field="quantity_reserved" header="Reserved" />
            <Column header="Available"><template #body="{ data }"><strong>{{ data.available }}</strong></template></Column>
            <Column header="Reorder"><template #body="{ data }">{{ data.reorder_threshold ?? '—' }}</template></Column>
            <Column header="Backorder"><template #body="{ data }"><Tag v-if="data.allow_backorder" value="allowed" severity="info" /></template></Column>
            <Column header="">
              <template #body="{ data }">
                <Button icon="pi pi-cog" text rounded severity="secondary" @click="openCfg(data)" />
                <Button icon="pi pi-history" text rounded severity="secondary" @click="loadMovements(data)" />
              </template>
            </Column>
          </DataTable>

          <div v-if="movements.length" class="mt">
            <h3>Movement ledger</h3>
            <DataTable :value="movements" dataKey="id" stripedRows>
              <Column field="type" header="Type"><template #body="{ data }"><Tag :value="data.type" severity="secondary" /></template></Column>
              <Column field="quantity" header="Qty (signed)" />
              <Column field="reference_type" header="Ref" />
              <Column header="When"><template #body="{ data }">{{ new Date(data.created_at).toLocaleString() }}</template></Column>
            </DataTable>
          </div>
        </template>
      </Card>
    </div>

    <Dialog v-model:visible="whDialog" header="New warehouse" modal :style="{ width: '380px' }">
      <div class="field"><label>Name</label><InputText v-model="whName" fluid /></div>
      <template #footer>
        <Button label="Cancel" severity="secondary" text @click="whDialog = false" />
        <Button label="Save" :loading="savingWh" @click="createWarehouse" />
      </template>
    </Dialog>

    <Dialog v-model:visible="adjDialog" header="Adjust stock" modal :style="{ width: '420px' }">
      <form class="form" @submit.prevent="saveAdj">
        <div class="field">
          <label>Warehouse</label>
          <Select v-model="adjForm.warehouse_id" :options="warehouses" optionLabel="name" optionValue="id" fluid />
        </div>
        <div class="field">
          <label>Type</label>
          <Select v-model="adjForm.type" :options="['receipt', 'return', 'adjustment']" fluid />
        </div>
        <div class="field">
          <label>Quantity (signed; negative to decrease)</label>
          <InputText v-model="adjForm.quantity" fluid />
        </div>
      </form>
      <template #footer>
        <Button label="Cancel" severity="secondary" text @click="adjDialog = false" />
        <Button label="Apply" :loading="savingAdj" @click="saveAdj" />
      </template>
    </Dialog>

    <Dialog v-model:visible="cfgDialog" header="Level config" modal :style="{ width: '420px' }">
      <form class="form" @submit.prevent="saveCfg">
        <div class="field">
          <label>Warehouse</label>
          <Select v-model="cfgForm.warehouse_id" :options="warehouses" optionLabel="name" optionValue="id" fluid />
        </div>
        <div class="field"><label>Reorder threshold</label><InputText v-model="cfgForm.reorder_threshold" fluid /></div>
        <div class="check"><Checkbox v-model="cfgForm.allow_backorder" binary inputId="bo" /><label for="bo">Allow backorder</label></div>
      </form>
      <template #footer>
        <Button label="Cancel" severity="secondary" text @click="cfgDialog = false" />
        <Button label="Save" :loading="savingCfg" @click="saveCfg" />
      </template>
    </Dialog>
  </div>
</template>

<style scoped>
.page h1 { margin: 0 0 1rem; }
.grid { display: grid; grid-template-columns: 1fr; gap: 1rem; }
.ch { display: flex; align-items: center; justify-content: space-between; }
.lookup { display: flex; align-items: center; gap: 0.5rem; }
.spacer { flex: 1; }
.mt { margin-top: 1rem; }
.mt h3 { margin: 0 0 0.5rem; }
.form { display: flex; flex-direction: column; gap: 0.9rem; }
.field { display: flex; flex-direction: column; gap: 0.3rem; }
.field label { font-size: 0.8rem; font-weight: 600; }
.check { display: flex; align-items: center; gap: 0.5rem; }
</style>
