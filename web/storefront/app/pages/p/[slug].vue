<script setup lang="ts">
import Button from 'primevue/button'
import Tag from 'primevue/tag'
import Message from 'primevue/message'

const route = useRoute()
const router = useRouter()
const client = useClient()
const { isAuthenticated } = useAuth()

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

import type { components } from '@teggo/api/schema'
type Pricing = components['schemas']['ProductPricing']

const adding = ref(false)
const feedback = ref<{ severity: 'success' | 'warn' | 'error'; text: string } | null>(null)

// Contract pricing is buyer-specific, so it's only fetched for an authenticated
// session (client-side, where the session cookie is present).
const pricing = ref<Pricing | null>(null)
async function loadPricing() {
  if (!isAuthenticated.value) return
  const { data } = await client.GET('/storefront/products/{slug}/pricing', {
    params: { path: { slug: route.params.slug as string } },
  })
  pricing.value = data ?? null
}
onMounted(loadPricing)

async function addToCart() {
  if (!product.value) return
  if (!isAuthenticated.value) {
    router.push({ path: '/login', query: { redirect: route.fullPath } })
    return
  }
  feedback.value = null
  adding.value = true
  const { error: err, response } = await client.POST('/storefront/cart/items', {
    body: { product_public_id: product.value.public_id, quantity: '1' },
  })
  adding.value = false
  if (!err) {
    feedback.value = { severity: 'success', text: 'Added to your cart.' }
    return
  }
  feedback.value =
    response?.status === 409
      ? { severity: 'warn', text: 'No price available — request a quote for this product.' }
      : { severity: 'error', text: 'Could not add to cart.' }
}

useSeoMeta({
  title: () => (product.value ? `${product.value.name} — Teggo Store` : 'Product'),
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

        <div v-if="pricing" class="pricing">
          <h3>Your pricing</h3>
          <p v-if="pricing.price_on_request" class="por">Price on request — add to a quote and our team will respond.</p>
          <table v-else class="tiers">
            <thead><tr><th>Quantity</th><th>Unit price</th></tr></thead>
            <tbody>
              <tr v-for="(t, i) in pricing.tiers" :key="i">
                <td>{{ t.min_quantity }}+ <span class="unit">/ {{ t.unit }}</span></td>
                <td>{{ t.value }} {{ pricing.currency }}</td>
              </tr>
            </tbody>
          </table>
        </div>

        <Message v-if="feedback" :severity="feedback.severity" :closable="false" class="feedback">{{ feedback.text }}</Message>
        <div class="actions">
          <Button label="Add to cart" icon="pi pi-shopping-cart" :loading="adding" @click="addToCart" />
          <NuxtLink to="/rfq">
            <Button label="Request a quote" icon="pi pi-file-edit" severity="secondary" outlined />
          </NuxtLink>
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
.pricing {
  margin: 1.25rem 0;
  padding: 1rem;
  border: 1px solid var(--p-surface-200, #e2e8f0);
  border-radius: 10px;
  background: var(--p-surface-0, #fff);
}
.pricing h3 { margin: 0 0 0.6rem; font-size: 0.95rem; }
.por { margin: 0; color: var(--p-text-muted-color, #64748b); }
.tiers { width: 100%; border-collapse: collapse; font-variant-numeric: tabular-nums; }
.tiers th { text-align: left; font-size: 0.78rem; color: var(--p-text-muted-color, #64748b); font-weight: 600; padding-bottom: 0.35rem; }
.tiers td { padding: 0.3rem 0; border-top: 1px solid var(--p-surface-100, #f1f5f9); }
.tiers .unit { color: var(--p-text-muted-color, #64748b); font-size: 0.8rem; }
.actions {
  display: flex;
  gap: 0.75rem;
  margin-top: 1.5rem;
  flex-wrap: wrap;
}
</style>
