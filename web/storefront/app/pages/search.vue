<script setup lang="ts">
import ProductCard from '~/components/ProductCard.vue'
import Message from 'primevue/message'

const route = useRoute()
const client = useClient()

const q = computed(() => (route.query.q as string)?.trim() ?? '')

// Re-fetch whenever the query term changes (key includes q).
const { data, error } = await useAsyncData(
  () => `search-${q.value}`,
  async () => {
    if (!q.value) return { items: [], page: 1 }
    const { data, error } = await client.GET('/storefront/products', {
      params: { query: { q: q.value, page: 1, page_size: 24 } },
    })
    if (error) throw createError({ statusCode: 502, statusMessage: 'Search unavailable' })
    return data
  },
  { watch: [q] },
)

useSeoMeta({
  title: () => (q.value ? `Search: ${q.value} — Teggo Store` : 'Search — Teggo Store'),
  description: () => `Product search results for "${q.value}".`,
})
</script>

<template>
  <section>
    <h1 class="title">
      Search <span v-if="q" class="muted">— “{{ q }}”</span>
    </h1>

    <Message v-if="error" severity="error" :closable="false">
      Search is unavailable right now. Is the API running?
    </Message>

    <p v-else-if="!q" class="muted">Type a term in the search box above.</p>

    <div v-else-if="data?.items?.length" class="grid">
      <ProductCard v-for="p in data.items" :key="p.public_id" :product="p" />
    </div>

    <p v-else class="muted">No products match “{{ q }}”.</p>
  </section>
</template>

<style scoped>
.title { margin: 0 0 1.25rem; }
.muted { color: var(--p-text-muted-color, #64748b); font-weight: 400; }
.grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(240px, 1fr));
  gap: 1rem;
}
</style>
