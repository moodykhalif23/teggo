<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useToast } from 'primevue/usetoast'
import Select from 'primevue/select'
import InputText from 'primevue/inputtext'
import InputNumber from 'primevue/inputnumber'
import Checkbox from 'primevue/checkbox'
import Button from 'primevue/button'
import Tag from 'primevue/tag'
import Message from 'primevue/message'
import { api, errMessage } from '@/lib/client'
import type { components } from '@teggo/api/schema'

type CpqDefinition = components['schemas']['CpqDefinition']
type ConfigureResult = components['schemas']['ConfigureResult']

const toast = useToast()
const products = ref<{ id: number; public_id: string; name: string; sku: string }[]>([])
const selected = ref<{ id: number; public_id: string } | null>(null)
const def = ref<CpqDefinition | null>(null)
const error = ref('')

const base = reactive({ base_price: '0', currency: 'USD' })
const group = reactive({ code: '', name: '', required: true, min_select: 1, max_select: 1 })
const optionForms = reactive<Record<number, { code: string; name: string; price_delta: string }>>({})
const rule = reactive({ kind: 'requires', option_id: null as number | null, related_option_id: null as number | null })

// live preview
const picked = ref<Record<number, boolean>>({})
const preview = ref<ConfigureResult | null>(null)

const allOptions = computed(() =>
  (def.value?.groups ?? []).flatMap((g) => (g.options ?? []).map((o) => ({ ...o, group: g.code }))),
)

async function loadProducts() {
  const { data } = await api.GET('/admin/products')
  // Only configurable products are eligible.
  products.value = ((data?.items as any[]) ?? []).filter((p) => p.type === 'configurable')
}

async function loadConfig() {
  error.value = ''
  def.value = null
  preview.value = null
  picked.value = {}
  if (!selected.value) return
  const { data, error: err } = await api.GET('/admin/products/{id}/config', { params: { path: { id: selected.value.id } } })
  if (err) {
    // 404 = not configured yet; show the base-price form.
    base.base_price = '0'
    base.currency = 'USD'
    return
  }
  def.value = data ?? null
  if (data) {
    base.base_price = data.base_price ?? '0'
    base.currency = data.currency ?? 'USD'
  }
}

async function saveBase() {
  if (!selected.value) return
  const { error: err } = await api.PUT('/admin/products/{id}/config', {
    params: { path: { id: selected.value.id } },
    body: { base_price: base.base_price, currency: base.currency, is_active: true },
  })
  if (err) {
    toast.add({ severity: 'error', summary: errMessage(err, 'Save failed'), life: 4000 })
    return
  }
  toast.add({ severity: 'success', summary: 'Config saved', life: 2000 })
  loadConfig()
}

async function addGroup() {
  if (!selected.value || !group.code || !group.name) return
  const { error: err } = await api.POST('/admin/products/{id}/option-groups', {
    params: { path: { id: selected.value.id } },
    body: { ...group },
  })
  if (err) {
    toast.add({ severity: 'error', summary: errMessage(err, 'Group failed'), life: 4000 })
    return
  }
  group.code = ''
  group.name = ''
  loadConfig()
}

async function addOption(gid: number) {
  const f = optionForms[gid]
  if (!f || !f.code || !f.name) return
  const { error: err } = await api.POST('/admin/option-groups/{gid}/options', {
    params: { path: { gid } },
    body: { code: f.code, name: f.name, price_delta: f.price_delta || '0' },
  })
  if (err) {
    toast.add({ severity: 'error', summary: errMessage(err, 'Option failed'), life: 4000 })
    return
  }
  optionForms[gid] = { code: '', name: '', price_delta: '' }
  loadConfig()
}

async function addRule() {
  if (!selected.value || !rule.option_id || !rule.related_option_id) return
  const { error: err } = await api.POST('/admin/products/{id}/config-rules', {
    params: { path: { id: selected.value.id } },
    body: { kind: rule.kind as 'requires' | 'excludes', option_id: rule.option_id, related_option_id: rule.related_option_id },
  })
  if (err) {
    toast.add({ severity: 'error', summary: errMessage(err, 'Rule failed'), life: 4000 })
    return
  }
  loadConfig()
}

async function runPreview() {
  if (!selected.value) return
  const selections = Object.entries(picked.value).filter(([, v]) => v).map(([k]) => Number(k))
  const { data } = await api.POST('/storefront/products/{publicID}/configure', {
    params: { path: { publicID: selected.value.public_id } },
    body: { selections },
  })
  preview.value = data ?? null
}

function optionForm(gid: number) {
  if (!optionForms[gid]) optionForms[gid] = { code: '', name: '', price_delta: '' }
  return optionForms[gid]
}

onMounted(loadProducts)
</script>

<template>
  <div class="page">
    <h1>Product configurator <span class="muted">CPQ</span></h1>
    <p class="muted">Define option groups, options (with price deltas), and rules for configurable products. The price preview uses the same engine the storefront and quotes use.</p>

    <div class="field">
      <label>Configurable product</label>
      <Select v-model="selected" :options="products" optionLabel="name" placeholder="Select a product"
              :emptyMessage="'No configurable products (set product type = configurable)'" @change="loadConfig" />
    </div>

    <Message v-if="error" severity="error" :closable="false">{{ error }}</Message>

    <template v-if="selected">
      <div class="card">
        <h3>Base price</h3>
        <div class="row">
          <div class="field"><label>Base price</label><InputText v-model="base.base_price" /></div>
          <div class="field"><label>Currency</label><InputText v-model="base.currency" maxlength="3" /></div>
          <Button label="Save" @click="saveBase" />
        </div>
      </div>

      <template v-if="def">
        <div class="cols">
          <div class="card">
            <h3>Option groups</h3>
            <div v-for="g in def.groups" :key="g.id" class="grp">
              <div class="grp-head"><strong>{{ g.name }}</strong> <span class="muted">{{ g.code }} · {{ g.required ? 'required' : 'optional' }} · {{ g.min_select }}–{{ g.max_select }}</span></div>
              <ul>
                <li v-for="o in g.options" :key="o.id"><code>#{{ o.id }}</code> {{ o.name }} <span class="muted">({{ o.code }}, +{{ o.price_delta }})</span></li>
              </ul>
              <div class="mrow">
                <InputText v-model="optionForm(g.id).code" placeholder="code" />
                <InputText v-model="optionForm(g.id).name" placeholder="name" />
                <InputText v-model="optionForm(g.id).price_delta" placeholder="+price" />
                <Button icon="pi pi-plus" size="small" @click="addOption(g.id)" />
              </div>
            </div>
            <h4>Add group</h4>
            <div class="mrow">
              <InputText v-model="group.code" placeholder="code" />
              <InputText v-model="group.name" placeholder="name" />
              <Checkbox v-model="group.required" :binary="true" /> <span class="muted">req</span>
              <InputNumber v-model="group.min_select" :min="0" showButtons style="width:5rem" />
              <InputNumber v-model="group.max_select" :min="1" showButtons style="width:5rem" />
              <Button icon="pi pi-plus" label="Group" size="small" @click="addGroup" />
            </div>
            <h4>Add rule</h4>
            <div class="mrow">
              <Select v-model="rule.kind" :options="['requires', 'excludes']" />
              <Select v-model="rule.option_id" :options="allOptions" optionLabel="name" optionValue="id" placeholder="if" />
              <Select v-model="rule.related_option_id" :options="allOptions" optionLabel="name" optionValue="id" placeholder="then" />
              <Button icon="pi pi-plus" label="Rule" size="small" @click="addRule" />
            </div>
            <ul class="rules">
              <li v-for="(r, i) in def.rules" :key="i"><Tag :value="r.kind" /> #{{ r.option_id }} → #{{ r.related_option_id }}</li>
            </ul>
          </div>

          <div class="card">
            <h3>Price preview</h3>
            <div v-for="g in def.groups" :key="g.id" class="grp">
              <div class="grp-head"><strong>{{ g.name }}</strong></div>
              <label v-for="o in g.options" :key="o.id" class="opt"><Checkbox v-model="picked[o.id]" :binary="true" /> {{ o.name }} <span class="muted">+{{ o.price_delta }}</span></label>
            </div>
            <Button label="Configure" icon="pi pi-calculator" @click="runPreview" />
            <div v-if="preview" class="preview">
              <template v-if="preview.valid">
                <Tag value="valid" severity="success" /> <strong class="price">{{ preview.unit_price }} {{ preview.currency }}</strong>
              </template>
              <template v-else>
                <Tag value="invalid" severity="danger" />
                <ul><li v-for="(e, i) in preview.errors" :key="i" class="err">{{ e }}</li></ul>
              </template>
            </div>
          </div>
        </div>
      </template>
      <Message v-else severity="info" :closable="false">No configuration yet — set a base price to begin.</Message>
    </template>
  </div>
</template>

<style scoped>
h1 { margin: 0 0 0.25rem; }
.muted { color: var(--p-text-muted-color, #64748b); font-weight: 400; }
.card { border: 1px solid var(--teggo-border, #cbd5e1); border-radius: var(--teggo-radius, 2px); padding: 1rem; margin: 1rem 0; }
.cols { display: grid; grid-template-columns: 1fr 1fr; gap: 1rem; }
.row { display: flex; gap: 0.75rem; align-items: flex-end; }
.field { display: flex; flex-direction: column; gap: 0.35rem; margin-bottom: 1rem; }
.field label { font-size: 0.85rem; font-weight: 600; }
.mrow { display: flex; gap: 0.5rem; align-items: center; margin: 0.5rem 0; flex-wrap: wrap; }
.mrow :deep(.p-inputtext) { width: 8rem; }
.grp { border-top: 1px solid var(--p-surface-100, #f1f5f9); padding: 0.5rem 0; }
.grp-head { margin-bottom: 0.25rem; }
.grp ul { margin: 0.25rem 0; padding-left: 1.25rem; font-size: 0.9rem; }
.opt { display: flex; align-items: center; gap: 0.5rem; padding: 0.2rem 0; }
.rules { list-style: none; padding: 0; font-size: 0.9rem; }
.preview { margin-top: 1rem; }
.price { font-size: 1.3rem; }
.err { color: var(--p-red-500, #ef4444); font-size: 0.9rem; }
h3 { margin: 0 0 0.5rem; }
h4 { margin: 0.75rem 0 0.25rem; }
</style>
