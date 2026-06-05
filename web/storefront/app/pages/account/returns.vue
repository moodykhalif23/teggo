<script setup lang="ts">
import DataTable from 'primevue/datatable'
import Column from 'primevue/column'
import Tag from 'primevue/tag'
import Message from 'primevue/message'

definePageMeta({ middleware: 'auth' })
useSeoMeta({ title: 'My returns — Teggo Store' })

const client = useClient()

const { data, error } = await useAsyncData('my-returns', async () => {
  const { data, error } = await client.GET('/storefront/returns')
  if (error) throw createError({ statusCode: 502, statusMessage: 'Could not load returns' })
  return data
})

function sev(s: string) {
  return s === 'received' || s === 'closed' ? 'success' : s === 'rejected' ? 'danger' : s === 'approved' ? 'info' : 'warn'
}
</script>

<template>
  <section class="wrap">
    <h1 class="title">My returns</h1>
    <Message v-if="error" severity="error" :closable="false">Could not load your returns.</Message>
    <DataTable :value="data?.items ?? []" dataKey="id" stripedRows>
      <template #empty>No returns yet. Start one from an order.</template>
      <Column header="RMA"><template #body="{ data }">{{ data.public_id.slice(0, 8) }}…</template></Column>
      <Column header="Status"><template #body="{ data }"><Tag :value="data.status" :severity="sev(data.status)" /></template></Column>
      <Column field="reason" header="Reason" />
    </DataTable>
  </section>
</template>

<style scoped>
.wrap { max-width: 720px; }
.title { margin: 0 0 1rem; }
</style>
