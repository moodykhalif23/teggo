<script setup lang="ts">
import DataTable from 'primevue/datatable'
import Column from 'primevue/column'
import Tag from 'primevue/tag'
import Message from 'primevue/message'

definePageMeta({ middleware: 'auth' })
useSeoMeta({ title: 'My orders — Teggo Store' })

const client = useClient()
const router = useRouter()

const { data, error } = await useAsyncData('my-orders', async () => {
  const { data, error } = await client.GET('/storefront/orders')
  if (error) throw createError({ statusCode: 502, statusMessage: 'Could not load orders' })
  return data
})

function sev(s: string) {
  if (s === 'cancelled') return 'danger'
  if (s === 'delivered' || s === 'closed') return 'success'
  if (s === 'pending' || s === 'on_hold') return 'warn'
  return 'info'
}
</script>

<template>
  <section>
    <h1 class="title">My orders</h1>
    <Message v-if="error" severity="error" :closable="false">Could not load your orders.</Message>
    <DataTable
      v-else
      :value="data?.items ?? []"
      dataKey="id"
      stripedRows
      @rowClick="router.push(`/account/orders/${$event.data.public_id}`)"
      class="clickable"
    >
      <template #empty>No orders yet.</template>
      <Column header="Reference"><template #body="{ data }">{{ data.public_id.slice(0, 8) }}…</template></Column>
      <Column header="Status"><template #body="{ data }"><Tag :value="data.status" :severity="sev(data.status)" /></template></Column>
      <Column header="Total"><template #body="{ data }">{{ data.grand_total }} {{ data.currency }}</template></Column>
    </DataTable>
  </section>
</template>

<style scoped>
.title { margin: 0 0 1rem; }
.clickable :deep(tbody tr) { cursor: pointer; }
</style>
