<script setup lang="ts">
import { Handle, Position } from '@vue-flow/core'
import Select from 'primevue/select'
import InputText from 'primevue/inputtext'
import Button from 'primevue/button'
import { OPS } from './catalog'

// data: { conds: reactive Cond[], add: fn, remove: (i)=>void, fields: ()=>string[], trigger: ()=>string }
defineProps<{ data: any }>()
</script>

<template>
  <div class="node conditions">
    <Handle type="target" :position="Position.Left" />
    <div class="head"><i class="pi pi-filter" /> Only if <span class="muted">(all match)</span></div>
    <div class="body nodrag">
      <div v-for="(c, i) in data.conds" :key="i" class="cond">
        <div class="line">
          <Select v-model="c.field" :options="data.fields()" editable placeholder="field" class="grow" />
          <Button icon="pi pi-times" text rounded severity="danger" @click="data.remove(i)" />
        </div>
        <div class="line">
          <Select v-model="c.op" :options="OPS" optionLabel="label" optionValue="value" class="op" />
          <InputText v-model="c.value" placeholder="value" class="grow" />
        </div>
      </div>
      <p v-if="!data.conds.length" class="muted empty">
        No conditions — runs on every <strong>{{ data.trigger() || 'event' }}</strong>.
      </p>
      <Button label="Add condition" icon="pi pi-plus" size="small" text @click="data.add()" />
    </div>
    <Handle type="source" :position="Position.Right" />
  </div>
</template>

<style scoped>
.node { width: 340px; border-radius: 10px; background: var(--p-surface-0, #fff); border: 1.5px solid var(--p-surface-200, #e2e8f0); box-shadow: 0 1px 3px rgba(0,0,0,0.06); }
.conditions { border-color: var(--p-amber-300, #fcd34d); }
.head { display: flex; align-items: center; gap: 0.4rem; padding: 0.5rem 0.75rem; font-weight: 600; font-size: 0.85rem; background: var(--p-amber-50, #fffbeb); border-radius: 9px 9px 0 0; cursor: grab; }
.body { padding: 0.6rem 0.75rem 0.75rem; }
.cond { border: 1px solid var(--p-surface-200, #e2e8f0); border-radius: 8px; padding: 0.5rem; margin-bottom: 0.5rem; }
.line { display: flex; align-items: center; gap: 0.4rem; }
.line + .line { margin-top: 0.4rem; }
.grow { flex: 1; min-width: 0; }
.op { width: 8.5rem; flex: none; }
.muted { color: var(--p-text-muted-color, #64748b); font-weight: 400; }
.empty { font-size: 0.82rem; margin: 0.2rem 0 0.5rem; }
/* field + value fill their flex cell; the op select fills its fixed-width cell. */
.grow :deep(.p-select), .grow :deep(.p-inputtext) { width: 100%; }
.op :deep(.p-select) { width: 100%; }
</style>
