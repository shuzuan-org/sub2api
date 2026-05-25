<template>
  <span class="relative inline-flex cursor-pointer" style="flex-shrink: 0" @click.stop="toggle">
    <span
      class="pricing-btn inline-flex items-center justify-center rounded-full font-bold leading-none transition-colors"
      :class="[buttonClass, size === 'sm' ? 'pricing-btn-sm' : 'pricing-btn-md']"
    >?</span>
    <Teleport to="body">
      <div v-if="open" class="fixed inset-0" style="z-index: 100000030" @click="open = false" />
      <div
        v-if="open"
        class="fixed w-80 max-h-72 overflow-auto rounded-xl border border-gray-200 bg-white p-3 shadow-xl dark:border-dark-600 dark:bg-dark-800"
        style="z-index: 100000031"
        :style="posStyle"
      >
        <div class="mb-2 text-xs font-semibold text-gray-700 dark:text-gray-300">{{ groupName }} - 模型价格</div>
        <div v-if="loading" class="flex justify-center py-4">
          <svg class="h-5 w-5 animate-spin text-gray-400" fill="none" viewBox="0 0 24 24">
            <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4" />
            <path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4z" />
          </svg>
        </div>
        <div v-else-if="error" class="py-2 text-center text-xs text-red-500">{{ error }}</div>
        <div v-else-if="unsupported" class="py-2 text-center text-xs text-gray-400">该平台按固定价格计费，不按模型区分</div>
        <div v-else-if="data.length === 0" class="py-2 text-center text-xs text-gray-400">暂无定价数据</div>
        <table v-else class="w-full text-xs">
          <thead>
            <tr class="border-b border-gray-100 dark:border-dark-600">
              <th class="pb-1.5 text-left font-medium text-gray-500 dark:text-gray-400">模型</th>
              <th class="pb-1.5 text-right font-medium text-gray-500 dark:text-gray-400">输入/MTok</th>
              <th class="pb-1.5 text-right font-medium text-gray-500 dark:text-gray-400">输出/MTok</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="m in data" :key="m.model" class="border-b border-gray-50 dark:border-dark-700 last:border-0">
              <td class="py-1.5 pr-2 text-gray-700 dark:text-gray-300 truncate max-w-[140px]" :title="m.model">{{ m.model }}</td>
              <td class="py-1.5 text-right tabular-nums">
                <div class="text-gray-600 dark:text-gray-400">{{ formatPrice(m.input_per_mtok) }} U</div>
                <div class="text-gray-400 dark:text-gray-500 text-[10px]">{{ formatUsdFromU(m.input_per_mtok) }}</div>
              </td>
              <td class="py-1.5 text-right tabular-nums">
                <div class="text-gray-600 dark:text-gray-400">{{ formatPrice(m.output_per_mtok) }} U</div>
                <div class="text-gray-400 dark:text-gray-500 text-[10px]">{{ formatUsdFromU(m.output_per_mtok) }}</div>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </Teleport>
  </span>
</template>

<script setup lang="ts">
import { ref, computed } from 'vue'
import { getGroupModelPricing, type ModelPricing } from '@/api/groups'
import { formatUsdFromU } from '@/utils/format'

interface Props {
  groupId: number
  groupName: string
  platform?: string
  /** 按钮尺寸样式 */
  size?: 'sm' | 'md'
}

const props = withDefaults(defineProps<Props>(), {
  size: 'sm'
})

const buttonClass = computed(() => {
  if (props.size === 'sm') return 'bg-black/10 dark:bg-white/15 hover:bg-black/20 dark:hover:bg-white/25'
  return 'bg-gray-200 dark:bg-dark-600 text-gray-500 dark:text-gray-400 hover:bg-gray-300 dark:hover:bg-dark-500'
})

function formatPrice(v: number): string {
  if (v >= 1000) return Math.round(v).toLocaleString()
  if (v >= 1) return v.toFixed(2)
  return v.toFixed(4)
}

// 不支持按模型定价的平台
const unsupportedPlatforms = new Set(['antigravity', 'sora'])
const unsupported = computed(() => props.platform && unsupportedPlatforms.has(props.platform))

const open = ref(false)
const loading = ref(false)
const error = ref('')
const data = ref<ModelPricing[]>([])
const posStyle = ref<Record<string, string>>({})

// 模块级缓存：同一 groupId 只请求一次，跨组件实例共享
const cache = new Map<number, ModelPricing[]>()

function clamp(val: number, min: number, max: number) {
  return Math.max(min, Math.min(max, val))
}

async function toggle(e: MouseEvent) {
  if (open.value) {
    open.value = false
    return
  }

  // 定位：带边界钳位
  const rect = (e.target as HTMLElement).getBoundingClientRect()
  const popW = 320, popH = 288 // w-80 = 320px, max-h-72 = 288px
  const top = clamp(rect.bottom + 6, 8, window.innerHeight - popH - 8)
  const left = clamp(rect.left - popW / 2, 8, window.innerWidth - popW - 8)
  posStyle.value = { top: `${top}px`, left: `${left}px` }
  open.value = true

  if (unsupported.value) return

  // 命中缓存
  if (cache.has(props.groupId)) {
    data.value = cache.get(props.groupId)!
    return
  }

  loading.value = true
  error.value = ''
  try {
    const result = await getGroupModelPricing(props.groupId)
    const safe = Array.isArray(result) ? result : []
    cache.set(props.groupId, safe)
    data.value = safe
  } catch (err: any) {
    error.value = err?.response?.data?.message || '获取价格失败'
  } finally {
    loading.value = false
  }
}
</script>

<style scoped>
.pricing-btn-sm {
  width: 14px;
  height: 14px;
  min-width: 14px;
  flex-shrink: 0;
  font-size: 9px;
}
.pricing-btn-md {
  width: 16px;
  height: 16px;
  min-width: 16px;
  flex-shrink: 0;
  font-size: 10px;
}
</style>
