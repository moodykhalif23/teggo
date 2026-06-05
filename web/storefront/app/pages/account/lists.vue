<script setup lang="ts">
import DataTable from 'primevue/datatable'
import Column from 'primevue/column'
import Button from 'primevue/button'
import InputText from 'primevue/inputtext'
import InputNumber from 'primevue/inputnumber'
import Message from 'primevue/message'
import Tag from 'primevue/tag'
import Dialog from 'primevue/dialog'
import type { components } from '@teggo/api/schema'

definePageMeta({ middleware: 'auth' })
useSeoMeta({ title: 'Shopping lists — Teggo Store' })

type List = components['schemas']['ShoppingList']
type ListItem = components['schemas']['ShoppingListItem']

const client = useClient()
const router = useRouter()

const lists = ref<List[]>([])
const selected = ref<List | null>(null)
const items = ref<ListItem[]>([])
const error = ref('')
const notice = ref('')
const busy = ref(false)

const newName = ref('')
const renameTarget = ref<List | null>(null)
const renameVisible = ref(false)
const renameValue = ref('')

async function loadLists() {
  error.value = ''
  const { data, error: err } = await client.GET('/storefront/shopping-lists')
  if (err || !data) {
    error.value = 'Could not load your lists.'
    return
  }
  lists.value = data.items ?? []
  // Keep the selection valid after a reload.
  if (selected.value && !lists.value.some((l) => l.id === selected.value!.id)) {
    selected.value = null
    items.value = []
  }
}

async function selectList(l: List) {
  selected.value = l
  notice.value = ''
  const { data } = await client.GET('/storefront/shopping-lists/{id}/items', { params: { path: { id: l.id } } })
  items.value = data?.items ?? []
}

async function createList() {
  const name = newName.value.trim()
  if (!name) return
  busy.value = true
  const { error: err } = await client.POST('/storefront/shopping-lists', { body: { name } })
  busy.value = false
  if (err) {
    error.value = 'Could not create the list (a default may already exist).'
    return
  }
  newName.value = ''
  await loadLists()
}

function openRename(l: List) {
  renameTarget.value = l
  renameValue.value = l.name
  renameVisible.value = true
}

async function saveRename() {
  if (!renameTarget.value) return
  const name = renameValue.value.trim()
  if (!name) return
  busy.value = true
  const { error: err } = await client.PATCH('/storefront/shopping-lists/{id}', {
    params: { path: { id: renameTarget.value.id } },
    body: { name },
  })
  busy.value = false
  if (!err) {
    if (selected.value?.id === renameTarget.value.id) selected.value = { ...selected.value, name }
    renameVisible.value = false
    renameTarget.value = null
    await loadLists()
  }
}

async function deleteList(l: List) {
  busy.value = true
  const { error: err } = await client.DELETE('/storefront/shopping-lists/{id}', { params: { path: { id: l.id } } })
  busy.value = false
  if (!err) {
    if (selected.value?.id === l.id) {
      selected.value = null
      items.value = []
    }
    await loadLists()
  }
}

async function updateQty(item: ListItem, quantity: number) {
  if (!selected.value) return
  busy.value = true
  const { error: err } = await client.PATCH('/storefront/shopping-lists/{id}/items/{itemID}', {
    params: { path: { id: selected.value.id, itemID: item.id } },
    body: { quantity: String(quantity) },
  })
  busy.value = false
  if (!err) await selectList(selected.value)
}

async function removeItem(item: ListItem) {
  if (!selected.value) return
  busy.value = true
  const { error: err } = await client.DELETE('/storefront/shopping-lists/{id}/items/{itemID}', {
    params: { path: { id: selected.value.id, itemID: item.id } },
  })
  busy.value = false
  if (!err) await selectList(selected.value)
}

async function convertToCart() {
  if (!selected.value) return
  notice.value = ''
  busy.value = true
  const { data, error: err } = await client.POST('/storefront/shopping-lists/{id}/convert-to-cart', {
    params: { path: { id: selected.value.id } },
  })
  busy.value = false
  if (err || !data) {
    error.value = 'Could not add this list to your cart.'
    return
  }
  const skipped = data.skipped_product_ids ?? []
  if (skipped.length) {
    notice.value = `Added to cart. ${skipped.length} item${skipped.length > 1 ? 's' : ''} skipped (price on request).`
  } else {
    router.push('/cart')
  }
}

await loadLists()
</script>

<template>
  <section>
    <h1 class="title">Shopping lists</h1>
    <Message v-if="error" severity="error" :closable="true" class="mb">{{ error }}</Message>
    <Message v-if="notice" severity="info" :closable="true" class="mb">{{ notice }}</Message>

    <div class="grid">
      <div class="col-lists">
        <div class="create">
          <InputText v-model="newName" placeholder="New list name" :disabled="busy" @keyup.enter="createList" />
          <Button icon="pi pi-plus" :disabled="busy || !newName.trim()" @click="createList" />
        </div>
        <ul class="lists">
          <li v-for="l in lists" :key="l.id" :class="{ active: selected?.id === l.id }" @click="selectList(l)">
            <span class="name">{{ l.name }} <Tag v-if="l.is_default" value="default" severity="secondary" /></span>
            <span class="row-actions">
              <Button icon="pi pi-pencil" text rounded size="small" :disabled="busy" @click.stop="openRename(l)" />
              <Button icon="pi pi-trash" text rounded size="small" severity="danger" :disabled="busy" @click.stop="deleteList(l)" />
            </span>
          </li>
          <li v-if="!lists.length" class="empty muted">No lists yet. Create one above.</li>
        </ul>
      </div>

      <div class="col-items">
        <template v-if="selected">
          <div class="items-head">
            <h2>{{ selected.name }}</h2>
            <Button label="Add all to cart" icon="pi pi-cart-plus" :disabled="busy || !items.length" @click="convertToCart" />
          </div>
          <DataTable :value="items" dataKey="id" stripedRows>
            <template #empty>This list is empty. Add products from the catalog.</template>
            <Column field="name" header="Product" />
            <Column field="sku" header="SKU" />
            <Column header="Qty">
              <template #body="{ data }">
                <InputNumber
                  :modelValue="Number(data.quantity)"
                  :min="1"
                  :disabled="busy"
                  showButtons
                  buttonLayout="horizontal"
                  class="qty"
                  @update:modelValue="updateQty(data, $event as number)"
                >
                  <template #incrementbuttonicon><i class="pi pi-plus" /></template>
                  <template #decrementbuttonicon><i class="pi pi-minus" /></template>
                </InputNumber>
              </template>
            </Column>
            <Column>
              <template #body="{ data }">
                <Button icon="pi pi-trash" text rounded severity="danger" :disabled="busy" @click="removeItem(data)" />
              </template>
            </Column>
          </DataTable>
        </template>
        <p v-else class="muted pick">Select a list to manage its items.</p>
      </div>
    </div>

    <Dialog v-model:visible="renameVisible" modal header="Rename list" :style="{ width: '24rem' }" :closable="!busy">
      <InputText v-model="renameValue" class="w-full" :disabled="busy" @keyup.enter="saveRename" />
      <template #footer>
        <Button label="Cancel" text :disabled="busy" @click="renameVisible = false" />
        <Button label="Save" :loading="busy" @click="saveRename" />
      </template>
    </Dialog>
  </section>
</template>

<style scoped>
.title { margin: 0 0 1rem; }
.mb { margin-bottom: 1rem; }
.grid { display: grid; grid-template-columns: 18rem 1fr; gap: 1.5rem; align-items: start; }
.create { display: flex; gap: 0.5rem; margin-bottom: 0.75rem; }
.create :deep(input) { flex: 1; }
.lists { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: 0.25rem; }
.lists li {
  display: flex; align-items: center; justify-content: space-between;
  padding: 0.5rem 0.6rem; border-radius: 8px; cursor: pointer;
  border: 1px solid var(--p-surface-200, #e2e8f0);
}
.lists li.active { border-color: var(--p-primary-color, #0ea5e9); background: var(--p-surface-50, #f8fafc); }
.lists li.empty { cursor: default; border-style: dashed; }
.name { display: flex; align-items: center; gap: 0.4rem; }
.items-head { display: flex; align-items: center; justify-content: space-between; margin-bottom: 0.75rem; }
.items-head h2 { margin: 0; }
.qty { width: 9rem; }
.muted { color: var(--p-text-muted-color, #64748b); }
.pick { padding: 2rem 0; text-align: center; }
.w-full { width: 100%; }
</style>
