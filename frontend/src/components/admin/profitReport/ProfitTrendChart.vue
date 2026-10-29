<template>
  <div class="card p-4">
    <div class="mb-3 flex flex-wrap items-baseline justify-between gap-2">
      <h3 class="text-sm font-semibold text-gray-900 dark:text-white">
        {{ t('admin.profitReport.chart.title') }}
        <span class="ml-2 text-xs font-normal text-gray-500 dark:text-gray-400">{{ t('admin.profitReport.chart.hint') }}</span>
      </h3>
      <div class="flex flex-wrap gap-4 text-xs text-gray-600 dark:text-gray-300">
        <span class="inline-flex items-center gap-1.5"><i class="inline-block h-0.5 w-3.5 rounded" :style="{ background: colors.rev }"></i>{{ t('admin.profitReport.chart.revenue') }}</span>
        <span class="inline-flex items-center gap-1.5"><i class="inline-block h-0.5 w-3.5 rounded" :style="{ background: colors.cost }"></i>{{ t('admin.profitReport.chart.cost') }}</span>
        <span class="inline-flex items-center gap-1.5"><i class="inline-block h-2.5 w-3.5 rounded-sm opacity-60" :style="{ background: colors.margin }"></i>{{ t('admin.profitReport.chart.margin') }}</span>
      </div>
    </div>
    <div v-if="loading" class="flex h-64 items-center justify-center"><LoadingSpinner /></div>
    <div v-else-if="points.length" class="h-64">
      <Bar :data="chartData" :options="chartOptions" />
    </div>
    <div v-else class="flex h-64 items-center justify-center text-sm text-gray-500 dark:text-gray-400">
      {{ t('admin.profitReport.chart.noData') }}
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  Chart as ChartJS,
  BarElement,
  CategoryScale,
  LinearScale,
  LineElement,
  PointElement,
  Tooltip,
  Legend
} from 'chart.js'
import type { ChartData, ChartOptions, TooltipItem } from 'chart.js'
import { Bar } from 'vue-chartjs'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'

ChartJS.register(BarElement, CategoryScale, LinearScale, LineElement, PointElement, Tooltip, Legend)

export interface ProfitTrendPoint {
  day: string
  rev: number
  cost: number
  margin: number
}

const props = defineProps<{
  points: ProfitTrendPoint[]
  loading?: boolean
  /** 把 U 值格式化成当前显示单位（含符号） */
  format: (v: number) => string
  /** 坐标轴用的短格式 */
  formatAxis: (v: number) => string
}>()

const { t } = useI18n()

const isDarkMode = computed(() => document.documentElement.classList.contains('dark'))
const colors = computed(() => ({
  rev: isDarkMode.value ? '#3987e5' : '#2a78d6',
  cost: isDarkMode.value ? '#d95926' : '#eb6834',
  margin: isDarkMode.value ? '#199e70' : '#1baf7a',
  grid: isDarkMode.value ? '#374151' : '#e5e7eb',
  text: isDarkMode.value ? '#9ca3af' : '#6b7280',
  tooltipBg: isDarkMode.value ? '#1f2937' : '#ffffff',
  tooltipFg: isDarkMode.value ? '#e5e7eb' : '#111827'
}))

// 混合图：两条折线 + 毛利柱。vue-chartjs 的 Bar 组件按 'bar' 定型，混合数据集需要显式断言。
const chartData = computed(() => ({
  labels: props.points.map((p) => p.day.slice(5)),
  datasets: [
    {
      type: 'line' as const,
      label: t('admin.profitReport.chart.revenue'),
      data: props.points.map((p) => p.rev),
      borderColor: colors.value.rev,
      backgroundColor: colors.value.rev,
      borderWidth: 2,
      pointRadius: props.points.length > 45 ? 0 : 2,
      pointHoverRadius: 4,
      tension: 0.2,
      order: 0
    },
    {
      type: 'line' as const,
      label: t('admin.profitReport.chart.cost'),
      data: props.points.map((p) => p.cost),
      borderColor: colors.value.cost,
      backgroundColor: colors.value.cost,
      borderWidth: 2,
      pointRadius: props.points.length > 45 ? 0 : 2,
      pointHoverRadius: 4,
      tension: 0.2,
      order: 1
    },
    {
      type: 'bar' as const,
      label: t('admin.profitReport.chart.margin'),
      data: props.points.map((p) => p.margin),
      backgroundColor: colors.value.margin + '8c',
      borderRadius: 2,
      maxBarThickness: 18,
      order: 2
    }
  ]
}) as unknown as ChartData<'bar', number[], string>)

const chartOptions = computed<ChartOptions<'bar'>>(() => ({
  responsive: true,
  maintainAspectRatio: false,
  interaction: { intersect: false, mode: 'index' },
  plugins: {
    legend: { display: false },
    tooltip: {
      backgroundColor: colors.value.tooltipBg,
      titleColor: colors.value.tooltipFg,
      bodyColor: colors.value.tooltipFg,
      borderColor: colors.value.grid,
      borderWidth: 1,
      callbacks: {
        title: (items: TooltipItem<'bar'>[]) => props.points[items[0]?.dataIndex ?? 0]?.day ?? '',
        label: (item: TooltipItem<'bar'>) => {
          const p = props.points[item.dataIndex]
          const v = Number(item.raw ?? 0)
          const suffix =
            item.datasetIndex === 2 && p && p.rev > 0
              ? ` (${((100 * p.margin) / p.rev).toFixed(1)}%)`
              : ''
          return ` ${item.dataset.label}: ${props.format(v)}${suffix}`
        }
      }
    }
  },
  scales: {
    x: {
      grid: { display: false },
      ticks: { color: colors.value.text, maxRotation: 0, autoSkip: true, maxTicksLimit: 10, font: { size: 11 } }
    },
    y: {
      grid: { color: colors.value.grid },
      ticks: {
        color: colors.value.text,
        font: { size: 11 },
        callback: (value: string | number) => props.formatAxis(Number(value))
      }
    }
  }
}))
</script>
