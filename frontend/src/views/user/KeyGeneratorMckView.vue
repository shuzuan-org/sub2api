<template>
  <div class="flex min-h-screen flex-col items-center justify-center bg-gray-50 px-4 py-12 dark:bg-dark-900">
    <!-- Site name header -->
    <div class="mb-8 text-center">
      <h1 class="text-2xl font-semibold text-gray-900 dark:text-white">
        {{ siteName }}
      </h1>
    </div>

    <!-- Card -->
    <div class="w-full max-w-xl rounded-2xl bg-white p-8 shadow-sm ring-1 ring-gray-100 dark:bg-dark-800 dark:ring-dark-700">
      <div class="mb-6 flex items-center gap-3">
        <div class="flex h-10 w-10 items-center justify-center rounded-xl bg-primary-50 text-primary-600 dark:bg-primary-900/30 dark:text-primary-400">
          <Icon name="key" size="md" />
        </div>
        <div>
          <h2 class="text-lg font-semibold text-gray-900 dark:text-white">你的 API Key</h2>
          <p class="text-sm text-gray-500 dark:text-dark-400">Your API Key</p>
        </div>
      </div>

      <!-- Loading -->
      <div v-if="loading" class="flex items-center justify-center py-12">
        <svg class="h-6 w-6 animate-spin text-gray-400" fill="none" viewBox="0 0 24 24">
          <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
          <path
            class="opacity-75"
            fill="currentColor"
            d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"
          ></path>
        </svg>
      </div>

      <!-- Error -->
      <div v-else-if="errorMessage" class="rounded-xl border border-red-200 bg-red-50 p-4 text-sm text-red-700 dark:border-red-900/50 dark:bg-red-900/20 dark:text-red-400">
        {{ errorMessage }}
        <button class="ml-2 underline" @click="loadKey">重试</button>
      </div>

      <!-- Empty state -->
      <div v-else-if="!apiKey" class="space-y-4">
        <div class="rounded-xl border border-dashed border-gray-300 bg-gray-50 p-6 text-center dark:border-dark-600 dark:bg-dark-900/50">
          <p class="text-sm text-gray-700 dark:text-dark-300">
            你还没有 API Key
          </p>
          <p class="mt-1 text-xs text-gray-500 dark:text-dark-400">
            前往 API Keys 页面创建一个新的 key
          </p>
        </div>
        <button class="btn btn-primary w-full" @click="goToKeys">
          去创建 API Key
        </button>
      </div>

      <!-- Key display -->
      <div v-else class="space-y-3">
        <div class="flex items-center gap-2 rounded-xl bg-gray-50 px-4 py-3 dark:bg-dark-900/50">
          <code class="flex-1 break-all font-mono text-sm text-gray-900 dark:text-gray-100">{{ displayValue }}</code>
          <button
            @click="showFull = !showFull"
            class="flex h-8 w-8 flex-shrink-0 items-center justify-center rounded-lg text-gray-500 transition-colors hover:bg-gray-200 hover:text-gray-900 dark:hover:bg-dark-700 dark:hover:text-white"
            :title="showFull ? '隐藏' : '显示'"
          >
            <Icon :name="showFull ? 'eyeOff' : 'eye'" size="sm" />
          </button>
          <button
            @click="copy"
            class="flex h-8 w-8 flex-shrink-0 items-center justify-center rounded-lg transition-colors"
            :class="justCopied
              ? 'bg-green-100 text-green-600 dark:bg-green-900/30 dark:text-green-400'
              : 'text-gray-500 hover:bg-gray-200 hover:text-gray-900 dark:hover:bg-dark-700 dark:hover:text-white'"
            :title="justCopied ? '已复制' : '复制'"
          >
            <Icon :name="justCopied ? 'check' : 'clipboard'" size="sm" />
          </button>
        </div>

        <p class="text-xs text-gray-500 dark:text-dark-400">
          如有多个 key，请前往
          <a class="text-primary-600 hover:underline dark:text-primary-400" href="/keys">API Keys 管理页</a>
          查看与管理。
        </p>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { useAppStore } from '@/stores/app'
import { useClipboard } from '@/composables/useClipboard'
import { keysAPI } from '@/api'
import Icon from '@/components/icons/Icon.vue'
import type { ApiKey } from '@/types'

const router = useRouter()
const appStore = useAppStore()
const { copyToClipboard } = useClipboard()

const loading = ref(true)
const apiKey = ref<ApiKey | null>(null)
const errorMessage = ref('')
const showFull = ref(false)
const justCopied = ref(false)

const siteName = computed(() => appStore.siteName || 'Sub2API')

const displayValue = computed(() => {
  if (!apiKey.value) return ''
  const k = apiKey.value.key
  if (showFull.value) return k
  if (k.length <= 12) return k
  return `${k.slice(0, 8)}${'•'.repeat(12)}${k.slice(-4)}`
})

async function loadKey() {
  loading.value = true
  errorMessage.value = ''
  try {
    const res = await keysAPI.list(1, 1)
    apiKey.value = res.items[0] ?? null
  } catch (e) {
    errorMessage.value = '加载 API Key 失败'
  } finally {
    loading.value = false
  }
}

async function copy() {
  if (!apiKey.value) return
  const ok = await copyToClipboard(apiKey.value.key, '已复制到剪贴板')
  if (ok) {
    justCopied.value = true
    setTimeout(() => { justCopied.value = false }, 1500)
  }
}

function goToKeys() {
  router.push('/keys')
}

onMounted(loadKey)
</script>
