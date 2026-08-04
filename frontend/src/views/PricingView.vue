<template>
  <div class="relative flex min-h-screen flex-col bg-gray-50 dark:bg-dark-950">
    <!-- Header -->
    <header class="relative z-20 px-6 py-4">
      <nav class="mx-auto flex max-w-6xl items-center justify-between">
        <router-link to="/home" class="flex items-center gap-3">
          <div class="h-10 w-10 overflow-hidden rounded-xl shadow-md">
            <img :src="siteLogo || '/logo.svg'" alt="Logo" class="h-full w-full object-contain" />
          </div>
          <span class="text-lg font-semibold tracking-tight text-gray-900 dark:text-white">{{ siteName }}</span>
        </router-link>
        <div class="flex items-center gap-3">
          <LocaleSwitcher />
          <a
            v-if="docUrl"
            :href="docUrl"
            target="_blank"
            rel="noopener noreferrer"
            class="rounded-lg p-2 text-gray-500 transition-colors hover:bg-gray-100 hover:text-gray-700 dark:text-dark-400 dark:hover:bg-dark-800 dark:hover:text-white"
          >
            <Icon name="book" size="md" />
          </a>
          <button
            @click="toggleTheme"
            class="rounded-lg p-2 text-gray-500 transition-colors hover:bg-gray-100 hover:text-gray-700 dark:text-dark-400 dark:hover:bg-dark-800 dark:hover:text-white"
          >
            <Icon v-if="isDark" name="sun" size="md" />
            <Icon v-else name="moon" size="md" />
          </button>
        </div>
      </nav>
    </header>

    <!-- Main -->
    <main class="mx-auto w-full max-w-6xl flex-1 px-6 py-12">
      <!-- Hero -->
      <div class="mb-10 text-center">
        <h1 class="mb-3 font-serif text-3xl font-bold tracking-tight text-gray-900 dark:text-white sm:text-4xl">
          {{ t('pricing.title') }}
        </h1>
        <p class="mx-auto max-w-lg text-base text-gray-500 dark:text-dark-400">
          {{ t('pricing.subtitle') }}
        </p>
      </div>

      <!-- Search -->
      <div class="mx-auto mb-8 max-w-md">
        <div class="relative">
          <div class="pointer-events-none absolute left-4 top-1/2 -translate-y-1/2 text-gray-400 dark:text-dark-500">
            <svg class="h-5 w-5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
              <path stroke-linecap="round" stroke-linejoin="round" d="m21 21-5.197-5.197m0 0A7.5 7.5 0 1 0 5.196 5.196a7.5 7.5 0 0 0 10.607 10.607z" />
            </svg>
          </div>
          <input
            v-model="searchQuery"
            type="text"
            :placeholder="t('pricing.searchPlaceholder')"
            class="input pl-12"
          />
        </div>
      </div>

      <!-- Loading -->
      <div v-if="loading" class="space-y-6">
        <div v-for="i in 3" :key="i" class="card p-6">
          <div class="mb-4 flex items-center gap-3">
            <div class="skeleton h-5 w-5 rounded"></div>
            <div class="skeleton h-6 w-48 rounded"></div>
          </div>
          <div class="space-y-3">
            <div v-for="j in 5" :key="j" class="skeleton h-10 w-full rounded"></div>
          </div>
        </div>
      </div>

      <!-- Error -->
      <div v-else-if="error" class="card mx-auto max-w-md p-8 text-center">
        <div class="mb-3 text-4xl">&#x26A0;</div>
        <p class="text-sm text-gray-600 dark:text-dark-400">{{ error }}</p>
        <button @click="fetchData" class="btn btn-primary mt-4">{{ t('pricing.retry') }}</button>
      </div>

      <!-- Empty -->
      <div v-else-if="filteredGroups.length === 0 && !searchQuery" class="card mx-auto max-w-md p-8 text-center">
        <p class="text-sm text-gray-500 dark:text-dark-400">{{ t('pricing.noData') }}</p>
      </div>

      <!-- No search results -->
      <div v-else-if="filteredGroups.length === 0 && searchQuery" class="card mx-auto max-w-md p-8 text-center">
        <p class="text-sm text-gray-500 dark:text-dark-400">{{ t('pricing.noResults') }}</p>
      </div>

      <!-- Group cards -->
      <div v-else class="space-y-8">
        <div v-for="group in filteredGroups" :key="group.group_name" class="card overflow-hidden">
          <!-- Group header -->
          <div class="card-header flex items-center justify-between">
            <div class="flex items-center gap-3">
              <PlatformIcon :platform="group.platform as any" size="md" />
              <h2 class="text-lg font-semibold text-gray-900 dark:text-white">{{ group.group_name }}</h2>
              <span class="badge badge-gray text-xs">{{ group.platform }}</span>
            </div>
            <div class="flex items-center gap-2">
              <span v-if="group.rate_multiplier < 1" class="badge bg-emerald-100 text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-400">
                {{ Math.round((1 - group.rate_multiplier) * 100) }}% OFF
              </span>
              <span class="text-xs text-gray-500 dark:text-dark-400">
                {{ group.filteredModels.length }} {{ t('pricing.models') }}
              </span>
            </div>
          </div>

          <!-- Model table -->
          <div class="table-wrapper">
            <table class="table w-full">
              <thead>
                <tr>
                  <th class="min-w-[200px]">{{ t('pricing.modelName') }}</th>
                  <th v-for="col in priceColumns" :key="col.key" class="whitespace-nowrap text-right">
                    {{ t(`pricing.${col.key}`) }}
                  </th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="model in group.filteredModels" :key="model.model">
                  <td>
                    <span class="font-mono text-sm text-gray-900 dark:text-white">{{ model.model }}</span>
                  </td>
                  <td v-for="col in priceColumns" :key="col.key" class="text-right">
                    <template v-if="!col.optional || col.value(model) > 0">
                      <div class="text-sm font-medium text-gray-900 dark:text-white">
                        {{ formatU(col.value(model)) }} U
                      </div>
                      <div class="text-xs text-gray-500 dark:text-dark-400">
                        {{ formatUsdFromU(col.value(model)) }}/MTok
                      </div>
                      <div v-if="model.discount_percent > 0" class="text-xs text-gray-400 line-through dark:text-dark-500">
                        {{ formatU(col.original(model)) }} U
                      </div>
                      <div v-if="col.note && col.note(model) > 0" class="text-xs text-gray-500 dark:text-dark-400">
                        {{ t('pricing.cacheCreate1h') }} {{ formatU(col.note(model)) }} U
                      </div>
                    </template>
                    <span v-else class="text-xs text-gray-400 dark:text-dark-500">
                      {{ t(hasCachePricing(model) ? 'pricing.cacheNotCharged' : 'pricing.noCachePrice') }}
                    </span>
                  </td>
                </tr>
              </tbody>
            </table>
          </div>
        </div>
      </div>

      <!-- Updated time -->
      <div v-if="updatedAt" class="mt-8 text-center text-xs text-gray-400 dark:text-dark-500">
        {{ t('pricing.lastUpdated') }}: {{ formatTime(updatedAt) }}
      </div>
    </main>

    <!-- Footer -->
    <footer class="px-6 py-6 text-center text-xs text-gray-400 dark:text-dark-500">
      &copy; {{ new Date().getFullYear() }} {{ siteName }}
    </footer>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores'
import {
  getPublicModelPricing,
  type PublicGroupPricing,
  type PublicModelPricing,
  type PublicPricingResponse,
} from '@/api/pricing'
import { formatUsdFromU } from '@/utils/format'
import LocaleSwitcher from '@/components/common/LocaleSwitcher.vue'
import PlatformIcon from '@/components/common/PlatformIcon.vue'
import Icon from '@/components/icons/Icon.vue'

const { t } = useI18n()
const appStore = useAppStore()

const siteName = computed(() => appStore.siteName || 'Sub2API')
const siteLogo = computed(() => appStore.siteLogo)
const docUrl = computed(() => appStore.docUrl)

const isDark = ref(document.documentElement.classList.contains('dark'))
function toggleTheme() {
  isDark.value = !isDark.value
  document.documentElement.classList.toggle('dark', isDark.value)
  localStorage.setItem('theme', isDark.value ? 'dark' : 'light')
}

const loading = ref(true)
const error = ref('')
const pricingData = ref<PublicPricingResponse | null>(null)
const searchQuery = ref('')
const updatedAt = ref('')

interface FilteredGroup extends PublicGroupPricing {
  filteredModels: PublicGroupPricing['models']
}

interface PriceColumn {
  /** i18n key under `pricing.` used as the column header. */
  key: string
  value: (m: PublicModelPricing) => number
  original: (m: PublicModelPricing) => number
  /** Columns whose price may legitimately be absent — rendered as "not supported", not 0 U. */
  optional?: boolean
  /** A qualified rate shown under the main price, e.g. the surcharged 1h cache write. */
  note?: (m: PublicModelPricing) => number
}

const priceColumns: PriceColumn[] = [
  {
    key: 'inputPrice',
    value: (m) => m.input_per_mtok_u,
    original: (m) => m.original_input_per_mtok_u,
  },
  {
    key: 'outputPrice',
    value: (m) => m.output_per_mtok_u,
    original: (m) => m.original_output_per_mtok_u,
  },
  {
    key: 'cacheCreatePrice',
    value: (m) => m.cache_create_per_mtok_u,
    original: (m) => m.original_cache_create_per_mtok_u,
    optional: true,
    note: (m) => m.cache_create_1h_per_mtok_u,
  },
  {
    key: 'cacheReadPrice',
    value: (m) => m.cache_read_per_mtok_u,
    original: (m) => m.original_cache_read_per_mtok_u,
    optional: true,
  },
]

const filteredGroups = computed<FilteredGroup[]>(() => {
  if (!pricingData.value?.groups) return []
  const q = searchQuery.value.toLowerCase().trim()
  return pricingData.value.groups
    .map((group) => {
      const filtered = q
        ? group.models.filter((m) => m.model.toLowerCase().includes(q))
        : group.models
      return { ...group, filteredModels: filtered }
    })
    .filter((g) => g.filteredModels.length > 0)
})

// A zero in one cache column means two different things. If the model is priced for the
// other kind of cache token, caching works and this half simply isn't billed; if neither
// is priced, the model has no prompt caching at all. Saying "not supported" for the
// former would contradict the price sitting in the next column.
function hasCachePricing(m: PublicModelPricing): boolean {
  return m.cache_create_per_mtok_u > 0 || m.cache_read_per_mtok_u > 0
}

function formatU(value: number): string {
  if (value >= 100) return value.toFixed(1)
  if (value >= 1) return value.toFixed(2)
  return value.toFixed(4)
}

function formatTime(iso: string): string {
  try {
    return new Date(iso).toLocaleString()
  } catch {
    return iso
  }
}

async function fetchData() {
  loading.value = true
  error.value = ''
  try {
    pricingData.value = await getPublicModelPricing()
    updatedAt.value = pricingData.value.updated_at
  } catch (e: any) {
    error.value = e?.message || 'Failed to load pricing data'
  } finally {
    loading.value = false
  }
}

onMounted(() => {
  appStore.fetchPublicSettings()
  // Init theme
  const saved = localStorage.getItem('theme')
  if (saved === 'dark' || (!saved && window.matchMedia('(prefers-color-scheme: dark)').matches)) {
    isDark.value = true
    document.documentElement.classList.add('dark')
  }
  fetchData()
})
</script>
