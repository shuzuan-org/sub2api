<template>
  <span
    :class="[
      'inline-flex items-center gap-1.5 rounded-md px-2 py-0.5 text-xs font-medium transition-colors',
      badgeClass
    ]"
  >
    <!-- Platform logo -->
    <PlatformIcon v-if="platform" :platform="platform" size="sm" />
    <!-- Group name -->
    <span class="truncate" :title="name">{{ name }}</span>
    <!-- Right side label -->
    <span v-if="showLabel" :class="labelClass">
      <template v-if="hasCustomRate">
        <!-- 原倍率删除线 + 专属倍率高亮 -->
        <span class="line-through opacity-50 mr-0.5">{{ rateMultiplier }}x</span>
        <span class="font-bold">{{ userRateMultiplier }}x</span>
      </template>
      <template v-else-if="daysRemaining !== null && daysRemaining !== undefined">
        {{ daysRemainingText }}
      </template>
      <template v-else>
        {{ rateMultiplier !== undefined ? `${rateMultiplier}x` : '' }}
      </template>
    </span>
    <!-- Pricing info button -->
    <ModelPricingPopover
      v-if="groupId && showPricingButton"
      :group-id="groupId"
      :group-name="name"
      :platform="platform"
      size="sm"
    />
  </span>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { GroupPlatform } from '@/types'
import PlatformIcon from './PlatformIcon.vue'
import ModelPricingPopover from './ModelPricingPopover.vue'

interface Props {
  name: string
  platform?: GroupPlatform
  rateMultiplier?: number
  userRateMultiplier?: number | null
  showRate?: boolean
  daysRemaining?: number | null
  groupId?: number | null
  showPricingButton?: boolean
}

const props = withDefaults(defineProps<Props>(), {
  showRate: true,
  daysRemaining: null,
  userRateMultiplier: null,
  groupId: null,
  showPricingButton: false
})

const { t } = useI18n()

const hasCustomRate = computed(() => {
  return (
    props.userRateMultiplier !== null &&
    props.userRateMultiplier !== undefined &&
    props.rateMultiplier !== undefined &&
    props.userRateMultiplier !== props.rateMultiplier
  )
})

const showLabel = computed(() => {
  if (!props.showRate) return false
  if (props.daysRemaining !== null && props.daysRemaining !== undefined) return true
  return props.rateMultiplier !== undefined || hasCustomRate.value
})

const daysRemainingText = computed(() => {
  if (props.daysRemaining === null || props.daysRemaining === undefined) return ''
  if (props.daysRemaining <= 0) {
    return t('admin.users.expired')
  }
  return t('admin.users.daysRemaining', { days: props.daysRemaining })
})

const labelClass = computed(() => {
  const base = 'px-1.5 py-0.5 rounded text-[10px] font-semibold'

  if (props.daysRemaining !== null && props.daysRemaining !== undefined) {
    if (props.daysRemaining <= 0 || props.daysRemaining <= 3) {
      return `${base} bg-red-200/80 text-red-800 dark:bg-red-800/50 dark:text-red-300`
    }
    if (props.daysRemaining <= 7) {
      return `${base} bg-amber-200/80 text-amber-800 dark:bg-amber-800/50 dark:text-amber-300`
    }
    if (props.platform === 'anthropic') {
      return `${base} bg-orange-200/60 text-orange-800 dark:bg-orange-800/40 dark:text-orange-300`
    }
    if (props.platform === 'openai') {
      return `${base} bg-emerald-200/60 text-emerald-800 dark:bg-emerald-800/40 dark:text-emerald-300`
    }
    if (props.platform === 'gemini') {
      return `${base} bg-blue-200/60 text-blue-800 dark:bg-blue-800/40 dark:text-blue-300`
    }
    if (props.platform === 'sora') {
      return `${base} bg-rose-200/60 text-rose-800 dark:bg-rose-800/40 dark:text-rose-300`
    }
    return `${base} bg-violet-200/60 text-violet-800 dark:bg-violet-800/40 dark:text-violet-300`
  }

  return `${base} bg-black/10 dark:bg-white/10`
})

const badgeClass = computed(() => {
  if (props.platform === 'anthropic') {
    return 'bg-amber-50 text-amber-700 dark:bg-amber-900/20 dark:text-amber-400'
  } else if (props.platform === 'openai') {
    return 'bg-green-50 text-green-700 dark:bg-green-900/20 dark:text-green-400'
  }
  if (props.platform === 'gemini') {
    return 'bg-sky-50 text-sky-700 dark:bg-sky-900/20 dark:text-sky-400'
  }
  if (props.platform === 'sora') {
    return 'bg-rose-50 text-rose-700 dark:bg-rose-900/20 dark:text-rose-400'
  }
  return 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-400'
})
</script>
