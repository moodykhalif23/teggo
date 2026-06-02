<script setup lang="ts">
import Button from 'primevue/button'
import Tag from 'primevue/tag'
import Message from 'primevue/message'

const route = useRoute()
const client = useClient()

const { data: product, error } = await useAsyncData(
  () => `product-${route.params.slug}`,
  async () => {
    const { data, error } = await client.GET('/storefront/products/{slug}', {
      params: { path: { slug: route.params.slug as string } },
    })
    if (error) throw createError({ statusCode: 404, statusMessage: 'Product not found' })
    return data
  },
)

useSeoMeta({
  title: () => (product.value ? `${product.value.name} — Oro Store` : 'Product'),
  description: () => product.value?.description ?? 'Product detail',
})
</script>

<template>
  <section>
    <Message v-if="error" severity="error" :closable="false">
      Product not found, or the API is unavailable.
    </Message>

    <article v-else-if="product" class="detail">
      <div class="gallery">
        <div class="placeholder"><i class="pi pi-image" /></div>
      </div>
      <div class="info">
        <span class="sku">{{ product.sku }}</span>
        <h1>{{ product.name }}</h1>
        <Tag :value="product.status" severity="secondary" />
        <p v-if="product.description" class="desc">{{ product.description }}</p>
        <div class="actions">
          <Button label="Add to cart" icon="pi pi-shopping-cart" />
          <Button label="Request a quote" icon="pi pi-file-edit" severity="secondary" outlined />
        </div>
      </div>
    </article>
  </section>
</template>

<style scoped>
.detail {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 2rem;
}
@media (max-width: 720px) {
  .detail {
    grid-template-columns: 1fr;
  }
}
.placeholder {
  aspect-ratio: 1;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 3rem;
  color: var(--p-surface-300, #cbd5e1);
  background: var(--p-surface-100, #f1f5f9);
  border-radius: 12px;
}
.sku {
  font-size: 0.85rem;
  color: var(--p-text-muted-color, #64748b);
}
.info h1 {
  margin: 0.25rem 0 0.75rem;
}
.desc {
  margin: 1rem 0;
  line-height: 1.6;
}
.actions {
  display: flex;
  gap: 0.75rem;
  margin-top: 1.5rem;
  flex-wrap: wrap;
}
</style>
