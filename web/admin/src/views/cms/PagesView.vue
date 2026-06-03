<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
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

type Page = components['schemas']['ContentPage']

const pages = ref<Page[]>([])
const loading = ref(false)
const error = ref('')
const toast = useToast()

const dialogOpen = ref(false)
const editingId = ref<number | null>(null)
const saving = ref(false)
const formError = ref('')
const form = reactive({ title: '', slug: '', blocks: '[]', seo: '{}' })

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

function openCreate() {
  editingId.value = null
  Object.assign(form, {
    title: '',
    slug: '',
    blocks: JSON.stringify([{ type: 'rich-text', id: 'b1', props: { html: '<p>Edit me</p>' } }], null, 2),
    seo: '{}',
  })
  formError.value = ''
  dialogOpen.value = true
}
function openEdit(p: Page) {
  editingId.value = p.id
  Object.assign(form, {
    title: p.title,
    slug: p.slug,
    blocks: JSON.stringify(p.blocks ?? [], null, 2),
    seo: JSON.stringify(p.seo ?? {}, null, 2),
  })
  formError.value = ''
  dialogOpen.value = true
}

async function save() {
  let blocks: unknown, seo: unknown
  try {
    blocks = JSON.parse(form.blocks)
    seo = JSON.parse(form.seo)
  } catch {
    formError.value = 'Blocks and SEO must be valid JSON.'
    return
  }
  if (!form.title || !form.slug) {
    formError.value = 'Title and slug are required.'
    return
  }
  saving.value = true
  const body = {
    title: form.title,
    slug: form.slug,
    blocks: blocks as Record<string, unknown>[],
    seo: seo as Record<string, unknown>,
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
  dialogOpen.value = false
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

    <Dialog v-model:visible="dialogOpen" modal :header="editingId ? 'Edit page' : 'New page'" :style="{ width: '42rem' }">
      <Message v-if="formError" severity="error" :closable="false" class="mb">{{ formError }}</Message>
      <div class="row">
        <div class="field"><label>Title</label><InputText v-model="form.title" /></div>
        <div class="field"><label>Slug</label><InputText v-model="form.slug" /></div>
      </div>
      <div class="field">
        <label>Blocks (JSON array)</label>
        <Textarea v-model="form.blocks" rows="8" class="mono" autoResize />
        <small class="muted">Types: hero, rich-text, product-grid, banner, cta</small>
      </div>
      <div class="field">
        <label>SEO (JSON object)</label>
        <Textarea v-model="form.seo" rows="3" class="mono" autoResize />
      </div>
      <template #footer>
        <Button label="Cancel" severity="secondary" text @click="dialogOpen = false" />
        <Button label="Save" :loading="saving" @click="save" />
      </template>
    </Dialog>
  </div>
</template>

<style scoped>
.header { display: flex; align-items: center; justify-content: space-between; }
.header h1 { margin: 0; }
.actions { display: flex; gap: 0.5rem; }
.muted { color: var(--p-text-muted-color, #64748b); font-weight: 400; }
.mb { margin-bottom: 1rem; }
.row { display: grid; grid-template-columns: 1fr 1fr; gap: 0.75rem; }
.field { display: flex; flex-direction: column; gap: 0.35rem; margin-bottom: 1rem; }
.field label { font-size: 0.85rem; font-weight: 600; }
.mono :deep(textarea) { font-family: ui-monospace, monospace; font-size: 0.8rem; }
</style>
