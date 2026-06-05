<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useToast } from 'primevue/usetoast'
import DataTable from 'primevue/datatable'
import Column from 'primevue/column'
import Dialog from 'primevue/dialog'
import InputText from 'primevue/inputtext'
import Select from 'primevue/select'
import Button from 'primevue/button'
import Tag from 'primevue/tag'
import Message from 'primevue/message'
import { api, errMessage } from '@/lib/client'
import { useCustomerOptions } from '@/composables/useRecordOptions'
import type { components } from '@teggo/api/schema'

type Provider = components['schemas']['IdentityProvider']

const toast = useToast()
const apiBase = import.meta.env.VITE_API_BASE_URL ?? ''
const providers = ref<Provider[]>([])
const error = ref('')

const { customers, customersLoaded, loadCustomers } = useCustomerOptions()
const custName = computed(() => Object.fromEntries(customers.value.map((c) => [c.id, c.name])))

const dialogOpen = ref(false)
const editingId = ref<number | null>(null)
const saving = ref(false)
const form = reactive({
  name: '', audience: 'admin', customer_id: null as number | null,
  issuer: '', authorization_endpoint: '', token_endpoint: '', jwks_uri: '',
  client_id: '', client_secret: '', scopes: 'openid email profile', is_active: true,
})

async function load() {
  error.value = ''
  const { data, error: err } = await api.GET('/admin/identity-providers')
  if (err || !data) {
    error.value = errMessage(err, 'Failed to load providers')
    return
  }
  providers.value = data.items ?? []
}

function openCreate() {
  editingId.value = null
  Object.assign(form, {
    name: '', audience: 'admin', customer_id: null, issuer: '', authorization_endpoint: '',
    token_endpoint: '', jwks_uri: '', client_id: '', client_secret: '', scopes: 'openid email profile', is_active: true,
  })
  dialogOpen.value = true
}
function openEdit(p: Provider) {
  editingId.value = p.id
  const c = (p.config ?? {}) as Record<string, any>
  Object.assign(form, {
    name: p.name, audience: p.audience, customer_id: p.customer_id ?? null,
    issuer: c.issuer ?? '', authorization_endpoint: c.authorization_endpoint ?? '',
    token_endpoint: c.token_endpoint ?? '', jwks_uri: c.jwks_uri ?? '',
    client_id: c.client_id ?? '', client_secret: '',
    scopes: Array.isArray(c.scopes) ? c.scopes.join(' ') : 'openid email profile', is_active: p.is_active,
  })
  dialogOpen.value = true
}

async function save() {
  if (!form.name) return
  const config: Record<string, any> = {
    issuer: form.issuer, authorization_endpoint: form.authorization_endpoint,
    token_endpoint: form.token_endpoint, jwks_uri: form.jwks_uri,
    client_id: form.client_id, scopes: form.scopes.split(/\s+/).filter(Boolean),
  }
  if (form.client_secret) config.client_secret = form.client_secret
  saving.value = true
  const body: components['schemas']['IdentityProviderInput'] = {
    type: 'oidc', name: form.name, audience: form.audience as 'admin' | 'storefront',
    customer_id: form.audience === 'storefront' ? form.customer_id : null,
    config, is_active: form.is_active,
  }
  const { error: err } = editingId.value
    ? await api.PUT('/admin/identity-providers/{id}', { params: { path: { id: editingId.value } }, body })
    : await api.POST('/admin/identity-providers', { body })
  saving.value = false
  if (err) {
    toast.add({ severity: 'error', summary: errMessage(err, 'Save failed'), life: 4000 })
    return
  }
  toast.add({ severity: 'success', summary: 'Provider saved', life: 2000 })
  dialogOpen.value = false
  load()
}

const loginURL = (p: Provider) => `${apiBase}/auth/sso/${p.id}/login`
const metadataURL = (p: Provider) => `${apiBase}/auth/sso/${p.id}/metadata`

onMounted(() => {
  load()
  loadCustomers()
})
</script>

<template>
  <div class="page">
    <div class="header">
      <h1>Identity providers <span class="muted">SSO (OIDC)</span></h1>
      <div class="actions">
        <Button icon="pi pi-refresh" severity="secondary" text @click="load" />
        <Button icon="pi pi-plus" label="New provider" @click="openCreate" />
      </div>
    </div>
    <p class="muted">OpenID Connect single sign-on. Admin-audience providers sign in seller-side staff; storefront-audience providers sign in a buying company's users (JIT-provisioned). Point your IdP's redirect URI at the provider's callback.</p>
    <Message v-if="error" severity="error" :closable="false" class="mb">{{ error }}</Message>

    <DataTable :value="providers" dataKey="id" stripedRows>
      <template #empty>No identity providers yet.</template>
      <Column field="name" header="Name" />
      <Column field="type" header="Type" />
      <Column field="audience" header="Audience" />
      <Column header="Customer"><template #body="{ data }">{{ data.customer_id ? (custName[data.customer_id] ?? `#${data.customer_id}`) : '—' }}</template></Column>
      <Column header="Secret"><template #body="{ data }"><i :class="data.has_secret ? 'pi pi-check' : 'pi pi-minus'" /></template></Column>
      <Column header="Active"><template #body="{ data }"><Tag :value="data.is_active ? 'active' : 'off'" :severity="data.is_active ? 'success' : 'secondary'" /></template></Column>
      <Column header="Login URL"><template #body="{ data }"><a :href="loginURL(data)" target="_blank" rel="noopener" class="lnk">test login</a></template></Column>
      <Column header="SP metadata"><template #body="{ data }"><a v-if="data.type === 'saml'" :href="metadataURL(data)" target="_blank" rel="noopener" class="lnk">metadata XML</a><span v-else class="muted">—</span></template></Column>
      <Column header="" style="width:4rem"><template #body="{ data }"><Button icon="pi pi-pencil" text rounded size="small" @click="openEdit(data)" /></template></Column>
    </DataTable>

    <Dialog v-model:visible="dialogOpen" modal :header="editingId ? 'Edit provider' : 'New provider'" :style="{ width: '40rem' }">
      <div class="row">
        <div class="field"><label>Name</label><InputText v-model="form.name" /></div>
        <div class="field"><label>Audience</label><Select v-model="form.audience" :options="['admin', 'storefront']" /></div>
        <div class="field" v-if="form.audience === 'storefront'">
          <label>Customer</label>
          <Select
            v-model="form.customer_id"
            :options="customers"
            optionLabel="name"
            optionValue="id"
            filter
            filterPlaceholder="Search customers…"
            placeholder="Select a customer"
            :emptyMessage="customersLoaded ? 'No customers' : 'Loading…'"
            showClear
          />
        </div>
      </div>
      <h4>OIDC config</h4>
      <div class="field"><label>Issuer</label><InputText v-model="form.issuer" placeholder="https://idp.example" /></div>
      <div class="row">
        <div class="field"><label>Authorization endpoint</label><InputText v-model="form.authorization_endpoint" /></div>
        <div class="field"><label>Token endpoint</label><InputText v-model="form.token_endpoint" /></div>
      </div>
      <div class="field"><label>JWKS URI</label><InputText v-model="form.jwks_uri" /></div>
      <div class="row">
        <div class="field"><label>Client ID</label><InputText v-model="form.client_id" /></div>
        <div class="field"><label>Client secret <span class="muted">(blank = keep)</span></label><InputText v-model="form.client_secret" type="password" /></div>
      </div>
      <div class="field"><label>Scopes</label><InputText v-model="form.scopes" placeholder="openid email profile" /></div>
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
h4 { margin: 0.5rem 0; }
.lnk { font-size: 0.85rem; }
</style>
