<script setup lang="ts">
import DataTable from 'primevue/datatable'
import Column from 'primevue/column'
import Tag from 'primevue/tag'
import Message from 'primevue/message'

definePageMeta({ middleware: 'auth' })
useSeoMeta({ title: 'Rebates — Teggo Store' })

const client = useClient()
const { data, error } = await useAsyncData('my-rebates', async () => {
  const { data, error } = await client.GET('/storefront/rebates')
  if (error) throw createError({ statusCode: 502, statusMessage: 'Could not load rebates' })
  return data
})
</script>

<template>
  <section>
    <h1 class="title">Rebates</h1>
    <Message v-if="error" severity="error" :closable="false">Could not load your rebates.</Message>

    <template v-else-if="data">
      <h2 class="sub">Current period</h2>
      <p class="muted small">Projected rebate based on your qualifying spend so far this period.</p>
      <DataTable :value="data.current ?? []" dataKey="program" stripedRows class="mb">
        <template #empty><span class="muted">No active rebate programs.</span></template>
        <Column field="program" header="Program" />
        <Column field="period_key" header="Period" />
        <Column header="Qualifying spend"><template #body="{ data: r }">{{ r.qualifying_total }} {{ r.currency }}</template></Column>
        <Column header="Rate"><template #body="{ data: r }">{{ r.rate_percent }}%</template></Column>
        <Column header="Projected rebate"><template #body="{ data: r }"><strong>{{ r.projected_rebate }} {{ r.currency }}</strong></template></Column>
      </DataTable>

      <h2 class="sub">Earned (settled)</h2>
      <DataTable :value="data.settlements ?? []" dataKey="id" stripedRows>
        <template #empty><span class="muted">No rebates settled yet.</span></template>
        <Column field="program_name" header="Program" />
        <Column field="period_key" header="Period" />
        <Column header="Amount"><template #body="{ data: r }">{{ r.rebate_amount }} {{ r.currency }}</template></Column>
        <Column header="Status"><template #body="{ data: r }"><Tag :value="r.status" severity="success" /></template></Column>
      </DataTable>
    </template>
  </section>
</template>

<style scoped>
.title { margin: 0 0 1rem; }
.sub { font-size: 1rem; margin: 1.25rem 0 0.25rem; }
.muted { color: var(--p-text-muted-color, #64748b); }
.small { font-size: 0.85rem; }
.mb { margin-bottom: 1.5rem; }
</style>
