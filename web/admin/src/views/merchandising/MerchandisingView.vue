<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { useToast } from 'primevue/usetoast'
import DataTable from 'primevue/datatable'
import Column from 'primevue/column'
import Button from 'primevue/button'
import InputText from 'primevue/inputtext'
import InputNumber from 'primevue/inputnumber'
import Select from 'primevue/select'
import Tag from 'primevue/tag'
import { api, errMessage } from '@/lib/client'
import { useProductOptions } from '@/composables/useRecordOptions'
import type { components } from '@teggo/api/schema'
import PageHeader from '@/components/PageHeader.vue'

type Synonym = components['schemas']['SearchSynonym']
type Redirect = components['schemas']['SearchRedirect']
type Rule = components['schemas']['MerchandisingRule']

const toast = useToast()
const { productOptions, productsLoaded, loadProducts } = useProductOptions()

const synonyms = ref<Synonym[]>([])
const redirects = ref<Redirect[]>([])
const rules = ref<Rule[]>([])

const synForm = reactive({ term: '', synonyms: '' })
const redForm = reactive({ query: '', target: '' })
const ruleForm = reactive<{ scope_type: 'query' | 'category'; scope_value: string; product_id: number | null; action: 'pin' | 'boost' | 'bury'; position: number }>({
  scope_type: 'query', scope_value: '', product_id: null, action: 'pin', position: 0,
})
const scopeTypes = ['query', 'category']
const actions = ['pin', 'boost', 'bury']

async function load() {
  const [s, rd, ru] = await Promise.all([
    api.GET('/admin/search-synonyms'),
    api.GET('/admin/search-redirects'),
    api.GET('/admin/merchandising-rules'),
  ])
  synonyms.value = s.data?.items ?? []
  redirects.value = rd.data?.items ?? []
  rules.value = ru.data?.items ?? []
}

function fail(e: unknown, msg: string) {
  toast.add({ severity: 'error', summary: errMessage(e, msg), life: 4000 })
}

async function saveSynonym() {
  if (!synForm.term.trim() || !synForm.synonyms.trim()) return
  const { error } = await api.POST('/admin/search-synonyms', { body: { term: synForm.term.trim(), synonyms: synForm.synonyms.trim() } })
  if (error) return fail(error, 'Save failed')
  synForm.term = ''
  synForm.synonyms = ''
  load()
}
async function delSynonym(s: Synonym) {
  const { error } = await api.DELETE('/admin/search-synonyms/{id}', { params: { path: { id: s.id } } })
  if (!error) load()
}

async function saveRedirect() {
  if (!redForm.query.trim() || !redForm.target.trim()) return
  const { error } = await api.POST('/admin/search-redirects', { body: { query: redForm.query.trim(), target: redForm.target.trim() } })
  if (error) return fail(error, 'Save failed')
  redForm.query = ''
  redForm.target = ''
  load()
}
async function delRedirect(rd: Redirect) {
  const { error } = await api.DELETE('/admin/search-redirects/{id}', { params: { path: { id: rd.id } } })
  if (!error) load()
}

async function saveRule() {
  if (!ruleForm.scope_value.trim() || !ruleForm.product_id) {
    toast.add({ severity: 'warn', summary: 'Scope value and product are required', life: 3000 })
    return
  }
  const { error } = await api.POST('/admin/merchandising-rules', {
    body: {
      scope_type: ruleForm.scope_type,
      scope_value: ruleForm.scope_value.trim(),
      product_id: ruleForm.product_id,
      action: ruleForm.action,
      position: ruleForm.position,
    },
  })
  if (error) return fail(error, 'Save failed')
  ruleForm.scope_value = ''
  ruleForm.product_id = null
  load()
}
async function delRule(ru: Rule) {
  const { error } = await api.DELETE('/admin/merchandising-rules/{id}', { params: { path: { id: ru.id } } })
  if (!error) load()
}

function actionSev(a: string) {
  return a === 'pin' ? 'success' : a === 'boost' ? 'info' : 'warn'
}

onMounted(() => {
  load()
  loadProducts()
})
</script>

<template>
  <div class="page">
    <PageHeader title="Search merchandising" />
    <p class="muted mb">Curate Postgres full-text search: expand queries with synonyms, jump common queries to a page, and pin / boost / bury products for a query or category.</p>

    <!-- Synonyms -->
    <section class="block">
      <h2>Synonyms</h2>
      <p class="muted small">When a buyer searches the <strong>term</strong>, results also match the synonym words.</p>
      <div class="addrow">
        <InputText v-model="synForm.term" placeholder="Term (e.g. tee)" class="grow" />
        <InputText v-model="synForm.synonyms" placeholder="Synonyms (e.g. t-shirt shirt)" class="grow2" />
        <Button label="Add" icon="pi pi-plus" @click="saveSynonym" />
      </div>
      <DataTable :value="synonyms" dataKey="id" stripedRows>
        <template #empty><span class="muted">No synonyms.</span></template>
        <Column field="term" header="Term" />
        <Column field="synonyms" header="Synonyms" />
        <Column header="" style="width: 4rem"><template #body="{ data }"><Button icon="pi pi-trash" text rounded severity="danger" @click="delSynonym(data)" /></template></Column>
      </DataTable>
    </section>

    <!-- Redirects -->
    <section class="block">
      <h2>Query redirects</h2>
      <p class="muted small">An exact query sends the buyer straight to a page (e.g. a campaign or category).</p>
      <div class="addrow">
        <InputText v-model="redForm.query" placeholder="Query (e.g. sale)" class="grow" />
        <InputText v-model="redForm.target" placeholder="Target path (e.g. /c/clearance)" class="grow2" />
        <Button label="Add" icon="pi pi-plus" @click="saveRedirect" />
      </div>
      <DataTable :value="redirects" dataKey="id" stripedRows>
        <template #empty><span class="muted">No redirects.</span></template>
        <Column field="query" header="Query" />
        <Column field="target" header="Target" />
        <Column header="" style="width: 4rem"><template #body="{ data }"><Button icon="pi pi-trash" text rounded severity="danger" @click="delRedirect(data)" /></template></Column>
      </DataTable>
    </section>

    <!-- Rules -->
    <section class="block">
      <h2>Pin / boost / bury</h2>
      <p class="muted small">Re-rank a product for a search query or a category. Pins float to the top (by position); boost moves up; bury moves down.</p>
      <div class="addrow wrap">
        <Select v-model="ruleForm.scope_type" :options="scopeTypes" class="sm" />
        <InputText v-model="ruleForm.scope_value" :placeholder="ruleForm.scope_type === 'query' ? 'query text' : 'category slug'" class="grow" />
        <Select v-model="ruleForm.product_id" :options="productOptions" optionLabel="label" optionValue="id" filter
          filterPlaceholder="Search products…" placeholder="Product" :emptyMessage="productsLoaded ? 'No products' : 'Loading…'" showClear class="grow2" />
        <Select v-model="ruleForm.action" :options="actions" class="sm" />
        <InputNumber v-model="ruleForm.position" :min="0" showButtons class="pos" />
        <Button label="Add rule" icon="pi pi-plus" @click="saveRule" />
      </div>
      <DataTable :value="rules" dataKey="id" stripedRows>
        <template #empty><span class="muted">No rules.</span></template>
        <Column header="Scope"><template #body="{ data }">{{ data.scope_type }}: <strong>{{ data.scope_value }}</strong></template></Column>
        <Column header="Product"><template #body="{ data }">{{ data.sku }} — {{ data.name }}</template></Column>
        <Column header="Action"><template #body="{ data }"><Tag :value="data.action" :severity="actionSev(data.action)" /></template></Column>
        <Column field="position" header="Pos" />
        <Column header="" style="width: 4rem"><template #body="{ data }"><Button icon="pi pi-trash" text rounded severity="danger" @click="delRule(data)" /></template></Column>
      </DataTable>
    </section>
  </div>
</template>

<style scoped>
.muted { color: var(--p-text-muted-color, #64748b); font-weight: 400; }
.small { font-size: 0.85rem; }
.mb { margin-bottom: 1rem; }
.block { margin-bottom: 2rem; }
.block h2 { font-size: 1rem; margin: 0 0 0.25rem; }
.addrow { display: flex; align-items: center; gap: 0.6rem; margin: 0.5rem 0 0.75rem; }
.addrow.wrap { flex-wrap: wrap; }
.addrow .grow { flex: 1; min-width: 8rem; }
.addrow .grow2 { flex: 2; min-width: 10rem; }
.addrow .sm { width: 8rem; }
.addrow .pos { width: 7rem; }
</style>
