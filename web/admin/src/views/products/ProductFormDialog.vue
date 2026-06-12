<script setup lang="ts">
import { reactive, ref, watch } from 'vue'
import { useToast } from 'primevue/usetoast'
import Dialog from 'primevue/dialog'
import InputText from 'primevue/inputtext'
import Textarea from 'primevue/textarea'
import Select from 'primevue/select'
import Button from 'primevue/button'
import Message from 'primevue/message'
import { api, errMessage } from '@/lib/client'
import { useCustomerOptions } from '@/composables/useRecordOptions'
import { useAuthStore } from '@/stores/auth'
import type { components } from '@teggo/api/schema'

type AdminProduct = components['schemas']['AdminProduct']
type ProductInput = components['schemas']['ProductInput']
type VisRule = components['schemas']['CatalogVisibilityRule']
type ProductImage = components['schemas']['ProductImage']

const props = defineProps<{ open: boolean; product: AdminProduct | null }>()
const emit = defineEmits<{ 'update:open': [boolean]; saved: [] }>()

const toast = useToast()
const saving = ref(false)
const error = ref('')
const attrsText = ref('{}')

// ---- Product photos (max 5) — reuse the DAM upload, then link by asset id ---
const auth = useAuthStore()
const apiBase = import.meta.env.VITE_API_BASE_URL ?? ''
const src = (u?: string | null) => (u ? `${apiBase}${u}` : '')
const MAX_IMAGES = 5
const images = ref<ProductImage[]>([])
const uploading = ref(false)
const imageInput = ref<HTMLInputElement | null>(null)

async function loadImages() {
  if (!props.product) {
    images.value = []
    return
  }
  const { data } = await api.GET('/admin/products/{id}/images', { params: { path: { id: props.product.id } } })
  images.value = data?.items ?? []
}
function pickImage() {
  imageInput.value?.click()
}
async function onImageFile(e: Event) {
  const input = e.target as HTMLInputElement
  const file = input.files?.[0]
  if (!file || !props.product) return
  uploading.value = true
  try {
    // 1) Upload the file to the media library (dedupe + renditions handled there).
    const fd = new FormData()
    fd.append('file', file)
    const up = await fetch(`${apiBase}/admin/media`, {
      method: 'POST',
      headers: { Authorization: `Bearer ${auth.token ?? ''}` },
      body: fd,
    })
    if (!up.ok) {
      const b = await up.json().catch(() => ({}))
      throw new Error(b.message ?? `Upload failed (${up.status})`)
    }
    const asset = (await up.json()) as { id: number }
    // 2) Link the uploaded asset to this product.
    const { error: err } = await api.POST('/admin/products/{id}/images', {
      params: { path: { id: props.product.id } },
      body: { media_asset_id: asset.id },
    })
    if (err) throw new Error(errMessage(err, 'Could not attach image'))
    await loadImages()
  } catch (e) {
    toast.add({ severity: 'error', summary: errMessage(e, 'Upload failed'), life: 4000 })
  } finally {
    uploading.value = false
    input.value = ''
  }
}
async function removeImage(id: number) {
  if (!props.product) return
  const { error: err } = await api.DELETE('/admin/products/{id}/images/{imageID}', {
    params: { path: { id: props.product.id, imageID: id } },
  })
  if (err) {
    toast.add({ severity: 'error', summary: errMessage(err, 'Delete failed'), life: 3500 })
    return
  }
  loadImages()
}

// Per-customer catalog visibility (only for an existing product).
const { customers, customersLoaded, loadCustomers } = useCustomerOptions()
const visRules = ref<VisRule[]>([])
const visCustomer = ref<number | null>(null)
const custName = (id?: number | null) => (id ? (customers.value.find((c) => c.id === id)?.name ?? `#${id}`) : '—')

async function loadVisibility() {
  if (!props.product) return
  const { data } = await api.GET('/admin/products/{id}/visibility', { params: { path: { id: props.product.id } } })
  visRules.value = data?.items ?? []
}
async function addVisibility() {
  if (!props.product || !visCustomer.value) return
  const { error: err } = await api.POST('/admin/products/{id}/visibility', {
    params: { path: { id: props.product.id } },
    body: { customer_id: visCustomer.value, visible: false },
  })
  if (err) {
    toast.add({ severity: 'error', summary: 'Could not add rule', detail: errMessage(err), life: 3500 })
    return
  }
  visCustomer.value = null
  loadVisibility()
}
async function removeVisibility(id: number) {
  const { error: err } = await api.DELETE('/admin/catalog-visibility/{id}', { params: { path: { id } } })
  if (!err) loadVisibility()
}

// ---- Translations (i18n: per-locale name/description) ----
type Translation = components['schemas']['ProductTranslation']
const translations = ref<Translation[]>([])
const transForm = reactive({ locale: '', name: '', description: '' })

async function loadTranslations() {
  if (!props.product) {
    translations.value = []
    return
  }
  const { data } = await api.GET('/admin/products/{id}/translations', { params: { path: { id: props.product.id } } })
  translations.value = data?.items ?? []
}
async function saveTranslation() {
  if (!props.product || !transForm.locale.trim() || !transForm.name.trim()) return
  const { error: err } = await api.PUT('/admin/products/{id}/translations', {
    params: { path: { id: props.product.id } },
    body: { locale: transForm.locale.trim(), name: transForm.name.trim(), description: transForm.description.trim() || null },
  })
  if (err) {
    toast.add({ severity: 'error', summary: 'Could not save translation', detail: errMessage(err), life: 3500 })
    return
  }
  transForm.locale = ''
  transForm.name = ''
  transForm.description = ''
  loadTranslations()
}
async function removeTranslation(locale: string) {
  if (!props.product) return
  const { error: err } = await api.DELETE('/admin/products/{id}/translations/{locale}', {
    params: { path: { id: props.product.id, locale } },
  })
  if (!err) loadTranslations()
}

const types = ['simple', 'configurable', 'kit', 'digital']
const statuses = ['draft', 'active', 'disabled']

const form = reactive<ProductInput>({
  sku: '',
  name: '',
  slug: '',
  type: 'simple',
  status: 'draft',
  unit: 'each',
  description: '',
})

watch(
  () => props.open,
  (isOpen) => {
    if (!isOpen) return
    error.value = ''
    if (props.product) {
      Object.assign(form, {
        sku: props.product.sku,
        name: props.product.name,
        slug: props.product.slug,
        type: props.product.type,
        status: props.product.status,
        unit: props.product.unit,
        description: props.product.description ?? '',
      })
      attrsText.value = JSON.stringify(props.product.attributes ?? {}, null, 2)
      loadCustomers()
      loadVisibility()
      loadImages()
      loadTranslations()
    } else {
      visRules.value = []
      images.value = []
      translations.value = []
      Object.assign(form, { sku: '', name: '', slug: '', type: 'simple', status: 'draft', unit: 'each', description: '' })
      attrsText.value = '{}'
    }
  },
)

function close() {
  emit('update:open', false)
}

async function save() {
  error.value = ''
  let attributes: Record<string, unknown> = {}
  try {
    attributes = attrsText.value.trim() ? JSON.parse(attrsText.value) : {}
  } catch {
    error.value = 'Attributes must be valid JSON'
    return
  }
  const body: ProductInput = { ...form, attributes }
  saving.value = true
  const res = props.product
    ? await api.PUT('/admin/products/{id}', { params: { path: { id: props.product.id } }, body })
    : await api.POST('/admin/products', { body })
  saving.value = false
  if (res.error || !res.data) {
    error.value = errMessage(res.error, 'Save failed')
    return
  }
  toast.add({ severity: 'success', summary: props.product ? 'Updated' : 'Created', detail: res.data.name, life: 2500 })
  emit('saved')
}
</script>

<template>
  <Dialog
    :visible="open"
    :header="product ? 'Edit product' : 'New product'"
    modal
    :style="{ width: '560px' }"
    @update:visible="emit('update:open', $event)"
  >
    <form class="form" @submit.prevent="save">
      <Message v-if="error" severity="error" :closable="false">{{ error }}</Message>
      <div class="grid2">
        <div class="field">
          <label>SKU</label>
          <InputText v-model="form.sku" fluid />
        </div>
        <div class="field">
          <label>Slug</label>
          <InputText v-model="form.slug" fluid />
        </div>
      </div>
      <div class="field">
        <label>Name</label>
        <InputText v-model="form.name" fluid />
      </div>
      <div class="grid2">
        <div class="field">
          <label>Type</label>
          <Select v-model="form.type" :options="types" fluid />
        </div>
        <div class="field">
          <label>Status</label>
          <Select v-model="form.status" :options="statuses" fluid />
        </div>
      </div>
      <div class="field">
        <label>Unit</label>
        <InputText v-model="form.unit" fluid />
      </div>
      <div class="field">
        <label>Description</label>
        <Textarea v-model="form.description" rows="2" fluid />
      </div>
      <div class="field">
        <label>Attributes (JSON)</label>
        <Textarea v-model="attrsText" rows="4" fluid class="mono" />
      </div>

      <div v-if="product" class="field photos">
        <label>Photos <span class="hint">(up to {{ MAX_IMAGES }} — shown to buyers)</span></label>
        <div class="img-grid">
          <div v-for="img in images" :key="img.id" class="img-cell">
            <img :src="src(img.url)" :alt="img.alt ?? ''" loading="lazy" />
            <button type="button" class="img-del" aria-label="Remove photo" @click="removeImage(img.id)">
              <i class="pi pi-times" />
            </button>
          </div>
          <button
            v-if="images.length < MAX_IMAGES"
            type="button"
            class="img-add"
            :disabled="uploading"
            @click="pickImage"
          >
            <i :class="uploading ? 'pi pi-spin pi-spinner' : 'pi pi-plus'" />
            <span>{{ uploading ? 'Uploading…' : 'Add photo' }}</span>
          </button>
        </div>
        <p v-if="!images.length" class="muted hint">No photos yet — add up to {{ MAX_IMAGES }}.</p>
        <input ref="imageInput" type="file" accept="image/*" hidden @change="onImageFile" />
      </div>

      <div v-if="product" class="field i18n">
        <label>Translations <span class="hint">(per-locale name &amp; description; falls back to the default above)</span></label>
        <ul class="tr-list">
          <li v-for="t in translations" :key="t.locale" class="tr">
            <span class="tr-locale">{{ t.locale }}</span>
            <span class="tr-name">{{ t.name }}</span>
            <Button icon="pi pi-times" text rounded size="small" severity="danger" @click="removeTranslation(t.locale)" />
          </li>
          <li v-if="!translations.length" class="muted">No translations yet.</li>
        </ul>
        <div class="tr-add">
          <InputText v-model="transForm.locale" placeholder="Locale (fr)" class="tr-loc" />
          <InputText v-model="transForm.name" placeholder="Name in locale" class="grow" />
        </div>
        <div class="tr-add">
          <InputText v-model="transForm.description" placeholder="Description (optional)" class="grow" />
          <Button label="Add" icon="pi pi-plus" size="small" :disabled="!transForm.locale.trim() || !transForm.name.trim()" @click="saveTranslation" />
        </div>
      </div>

      <div v-if="product" class="field vis">
        <label>Catalog visibility <span class="hint">(hide this product from specific customers)</span></label>
        <ul class="vis-list">
          <li v-for="r in visRules" :key="r.id">
            <span>{{ r.visible ? 'Visible to' : 'Hidden from' }} {{ r.customer_id ? custName(r.customer_id) : (r.customer_group_id ? `group #${r.customer_group_id}` : '—') }}</span>
            <Button icon="pi pi-times" text rounded size="small" severity="danger" @click="removeVisibility(r.id)" />
          </li>
          <li v-if="!visRules.length" class="muted">Visible to everyone.</li>
        </ul>
        <div class="vis-add">
          <Select
            v-model="visCustomer"
            :options="customers"
            optionLabel="name"
            optionValue="id"
            filter
            placeholder="Hide from customer…"
            :emptyMessage="customersLoaded ? 'No customers' : 'Loading…'"
            class="grow"
          />
          <Button label="Hide" icon="pi pi-eye-slash" size="small" :disabled="!visCustomer" @click="addVisibility" />
        </div>
      </div>
    </form>
    <template #footer>
      <Button label="Cancel" severity="secondary" text @click="close" />
      <Button label="Save" :loading="saving" @click="save" />
    </template>
  </Dialog>
</template>

<style scoped>
.form {
  display: flex;
  flex-direction: column;
  gap: 0.9rem;
}
.grid2 {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 0.9rem;
}
.field {
  display: flex;
  flex-direction: column;
  gap: 0.3rem;
}
.field label {
  font-size: 0.8rem;
  font-weight: 600;
}
.mono :deep(textarea) {
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 0.85rem;
}
.photos { border-top: 1px solid var(--p-surface-200, #e2e8f0); padding-top: 0.8rem; }
.muted { color: var(--p-text-muted-color, #64748b); }
.img-grid { display: flex; flex-wrap: wrap; gap: 0.6rem; }
.img-cell {
  position: relative;
  width: 76px;
  height: 76px;
  border-radius: var(--teggo-radius, 6px);
  overflow: hidden;
  border: 1px solid var(--teggo-border, #e2e8f0);
  background: var(--p-surface-100, #f1f5f9);
}
.img-cell img { width: 100%; height: 100%; object-fit: cover; display: block; }
.img-del {
  position: absolute;
  top: 3px;
  right: 3px;
  width: 20px;
  height: 20px;
  display: flex;
  align-items: center;
  justify-content: center;
  border: none;
  border-radius: 50%;
  background: rgba(15, 23, 42, 0.6);
  color: #fff;
  font-size: 0.65rem;
  cursor: pointer;
}
.img-del:hover { background: rgba(15, 23, 42, 0.85); }
.img-add {
  width: 76px;
  height: 76px;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 0.2rem;
  border: 1px dashed var(--p-surface-300, #cbd5e1);
  border-radius: var(--teggo-radius, 6px);
  background: var(--p-surface-50, #f8fafc);
  color: var(--p-text-muted-color, #64748b);
  font-size: 0.72rem;
  cursor: pointer;
  transition: border-color 0.15s, color 0.15s;
}
.img-add:hover:not(:disabled) { border-color: var(--p-primary-color, #16a34a); color: var(--p-primary-color, #16a34a); }
.img-add:disabled { cursor: progress; opacity: 0.7; }
.i18n { border-top: 1px solid var(--p-surface-200, #e2e8f0); padding-top: 0.8rem; }
.tr-list { list-style: none; margin: 0 0 0.5rem; padding: 0; display: flex; flex-direction: column; gap: 0.25rem; }
.tr { display: flex; align-items: center; gap: 0.6rem; font-size: 0.85rem; }
.tr-locale { font-weight: 700; text-transform: uppercase; min-width: 2.5rem; }
.tr-name { flex: 1; }
.tr-add { display: flex; gap: 0.5rem; margin-bottom: 0.4rem; }
.tr-add .tr-loc { width: 6rem; }
.tr-add .grow { flex: 1; }
.vis { border-top: 1px solid var(--p-surface-200, #e2e8f0); padding-top: 0.8rem; }
.hint { font-weight: 400; color: var(--p-text-muted-color, #64748b); font-size: 0.75rem; }
.vis-list { list-style: none; margin: 0 0 0.5rem; padding: 0; display: flex; flex-direction: column; gap: 0.25rem; }
.vis-list li { display: flex; align-items: center; justify-content: space-between; font-size: 0.85rem; }
.vis-list li.muted { color: var(--p-text-muted-color, #64748b); }
.vis-add { display: flex; gap: 0.5rem; align-items: center; }
.grow { flex: 1; }
</style>
