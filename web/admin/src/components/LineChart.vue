<script setup lang="ts">
// Tree-shaken ECharts line chart (Brian's directive: Apache ECharts everywhere).
import { computed } from 'vue'
import VChart from 'vue-echarts'
import { use } from 'echarts/core'
import { CanvasRenderer } from 'echarts/renderers'
import { LineChart as ELineChart } from 'echarts/charts'
import { GridComponent, TooltipComponent } from 'echarts/components'
import type { EChartsOption } from 'echarts'

use([CanvasRenderer, ELineChart, GridComponent, TooltipComponent])

const props = defineProps<{ labels: string[]; values: number[]; name?: string }>()

const option = computed<EChartsOption>(() => ({
  grid: { left: 8, right: 16, top: 16, bottom: 24, containLabel: true },
  tooltip: { trigger: 'axis' },
  xAxis: { type: 'category', data: props.labels, axisTick: { show: false }, axisLine: { lineStyle: { color: '#cbd5e1' } } },
  yAxis: { type: 'value', splitLine: { lineStyle: { color: '#eef2f7' } } },
  series: [
    {
      name: props.name ?? 'Value',
      type: 'line',
      data: props.values,
      smooth: true,
      showSymbol: false,
      lineStyle: { color: '#0ea5e9', width: 2 },
      areaStyle: { color: 'rgba(14,165,233,0.12)' },
    },
  ],
}))
</script>

<template>
  <VChart :option="option" autoresize class="chart" />
</template>

<style scoped>
.chart { height: 300px; width: 100%; }
</style>
