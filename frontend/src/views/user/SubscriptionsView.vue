<template>
  <AppLayout>
    <div class="space-y-6">
      <!-- Loading State -->
      <div v-if="loading" class="flex justify-center py-12">
        <div
          class="h-8 w-8 animate-spin rounded-full border-2 border-primary-500 border-t-transparent"
        ></div>
      </div>

      <!-- Empty State -->
      <div v-else-if="subscriptions.length === 0 && availablePlans.length === 0" class="card p-12 text-center">
        <div
          class="mx-auto mb-4 flex h-16 w-16 items-center justify-center rounded-full bg-gray-100 dark:bg-dark-700"
        >
          <Icon name="creditCard" size="xl" class="text-gray-400" />
        </div>
        <h3 class="mb-2 text-lg font-semibold text-gray-900 dark:text-white">
          {{ t('userSubscriptions.noActiveSubscriptions') }}
        </h3>
        <p class="text-gray-500 dark:text-dark-400">
          {{ t('userSubscriptions.noActiveSubscriptionsDesc') }}
        </p>
      </div>

      <template v-else>
        <!-- 叠加限额总览卡片 -->
        <div v-if="merged && merged.has_active" class="card overflow-hidden">
          <div class="border-b border-gray-100 px-6 py-4 dark:border-dark-700">
            <h2 class="text-base font-semibold text-gray-900 dark:text-white">
              当前叠加限额
            </h2>
            <p class="mt-0.5 text-xs text-gray-500 dark:text-dark-400">
              {{ merged.active_count }} 个活跃订阅叠加后的总限额
            </p>
          </div>
          <div class="grid gap-6 p-6" :class="mergedLimitCount === 3 ? 'sm:grid-cols-2 lg:grid-cols-3' : mergedLimitCount === 2 ? 'sm:grid-cols-2' : ''">
            <!-- 无任何限额 -->
            <div
              v-if="!merged.daily_limit_usd && !merged.weekly_limit_usd && !merged.monthly_limit_usd"
              class="flex items-center justify-center rounded-xl bg-gradient-to-r from-emerald-50 to-teal-50 py-6 dark:from-emerald-900/20 dark:to-teal-900/20"
            >
              <div class="flex items-center gap-3">
                <span class="text-4xl text-emerald-600 dark:text-emerald-400">∞</span>
                <div>
                  <p class="text-sm font-medium text-emerald-700 dark:text-emerald-300">
                    {{ t('userSubscriptions.unlimited') }}
                  </p>
                  <p class="text-xs text-emerald-600/70 dark:text-emerald-400/70">
                    {{ t('userSubscriptions.unlimitedDesc') }}
                  </p>
                </div>
              </div>
            </div>

            <!-- 日限额 -->
            <div v-if="merged.daily_limit_usd" class="space-y-2">
              <div class="flex items-center justify-between">
                <span class="text-sm font-medium text-gray-700 dark:text-gray-300">每日限额</span>
                <span class="text-sm text-gray-500 dark:text-dark-400">
                  {{ merged.daily_used_usd.toFixed(2) }} / {{ merged.daily_limit_usd.toFixed(2) }} U
                </span>
              </div>
              <div class="relative h-2 overflow-hidden rounded-full bg-gray-200 dark:bg-dark-600">
                <div
                  class="absolute inset-y-0 left-0 rounded-full transition-all duration-300"
                  :class="getProgressBarClass(merged.daily_used_usd, merged.daily_limit_usd)"
                  :style="{ width: getProgressWidth(merged.daily_used_usd, merged.daily_limit_usd) }"
                ></div>
              </div>
              <p class="text-xs text-gray-400 dark:text-dark-500">
                剩余 {{ Math.max(0, merged.daily_limit_usd - merged.daily_used_usd).toFixed(2) }} U
              </p>
            </div>

            <!-- 周限额 -->
            <div v-if="merged.weekly_limit_usd" class="space-y-2">
              <div class="flex items-center justify-between">
                <span class="text-sm font-medium text-gray-700 dark:text-gray-300">每周限额</span>
                <span class="text-sm text-gray-500 dark:text-dark-400">
                  {{ merged.weekly_used_usd.toFixed(2) }} / {{ merged.weekly_limit_usd.toFixed(2) }} U
                </span>
              </div>
              <div class="relative h-2 overflow-hidden rounded-full bg-gray-200 dark:bg-dark-600">
                <div
                  class="absolute inset-y-0 left-0 rounded-full transition-all duration-300"
                  :class="getProgressBarClass(merged.weekly_used_usd, merged.weekly_limit_usd)"
                  :style="{ width: getProgressWidth(merged.weekly_used_usd, merged.weekly_limit_usd) }"
                ></div>
              </div>
              <p class="text-xs text-gray-400 dark:text-dark-500">
                剩余 {{ Math.max(0, merged.weekly_limit_usd - merged.weekly_used_usd).toFixed(2) }} U
              </p>
            </div>

            <!-- 月限额 -->
            <div v-if="merged.monthly_limit_usd" class="space-y-2">
              <div class="flex items-center justify-between">
                <span class="text-sm font-medium text-gray-700 dark:text-gray-300">每月限额</span>
                <span class="text-sm text-gray-500 dark:text-dark-400">
                  {{ merged.monthly_used_usd.toFixed(2) }} / {{ merged.monthly_limit_usd.toFixed(2) }} U
                </span>
              </div>
              <div class="relative h-2 overflow-hidden rounded-full bg-gray-200 dark:bg-dark-600">
                <div
                  class="absolute inset-y-0 left-0 rounded-full transition-all duration-300"
                  :class="getProgressBarClass(merged.monthly_used_usd, merged.monthly_limit_usd)"
                  :style="{ width: getProgressWidth(merged.monthly_used_usd, merged.monthly_limit_usd) }"
                ></div>
              </div>
              <p class="text-xs text-gray-400 dark:text-dark-500">
                剩余 {{ Math.max(0, merged.monthly_limit_usd - merged.monthly_used_usd).toFixed(2) }} U
              </p>
            </div>
          </div>
        </div>

        <!-- Available Plans for Purchase -->
        <div v-if="purchasablePlans.length > 0" class="space-y-3">
          <h2 class="text-lg font-semibold text-gray-900 dark:text-white">
            {{ t('userSubscriptions.availablePlans') }}
          </h2>
          <div class="grid gap-4 lg:grid-cols-3">
            <div
              v-for="plan in purchasablePlans"
              :key="'plan-' + plan.id"
              class="card p-4 transition-shadow hover:shadow-md"
            >
              <div class="mb-3">
                <h3 class="font-semibold text-gray-900 dark:text-white">{{ plan.name }}</h3>
                <p v-if="plan.description" class="mt-1 text-xs text-gray-500 dark:text-dark-400">
                  {{ plan.description }}
                </p>
              </div>
              <div class="mb-3 space-y-1 text-xs text-gray-600 dark:text-dark-300">
                <div v-if="plan.daily_limit_usd" class="flex justify-between">
                  <span>{{ t('userSubscriptions.dailyLimit') }}</span>
                  <span class="font-medium">{{ plan.daily_limit_usd.toFixed(2) }} U</span>
                </div>
                <div v-if="plan.weekly_limit_usd" class="flex justify-between">
                  <span>{{ t('userSubscriptions.weeklyLimit') }}</span>
                  <span class="font-medium">{{ plan.weekly_limit_usd.toFixed(2) }} U</span>
                </div>
                <div v-if="plan.monthly_limit_usd" class="flex justify-between">
                  <span>{{ t('userSubscriptions.monthlyLimit') }}</span>
                  <span class="font-medium">{{ plan.monthly_limit_usd.toFixed(2) }} U</span>
                </div>
                <div class="flex justify-between">
                  <span>{{ t('userSubscriptions.validity') }}</span>
                  <span class="font-medium">{{ plan.default_validity_days }} {{ t('userSubscriptions.days') }}</span>
                </div>
              </div>
              <div class="flex items-center justify-between">
                <span class="text-lg font-bold text-primary-600 dark:text-primary-400">
                  {{ plan.price?.toFixed(2) || '0.00' }} U
                </span>
                <button
                  class="btn btn-primary btn-sm"
                  :disabled="purchasing === plan.id"
                  @click="handlePurchase(plan)"
                >
                  <span v-if="purchasing === plan.id" class="flex items-center gap-1">
                    <span class="h-3 w-3 animate-spin rounded-full border border-white border-t-transparent"></span>
                    {{ t('common.processing') }}
                  </span>
                  <span v-else>{{ t('userSubscriptions.purchase') }}</span>
                </button>
              </div>
            </div>
          </div>
        </div>

        <!-- Subscriptions Grid -->
        <div v-if="subscriptions.length > 0">
          <h2 class="mb-3 text-lg font-semibold text-gray-900 dark:text-white">
            我的订阅
          </h2>
          <div class="grid gap-6 lg:grid-cols-2">
            <div
              v-for="subscription in subscriptions"
              :key="subscription.id"
              class="card overflow-hidden"
            >
              <!-- Header -->
              <div
                class="flex items-center justify-between border-b border-gray-100 p-4 dark:border-dark-700"
              >
                <div class="flex items-center gap-3">
                  <div
                    class="flex h-10 w-10 items-center justify-center rounded-xl bg-purple-100 dark:bg-purple-900/30"
                  >
                    <Icon name="creditCard" size="md" class="text-purple-600 dark:text-purple-400" />
                  </div>
                  <div>
                    <h3 class="font-semibold text-gray-900 dark:text-white">
                      {{ subscription.plan?.name || `Plan #${subscription.plan_id}` }}
                    </h3>
                    <p class="text-xs text-gray-500 dark:text-dark-400">
                      {{ subscription.plan?.description || '' }}
                    </p>
                  </div>
                </div>
                <span
                  :class="[
                    'badge',
                    subscription.status === 'active'
                      ? 'badge-success'
                      : subscription.status === 'expired'
                        ? 'badge-warning'
                        : 'badge-danger'
                  ]"
                >
                  {{ t(`userSubscriptions.status.${subscription.status}`) }}
                </span>
              </div>

              <!-- Usage Progress -->
              <div class="space-y-4 p-4">
                <!-- Expiration Info -->
                <div v-if="subscription.expires_at" class="flex items-center justify-between text-sm">
                  <span class="text-gray-500 dark:text-dark-400">{{
                    t('userSubscriptions.expires')
                  }}</span>
                  <span :class="getExpirationClass(subscription.expires_at)">
                    {{ formatExpirationDate(subscription.expires_at) }}
                  </span>
                </div>
                <div v-else class="flex items-center justify-between text-sm">
                  <span class="text-gray-500 dark:text-dark-400">{{
                    t('userSubscriptions.expires')
                  }}</span>
                  <span class="text-gray-700 dark:text-gray-300">{{
                    t('userSubscriptions.noExpiration')
                  }}</span>
                </div>

                <!-- Daily Usage -->
                <div v-if="subscription.plan?.daily_limit_usd" class="space-y-2">
                  <div class="flex items-center justify-between">
                    <span class="text-sm font-medium text-gray-700 dark:text-gray-300">
                      {{ t('userSubscriptions.daily') }}
                    </span>
                    <span class="text-sm text-gray-500 dark:text-dark-400">
                      {{ (subscription.daily_usage_usd || 0).toFixed(2) }} / {{
                        subscription.plan.daily_limit_usd.toFixed(2)
                      }} U
                    </span>
                  </div>
                  <div class="relative h-2 overflow-hidden rounded-full bg-gray-200 dark:bg-dark-600">
                    <div
                      class="absolute inset-y-0 left-0 rounded-full transition-all duration-300"
                      :class="
                        getProgressBarClass(
                          subscription.daily_usage_usd,
                          subscription.plan.daily_limit_usd
                        )
                      "
                      :style="{
                        width: getProgressWidth(
                          subscription.daily_usage_usd,
                          subscription.plan.daily_limit_usd
                        )
                      }"
                    ></div>
                  </div>
                  <p
                    v-if="subscription.daily_window_start"
                    class="text-xs text-gray-500 dark:text-dark-400"
                  >
                    {{
                      t('userSubscriptions.resetIn', {
                        time: formatResetTime(subscription.daily_window_start, 24)
                      })
                    }}
                  </p>
                </div>

                <!-- Weekly Usage -->
                <div v-if="subscription.plan?.weekly_limit_usd" class="space-y-2">
                  <div class="flex items-center justify-between">
                    <span class="text-sm font-medium text-gray-700 dark:text-gray-300">
                      {{ t('userSubscriptions.weekly') }}
                    </span>
                    <span class="text-sm text-gray-500 dark:text-dark-400">
                      {{ (subscription.weekly_usage_usd || 0).toFixed(2) }} / {{
                        subscription.plan.weekly_limit_usd.toFixed(2)
                      }} U
                    </span>
                  </div>
                  <div class="relative h-2 overflow-hidden rounded-full bg-gray-200 dark:bg-dark-600">
                    <div
                      class="absolute inset-y-0 left-0 rounded-full transition-all duration-300"
                      :class="
                        getProgressBarClass(
                          subscription.weekly_usage_usd,
                          subscription.plan.weekly_limit_usd
                        )
                      "
                      :style="{
                        width: getProgressWidth(
                          subscription.weekly_usage_usd,
                          subscription.plan.weekly_limit_usd
                        )
                      }"
                    ></div>
                  </div>
                  <p
                    v-if="subscription.weekly_window_start"
                    class="text-xs text-gray-500 dark:text-dark-400"
                  >
                    {{
                      t('userSubscriptions.resetIn', {
                        time: formatResetTime(subscription.weekly_window_start, 168)
                      })
                    }}
                  </p>
                </div>

                <!-- Monthly Usage -->
                <div v-if="subscription.plan?.monthly_limit_usd" class="space-y-2">
                  <div class="flex items-center justify-between">
                    <span class="text-sm font-medium text-gray-700 dark:text-gray-300">
                      {{ t('userSubscriptions.monthly') }}
                    </span>
                    <span class="text-sm text-gray-500 dark:text-dark-400">
                      {{ (subscription.monthly_usage_usd || 0).toFixed(2) }} / {{
                        subscription.plan.monthly_limit_usd.toFixed(2)
                      }} U
                    </span>
                  </div>
                  <div class="relative h-2 overflow-hidden rounded-full bg-gray-200 dark:bg-dark-600">
                    <div
                      class="absolute inset-y-0 left-0 rounded-full transition-all duration-300"
                      :class="
                        getProgressBarClass(
                          subscription.monthly_usage_usd,
                          subscription.plan.monthly_limit_usd
                        )
                      "
                      :style="{
                        width: getProgressWidth(
                          subscription.monthly_usage_usd,
                          subscription.plan.monthly_limit_usd
                        )
                      }"
                    ></div>
                  </div>
                  <p
                    v-if="subscription.monthly_window_start"
                    class="text-xs text-gray-500 dark:text-dark-400"
                  >
                    {{
                      t('userSubscriptions.resetIn', {
                        time: formatResetTime(subscription.monthly_window_start, 720)
                      })
                    }}
                  </p>
                </div>

                <!-- No limits configured - Unlimited badge -->
                <div
                  v-if="
                    !subscription.plan?.daily_limit_usd &&
                    !subscription.plan?.weekly_limit_usd &&
                    !subscription.plan?.monthly_limit_usd
                  "
                  class="flex items-center justify-center rounded-xl bg-gradient-to-r from-emerald-50 to-teal-50 py-6 dark:from-emerald-900/20 dark:to-teal-900/20"
                >
                  <div class="flex items-center gap-3">
                    <span class="text-4xl text-emerald-600 dark:text-emerald-400">∞</span>
                    <div>
                      <p class="text-sm font-medium text-emerald-700 dark:text-emerald-300">
                        {{ t('userSubscriptions.unlimited') }}
                      </p>
                      <p class="text-xs text-emerald-600/70 dark:text-emerald-400/70">
                        {{ t('userSubscriptions.unlimitedDesc') }}
                      </p>
                    </div>
                  </div>
                </div>
              </div>
            </div>
          </div>
        </div>
      </template>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores/app'
import { useAuthStore } from '@/stores/auth'
import subscriptionsAPI, { type MergedSubscriptionState } from '@/api/subscriptions'
import { apiClient } from '@/api/client'
import type { UserSubscription, SubscriptionPlan } from '@/types'
import AppLayout from '@/components/layout/AppLayout.vue'
import Icon from '@/components/icons/Icon.vue'
import { formatDateOnly } from '@/utils/format'

const { t } = useI18n()
const router = useRouter()
const appStore = useAppStore()
const authStore = useAuthStore()

const subscriptions = ref<UserSubscription[]>([])
const availablePlans = ref<SubscriptionPlan[]>([])
const merged = ref<MergedSubscriptionState | null>(null)
const loading = ref(true)
const purchasing = ref<number | null>(null)

// 所有有价格的 public 计划都可购买（支持重复购买叠加）
const purchasablePlans = computed(() =>
  availablePlans.value.filter((p) => p.price != null && p.price > 0)
)

// 叠加限额中有几个维度
const mergedLimitCount = computed(() => {
  if (!merged.value) return 0
  let count = 0
  if (merged.value.daily_limit_usd) count++
  if (merged.value.weekly_limit_usd) count++
  if (merged.value.monthly_limit_usd) count++
  return count
})

async function loadSubscriptions() {
  try {
    loading.value = true
    const [subs, plans, mergedState] = await Promise.all([
      subscriptionsAPI.getMySubscriptions(),
      apiClient.get<SubscriptionPlan[]>('/subscription-plans').then((r) => r.data),
      subscriptionsAPI.getMergedSubscription().catch(() => null)
    ])
    subscriptions.value = subs
    availablePlans.value = plans
    merged.value = mergedState
  } catch (error) {
    console.error('Failed to load subscriptions:', error)
    appStore.showError(t('userSubscriptions.failedToLoad'))
  } finally {
    loading.value = false
  }
}

async function handlePurchase(plan: SubscriptionPlan) {
  if (purchasing.value) return

  const price = plan.price || 0
  let balance = authStore.user?.balance || 0
  try {
    const latestUser = await authStore.refreshUser()
    balance = latestUser.balance
  } catch {
    // 使用本地缓存余额继续判断；真正扣费仍以后端事务校验为准。
  }

  if (balance < price) {
    appStore.showWarning(t('userSubscriptions.insufficientBalanceRecharge'))
    await router.push({ name: 'Payment', query: { tab: 'recharge' } })
    return
  }

  const confirmed = window.confirm(
    t('userSubscriptions.purchaseConfirm', {
      name: plan.name,
      price: price.toFixed(2),
      days: plan.default_validity_days
    })
  )
  if (!confirmed) return

  try {
    purchasing.value = plan.id
    await subscriptionsAPI.purchaseSubscription(plan.id)
    await authStore.refreshUser()
    appStore.showSuccess(t('userSubscriptions.purchaseSuccess', { name: plan.name }))
    await loadSubscriptions()
  } catch (error: any) {
    if (error?.code === 'INSUFFICIENT_BALANCE') {
      appStore.showWarning(t('userSubscriptions.insufficientBalanceRecharge'))
      await router.push({ name: 'Payment', query: { tab: 'recharge' } })
      return
    }

    const msg = error?.response?.data?.message || error?.message || t('userSubscriptions.purchaseFailed')
    appStore.showError(msg)
  } finally {
    purchasing.value = null
  }
}

function getProgressWidth(used: number | undefined, limit: number | null | undefined): string {
  if (!limit || limit === 0) return '0%'
  const percentage = Math.min(((used || 0) / limit) * 100, 100)
  return `${percentage}%`
}

function getProgressBarClass(used: number | undefined, limit: number | null | undefined): string {
  if (!limit || limit === 0) return 'bg-gray-400'
  const percentage = ((used || 0) / limit) * 100
  if (percentage >= 90) return 'bg-red-500'
  if (percentage >= 70) return 'bg-orange-500'
  return 'bg-green-500'
}

function formatExpirationDate(expiresAt: string): string {
  const now = new Date()
  const expires = new Date(expiresAt)
  const diff = expires.getTime() - now.getTime()
  const days = Math.ceil(diff / (1000 * 60 * 60 * 24))

  if (days < 0) {
    return t('userSubscriptions.status.expired')
  }

  const dateStr = formatDateOnly(expires)

  if (days === 0) {
    return `${dateStr} (Today)`
  }
  if (days === 1) {
    return `${dateStr} (Tomorrow)`
  }

  return t('userSubscriptions.daysRemaining', { days }) + ` (${dateStr})`
}

function getExpirationClass(expiresAt: string): string {
  const now = new Date()
  const expires = new Date(expiresAt)
  const diff = expires.getTime() - now.getTime()
  const days = Math.ceil(diff / (1000 * 60 * 60 * 24))

  if (days <= 0) return 'text-red-600 dark:text-red-400 font-medium'
  if (days <= 3) return 'text-red-600 dark:text-red-400'
  if (days <= 7) return 'text-orange-600 dark:text-orange-400'
  return 'text-gray-700 dark:text-gray-300'
}

function formatResetTime(windowStart: string | null, windowHours: number): string {
  if (!windowStart) return t('userSubscriptions.windowNotActive')

  const start = new Date(windowStart)
  const end = new Date(start.getTime() + windowHours * 60 * 60 * 1000)
  const now = new Date()
  const diff = end.getTime() - now.getTime()

  if (diff <= 0) return t('userSubscriptions.windowNotActive')

  const hours = Math.floor(diff / (1000 * 60 * 60))
  const minutes = Math.floor((diff % (1000 * 60 * 60)) / (1000 * 60))

  if (hours > 24) {
    const days = Math.floor(hours / 24)
    const remainingHours = hours % 24
    return `${days}d ${remainingHours}h`
  }

  if (hours > 0) {
    return `${hours}h ${minutes}m`
  }

  return `${minutes}m`
}

onMounted(() => {
  loadSubscriptions()
})
</script>
