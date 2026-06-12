<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { useToast } from 'primevue/usetoast'
import DataTable from 'primevue/datatable'
import Column from 'primevue/column'
import Button from 'primevue/button'
import InputText from 'primevue/inputtext'
import Message from 'primevue/message'
import { api, errMessage } from '@/lib/client'
import type { components } from '@teggo/api/schema'
import PageHeader from '@/components/PageHeader.vue'
import EmptyState from '@/components/EmptyState.vue'

type FxRate = components['schemas']['FxRate']

const rows = ref<FxRate[]>([])
const loading = ref(false)
const error = ref('')
const toast = useToast()

const form = reactive({ base_currency: '', quote_currency: '', rate: '' })
const formError = ref('')
const saving = ref(false)

async function load() {
  loading.value = true
  error.value = ''
  const { data, error: err } = await api.GET('/admin/fx-rates')
  loading.value = false
  if (err || !data) {
    error.value = errMessage(err, 'Failed to load rates')
    return
  }
  rows.value = data.items ?? []
}

async function save() {
  formError.value = ''
  const base = form.base_currency.trim().toUpperCase()
  const quote = form.quote_currency.trim().toUpperCase()
  if (base.length !== 3 || quote.length !== 3 || base === quote || !form.rate.trim()) {
    formError.value = 'Enter two different 3-letter codes and a positive rate.'
    return
  }
  saving.value = true
  const { error: err } = await api.POST('/admin/fx-rates', {
    body: { base_currency: base, quote_currency: quote, rate: form.rate.trim() },
  })
  saving.value = false
  if (err) {
    formError.value = errMessage(err, 'Save failed')
    return
  }
  toast.add({ severity: 'success', summary: 'Rate saved', life: 2000 })
  form.base_currency = ''
  form.quote_currency = ''
  form.rate = ''
  load()
}

async function remove(rate: FxRate) {
  const { error: err } = await api.DELETE('/admin/fx-rates/{id}', { params: { path: { id: rate.id } } })
  if (err) {
    toast.add({ severity: 'error', summary: errMessage(err, 'Delete failed'), life: 4000 })
    return
  }
  load()
}

onMounted(load)
</script>

<template>
  <div class="page">
    <PageHeader title="Exchange rates" :meta="rows.length">
      <template #actions>
        <Button icon="pi pi-refresh" severity="secondary" text @click="load" />
      </template>
    </PageHeader>
    <p class="muted mb">
      Rates convert prices into a buyer's display currency and lock onto orders at placement.
      <strong>Rate</strong> = units of the quote currency per 1 of the base (e.g. KES→USD ≈ 0.0075).
      Posting a pair again records a new current rate (history is kept).
    </p>

    <Message v-if="error" severity="error" :closable="false" class="mb">{{ error }}</Message>

    <form class="addrow mb" @submit.prevent="save">
      <InputText v-model="form.base_currency" placeholder="Base (KES)" maxlength="3" class="ccy" />
      <i class="pi pi-arrow-right sep" />
      <InputText v-model="form.quote_currency" placeholder="Quote (USD)" maxlength="3" class="ccy" />
      <InputText v-model="form.rate" placeholder="Rate (0.0075)" class="rate" />
      <Button label="Add rate" icon="pi pi-plus" :loading="saving" type="submit" />
    </form>
    <Message v-if="formError" severity="error" :closable="false" class="mb">{{ formError }}</Message>

    <DataTable :value="rows" :loading="loading" dataKey="id" stripedRows>
      <template #empty>
        <EmptyState icon="pi pi-money-bill" title="No exchange rates" message="Add a rate to let buyers see prices in their own currency." />
      </template>
      <Column header="Pair"><template #body="{ data }">{{ data.base_currency }} → {{ data.quote_currency }}</template></Column>
      <Column field="rate" header="Rate" />
      <Column header="As of"><template #body="{ data }">{{ new Date(data.as_of).toLocaleString() }}</template></Column>
      <Column header="" style="width: 4rem">
        <template #body="{ data }"><Button icon="pi pi-trash" text rounded severity="danger" @click="remove(data)" /></template>
      </Column>
    </DataTable>
  </div>
</template>

<style scoped>
.mb { margin-bottom: 1rem; }
.muted { color: var(--p-text-muted-color, #64748b); }
.addrow { display: flex; align-items: center; gap: 0.6rem; flex-wrap: wrap; }
.addrow .ccy { width: 7rem; text-transform: uppercase; }
.addrow .rate { width: 10rem; }
.addrow .sep { color: var(--p-text-muted-color, #94a3b8); }
</style>
