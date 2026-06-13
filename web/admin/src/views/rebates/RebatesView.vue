<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { useToast } from 'primevue/usetoast'
import { useConfirm } from 'primevue/useconfirm'
import DataTable from 'primevue/datatable'
import Column from 'primevue/column'
import Button from 'primevue/button'
import Dialog from 'primevue/dialog'
import InputText from 'primevue/inputtext'
import Select from 'primevue/select'
import ToggleSwitch from 'primevue/toggleswitch'
import Tag from 'primevue/tag'
import Message from 'primevue/message'
import { api, errMessage } from '@/lib/client'
import { useCurrency } from '@/composables/useCurrency'
import type { components } from '@teggo/api/schema'
import PageHeader from '@/components/PageHeader.vue'
import EmptyState from '@/components/EmptyState.vue'

type Program = components['schemas']['RebateProgram']
type Report = components['schemas']['RebateReport']
type Settlement = components['schemas']['RebateSettlement']

const toast = useToast()
const confirm = useConfirm()
const { currency } = useCurrency()
const programs = ref<Program[]>([])
const loading = ref(false)
const error = ref('')
const periods = ['monthly', 'quarterly', 'annual']

async function load() {
  loading.value = true
  error.value = ''
  const { data, error: err } = await api.GET('/admin/rebates')
  loading.value = false
  if (err || !data) {
    error.value = errMessage(err, 'Failed to load rebate programs')
    return
  }
  programs.value = data.items ?? []
}

// ---- create ----
const createOpen = ref(false)
const createSaving = ref(false)
const createErr = ref('')
const form = reactive<{ name: string; period: string; currency: string; is_active: boolean }>({
  name: '', period: 'quarterly', currency: '', is_active: true,
})
function openCreate() {
  Object.assign(form, { name: '', period: 'quarterly', currency: currency.value || 'USD', is_active: true })
  createErr.value = ''
  createOpen.value = true
}
async function createProgram() {
  if (!form.name.trim() || form.currency.trim().length !== 3) {
    createErr.value = 'Name and a 3-letter currency are required.'
    return
  }
  createSaving.value = true
  const { error: err } = await api.POST('/admin/rebates', {
    body: { name: form.name.trim(), period: form.period as 'monthly' | 'quarterly' | 'annual', currency: form.currency.trim().toUpperCase(), is_active: form.is_active },
  })
  createSaving.value = false
  if (err) {
    createErr.value = errMessage(err, 'Save failed')
    return
  }
  createOpen.value = false
  load()
}

async function remove(p: Program) {
  confirm.require({
    message: `Delete rebate program "${p.name}"?`,
    header: 'Confirm delete',
    icon: 'pi pi-exclamation-triangle',
    rejectProps: { label: 'Cancel', severity: 'secondary', outlined: true },
    acceptProps: { label: 'Delete', severity: 'danger' },
    accept: async () => {
      const { error: err } = await api.DELETE('/admin/rebates/{id}', { params: { path: { id: p.id } } })
      if (err) {
        toast.add({ severity: 'error', summary: errMessage(err, 'Delete failed'), life: 4000 })
        return
      }
      load()
    },
  })
}

// ---- detail (tiers + report + settle) ----
const detailOpen = ref(false)
const detail = ref<Program | null>(null)
const report = ref<Report | null>(null)
const settlements = ref<Settlement[]>([])
const tierForm = reactive({ min_amount: '', rate_percent: '' })

async function openDetail(p: Program) {
  const { data } = await api.GET('/admin/rebates/{id}', { params: { path: { id: p.id } } })
  detail.value = data ?? p
  report.value = null
  settlements.value = []
  detailOpen.value = true
  refreshReport()
  refreshSettlements()
}
async function refreshReport() {
  if (!detail.value) return
  const { data } = await api.GET('/admin/rebates/{id}/report', { params: { path: { id: detail.value.id } } })
  report.value = data ?? null
}
async function refreshSettlements() {
  if (!detail.value) return
  const { data } = await api.GET('/admin/rebates/{id}/settlements', { params: { path: { id: detail.value.id } } })
  settlements.value = data?.items ?? []
}
async function reloadDetail() {
  if (!detail.value) return
  const { data } = await api.GET('/admin/rebates/{id}', { params: { path: { id: detail.value.id } } })
  detail.value = data ?? detail.value
}
async function addTier() {
  if (!detail.value || !tierForm.min_amount.trim() || !tierForm.rate_percent.trim()) return
  const { error: err } = await api.POST('/admin/rebates/{id}/tiers', {
    params: { path: { id: detail.value.id } },
    body: { min_amount: tierForm.min_amount.trim(), rate_percent: tierForm.rate_percent.trim() },
  })
  if (err) {
    toast.add({ severity: 'error', summary: errMessage(err, 'Could not add tier'), life: 3500 })
    return
  }
  tierForm.min_amount = ''
  tierForm.rate_percent = ''
  reloadDetail()
  refreshReport()
}
async function removeTier(tierId: number) {
  if (!detail.value) return
  const { error: err } = await api.DELETE('/admin/rebates/{id}/tiers/{tierID}', { params: { path: { id: detail.value.id, tierID: tierId } } })
  if (!err) {
    reloadDetail()
    refreshReport()
  }
}
async function settle() {
  if (!detail.value) return
  const { data, error: err } = await api.POST('/admin/rebates/{id}/settle', { params: { path: { id: detail.value.id } } })
  if (err) {
    toast.add({ severity: 'error', summary: errMessage(err, 'Settle failed'), life: 4000 })
    return
  }
  if (data?.scheduled) {
    // Large programs settle as a background job — results appear once it runs.
    toast.add({ severity: 'info', summary: 'Settlement scheduled', detail: 'Issuing credit notes in the background; refresh shortly.', life: 4000 })
  } else {
    toast.add({
      severity: 'success',
      summary: `Settled ${data?.settled ?? 0} · ${data?.total_rebate ?? 0} ${detail.value.currency}`,
      detail: data?.skipped ? `${data.skipped} already settled or below threshold` : undefined,
      life: 4000,
    })
  }
  refreshSettlements()
}

onMounted(load)
</script>

<template>
  <div class="page">
    <PageHeader title="Rebates" :meta="programs.length">
      <template #actions>
        <Button icon="pi pi-refresh" severity="secondary" text @click="load" />
        <Button icon="pi pi-plus" label="New program" @click="openCreate" />
      </template>
    </PageHeader>
    <p class="muted mb">Tiered volume incentives. Qualifying order spend accrues per customer per period; settling a period snapshots the rebate and issues a credit note.</p>

    <Message v-if="error" severity="error" :closable="false" class="mb">{{ error }}</Message>

    <DataTable :value="programs" :loading="loading" dataKey="id" stripedRows paginator :rows="15">
      <template #empty>
        <EmptyState icon="pi pi-percentage" title="No rebate programs" message="Create a program with spend tiers to reward high-volume buyers.">
          <Button icon="pi pi-plus" label="New program" @click="openCreate" />
        </EmptyState>
      </template>
      <Column field="name" header="Name" />
      <Column field="period" header="Period" />
      <Column field="currency" header="Currency" />
      <Column header="Scope"><template #body="{ data }">{{ data.customer_id ? `Customer #${data.customer_id}` : 'All customers' }}</template></Column>
      <Column header="Status"><template #body="{ data }"><Tag :value="data.is_active ? 'active' : 'inactive'" :severity="data.is_active ? 'success' : 'secondary'" /></template></Column>
      <Column header="" style="width: 8rem">
        <template #body="{ data }">
          <Button icon="pi pi-eye" severity="secondary" text rounded title="Manage" @click="openDetail(data)" />
          <Button icon="pi pi-trash" severity="danger" text rounded @click="remove(data)" />
        </template>
      </Column>
    </DataTable>

    <!-- Create -->
    <Dialog v-model:visible="createOpen" modal header="New rebate program" :style="{ width: '32rem' }">
      <Message v-if="createErr" severity="error" :closable="false" class="mb">{{ createErr }}</Message>
      <div class="field"><label>Name</label><InputText v-model="form.name" fluid /></div>
      <div class="grid2">
        <div class="field"><label>Period</label><Select v-model="form.period" :options="periods" fluid /></div>
        <div class="field"><label>Currency</label><InputText v-model="form.currency" maxlength="3" fluid /></div>
      </div>
      <div class="field switch"><label>Active</label><ToggleSwitch v-model="form.is_active" /></div>
      <template #footer>
        <Button label="Cancel" severity="secondary" text @click="createOpen = false" />
        <Button label="Create" :loading="createSaving" @click="createProgram" />
      </template>
    </Dialog>

    <!-- Detail: tiers + report + settle -->
    <Dialog v-model:visible="detailOpen" modal :header="detail ? detail.name : 'Program'" :style="{ width: '46rem' }">
      <template v-if="detail">
        <h4>Tiers <span class="muted">(min spend → rebate %)</span></h4>
        <DataTable :value="detail.tiers ?? []" dataKey="id" class="mb">
          <template #empty><span class="muted">No tiers yet — add at least one.</span></template>
          <Column header="Min spend"><template #body="{ data }">{{ data.min_amount }} {{ detail.currency }}</template></Column>
          <Column header="Rate"><template #body="{ data }">{{ data.rate_percent }}%</template></Column>
          <Column header="" style="width: 4rem"><template #body="{ data }"><Button icon="pi pi-trash" text rounded severity="danger" @click="removeTier(data.id)" /></template></Column>
        </DataTable>
        <div class="addrow mb">
          <InputText v-model="tierForm.min_amount" placeholder="Min spend (10000)" class="grow" />
          <InputText v-model="tierForm.rate_percent" placeholder="Rate % (2.5)" class="sm" />
          <Button label="Add tier" icon="pi pi-plus" size="small" @click="addTier" />
        </div>

        <div class="report-head">
          <h4>This period <span v-if="report" class="muted">({{ report.period_key }})</span></h4>
          <Button label="Settle period" icon="pi pi-check" size="small" severity="secondary" outlined @click="settle" />
        </div>
        <DataTable :value="report?.rows ?? []" dataKey="customer_id" class="mb">
          <template #empty><span class="muted">No qualifying orders this period.</span></template>
          <Column field="customer_id" header="Customer" />
          <Column header="Qualifying"><template #body="{ data }">{{ data.qualifying_total }} {{ report?.currency }}</template></Column>
          <Column field="orders" header="Orders" />
          <Column header="Rate"><template #body="{ data }">{{ data.rate_percent }}%</template></Column>
          <Column header="Rebate"><template #body="{ data }">{{ data.rebate_amount }} {{ report?.currency }}</template></Column>
        </DataTable>

        <h4>Settlements</h4>
        <DataTable :value="settlements" dataKey="id">
          <template #empty><span class="muted">No settlements yet.</span></template>
          <Column field="period_key" header="Period" />
          <Column field="customer_id" header="Customer" />
          <Column header="Rebate"><template #body="{ data }">{{ data.rebate_amount }} {{ data.currency }}</template></Column>
          <Column header="Status"><template #body="{ data }"><Tag :value="data.status" severity="success" /></template></Column>
        </DataTable>
      </template>
      <template #footer><Button label="Close" severity="secondary" text @click="detailOpen = false" /></template>
    </Dialog>
  </div>
</template>

<style scoped>
.mb { margin-bottom: 1rem; }
.muted { color: var(--p-text-muted-color, #64748b); font-weight: 400; }
.grid2 { display: grid; grid-template-columns: 1fr 1fr; gap: 1rem; }
.field { display: flex; flex-direction: column; gap: 0.35rem; margin-bottom: 1rem; }
.field label { font-size: 0.85rem; font-weight: 600; }
.switch { }
h4 { margin: 0.5rem 0; font-size: 0.95rem; }
.addrow { display: flex; align-items: center; gap: 0.6rem; }
.addrow .grow { flex: 1; }
.addrow .sm { width: 8rem; }
.report-head { display: flex; align-items: center; justify-content: space-between; }
</style>
