<script setup lang="ts">
import Button from 'primevue/button'

useSeoMeta({
  title: 'Oro Store — B2B commerce',
  description: 'Browse the catalog, request quotes, and reorder fast. Built on oro-folk.',
})
</script>

<template>
  <section class="hero">
    <h1>Welcome to the Oro Store</h1>
    <p>
      A server-rendered B2B storefront. Browse the catalog, see your contract pricing, build
      shopping lists, and request quotes.
    </p>
    <NuxtLink to="/c/all">
      <Button label="Browse catalog" icon="pi pi-arrow-right" iconPos="right" />
    </NuxtLink>
  </section>
</template>

<style scoped>
.hero {
  padding: 4rem 0;
  max-width: 640px;
}
.hero h1 {
  font-size: 2.25rem;
  margin: 0 0 1rem;
}
.hero p {
  font-size: 1.1rem;
  color: var(--p-text-muted-color, #64748b);
  margin: 0 0 1.5rem;
}
</style>
