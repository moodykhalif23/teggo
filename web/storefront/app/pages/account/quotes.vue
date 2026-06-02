<script setup lang="ts">
import DataTable from 'primevue/datatable'
import Column from 'primevue/column'
import Tag from 'primevue/tag'
import Message from 'primevue/message'

definePageMeta({ middleware: 'auth' })
useSeoMeta({ title: 'My quotes — Teggo Store' })

const client = useClient()
const router = useRouter()

const { data, error } = await useAsyncData('my-quotes', async () => {
  const { data, error } = await client.GET('/storefront/quotes')
  if (error) throw createError({ statusCode: 502, statusMessage: 'Could not load quotes' })
  return data
})

function sev(s: string) {
  return s === 'sent' || s === 'revised' ? 'info' : s === 'accepted' ? 'success' : s === 'declined' || s === 'expired' ? 'danger' : 'secondary'
}
</script>

<template>
  <section>
    <h1 class="title">My quotes</h1>
    <Message v-if="error" severity="error" :closable="false">Could not load your quotes.</Message>
    <DataTable
      v-else
      :value="data?.items ?? []"
      dataKey="id"
      stripedRows
      @rowClick="router.push(`/quotes/${$event.data.public_id}`)"
      class="clickable"
    >
      <template #empty>No quotes yet — your requests will appear here once we respond.</template>
      <Column header="Reference"><template #body="{ data }">{{ data.public_id.slice(0, 8) }}…</template></Column>
      <Column header="Status"><template #body="{ data }"><Tag :value="data.status" :severity="sev(data.status)" /></template></Column>
      <Column header="Total"><template #body="{ data }">{{ data.subtotal }} {{ data.currency }}</template></Column>
      <Column header="Valid until">
        <template #body="{ data }">{{ data.valid_until ? new Date(data.valid_until).toLocaleDateString() : '—' }}</template>
      </Column>
    </DataTable>
  </section>
</template>

<style scoped>
.title { margin: 0 0 1rem; }
.clickable :deep(tbody tr) { cursor: pointer; }
</style>
