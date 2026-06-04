<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useToast } from 'primevue/usetoast'
import Button from 'primevue/button'
import Dialog from 'primevue/dialog'
import InputText from 'primevue/inputtext'
import InputNumber from 'primevue/inputnumber'
import Select from 'primevue/select'
import Tag from 'primevue/tag'
import Message from 'primevue/message'
import DataTable from 'primevue/datatable'
import Column from 'primevue/column'
import { api, errMessage } from '@/lib/client'
import { useAuthStore } from '@/stores/auth'
import type { components } from '@teggo/api/schema'

type MediaAsset = components['schemas']['MediaAsset']
type Preset = components['schemas']['TransformationPreset']

const auth = useAuthStore()
const toast = useToast()
const apiBase = import.meta.env.VITE_API_BASE_URL ?? ''

const assets = ref<MediaAsset[]>([])
const presets = ref<Preset[]>([])
const loading = ref(false)
const error = ref('')

// Resolve an API-relative URL (/media/...) against the API base for <img src>.
const src = (u?: string | null) => (u ? `${apiBase}${u}` : '')

async function load() {
  loading.value = true
  error.value = ''
  const { data, error: err } = await api.GET('/admin/media')
  const { data: pdata } = await api.GET('/admin/transformation-presets')
  loading.value = false
  if (err || !data) {
    error.value = errMessage(err, 'Failed to load media')
    return
  }
  assets.value = data.items ?? []
  presets.value = pdata?.items ?? []
}

// ---- upload (multipart; plain fetch — the typed client is JSON-oriented) ----
const fileInput = ref<HTMLInputElement | null>(null)
const uploading = ref(false)
const uploadTags = ref('')

function pickFile() {
  fileInput.value?.click()
}
async function onFile(e: Event) {
  const input = e.target as HTMLInputElement
  const file = input.files?.[0]
  if (!file) return
  uploading.value = true
  try {
    const fd = new FormData()
    fd.append('file', file)
    if (uploadTags.value.trim()) fd.append('tags', uploadTags.value.trim())
    const res = await fetch(`${apiBase}/admin/media`, {
      method: 'POST',
      headers: { Authorization: `Bearer ${auth.token ?? ''}` },
      body: fd,
    })
    if (!res.ok) {
      const body = await res.json().catch(() => ({}))
      throw new Error(body.message ?? `Upload failed (${res.status})`)
    }
    toast.add({ severity: 'success', summary: 'Uploaded', life: 2500 })
    uploadTags.value = ''
    await load()
  } catch (err) {
    toast.add({ severity: 'error', summary: errMessage(err, 'Upload failed'), life: 4000 })
  } finally {
    uploading.value = false
    input.value = ''
  }
}

// ---- asset detail / edit ----
const detail = ref<MediaAsset | null>(null)
const detailOpen = ref(false)
const editTags = ref('')
const saving = ref(false)

async function openDetail(a: MediaAsset) {
  const { data } = await api.GET('/admin/media/{id}', { params: { path: { id: a.id } } })
  detail.value = data ?? a
  editTags.value = (detail.value?.tags ?? []).join(', ')
  detailOpen.value = true
}

async function saveMeta() {
  if (!detail.value) return
  saving.value = true
  const tags = editTags.value.split(',').map((t) => t.trim()).filter(Boolean)
  const { data, error: err } = await api.PUT('/admin/media/{id}', {
    params: { path: { id: detail.value.id } },
    body: { alt: detail.value.alt, folder: detail.value.folder, tags },
  })
  saving.value = false
  if (err) {
    toast.add({ severity: 'error', summary: errMessage(err, 'Save failed'), life: 4000 })
    return
  }
  detail.value = data ?? detail.value
  toast.add({ severity: 'success', summary: 'Saved', life: 2000 })
  load()
}

// ---- presets ----
const presetOpen = ref(false)
const savingPreset = ref(false)
const presetForm = reactive({ name: '', width: 400, height: 400, fit: 'cover', format: 'jpeg', quality: 82 })
const fitOptions = ['cover', 'contain', 'fill', 'inside']
const formatOptions = ['jpeg', 'png', 'webp', 'avif']

async function savePreset() {
  if (!presetForm.name) return
  savingPreset.value = true
  const { error: err } = await api.POST('/admin/transformation-presets', { body: { ...presetForm } })
  savingPreset.value = false
  if (err) {
    toast.add({ severity: 'error', summary: errMessage(err, 'Preset save failed'), life: 4000 })
    return
  }
  toast.add({ severity: 'success', summary: 'Preset created', life: 2000 })
  presetOpen.value = false
  presetForm.name = ''
  load()
}

const statusSeverity = (s?: string) =>
  s === 'ready' ? 'success' : s === 'error' ? 'danger' : 'warn'

const transformList = computed(() =>
  detail.value?.transforms ? Object.entries(detail.value.transforms) : [],
)

onMounted(load)
</script>

<template>
  <div class="page">
    <div class="header">
      <h1>Media library</h1>
      <div class="actions">
        <InputText v-model="uploadTags" placeholder="tags (comma-sep)" class="tags-in" />
        <input ref="fileInput" type="file" accept="image/*" hidden @change="onFile" />
        <Button icon="pi pi-upload" label="Upload" :loading="uploading" @click="pickFile" />
        <Button icon="pi pi-sliders-h" label="Presets" severity="secondary" @click="presetOpen = true" />
        <Button icon="pi pi-refresh" severity="secondary" text @click="load" />
      </div>
    </div>
    <p class="muted">Upload once; responsive renditions ({{ presets.map((p) => p.name).join(', ') || 'no presets' }}) are derived asynchronously. Re-uploading identical files dedupes.</p>

    <Message v-if="error" severity="error" :closable="false" class="mb">{{ error }}</Message>

    <div v-if="loading" class="muted">Loading…</div>
    <div v-else-if="!assets.length" class="muted">No media yet — upload an image to start.</div>
    <div v-else class="grid">
      <button v-for="a in assets" :key="a.id" class="card" @click="openDetail(a)">
        <div class="thumb"><img :src="src(a.url)" :alt="a.alt ?? ''" loading="lazy" /></div>
        <div class="meta">
          <Tag :value="a.status" :severity="statusSeverity(a.status)" />
          <span class="dim">{{ a.width }}×{{ a.height }}</span>
        </div>
      </button>
    </div>

    <!-- Asset detail -->
    <Dialog v-model:visible="detailOpen" modal header="Asset" :style="{ width: '40rem' }">
      <template v-if="detail">
        <div class="detail">
          <img :src="src(detail.url)" :alt="detail.alt ?? ''" class="preview" />
          <div class="fields">
            <div class="field"><label>Alt text</label><InputText v-model="detail.alt as string" /></div>
            <div class="field"><label>Folder</label><InputText v-model="detail.folder as string" /></div>
            <div class="field"><label>Tags</label><InputText v-model="editTags" placeholder="comma-separated" /></div>
            <Button label="Save" :loading="saving" @click="saveMeta" />
          </div>
        </div>
        <h4>Renditions</h4>
        <div v-if="detail.renditions?.length" class="rends">
          <div v-for="r in detail.renditions" :key="r.id" class="rend">
            <img :src="src(r.url)" :alt="r.preset" />
            <span>{{ r.preset }} · {{ r.width }}×{{ r.height }} · {{ r.format }}</span>
          </div>
        </div>
        <p v-else class="muted">No renditions yet (still processing).</p>
        <h4>Signed transform URLs</h4>
        <ul class="transforms">
          <li v-for="[name, url] in transformList" :key="name"><code>{{ name }}</code> <a :href="src(url)" target="_blank" rel="noopener">open</a></li>
        </ul>
      </template>
    </Dialog>

    <!-- Presets -->
    <Dialog v-model:visible="presetOpen" modal header="Transformation presets" :style="{ width: '34rem' }">
      <DataTable :value="presets" dataKey="id" stripedRows class="mb">
        <template #empty>No presets.</template>
        <Column field="name" header="Name" />
        <Column header="Size"><template #body="{ data }">{{ data.width ?? '·' }}×{{ data.height ?? '·' }}</template></Column>
        <Column field="fit" header="Fit" />
        <Column field="format" header="Format" />
        <Column field="quality" header="Q" />
      </DataTable>
      <h4>New preset</h4>
      <div class="prow">
        <div class="field"><label>Name</label><InputText v-model="presetForm.name" /></div>
        <div class="field"><label>Width</label><InputNumber v-model="presetForm.width" :useGrouping="false" /></div>
        <div class="field"><label>Height</label><InputNumber v-model="presetForm.height" :useGrouping="false" /></div>
      </div>
      <div class="prow">
        <div class="field"><label>Fit</label><Select v-model="presetForm.fit" :options="fitOptions" /></div>
        <div class="field"><label>Format</label><Select v-model="presetForm.format" :options="formatOptions" /></div>
        <div class="field"><label>Quality</label><InputNumber v-model="presetForm.quality" :min="1" :max="100" /></div>
      </div>
      <template #footer>
        <Button label="Close" severity="secondary" text @click="presetOpen = false" />
        <Button label="Create preset" :loading="savingPreset" @click="savePreset" />
      </template>
    </Dialog>
  </div>
</template>

<style scoped>
.header { display: flex; align-items: center; justify-content: space-between; }
.header h1 { margin: 0; }
.actions { display: flex; gap: 0.5rem; align-items: center; }
.tags-in { width: 12rem; }
.muted { color: var(--p-text-muted-color, #64748b); }
.mb { margin-bottom: 1rem; }
.grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(150px, 1fr)); gap: 1rem; margin-top: 1rem; }
.card { border: 1px solid var(--teggo-border, #cbd5e1); border-radius: var(--teggo-radius, 2px); overflow: hidden; background: var(--p-surface-0, #fff); cursor: pointer; padding: 0; text-align: left; }
.thumb { aspect-ratio: 1; background: var(--p-surface-100, #f1f5f9); display: flex; align-items: center; justify-content: center; }
.thumb img { width: 100%; height: 100%; object-fit: cover; }
.meta { display: flex; align-items: center; justify-content: space-between; padding: 0.5rem; }
.dim { font-size: 0.8rem; color: var(--p-text-muted-color, #64748b); }
.detail { display: flex; gap: 1.25rem; margin-bottom: 1rem; }
.preview { width: 180px; height: 180px; object-fit: contain; background: var(--p-surface-100, #f1f5f9); border-radius: var(--teggo-radius, 2px); }
.fields { flex: 1; }
.field { display: flex; flex-direction: column; gap: 0.35rem; margin-bottom: 0.75rem; }
.field label { font-size: 0.85rem; font-weight: 600; }
.rends { display: flex; flex-wrap: wrap; gap: 0.75rem; }
.rend { display: flex; flex-direction: column; align-items: center; font-size: 0.75rem; gap: 0.25rem; }
.rend img { width: 90px; height: 90px; object-fit: cover; border-radius: var(--teggo-radius, 2px); border: 1px solid var(--teggo-border, #cbd5e1); }
.transforms { font-size: 0.85rem; }
.prow { display: grid; grid-template-columns: 1fr 1fr 1fr; gap: 0.75rem; }
</style>
