<script setup lang="ts">
import { onMounted, ref } from 'vue'
import Card from 'primevue/card'
import Message from 'primevue/message'
import { api, errMessage } from '@/lib/client'
import type { components } from '@teggo/api/schema'

type Dashboard = components['schemas']['VendorDashboard']
type Vendor = components['schemas']['Vendor']

const dash = ref<Dashboard | null>(null)
const vendor = ref<Vendor | null>(null)
const error = ref('')

async function load() {
  const { data, error: err } = await api.GET('/vendor/dashboard')
  if (err || !data) {
    error.value = errMessage(err, 'Failed to load dashboard')
    return
  }
  dash.value = data
  const me = await api.GET('/vendor/me')
  vendor.value = me.data ?? null
}

onMounted(load)
</script>

<template>
  <div class="page">
    <h1>{{ vendor?.name ?? 'Dashboard' }}</h1>
    <p class="muted">Your marketplace performance at a glance.</p>

    <Message v-if="error" severity="error" :closable="false" class="mb">{{ error }}</Message>

    <div v-if="dash" class="cards">
      <Card><template #title>Orders</template><template #content><div class="big">{{ dash.order_count }}</div></template></Card>
      <Card><template #title>Gross sales</template><template #content><div class="big">{{ dash.gross_total }}</div></template></Card>
      <Card><template #title>Commission</template><template #content><div class="big">{{ dash.commission_total }}</div></template></Card>
      <Card><template #title>Net earnings</template><template #content><div class="big net">{{ dash.net_total }}</div></template></Card>
    </div>
  </div>
</template>

<style scoped>
.page h1 { margin: 0; }
.muted { color: #64748b; }
.mb { margin-bottom: 1rem; }
.cards { display: grid; grid-template-columns: repeat(4, 1fr); gap: 1rem; margin-top: 1.25rem; }
.big { font-size: 1.8rem; font-weight: 700; font-variant-numeric: tabular-nums; }
.net { color: #15803d; }
@media (max-width: 900px) { .cards { grid-template-columns: 1fr 1fr; } }
</style>
