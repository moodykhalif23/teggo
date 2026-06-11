<script setup lang="ts">
import { Handle, Position } from '@vue-flow/core'
import Select from 'primevue/select'

// data: { state: reactive { event }, options: string[] }
defineProps<{ data: any }>()
</script>

<template>
  <div class="node trigger">
    <div class="head"><i class="pi pi-bolt" /> When this happens</div>
    <div class="body nodrag">
      <Select
        v-model="data.state.event"
        :options="data.options"
        editable
        placeholder="Select or type an event"
        class="w-full"
      />
    </div>
    <Handle type="source" :position="Position.Right" />
  </div>
</template>

<style scoped>
.node { width: 240px; border-radius: 10px; background: var(--p-surface-0, #fff); border: 1.5px solid var(--p-surface-200, #e2e8f0); box-shadow: 0 1px 3px rgba(0,0,0,0.06); }
.trigger { border-color: var(--p-primary-300, #93c5fd); }
.head { display: flex; align-items: center; gap: 0.4rem; padding: 0.5rem 0.75rem; font-weight: 600; font-size: 0.85rem; background: var(--p-primary-50, #eff6ff); border-radius: 9px 9px 0 0; cursor: grab; }
.body { padding: 0.75rem; }
.w-full { width: 100%; }
.body :deep(.p-select) { width: 100%; }
</style>
