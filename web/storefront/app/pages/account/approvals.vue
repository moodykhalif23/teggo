<script setup lang="ts">
import DataTable from 'primevue/datatable'
import Column from 'primevue/column'
import Button from 'primevue/button'
import Message from 'primevue/message'
import type { components } from '@teggo/api/schema'

definePageMeta({ middleware: 'auth' })
useSeoMeta({ title: 'Order approvals — Teggo Store' })

type Approval = components['schemas']['ApprovalSummary']

const client = useClient()
const router = useRouter()

const items = ref<Approval[]>([])
const forbidden = ref(false)
const error = ref('')
const notice = ref('')
const busy = ref('')

async function load() {
  error.value = ''
  const { data, error: err, response } = await client.GET('/storefront/account/approvals')
  if (response?.status === 403) {
    forbidden.value = true
    return
  }
  if (err || !data) {
    error.value = 'Could not load pending approvals.'
    return
  }
  items.value = data.items ?? []
}

async function decide(a: Approval, action: 'approve' | 'reject') {
  busy.value = a.public_id
  notice.value = ''
  const path = action === 'approve'
    ? '/storefront/account/approvals/{publicID}/approve'
    : '/storefront/account/approvals/{publicID}/reject'
  const { error: err, response } = await client.POST(path, { params: { path: { publicID: a.public_id } } })
  busy.value = ''
  if (err) {
    error.value = response?.status === 403
      ? 'You cannot act on an order you placed yourself.'
      : 'Could not record your decision.'
    return
  }
  notice.value = action === 'approve' ? 'Order approved and released.' : 'Order rejected.'
  await load()
}

await load()
</script>

<template>
  <section class="wrap">
    <h1 class="title">Order approvals</h1>

    <Message v-if="forbidden" severity="warn" :closable="false">
      You need the approver or admin role to review approvals.
    </Message>

    <template v-else>
      <Message v-if="error" severity="error" :closable="true" class="mb">{{ error }}</Message>
      <Message v-if="notice" severity="success" :closable="true" class="mb">{{ notice }}</Message>

      <DataTable :value="items" dataKey="public_id" stripedRows>
        <template #empty>No orders are awaiting approval.</template>
        <Column header="Order"><template #body="{ data }">{{ data.public_id.slice(0, 8) }}…</template></Column>
        <Column header="Total"><template #body="{ data }">{{ data.grand_total }} {{ data.currency }}</template></Column>
        <Column header="Placed by"><template #body="{ data }">{{ data.placed_by_user ? `User #${data.placed_by_user}` : '—' }}</template></Column>
        <Column header="Actions">
          <template #body="{ data }">
            <div class="acts">
              <Button label="Approve" icon="pi pi-check" size="small" :loading="busy === data.public_id" @click="decide(data, 'approve')" />
              <Button label="Reject" icon="pi pi-times" size="small" severity="danger" outlined :loading="busy === data.public_id" @click="decide(data, 'reject')" />
            </div>
          </template>
        </Column>
      </DataTable>
    </template>

    <Button class="back" icon="pi pi-arrow-left" label="Account settings" text severity="secondary" @click="router.push('/account/settings')" />
  </section>
</template>

<style scoped>
.wrap { max-width: 820px; }
.title { margin: 0 0 1rem; }
.mb { margin-bottom: 1rem; }
.acts { display: flex; gap: 0.5rem; }
.back { margin-top: 1rem; }
</style>
