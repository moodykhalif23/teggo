<script setup lang="ts">
import { computed } from 'vue'
import { useAuthStore } from '@/stores/auth'
import Card from 'primevue/card'
import Tag from 'primevue/tag'

const auth = useAuthStore()
const perms = computed(() => auth.permissions)
</script>

<template>
  <div class="page">
    <h1>Dashboard</h1>
    <p class="muted">
      Back-office shell for the oro-folk B2B platform. Modules are added per the build order
      in <code>docs/</code> — each becomes a route + a PrimeVue view backed by the Go API.
    </p>

    <div class="grid">
      <Card>
        <template #title>Session</template>
        <template #content>
          <p><strong>Organization:</strong> {{ auth.orgId ?? '—' }}</p>
          <p><strong>Permissions</strong></p>
          <div class="tags">
            <Tag v-for="p in perms" :key="p" :value="p" severity="secondary" />
            <span v-if="!perms.length" class="muted">none</span>
          </div>
        </template>
      </Card>

      <Card>
        <template #title>Next modules</template>
        <template #content>
          <ul class="roadmap">
            <li>Customers &amp; accounts (hierarchy)</li>
            <li>Catalog &amp; PIM (attributes, categories)</li>
            <li>Pricing engine (price lists → combined prices)</li>
            <li>RFQ → Quote → Order</li>
            <li>Order-to-cash (invoices, payments)</li>
          </ul>
        </template>
      </Card>
    </div>
  </div>
</template>

<style scoped>
.page h1 {
  margin: 0 0 0.25rem;
}
.muted {
  color: var(--p-text-muted-color, #64748b);
}
.grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(280px, 1fr));
  gap: 1rem;
  margin-top: 1rem;
}
.tags {
  display: flex;
  flex-wrap: wrap;
  gap: 0.4rem;
  margin-top: 0.25rem;
}
.roadmap {
  margin: 0;
  padding-left: 1.1rem;
  line-height: 1.7;
}
</style>
