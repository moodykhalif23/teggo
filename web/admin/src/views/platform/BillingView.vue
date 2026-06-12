<script setup lang="ts">
import { computed, onMounted } from 'vue'
import Card from 'primevue/card'
import Tag from 'primevue/tag'
import ProgressBar from 'primevue/progressbar'
import PageHeader from '@/components/PageHeader.vue'
import { useBillingStore } from '@/stores/billing'

// Billing & usage (SAAS.md #2): the org's plan, what it enables, and how much
// of each metered limit the org has consumed this period.

const billing = useBillingStore()

const featureLabels: Record<string, string> = {
  subscriptions: 'Recurring orders',
  rebates: 'Rebates & volume incentives',
  fx: 'Multi-currency & FX',
  merchandising: 'Search merchandising',
  assistant: 'AI assistant',
}
const meterLabels: Record<string, string> = {
  orders: 'Orders this month',
  ai_calls: 'AI assistant calls this month',
  storage_bytes: 'Media storage',
}

const meters = computed(() => {
  const v = billing.view
  if (!v) return []
  const limits = v.limits ?? {}
  const usage = v.usage ?? {}
  return Object.keys(meterLabels).map((metric) => {
    const used = usage[metric] ?? 0
    const limit = limits[metric]
    return {
      metric,
      label: meterLabels[metric],
      used,
      limit,
      pct: limit ? Math.min(100, Math.round((used / limit) * 100)) : 0,
    }
  })
})

function fmt(metric: string, n: number | undefined): string {
  if (n == null) return '∞'
  if (metric === 'storage_bytes') {
    if (n >= 1 << 30) return (n / (1 << 30)).toFixed(1) + ' GiB'
    if (n >= 1 << 20) return (n / (1 << 20)).toFixed(1) + ' MiB'
    return n + ' B'
  }
  return String(n)
}

onMounted(() => billing.load())
</script>

<template>
  <div class="page">
    <PageHeader title="Billing & usage" subtitle="Your plan, what it enables, and this period's consumption." />

    <div class="grid">
      <Card>
        <template #title>Plan</template>
        <template #content>
          <template v-if="billing.view?.plan?.code">
            <div class="plan-row">
              <span class="plan-name">{{ billing.view.plan.name }}</span>
              <Tag :value="billing.view.status || 'active'" severity="success" />
            </div>
            <p class="muted price">
              {{ billing.view.plan.price === '0' || billing.view.plan.price === '0.0000' ? 'Free' : `${billing.view.plan.price} ${billing.view.plan.currency}/month` }}
            </p>
            <p class="muted small">Plan changes are handled by your platform operator.</p>
          </template>
          <p v-else class="muted">This organization is not metered.</p>
        </template>
      </Card>

      <Card>
        <template #title>Usage</template>
        <template #content>
          <div v-for="m in meters" :key="m.metric" class="meter">
            <div class="meter-head">
              <span>{{ m.label }}</span>
              <span class="muted">{{ fmt(m.metric, m.used) }} / {{ fmt(m.metric, m.limit) }}</span>
            </div>
            <ProgressBar v-if="m.limit" :value="m.pct" :showValue="false" style="height: 8px" />
            <p v-else class="muted small unlimited">Unlimited on your plan</p>
          </div>
        </template>
      </Card>

      <Card>
        <template #title>Features</template>
        <template #content>
          <ul class="features">
            <li v-for="(label, key) in featureLabels" :key="key">
              <i :class="billing.allows(key) ? 'pi pi-check-circle on' : 'pi pi-lock off'" />
              <span :class="{ muted: !billing.allows(key) }">{{ label }}</span>
            </li>
          </ul>
        </template>
      </Card>
    </div>
  </div>
</template>

<style scoped>
.grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(280px, 1fr));
  gap: 1.25rem;
}
.plan-row {
  display: flex;
  align-items: center;
  gap: 0.75rem;
}
.plan-name {
  font-size: 1.2rem;
  font-weight: 700;
}
.price {
  margin: 0.5rem 0 0.25rem;
}
.muted {
  color: var(--p-text-muted-color, #64748b);
  font-weight: 400;
}
.small {
  font-size: 0.85rem;
}
.meter {
  margin-bottom: 1.1rem;
}
.meter-head {
  display: flex;
  justify-content: space-between;
  font-size: 0.9rem;
  margin-bottom: 0.35rem;
}
.unlimited {
  margin: 0;
}
.features {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 0.6rem;
}
.features li {
  display: flex;
  align-items: center;
  gap: 0.55rem;
}
.features .on {
  color: var(--p-primary-color, #16a34a);
}
.features .off {
  color: var(--p-text-muted-color, #94a3b8);
}
</style>
