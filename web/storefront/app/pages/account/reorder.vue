<script setup lang="ts">
import DataTable from 'primevue/datatable'
import Column from 'primevue/column'
import Tag from 'primevue/tag'
import Button from 'primevue/button'
import Message from 'primevue/message'
import type { components } from '@teggo/api/schema'

definePageMeta({ middleware: 'auth' })
useSeoMeta({ title: 'Time to reorder — Teggo Store' })

type Suggestion = components['schemas']['ReorderSuggestion']

const client = useClient()
const router = useRouter()
const adding = ref('')
const notice = ref('')

const { data, error } = await useAsyncData('reorder-suggestions', async () => {
  const { data, error } = await client.GET('/storefront/account/reorder-suggestions')
  if (error) throw createError({ statusCode: 502, statusMessage: 'Could not load suggestions' })
  return data
})

async function addToCart(s: Suggestion) {
  adding.value = s.sku
  notice.value = ''
  const { error: err } = await client.POST('/storefront/cart/bulk', { body: { lines: [{ sku: s.sku, quantity: '1' }] } })
  adding.value = ''
  if (err) {
    notice.value = `Could not add ${s.sku}.`
    return
  }
  notice.value = `${s.name} added to your cart.`
}
</script>

<template>
  <section class="wrap">
    <h1 class="title">Time to reorder</h1>
    <p class="muted">Based on how often you order these, they're due (or overdue) for a refill.</p>

    <Message v-if="error" severity="error" :closable="false">Could not load your suggestions.</Message>
    <Message v-if="notice" severity="success" :closable="true" class="mb">{{ notice }}</Message>

    <DataTable :value="data?.items ?? []" dataKey="slug" stripedRows>
      <template #empty>Nothing due for reorder yet — order a few times and we'll spot the pattern.</template>
      <Column header="Product">
        <template #body="{ data }">
          <NuxtLink :to="`/p/${data.slug}`" class="lnk">{{ data.name }}</NuxtLink>
          <div class="sku">{{ data.sku }}</div>
        </template>
      </Column>
      <Column header="Cadence"><template #body="{ data }">~every {{ data.avg_interval_days }} days</template></Column>
      <Column header="Last ordered"><template #body="{ data }">{{ data.days_since }} days ago</template></Column>
      <Column header="">
        <template #body="{ data }">
          <Tag v-if="data.days_overdue > 0" :value="`${data.days_overdue}d overdue`" severity="warn" />
          <Tag v-else value="due now" severity="info" />
        </template>
      </Column>
      <Column header="">
        <template #body="{ data }">
          <Button label="Add to cart" icon="pi pi-cart-plus" size="small" :loading="adding === data.sku" @click="addToCart(data)" />
        </template>
      </Column>
    </DataTable>

    <div class="foot">
      <Button label="Go to cart" icon="pi pi-arrow-right" iconPos="right" text @click="router.push('/cart')" />
    </div>
  </section>
</template>

<style scoped>
.wrap { max-width: 880px; }
.title { margin: 0 0 0.4rem; }
.muted { color: var(--p-text-muted-color, #64748b); margin-bottom: 1rem; }
.mb { margin-bottom: 1rem; }
.sku { font-size: 0.78rem; color: var(--p-text-muted-color, #64748b); }
.lnk { color: var(--p-primary-color, #0ea5e9); text-decoration: none; }
.foot { margin-top: 1rem; }
</style>
