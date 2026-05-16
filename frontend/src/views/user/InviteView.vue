<template>
  <AppLayout>
    <div class="space-y-6">
      <!-- 顶部标题区 -->
      <div class="flex flex-col gap-4 lg:flex-row lg:items-start lg:justify-between">
        <div class="space-y-3">
          <span
            class="inline-flex items-center rounded-full bg-primary-50 px-3 py-1 text-xs font-medium text-primary-700 dark:bg-primary-900/20 dark:text-primary-300"
          >
            <!-- {{ t('invite.badge') }} -->
          </span>
        </div>
        <div class="flex flex-shrink-0 gap-3">
          <button class="btn btn-secondary" :disabled="!summary?.code" @click="copy(summary?.code)">
            {{ t('invite.copyCode') }}
          </button>
          <button class="btn btn-primary" :disabled="!inviteLink" @click="copy(inviteLink)">
            {{ t('invite.copyLink') }}
          </button>
        </div>
      </div>

      <!-- Loading -->
      <div v-if="loading" class="flex justify-center py-16">
        <div
          class="h-8 w-8 animate-spin rounded-full border-2 border-primary-500 border-t-transparent"
        ></div>
      </div>

      <template v-else>
        <div class="grid gap-6 lg:grid-cols-2">
          <!-- 邀请链接信息 -->
          <div class="card">
            <div
              class="flex items-center justify-between border-b border-gray-100 px-6 py-4 dark:border-dark-700"
            >
              <h2 class="text-base font-semibold text-gray-900 dark:text-white">
                {{ t('invite.linkSectionTitle') }}
              </h2>
              <span class="text-xs text-gray-400 dark:text-dark-500">
                {{ t('invite.codeFormatHint') }}
              </span>
            </div>
            <div class="space-y-4 p-6">
              <div class="grid gap-4 sm:grid-cols-2">
                <div class="rounded-xl border border-gray-200 p-4 dark:border-dark-600">
                  <p class="text-xs text-gray-500 dark:text-dark-400">
                    {{ t('invite.myCode') }}
                  </p>
                  <p
                    class="mt-2 font-mono text-2xl font-bold tracking-widest text-gray-900 dark:text-white"
                  >
                    {{ summary?.code || '------' }}
                  </p>
                </div>
                <div class="rounded-xl border border-gray-200 p-4 dark:border-dark-600">
                  <p class="text-xs text-gray-500 dark:text-dark-400">
                    {{ t('invite.myLink') }}
                  </p>
                  <p class="mt-2 break-all text-sm text-gray-700 dark:text-dark-300">
                    {{ inviteLink || t('invite.linkUnavailable') }}
                  </p>
                </div>
              </div>
              <div class="flex flex-wrap gap-2">
                <span
                  v-for="(tag, i) in [
                    t('invite.tagBound'),
                    t('invite.tagAutoAttribute'),
                    t('invite.tagCommissionLater')
                  ]"
                  :key="i"
                  class="inline-flex items-center rounded-lg bg-gray-100 px-3 py-1.5 text-xs text-gray-600 dark:bg-dark-700 dark:text-dark-300"
                >
                  {{ tag }}
                </span>
              </div>
            </div>
          </div>

          <!-- 邀请数据统计 -->
          <div class="card">
            <div
              class="flex items-center justify-between border-b border-gray-100 px-6 py-4 dark:border-dark-700"
            >
              <h2 class="text-base font-semibold text-gray-900 dark:text-white">
                {{ t('invite.statsSectionTitle') }}
              </h2>
              <span class="text-xs text-gray-400 dark:text-dark-500">
                {{ t('invite.statsPeriodHint') }}
              </span>
            </div>
            <div class="grid grid-cols-2 gap-4 p-6">
              <div class="rounded-xl border border-gray-200 p-4 dark:border-dark-600">
                <p class="text-xs text-gray-500 dark:text-dark-400">
                  {{ t('invite.statInvitedCount') }}
                </p>
                <p class="mt-2 text-2xl font-bold text-gray-900 dark:text-white">
                  {{ summary?.stats.invited_count ?? 0 }}
                </p>
              </div>
              <div class="rounded-xl border border-gray-200 p-4 dark:border-dark-600">
                <p class="text-xs text-gray-500 dark:text-dark-400">
                  {{ t('invite.statRechargedCount') }}
                </p>
                <p class="mt-2 text-2xl font-bold text-gray-400 dark:text-dark-500">
                  {{ t('invite.placeholderHint') }}
                </p>
              </div>
              <div
                class="rounded-xl border border-emerald-200 bg-emerald-50 p-4 dark:border-emerald-800/40 dark:bg-emerald-900/20"
              >
                <p class="text-xs text-emerald-700 dark:text-emerald-300">
                  {{ t('invite.statTotalCommission') }}
                </p>
                <p class="mt-2 text-2xl font-bold text-emerald-600 dark:text-emerald-400">
                  {{ t('invite.placeholderHint') }}
                </p>
              </div>
              <div
                class="rounded-xl border border-amber-200 bg-amber-50 p-4 dark:border-amber-800/40 dark:bg-amber-900/20"
              >
                <p class="text-xs text-amber-700 dark:text-amber-300">
                  {{ t('invite.statWithdrawable') }}
                </p>
                <p class="mt-2 text-2xl font-bold text-amber-600 dark:text-amber-400">
                  {{ t('invite.placeholderHint') }}
                </p>
              </div>
            </div>
          </div>
        </div>

        <!-- 邀请详情记录 -->
        <div class="card">
          <div
            class="flex flex-col gap-4 border-b border-gray-100 px-6 py-4 dark:border-dark-700 sm:flex-row sm:items-center sm:justify-between"
          >
            <div>
              <h2 class="text-base font-semibold text-gray-900 dark:text-white">
                {{ t('invite.recordsSectionTitle') }}
              </h2>
              <p class="mt-0.5 text-xs text-gray-500 dark:text-dark-400">
                {{ t('invite.recordsSortHint') }}
              </p>
            </div>
            <div class="flex gap-3">
              <input
                v-model="searchInput"
                type="text"
                :placeholder="t('invite.searchPlaceholder')"
                class="input w-64"
                @keyup.enter="applySearch"
              />
              <button class="btn btn-secondary" @click="applySearch">
                {{ t('invite.searchButton') }}
              </button>
            </div>
          </div>

          <div class="p-6">
            <div v-if="loadingRecords" class="flex justify-center py-10">
              <div
                class="h-6 w-6 animate-spin rounded-full border-2 border-primary-500 border-t-transparent"
              ></div>
            </div>

            <div v-else-if="records.length === 0" class="py-12 text-center">
              <p class="text-sm text-gray-500 dark:text-dark-400">
                {{ t('invite.empty') }}
              </p>
            </div>

            <div v-else class="overflow-x-auto">
              <table class="w-full text-left text-sm">
                <thead>
                  <tr class="border-b border-gray-100 text-xs text-gray-500 dark:border-dark-700 dark:text-dark-400">
                    <th class="pb-3 font-medium">{{ t('invite.colUser') }}</th>
                    <th class="pb-3 font-medium">{{ t('invite.colRegisteredAt') }}</th>
                    <th class="pb-3 font-medium">{{ t('invite.colTotalRecharge') }}</th>
                    <th class="pb-3 font-medium">{{ t('invite.colStatus') }}</th>
                  </tr>
                </thead>
                <tbody>
                  <tr
                    v-for="(r, i) in records"
                    :key="i"
                    class="border-b border-gray-50 dark:border-dark-800"
                  >
                    <td class="py-4">
                      <div class="flex items-center gap-3">
                        <div
                          class="flex h-9 w-9 items-center justify-center rounded-full bg-primary-100 text-sm font-medium uppercase text-primary-600 dark:bg-primary-900/30 dark:text-primary-300"
                        >
                          {{ (r.nickname || r.email).charAt(0) }}
                        </div>
                        <div>
                          <p class="font-medium text-gray-900 dark:text-white">{{ r.email }}</p>
                          <p v-if="r.nickname" class="text-xs text-gray-500 dark:text-dark-400">
                            {{ r.nickname }}
                          </p>
                        </div>
                      </div>
                    </td>
                    <td class="py-4 text-gray-600 dark:text-dark-300">{{ r.registered_at }}</td>
                    <td class="py-4 font-medium text-gray-900 dark:text-white">
                      ¥{{ r.total_recharge.toFixed(2) }}
                    </td>
                    <td class="py-4">
                      <span
                        class="inline-flex items-center rounded-md bg-blue-50 px-2 py-1 text-xs font-medium text-blue-600 dark:bg-blue-900/20 dark:text-blue-300"
                      >
                        {{ t('invite.statusRegistered') }}
                      </span>
                    </td>
                  </tr>
                </tbody>
              </table>

              <!-- 分页 -->
              <div
                v-if="totalPages > 1"
                class="mt-6 flex items-center justify-between text-sm text-gray-500 dark:text-dark-400"
              >
                <span>{{ t('invite.pageInfo', { page, totalPages, total }) }}</span>
                <div class="flex gap-2">
                  <button
                    class="btn btn-secondary"
                    :disabled="page <= 1"
                    @click="changePage(page - 1)"
                  >
                    {{ t('invite.prevPage') }}
                  </button>
                  <button
                    class="btn btn-secondary"
                    :disabled="page >= totalPages"
                    @click="changePage(page + 1)"
                  >
                    {{ t('invite.nextPage') }}
                  </button>
                </div>
              </div>
            </div>
          </div>
        </div>

        <!-- 数据备注 -->
        <div
          class="card border-gray-200 bg-gray-50 px-6 py-4 text-xs text-gray-500 dark:border-dark-700 dark:bg-dark-800 dark:text-dark-400"
        >
          {{ t('invite.dataNote') }}
        </div>
      </template>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores/app'
import { inviteAPI, type InviteSummary } from '@/api/invite'
import AppLayout from '@/components/layout/AppLayout.vue'

const { t } = useI18n()
const appStore = useAppStore()

const loading = ref(true)
const loadingRecords = ref(false)
const summary = ref<InviteSummary | null>(null)
const records = computed(() => summary.value?.records ?? [])
const total = computed(() => summary.value?.total ?? 0)
const page = ref(1)
const pageSize = ref(20)
const searchInput = ref('')
const search = ref('')

const totalPages = computed(() => Math.max(1, Math.ceil(total.value / pageSize.value)))

const inviteLink = computed(() => {
  if (summary.value?.link) return summary.value.link
  if (summary.value?.code) {
    return `${window.location.origin}/register?invite=${summary.value.code}`
  }
  return ''
})

async function load() {
  loadingRecords.value = true
  try {
    summary.value = await inviteAPI.getInviteSummary({
      page: page.value,
      page_size: pageSize.value,
      search: search.value || undefined
    })
  } catch (error) {
    console.error('Failed to load invite summary:', error)
    appStore.showError(t('invite.loadFailed'))
  } finally {
    loading.value = false
    loadingRecords.value = false
  }
}

function applySearch() {
  search.value = searchInput.value.trim()
  page.value = 1
  load()
}

function changePage(p: number) {
  if (p < 1 || p > totalPages.value) return
  page.value = p
  load()
}

async function copy(text?: string) {
  if (!text) return
  try {
    await navigator.clipboard.writeText(text)
    appStore.showSuccess(t('invite.copied'))
  } catch {
    appStore.showError(t('invite.copyFailed'))
  }
}

onMounted(load)
</script>
