<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useToast } from 'primevue/usetoast'
import Card from 'primevue/card'
import Tabs from 'primevue/tabs'
import TabList from 'primevue/tablist'
import Tab from 'primevue/tab'
import TabPanels from 'primevue/tabpanels'
import TabPanel from 'primevue/tabpanel'
import DataTable from 'primevue/datatable'
import Column from 'primevue/column'
import Button from 'primevue/button'
import Tag from 'primevue/tag'
import Dialog from 'primevue/dialog'
import InputText from 'primevue/inputtext'
import InputNumber from 'primevue/inputnumber'
import Password from 'primevue/password'
import Select from 'primevue/select'
import Checkbox from 'primevue/checkbox'
import Message from 'primevue/message'
import { api, errMessage } from '@/lib/client'
import type { components } from '@teggo/api/schema'

type Customer = components['schemas']['Customer']
type CustomerUser = components['schemas']['CustomerUser']
type CustomerAddress = components['schemas']['CustomerAddress']

const route = useRoute()
const router = useRouter()
const toast = useToast()
const id = Number(route.params.id)

const customer = ref<Customer | null>(null)
const ancestors = ref<{ id: number; depth: number }[]>([])
const users = ref<CustomerUser[]>([])
const addresses = ref<CustomerAddress[]>([])
type Budget = components['schemas']['CustomerBudget']
const budgets = ref<Budget[]>([])
const error = ref('')
const loading = ref(false)

async function load() {
  loading.value = true
  error.value = ''
  const [c, h, u, a] = await Promise.all([
    api.GET('/admin/customers/{id}', { params: { path: { id } } }),
    api.GET('/admin/customers/{id}/hierarchy', { params: { path: { id } } }),
    api.GET('/admin/customers/{id}/users', { params: { path: { id } } }),
    api.GET('/admin/customers/{id}/addresses', { params: { path: { id } } }),
  ])
  loading.value = false
  if (c.error || !c.data) {
    error.value = errMessage(c.error, 'Customer not found')
    return
  }
  customer.value = c.data
  ancestors.value = h.data?.ancestors ?? []
  users.value = u.data?.items ?? []
  addresses.value = a.data?.items ?? []
  const b = await api.GET('/admin/customers/{id}/budgets', { params: { path: { id } } })
  budgets.value = b.data?.items ?? []
  loadInvites()
}

// --- budgets ---
const budgetDialog = ref(false)
const savingBudget = ref(false)
const budgetForm = reactive({ cost_center: '', period: 'monthly' as 'monthly' | 'quarterly' | 'annual', amount: '', currency: 'USD' })
const periods = ['monthly', 'quarterly', 'annual']
function openBudget() {
  Object.assign(budgetForm, { cost_center: '', period: 'monthly', amount: '', currency: 'USD' })
  budgetDialog.value = true
}
async function saveBudget() {
  savingBudget.value = true
  const { error: err } = await api.POST('/admin/customers/{id}/budgets', {
    params: { path: { id } },
    body: { cost_center: budgetForm.cost_center, period: budgetForm.period, amount: budgetForm.amount, currency: budgetForm.currency },
  })
  savingBudget.value = false
  if (err) {
    toast.add({ severity: 'error', summary: 'Failed', detail: errMessage(err), life: 4000 })
    return
  }
  budgetDialog.value = false
  toast.add({ severity: 'success', summary: 'Budget saved', life: 2000 })
  load()
}
async function deleteBudget(b: Budget) {
  const { error: err } = await api.DELETE('/admin/customers/{id}/budgets/{budgetID}', { params: { path: { id, budgetID: b.id } } })
  if (!err) load()
}

// --- add user ---
const userDialog = ref(false)
const savingUser = ref(false)
const userForm = reactive({ email: '', password: '', full_name: '', role: 'buyer' as 'buyer' | 'approver' | 'admin', spending_limit: '' })
function openUser() {
  Object.assign(userForm, { email: '', password: '', full_name: '', role: 'buyer', spending_limit: '' })
  userDialog.value = true
}
async function saveUser() {
  savingUser.value = true
  const { error: err } = await api.POST('/admin/customers/{id}/users', {
    params: { path: { id } },
    body: {
      email: userForm.email,
      password: userForm.password,
      full_name: userForm.full_name,
      role: userForm.role,
      spending_limit: userForm.spending_limit || null,
    },
  })
  savingUser.value = false
  if (err) {
    toast.add({ severity: 'error', summary: 'Failed', detail: errMessage(err), life: 4000 })
    return
  }
  userDialog.value = false
  toast.add({ severity: 'success', summary: 'User added', life: 2000 })
  load()
}

// --- invite links ---
type Invite = components['schemas']['CustomerInvite']
const invites = ref<Invite[]>([])
const inviteDialog = ref(false)
const savingInvite = ref(false)
const inviteForm = reactive({ role: 'buyer' as 'buyer' | 'approver' | 'admin', expires_in_days: 14, spending_limit: '' })
// The join link lives on the storefront domain (from the org's website record).
const storefrontDomain = ref('')

async function loadInvites() {
  const [inv, ws] = await Promise.all([
    api.GET('/admin/customers/{id}/invites', { params: { path: { id } } }),
    storefrontDomain.value ? Promise.resolve(null) : api.GET('/admin/websites'),
  ])
  invites.value = inv.data?.items ?? []
  if (ws?.data?.items?.length) storefrontDomain.value = ws.data.items[0].domain
}

function inviteLink(i: Invite) {
  const domain = storefrontDomain.value || window.location.host
  const proto = domain.includes('localhost') ? 'http' : 'https'
  return `${proto}://${domain}/join/${i.token}`
}
function inviteStatus(i: Invite): { label: string; severity: string } {
  if (i.revoked_at) return { label: 'revoked', severity: 'secondary' }
  if (new Date(i.expires_at) < new Date()) return { label: 'expired', severity: 'warn' }
  return { label: 'active', severity: 'success' }
}
async function copyInviteLink(i: Invite) {
  await navigator.clipboard.writeText(inviteLink(i))
  toast.add({ severity: 'success', summary: 'Link copied', detail: inviteLink(i), life: 3000 })
}
function openInvite() {
  Object.assign(inviteForm, { role: 'buyer', expires_in_days: 14, spending_limit: '' })
  inviteDialog.value = true
}
async function saveInvite() {
  savingInvite.value = true
  const { data, error: err } = await api.POST('/admin/customers/{id}/invites', {
    params: { path: { id } },
    body: { role: inviteForm.role, expires_in_days: inviteForm.expires_in_days, spending_limit: inviteForm.spending_limit || null },
  })
  savingInvite.value = false
  if (err || !data) {
    toast.add({ severity: 'error', summary: 'Failed', detail: errMessage(err), life: 4000 })
    return
  }
  inviteDialog.value = false
  await loadInvites()
  await copyInviteLink(data)
}
async function revokeInvite(i: Invite) {
  const { error: err } = await api.DELETE('/admin/customers/{id}/invites/{inviteID}', {
    params: { path: { id, inviteID: i.id } },
  })
  if (err) {
    toast.add({ severity: 'error', summary: 'Failed', detail: errMessage(err), life: 4000 })
    return
  }
  loadInvites()
}

// --- add address ---
const addrDialog = ref(false)
const savingAddr = ref(false)
const addrForm = reactive({
  type: 'shipping' as 'billing' | 'shipping',
  is_default: false,
  line1: '',
  line2: '',
  city: '',
  region: '',
  postal_code: '',
  country: '',
})
function openAddr() {
  Object.assign(addrForm, { type: 'shipping', is_default: false, line1: '', line2: '', city: '', region: '', postal_code: '', country: '' })
  addrDialog.value = true
}
async function saveAddr() {
  savingAddr.value = true
  const { error: err } = await api.POST('/admin/customers/{id}/addresses', {
    params: { path: { id } },
    body: {
      type: addrForm.type,
      is_default: addrForm.is_default,
      line1: addrForm.line1,
      line2: addrForm.line2 || null,
      city: addrForm.city,
      region: addrForm.region || null,
      postal_code: addrForm.postal_code || null,
      country: addrForm.country,
    },
  })
  savingAddr.value = false
  if (err) {
    toast.add({ severity: 'error', summary: 'Failed', detail: errMessage(err), life: 4000 })
    return
  }
  addrDialog.value = false
  toast.add({ severity: 'success', summary: 'Address added', life: 2000 })
  load()
}

onMounted(load)
</script>

<template>
  <div class="page">
    <Button icon="pi pi-arrow-left" label="Customers" text severity="secondary" @click="router.push({ name: 'customers' })" />
    <Message v-if="error" severity="error" :closable="false" class="mb">{{ error }}</Message>

    <template v-if="customer">
      <h1 class="title">{{ customer.name }}</h1>

      <div class="grid">
        <Card>
          <template #title>Overview</template>
          <template #content>
            <dl class="kv">
              <dt>Tax ID</dt><dd>{{ customer.tax_id ?? '—' }}</dd>
              <dt>Payment terms</dt><dd>{{ customer.payment_terms_days }} days</dd>
              <dt>Credit limit</dt><dd>{{ customer.credit_limit }}</dd>
              <dt>Group</dt><dd>{{ customer.customer_group_id ?? '—' }}</dd>
              <dt>Active</dt>
              <dd><Tag :value="customer.is_active ? 'active' : 'inactive'" :severity="customer.is_active ? 'success' : 'secondary'" /></dd>
            </dl>
          </template>
        </Card>

        <Card>
          <template #title>Hierarchy (ancestors)</template>
          <template #content>
            <ol v-if="ancestors.length" class="ancestors">
              <li v-for="a in ancestors" :key="a.id">Customer #{{ a.id }} <span class="muted">(depth {{ a.depth }})</span></li>
            </ol>
            <p v-else class="muted">Top-level customer (no parent).</p>
          </template>
        </Card>
      </div>

      <Tabs value="users" class="tabs">
        <TabList>
          <Tab value="users">Users ({{ users.length }})</Tab>
          <Tab value="invites">Invite links ({{ invites.length }})</Tab>
          <Tab value="addresses">Addresses ({{ addresses.length }})</Tab>
          <Tab value="budgets">Budgets ({{ budgets.length }})</Tab>
        </TabList>
        <TabPanels>
          <TabPanel value="users">
            <div class="tabhead">
              <Button icon="pi pi-plus" label="Add user" size="small" @click="openUser" />
            </div>
            <DataTable :value="users" dataKey="id" stripedRows>
              <template #empty>No users.</template>
              <Column field="full_name" header="Name" />
              <Column field="email" header="Email" />
              <Column field="role" header="Role" />
              <Column header="Spending limit"><template #body="{ data }">{{ data.spending_limit ?? '—' }}</template></Column>
            </DataTable>
          </TabPanel>
          <TabPanel value="invites">
            <div class="tabhead">
              <Button icon="pi pi-link" label="New invite link" size="small" @click="openInvite" />
            </div>
            <p class="muted hint">Share a link so this company's buyers can register themselves on the storefront.</p>
            <DataTable :value="invites" dataKey="id" stripedRows>
              <template #empty>No invite links yet.</template>
              <Column header="Link">
                <template #body="{ data }"><code class="link-code">/join/{{ data.token.slice(0, 8) }}…</code></template>
              </Column>
              <Column field="role" header="Role" />
              <Column header="Expires"><template #body="{ data }">{{ new Date(data.expires_at).toLocaleDateString() }}</template></Column>
              <Column field="use_count" header="Signups" />
              <Column header="Status">
                <template #body="{ data }"><Tag :value="inviteStatus(data).label" :severity="inviteStatus(data).severity" /></template>
              </Column>
              <Column header="" style="width: 7rem">
                <template #body="{ data }">
                  <Button icon="pi pi-copy" text rounded size="small" title="Copy link" @click="copyInviteLink(data)" />
                  <Button
                    v-if="inviteStatus(data).label === 'active'"
                    icon="pi pi-ban" text rounded severity="danger" size="small" title="Revoke"
                    @click="revokeInvite(data)"
                  />
                </template>
              </Column>
            </DataTable>
          </TabPanel>
          <TabPanel value="addresses">
            <div class="tabhead">
              <Button icon="pi pi-plus" label="Add address" size="small" @click="openAddr" />
            </div>
            <DataTable :value="addresses" dataKey="id" stripedRows>
              <template #empty>No addresses.</template>
              <Column field="type" header="Type" />
              <Column field="line1" header="Line 1" />
              <Column field="city" header="City" />
              <Column field="country" header="Country" />
              <Column header="Default"><template #body="{ data }"><Tag v-if="data.is_default" value="default" severity="info" /></template></Column>
            </DataTable>
          </TabPanel>
          <TabPanel value="budgets">
            <div class="tabhead">
              <Button icon="pi pi-plus" label="Add budget" size="small" @click="openBudget" />
            </div>
            <DataTable :value="budgets" dataKey="id" stripedRows>
              <template #empty>No budgets — spend is uncapped.</template>
              <Column header="Cost center"><template #body="{ data }">{{ data.cost_center || 'Company-wide' }}</template></Column>
              <Column field="period" header="Period" />
              <Column header="Amount"><template #body="{ data }">{{ data.amount }} {{ data.currency }}</template></Column>
              <Column header="" style="width: 4rem"><template #body="{ data }"><Button icon="pi pi-trash" text rounded severity="danger" size="small" @click="deleteBudget(data)" /></template></Column>
            </DataTable>
          </TabPanel>
        </TabPanels>
      </Tabs>
    </template>

    <!-- Add budget dialog -->
    <Dialog v-model:visible="budgetDialog" header="Add budget" modal :style="{ width: '420px' }">
      <form class="form" @submit.prevent="saveBudget">
        <div class="field"><label>Cost center <span class="muted">(blank = company-wide)</span></label><InputText v-model="budgetForm.cost_center" fluid /></div>
        <div class="field"><label>Period</label><Select v-model="budgetForm.period" :options="periods" fluid /></div>
        <div class="field"><label>Amount</label><InputText v-model="budgetForm.amount" fluid placeholder="e.g. 5000" /></div>
        <div class="field"><label>Currency</label><InputText v-model="budgetForm.currency" fluid /></div>
      </form>
      <template #footer>
        <Button label="Cancel" severity="secondary" text @click="budgetDialog = false" />
        <Button label="Save" :loading="savingBudget" @click="saveBudget" />
      </template>
    </Dialog>

    <!-- Add user dialog -->
    <Dialog v-model:visible="userDialog" header="Add customer user" modal :style="{ width: '440px' }">
      <form class="form" @submit.prevent="saveUser">
        <div class="field"><label>Full name</label><InputText v-model="userForm.full_name" fluid /></div>
        <div class="field"><label>Email</label><InputText v-model="userForm.email" fluid /></div>
        <div class="field"><label>Password</label><Password v-model="userForm.password" :feedback="false" toggleMask fluid /></div>
        <div class="field"><label>Role</label><Select v-model="userForm.role" :options="['buyer', 'approver', 'admin']" fluid /></div>
        <div class="field"><label>Spending limit (optional)</label><InputText v-model="userForm.spending_limit" fluid /></div>
      </form>
      <template #footer>
        <Button label="Cancel" severity="secondary" text @click="userDialog = false" />
        <Button label="Save" :loading="savingUser" @click="saveUser" />
      </template>
    </Dialog>

    <!-- New invite link dialog -->
    <Dialog v-model:visible="inviteDialog" header="New invite link" modal :style="{ width: '420px' }">
      <form class="form" @submit.prevent="saveInvite">
        <div class="field"><label>Role for everyone who joins</label><Select v-model="inviteForm.role" :options="['buyer', 'approver', 'admin']" fluid /></div>
        <div class="field"><label>Expires in (days)</label><InputNumber v-model="inviteForm.expires_in_days" :min="1" :max="365" showButtons fluid /></div>
        <div class="field"><label>Spending limit (optional)</label><InputText v-model="inviteForm.spending_limit" fluid placeholder="blank = unlimited" /></div>
      </form>
      <template #footer>
        <Button label="Cancel" severity="secondary" text @click="inviteDialog = false" />
        <Button label="Create &amp; copy link" icon="pi pi-link" :loading="savingInvite" @click="saveInvite" />
      </template>
    </Dialog>

    <!-- Add address dialog -->
    <Dialog v-model:visible="addrDialog" header="Add address" modal :style="{ width: '480px' }">
      <form class="form" @submit.prevent="saveAddr">
        <div class="grid2">
          <div class="field"><label>Type</label><Select v-model="addrForm.type" :options="['billing', 'shipping']" fluid /></div>
          <div class="check"><Checkbox v-model="addrForm.is_default" binary inputId="def" /><label for="def">Default</label></div>
        </div>
        <div class="field"><label>Line 1</label><InputText v-model="addrForm.line1" fluid /></div>
        <div class="field"><label>Line 2</label><InputText v-model="addrForm.line2" fluid /></div>
        <div class="grid2">
          <div class="field"><label>City</label><InputText v-model="addrForm.city" fluid /></div>
          <div class="field"><label>Region</label><InputText v-model="addrForm.region" fluid /></div>
        </div>
        <div class="grid2">
          <div class="field"><label>Postal code</label><InputText v-model="addrForm.postal_code" fluid /></div>
          <div class="field"><label>Country (2-letter)</label><InputText v-model="addrForm.country" maxlength="2" fluid /></div>
        </div>
      </form>
      <template #footer>
        <Button label="Cancel" severity="secondary" text @click="addrDialog = false" />
        <Button label="Save" :loading="savingAddr" @click="saveAddr" />
      </template>
    </Dialog>
  </div>
</template>

<style scoped>
.title { margin: 0.5rem 0 1rem; }
.mb { margin-bottom: 1rem; }
.grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(280px, 1fr)); gap: 1rem; margin-bottom: 1.5rem; }
.kv { display: grid; grid-template-columns: auto 1fr; gap: 0.4rem 1rem; margin: 0; }
.kv dt { font-weight: 600; color: var(--p-text-muted-color, #64748b); }
.kv dd { margin: 0; }
.ancestors { margin: 0; padding-left: 1.1rem; line-height: 1.8; }
.muted { color: var(--p-text-muted-color, #64748b); }
.tabhead { display: flex; justify-content: flex-end; margin-bottom: 0.75rem; }
.form { display: flex; flex-direction: column; gap: 0.9rem; }
.grid2 { display: grid; grid-template-columns: 1fr 1fr; gap: 0.9rem; align-items: end; }
.field { display: flex; flex-direction: column; gap: 0.3rem; }
.field label { font-size: 0.8rem; font-weight: 600; }
.check { display: flex; align-items: center; gap: 0.5rem; padding-bottom: 0.5rem; }
.hint { margin: -0.25rem 0 0.75rem; font-size: 0.88rem; }
.link-code { font-size: 0.82rem; }
</style>
