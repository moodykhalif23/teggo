<script setup lang="ts">
import ProductCard from '~/components/ProductCard.vue'
import Message from 'primevue/message'
import type { ProductList } from '~/types'

const route = useRoute()
const api = useApi()

// SSR fetch so the catalog page is crawlable. The Go storefront endpoint
// accepts ?category= (and ?q=, ?filter=, paging); 'all' lists everything.
const { data, error } = await useAsyncData(
  () => `products-${route.params.slug}`,
  () => {
    const slug = route.params.slug as string
    const qs = new URLSearchParams({ page: '1', page_size: '24' })
    if (slug && slug !== 'all') qs.set('category', slug)
    return api<ProductList>(`/storefront/products?${qs.toString()}`)
  },
)

useSeoMeta({
  title: () => `Catalog — ${route.params.slug}`,
  description: 'Browse products in the Oro Store catalog.',
})
</script>

<template>
  <section>
    <h1 class="title">Catalog</h1>

    <Message v-if="error" severity="error" :closable="false">
      Could not load products. Is the API running on the configured base URL?
    </Message>

    <div v-else-if="data?.items?.length" class="grid">
      <ProductCard v-for="p in data.items" :key="p.public_id" :product="p" />
    </div>

    <p v-else class="muted">No products found.</p>
  </section>
</template>

<style scoped>
.title {
  margin: 0 0 1.25rem;
}
.grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(240px, 1fr));
  gap: 1rem;
}
.muted {
  color: var(--p-text-muted-color, #64748b);
}
</style>
