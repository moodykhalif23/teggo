<script setup lang="ts">
import DataTable from 'primevue/datatable'
import Column from 'primevue/column'
import Tag from 'primevue/tag'
import Message from 'primevue/message'

definePageMeta({ middleware: 'auth' })
useSeoMeta({ title: 'My invoices — Teggo Store' })

const client = useClient()
const router = useRouter()

const { data, error } = await useAsyncData('my-invoices', async () => {
  const { data, error } = await client.GET('/storefront/invoices')
  if (error) throw createError({ statusCode: 502, statusMessage: 'Could not load invoices' })
  return data
})

function sev(s: string) {
  return s === 'paid' ? 'success' : s === 'overdue' ? 'danger' : s === 'void' ? 'secondary' : 'info'
}
</script>

<template>
  <section>
    <h1 class="title">My invoices</h1>
    <Message v-if="error" severity="error" :closable="false">Could not load your invoices.</Message>
    <DataTable
      v-else
      :value="data?.items ?? []"
      dataKey="id"
      stripedRows
      @rowClick="router.push(`/account/invoices/${$event.data.public_id}`)"
      class="clickable"
    >
      <template #empty>No invoices yet.</template>
      <Column header="Reference"><template #body="{ data }">{{ data.public_id.slice(0, 8) }}…</template></Column>
      <Column header="Status"><template #body="{ data }"><Tag :value="data.status" :severity="sev(data.status)" /></template></Column>
      <Column header="Total"><template #body="{ data }">{{ data.grand_total }} {{ data.currency }}</template></Column>
      <Column header="Due">
        <template #body="{ data }">{{ data.due_at ? new Date(data.due_at).toLocaleDateString() : '—' }}</template>
      </Column>
    </DataTable>
  </section>
</template>

<style scoped>
.title { margin: 0 0 1rem; }
.clickable :deep(tbody tr) { cursor: pointer; }
</style>
