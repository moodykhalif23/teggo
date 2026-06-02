<script setup lang="ts">
// Tree-shaken ECharts wrapper (Brian's directive: Apache ECharts everywhere).
// Register only the modules we use rather than the full echarts bundle.
import { computed } from 'vue'
import VChart from 'vue-echarts'
import { use } from 'echarts/core'
import { CanvasRenderer } from 'echarts/renderers'
import { BarChart as EBarChart } from 'echarts/charts'
import { GridComponent, TooltipComponent } from 'echarts/components'
import type { EChartsOption } from 'echarts'

use([CanvasRenderer, EBarChart, GridComponent, TooltipComponent])

const props = defineProps<{
  labels: string[]
  values: number[]
  name?: string
}>()

const option = computed<EChartsOption>(() => ({
  grid: { left: 8, right: 16, top: 16, bottom: 24, containLabel: true },
  tooltip: { trigger: 'axis', axisPointer: { type: 'shadow' } },
  xAxis: {
    type: 'category',
    data: props.labels,
    axisTick: { show: false },
    axisLine: { lineStyle: { color: '#cbd5e1' } },
  },
  yAxis: {
    type: 'value',
    minInterval: 1,
    splitLine: { lineStyle: { color: '#eef2f7' } },
  },
  series: [
    {
      name: props.name ?? 'Count',
      type: 'bar',
      data: props.values,
      barMaxWidth: 48,
      itemStyle: { color: '#0ea5e9', borderRadius: [6, 6, 0, 0] },
    },
  ],
}))
</script>

<template>
  <VChart :option="option" autoresize class="chart" />
</template>

<style scoped>
.chart { height: 280px; width: 100%; }
</style>
