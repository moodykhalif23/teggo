<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useToast } from 'primevue/usetoast'
import DataTable from 'primevue/datatable'
import Column from 'primevue/column'
import Tag from 'primevue/tag'
import Button from 'primevue/button'
import InputText from 'primevue/inputtext'
import Textarea from 'primevue/textarea'
import Message from 'primevue/message'
import Fieldset from 'primevue/fieldset'
import Dialog from 'primevue/dialog'
import { api, errMessage } from '@/lib/client'
import type { components } from '@teggo/api/schema'
import type { Block } from '@teggo/blocks'
import BlockPalette from './blocks/BlockPalette.vue'
import BlockCanvas from './blocks/BlockCanvas.vue'
import BlockInspector from './blocks/BlockInspector.vue'
import { makeBlock, newBlockId } from './blocks/fields'

type Page = components['schemas']['ContentPage']

const pages = ref<Page[]>([])
const loading = ref(false)
const error = ref('')
const toast = useToast()

// 'list' shows the table; 'edit' shows the builder.
const mode = ref<'list' | 'edit'>('list')
const editingId = ref<number | null>(null)
const saving = ref(false)
const formError = ref('')

const title = ref('')
const slug = ref('')
const seo = ref<Record<string, any>>({})
const blocks = ref<Block[]>([])
const selectedId = ref<string | null>(null)
const categories = ref<components['schemas']['Category'][]>([])

const advancedJson = ref('')

// AI page generation.
const aiOpen = ref(false)
const aiPrompt = ref('')
const aiBusy = ref(false)

const selectedBlock = computed(() => blocks.value.find((b) => b.id === selectedId.value) ?? null)

async function load() {
  loading.value = true
  error.value = ''
  const { data, error: err } = await api.GET('/admin/pages')
  loading.value = false
  if (err || !data) {
    error.value = errMessage(err, 'Failed to load pages')
    return
  }
  pages.value = data.items ?? []
}

// Loaded lazily once when the builder opens — feeds the product-grid category picker.
async function loadCategories() {
  if (categories.value.length) return
  const { data } = await api.GET('/admin/categories')
  if (data?.items) categories.value = data.items
}

function openCreate() {
  editingId.value = null
  title.value = ''
  slug.value = ''
  seo.value = {}
  blocks.value = [makeBlock('rich-text')]
  selectedId.value = blocks.value[0].id ?? null
  formError.value = ''
  mode.value = 'edit'
  loadCategories()
}

function openEdit(p: Page) {
  editingId.value = p.id
  title.value = p.title
  slug.value = p.slug
  seo.value = JSON.parse(JSON.stringify(p.seo ?? {}))
  // Clone blocks and guarantee every block has a stable id for the builder.
  blocks.value = ((p.blocks as Block[] | undefined) ?? []).map((b) => ({
    type: b.type,
    id: b.id || newBlockId(),
    props: JSON.parse(JSON.stringify(b.props ?? {})),
  }))
  selectedId.value = blocks.value[0]?.id ?? null
  formError.value = ''
  mode.value = 'edit'
  loadCategories()
}

function backToList() {
  mode.value = 'list'
}

function addBlock(type: string) {
  const b = makeBlock(type)
  blocks.value.push(b)
  selectedId.value = b.id ?? null
}
function duplicateBlock(index: number) {
  const src = blocks.value[index]
  const copy: Block = { type: src.type, id: newBlockId(), props: JSON.parse(JSON.stringify(src.props ?? {})) }
  blocks.value.splice(index + 1, 0, copy)
  selectedId.value = copy.id ?? null
}
function removeBlock(index: number) {
  const removed = blocks.value[index]
  blocks.value.splice(index, 1)
  if (removed.id === selectedId.value) selectedId.value = blocks.value[0]?.id ?? null
}

function loadAdvancedFromBuilder() {
  advancedJson.value = JSON.stringify(blocks.value, null, 2)
}
function applyAdvancedToBuilder() {
  try {
    const parsed = JSON.parse(advancedJson.value)
    if (!Array.isArray(parsed)) throw new Error('not an array')
    blocks.value = parsed.map((b: any) => ({ type: b.type, id: b.id || newBlockId(), props: b.props ?? {} }))
    selectedId.value = blocks.value[0]?.id ?? null
    toast.add({ severity: 'success', summary: 'Applied JSON to builder', life: 2000 })
  } catch {
    toast.add({ severity: 'error', summary: 'Invalid blocks JSON', life: 3500 })
  }
}

async function generateWithAI() {
  if (!aiPrompt.value.trim() || aiBusy.value) return
  aiBusy.value = true
  const { data, error: err } = await api.POST('/admin/pages/ai-generate', {
    body: { prompt: aiPrompt.value.trim() },
  })
  aiBusy.value = false
  if (err || !data) {
    toast.add({ severity: 'error', summary: 'Generation failed', detail: errMessage(err), life: 4000 })
    return
  }
  // The generated blocks share the builder's shape — load them into the canvas.
  blocks.value = ((data.blocks as Block[] | undefined) ?? []).map((b: any) => ({
    type: b.type,
    id: b.id || newBlockId(),
    props: b.props ?? {},
  }))
  selectedId.value = blocks.value[0]?.id ?? null
  aiOpen.value = false
  toast.add({
    severity: 'success',
    summary: 'Page generated',
    detail: data.notes || 'Review and tweak the blocks, then save.',
    life: 5000,
  })
}

async function save() {
  if (!title.value || !slug.value) {
    formError.value = 'Title and slug are required.'
    return
  }
  saving.value = true
  formError.value = ''
  const body = {
    title: title.value,
    slug: slug.value,
    blocks: blocks.value as unknown as Record<string, unknown>[],
    seo: seo.value,
  }
  const { error: err } = editingId.value
    ? await api.PUT('/admin/pages/{id}', { params: { path: { id: editingId.value } }, body })
    : await api.POST('/admin/pages', { body })
  saving.value = false
  if (err) {
    formError.value = errMessage(err, 'Save failed')
    return
  }
  toast.add({ severity: 'success', summary: 'Page saved', life: 2500 })
  mode.value = 'list'
  load()
}

async function setStatus(p: Page, action: 'publish' | 'archive') {
  const path = action === 'publish' ? '/admin/pages/{id}/publish' : '/admin/pages/{id}/archive'
  const { error: err } = await api.POST(path, { params: { path: { id: p.id } } })
  if (err) {
    toast.add({ severity: 'error', summary: 'Failed', detail: errMessage(err), life: 4000 })
    return
  }
  load()
}

function sev(s: string) {
  return s === 'published' ? 'success' : s === 'archived' ? 'secondary' : 'warn'
}

onMounted(load)
</script>

<template>
  <div class="page">
    <!-- ───────────────── LIST ───────────────── -->
    <template v-if="mode === 'list'">
      <div class="header">
        <h1>Content pages <span class="muted">({{ pages.length }})</span></h1>
        <div class="actions">
          <Button icon="pi pi-refresh" severity="secondary" text @click="load" />
          <Button icon="pi pi-plus" label="New page" @click="openCreate" />
        </div>
      </div>
      <p class="muted">Block-based pages served to the storefront. Publish to make a page public.</p>

      <Message v-if="error" severity="error" :closable="false" class="mb">{{ error }}</Message>

      <DataTable :value="pages" :loading="loading" dataKey="id" stripedRows>
        <template #empty>No pages yet.</template>
        <Column field="title" header="Title" />
        <Column field="slug" header="Slug" />
        <Column field="locale" header="Locale" />
        <Column header="Status">
          <template #body="{ data }"><Tag :value="data.status" :severity="sev(data.status)" /></template>
        </Column>
        <Column header="" style="width: 16rem">
          <template #body="{ data }">
            <Button icon="pi pi-pencil" severity="secondary" text rounded @click="openEdit(data)" />
            <Button v-if="data.status !== 'published'" label="Publish" size="small" outlined @click="setStatus(data, 'publish')" />
            <Button v-else label="Archive" size="small" severity="secondary" outlined @click="setStatus(data, 'archive')" />
          </template>
        </Column>
      </DataTable>
    </template>

    <!-- ───────────────── BUILDER ───────────────── -->
    <template v-else>
      <div class="header">
        <div class="title-line">
          <Button icon="pi pi-arrow-left" severity="secondary" text rounded @click="backToList" />
          <h1>{{ editingId ? 'Edit page' : 'New page' }}</h1>
        </div>
        <div class="actions">
          <Button label="Generate with AI" icon="pi pi-sparkles" severity="help" outlined @click="aiOpen = true" />
          <Button label="Cancel" severity="secondary" text @click="backToList" />
          <Button label="Save" icon="pi pi-check" :loading="saving" @click="save" />
        </div>
      </div>

      <Message v-if="formError" severity="error" :closable="false" class="mb">{{ formError }}</Message>

      <div class="meta-row">
        <div class="field"><label>Title</label><InputText v-model="title" /></div>
        <div class="field"><label>Slug</label><InputText v-model="slug" /></div>
      </div>

      <div class="builder">
        <aside class="pane palette-pane">
          <BlockPalette @add="addBlock" />
        </aside>
        <main class="pane canvas-pane">
          <BlockCanvas
            :blocks="blocks"
            :selected-id="selectedId"
            @select="selectedId = $event"
            @duplicate="duplicateBlock"
            @remove="removeBlock"
          />
        </main>
        <aside class="pane inspector-pane">
          <BlockInspector :block="selectedBlock" :categories="categories" />
        </aside>
      </div>

      <Fieldset legend="SEO" :toggleable="true" :collapsed="true" class="mt">
        <div class="meta-row">
          <div class="field"><label>SEO title</label><InputText v-model="seo.title" /></div>
          <div class="field"><label>SEO description</label><InputText v-model="seo.description" /></div>
        </div>
      </Fieldset>

      <Fieldset legend="Advanced (blocks JSON)" :toggleable="true" :collapsed="true" class="mt" @toggle="loadAdvancedFromBuilder">
        <p class="muted mb">Escape hatch for power users. Load the current blocks, edit raw JSON, then apply.</p>
        <Textarea v-model="advancedJson" rows="10" class="mono" autoResize />
        <div class="adv-actions">
          <Button label="Load from builder" size="small" severity="secondary" outlined @click="loadAdvancedFromBuilder" />
          <Button label="Apply to builder" size="small" outlined @click="applyAdvancedToBuilder" />
        </div>
      </Fieldset>

      <Dialog v-model:visible="aiOpen" modal header="Generate page with AI" :style="{ width: '34rem' }">
        <p class="muted mb">
          Describe the page you want. The AI builds blocks you can then tweak and save.
          <strong>This replaces the current blocks.</strong>
        </p>
        <Textarea
          v-model="aiPrompt"
          rows="4"
          autoResize
          class="ai-prompt"
          placeholder="e.g. A landing page for industrial tools — a hero, a promo banner for a clearance sale, and a grid of safety equipment."
          @keydown.enter.meta="generateWithAI"
        />
        <template #footer>
          <Button label="Cancel" severity="secondary" text @click="aiOpen = false" />
          <Button label="Generate" icon="pi pi-sparkles" :loading="aiBusy" :disabled="!aiPrompt.trim()" @click="generateWithAI" />
        </template>
      </Dialog>
    </template>
  </div>
</template>

<style scoped>
.header { display: flex; align-items: center; justify-content: space-between; }
.header h1 { margin: 0; }
.title-line { display: flex; align-items: center; gap: 0.5rem; }
.actions { display: flex; gap: 0.5rem; }
.muted { color: var(--p-text-muted-color, #64748b); font-weight: 400; }
.mb { margin-bottom: 1rem; }
.mt { margin-top: 1.25rem; }

.meta-row { display: grid; grid-template-columns: 1fr 1fr; gap: 0.75rem; margin: 1rem 0; }
.field { display: flex; flex-direction: column; gap: 0.35rem; }
.field label { font-size: 0.85rem; font-weight: 600; }

.builder {
  display: grid;
  grid-template-columns: 220px minmax(0, 1fr) 320px;
  gap: 1rem;
  align-items: start;
}
.pane { background: var(--p-surface-50, #f8fafc); border: 1px solid var(--p-surface-200, #e2e8f0); border-radius: 10px; padding: 1rem; }
.canvas-pane { background: var(--p-surface-0, #fff); }
.palette-pane, .inspector-pane { position: sticky; top: 1rem; }

.mono :deep(textarea) { font-family: ui-monospace, monospace; font-size: 0.8rem; }
.adv-actions { display: flex; gap: 0.5rem; margin-top: 0.75rem; }
.ai-prompt { width: 100%; }
</style>
