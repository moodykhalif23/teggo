<script setup lang="ts">
import ProductCard from '~/components/ProductCard.vue'
import Message from 'primevue/message'
import Select from 'primevue/select'
import Button from 'primevue/button'

const route = useRoute()
const router = useRouter()
const client = useClient()

const q = computed(() => (route.query.q as string)?.trim() ?? '')
const sort = computed(() => (route.query.sort as string) || 'relevance')

// Selected attribute facets come from the URL (?filter=<json>) so results are
// shareable and SSR-stable.
const selected = computed<Record<string, string>>(() => {
  try {
    return route.query.filter ? JSON.parse(route.query.filter as string) : {}
  } catch {
    return {}
  }
})

const sortOptions = [
  { label: 'Relevance', value: 'relevance' },
  { label: 'Name', value: 'name' },
  { label: 'Newest', value: 'newest' },
]

const { data, error } = await useAsyncData(
  () => `catalog-${q.value}-${route.query.filter ?? ''}-${sort.value}`,
  async () => {
    const query: Record<string, string | number> = { page: 1, page_size: 24, sort: sort.value }
    if (q.value) query.q = q.value
    if (route.query.filter) query.filter = route.query.filter as string
    const { data, error } = await client.GET('/storefront/catalog', { params: { query } })
    if (error) throw createError({ statusCode: 502, statusMessage: 'Search unavailable' })
    return data
  },
  { watch: [() => route.query.q, () => route.query.filter, () => route.query.sort] },
)

useSeoMeta({
  title: () => (q.value ? `Search: ${q.value} — Teggo Store` : 'Catalog — Teggo Store'),
  description: () => `Product search and filtering${q.value ? ` for "${q.value}"` : ''}.`,
})

function navigate(next: { filter?: Record<string, string>; sort?: string }) {
  const filter = next.filter ?? selected.value
  const query: Record<string, string> = {}
  if (q.value) query.q = q.value
  if (next.sort ?? route.query.sort) query.sort = (next.sort ?? route.query.sort) as string
  if (Object.keys(filter).length) query.filter = JSON.stringify(filter)
  router.push({ path: '/search', query })
}

function toggleFacet(attr: string, value: string) {
  const f = { ...selected.value }
  if (f[attr] === value) delete f[attr]
  else f[attr] = value
  navigate({ filter: f })
}

function clearFilters() {
  navigate({ filter: {} })
}

function isSelected(attr: string, value: string) {
  return selected.value[attr] === value
}
</script>

<template>
  <section>
    <div class="head">
      <h1 class="title">
        Catalog <span v-if="q" class="muted">— “{{ q }}”</span>
        <span v-if="data" class="muted count">({{ data.total }})</span>
      </h1>
      <Select
        :modelValue="sort"
        :options="sortOptions"
        optionLabel="label"
        optionValue="value"
        @update:modelValue="navigate({ sort: $event })"
      />
    </div>

    <Message v-if="error" severity="error" :closable="false">Search is unavailable right now.</Message>

    <div v-else class="layout">
      <aside class="facets">
        <div class="facets-head">
          <strong>Filters</strong>
          <Button v-if="Object.keys(selected).length" label="Clear" size="small" text @click="clearFilters" />
        </div>
        <div v-for="f in data?.facets ?? []" :key="f.attr" class="facet">
          <div class="facet-name">{{ f.attr }}</div>
          <button
            v-for="v in f.values"
            :key="v.value"
            class="facet-value"
            :class="{ on: isSelected(f.attr, v.value) }"
            @click="toggleFacet(f.attr, v.value)"
          >
            <span>{{ v.value }}</span>
            <span class="n">{{ v.count }}</span>
          </button>
        </div>
        <p v-if="!(data?.facets?.length)" class="muted">No filters available.</p>
      </aside>

      <div class="results">
        <div v-if="data?.items?.length" class="grid">
          <ProductCard v-for="p in data.items" :key="p.public_id" :product="p" />
        </div>
        <p v-else class="muted">No products match your filters.</p>
      </div>
    </div>
  </section>
</template>

<style scoped>
.head { display: flex; align-items: center; justify-content: space-between; margin-bottom: 1.25rem; }
.title { margin: 0; }
.count { font-weight: 400; }
.muted { color: var(--p-text-muted-color, #64748b); font-weight: 400; }
.layout { display: grid; grid-template-columns: 220px 1fr; gap: 1.5rem; align-items: start; }
.facets { border: 1px solid var(--p-surface-200, #e2e8f0); border-radius: 10px; padding: 1rem; }
.facets-head { display: flex; align-items: center; justify-content: space-between; margin-bottom: 0.5rem; }
.facet { margin-bottom: 1rem; }
.facet-name { font-size: 0.8rem; text-transform: uppercase; letter-spacing: 0.04em; color: var(--p-text-muted-color, #64748b); margin-bottom: 0.35rem; }
.facet-value {
  display: flex; justify-content: space-between; width: 100%; gap: 0.5rem;
  background: none; border: none; padding: 0.3rem 0.4rem; cursor: pointer; border-radius: 6px;
  text-align: left; font-size: 0.9rem; color: inherit;
}
.facet-value:hover { background: var(--p-surface-100, #f1f5f9); }
.facet-value.on { background: var(--p-primary-100, #e0f2fe); font-weight: 600; }
.facet-value .n { color: var(--p-text-muted-color, #64748b); }
.results .grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(220px, 1fr)); gap: 1rem; }
</style>
