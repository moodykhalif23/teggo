<script setup lang="ts">
import { computed } from 'vue'
import { Handle, Position } from '@vue-flow/core'
import Select from 'primevue/select'
import InputText from 'primevue/inputtext'
import Button from 'primevue/button'
import { ACTION_CATALOG, actionDef } from './catalog'

// data: { act: reactive Act, onRemove: fn }
const props = defineProps<{ data: any }>()
const def = computed(() => actionDef(props.data.act.key))
</script>

<template>
  <div class="node action">
    <Handle type="target" :position="Position.Left" />
    <div class="head">
      <span><i class="pi pi-play" /> Then do</span>
      <Button icon="pi pi-times" text rounded severity="danger" class="rm" @click="data.onRemove()" />
    </div>
    <div class="body nodrag">
      <Select
        v-model="data.act.key"
        :options="ACTION_CATALOG"
        optionLabel="label"
        optionValue="key"
        placeholder="action"
        class="w-full"
      />
      <div v-if="def?.params.length" class="params">
        <div v-for="p in def.params" :key="p.name" class="field">
          <label>{{ p.label }}</label>
          <InputText
            :modelValue="data.act.params[p.name] ?? ''"
            :placeholder="p.placeholder"
            @update:modelValue="data.act.params[p.name] = $event as string"
          />
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.node { width: 240px; border-radius: 10px; background: var(--p-surface-0, #fff); border: 1.5px solid var(--p-surface-200, #e2e8f0); box-shadow: 0 1px 3px rgba(0,0,0,0.06); }
.action { border-color: var(--p-green-300, #86efac); }
.head { display: flex; align-items: center; justify-content: space-between; gap: 0.4rem; padding: 0.35rem 0.5rem 0.35rem 0.75rem; font-weight: 600; font-size: 0.85rem; background: var(--p-green-50, #f0fdf4); border-radius: 9px 9px 0 0; cursor: grab; }
.rm { width: 1.75rem; height: 1.75rem; }
.body { padding: 0.75rem; }
.w-full { width: 100%; }
.body :deep(.p-select) { width: 100%; }
.params { margin-top: 0.6rem; display: flex; flex-direction: column; gap: 0.5rem; }
.field { display: flex; flex-direction: column; gap: 0.25rem; }
.field label { font-size: 0.78rem; font-weight: 600; }
.field :deep(.p-inputtext) { width: 100%; }
</style>
