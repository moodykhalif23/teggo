<script setup lang="ts">
import ProductCard from '~/components/ProductCard.vue'
import Message from 'primevue/message'
import Button from 'primevue/button'
import type { components } from '@teggo/api/schema'

type Page = components['schemas']['ContentPage']
type Product = components['schemas']['StorefrontProduct']

const route = useRoute()
const client = useClient()
const slug = route.params.slug as string

// SSR fetch so CMS pages are crawlable. Sends the auth cookie so targeted
// (customer-group) pages resolve for signed-in buyers.
const { data: page, error } = await useAsyncData(`page-${slug}`, async () => {
  const { data, error } = await client.GET('/storefront/pages/{slug}', { params: { path: { slug } } })
  if (error || !data) throw createError({ statusCode: 404, statusMessage: 'Page not found' })
  return data as Page
})

const seo = computed(() => (page.value?.seo ?? {}) as Record<string, string>)
useSeoMeta({
  title: () => seo.value.title || page.value?.title || 'Teggo',
  description: () => seo.value.description || '',
})

// Loosely-typed block accessors (blocks are an additive JSONB union).
type Block = { type: string; id?: string; props?: Record<string, unknown> }
const blocks = computed<Block[]>(() => (page.value?.blocks as Block[] | undefined) ?? [])
function props(b: Block) {
  return (b.props ?? {}) as Record<string, any>
}
</script>

<template>
  <section>
    <Message v-if="error" severity="error" :closable="false">Page not found.</Message>

    <template v-else-if="page">
      <component :is="'div'" v-for="(b, i) in blocks" :key="b.id || i" class="block">
        <!-- hero -->
        <div v-if="b.type === 'hero'" class="hero">
          <h1>{{ props(b).heading }}</h1>
          <p v-if="props(b).subheading" class="sub">{{ props(b).subheading }}</p>
          <NuxtLink v-if="props(b).cta" :to="props(b).cta.href">
            <Button :label="props(b).cta.label" />
          </NuxtLink>
        </div>

        <!-- rich-text -->
        <!-- eslint-disable-next-line vue/no-v-html -->
        <div v-else-if="b.type === 'rich-text'" class="rich" v-html="props(b).html" />

        <!-- banner / cta -->
        <div v-else-if="b.type === 'banner' || b.type === 'cta'" class="banner">
          <span>{{ props(b).heading || props(b).text }}</span>
          <NuxtLink v-if="props(b).cta" :to="props(b).cta.href"><Button :label="props(b).cta.label" size="small" /></NuxtLink>
        </div>

        <!-- product-grid (products resolved server-side) -->
        <div v-else-if="b.type === 'product-grid'" class="pg">
          <h2 v-if="props(b).heading">{{ props(b).heading }}</h2>
          <div class="grid">
            <ProductCard v-for="p in (props(b).products as Product[] | undefined) ?? []" :key="p.public_id" :product="p" />
          </div>
        </div>
      </component>
    </template>
  </section>
</template>

<style scoped>
.block { margin-bottom: 2rem; }
.hero { text-align: center; padding: 3rem 1rem; background: var(--p-surface-50, #f8fafc); border-radius: 12px; }
.hero h1 { margin: 0 0 0.5rem; font-size: 2rem; }
.sub { color: var(--p-text-muted-color, #64748b); margin-bottom: 1rem; }
.rich { line-height: 1.7; }
.banner { display: flex; align-items: center; justify-content: space-between; gap: 1rem; padding: 1rem 1.5rem; background: var(--p-primary-50, #e0f2fe); border-radius: 10px; }
.pg .grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(240px, 1fr)); gap: 1rem; }
</style>
