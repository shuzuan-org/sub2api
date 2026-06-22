<template>
  <div v-if="showBindAction" class="card overflow-hidden border-amber-200 bg-gradient-to-r from-amber-50 to-amber-100/50 dark:border-amber-800 dark:from-amber-900/30 dark:to-amber-900/10">
    <div class="flex items-center justify-between gap-4 px-6 py-4">
      <div class="flex items-center gap-4">
        <div class="flex h-12 w-12 flex-shrink-0 items-center justify-center rounded-xl bg-amber-200 dark:bg-amber-700">
          <Icon name="gift" size="lg" class="text-amber-700 dark:text-amber-300" />
        </div>
        <div>
          <h3 class="text-base font-semibold text-gray-900 dark:text-white">
            {{ t('dashboard.phonePromo.title') }}
          </h3>
          <p class="text-sm text-gray-600 dark:text-gray-400">
            {{ t('dashboard.phonePromo.description') }}
          </p>
        </div>
      </div>
      <div class="flex flex-shrink-0 items-center gap-3">
        <button
          class="btn btn-primary"
          @click="router.push('/profile')"
        >
          {{ t('dashboard.phonePromo.action') }}
        </button>
        <a
          href="https://metacode.metask-ai.com/"
          target="_blank"
          rel="noopener noreferrer"
          class="btn btn-secondary"
        >
          <Icon name="download" size="sm" />
          {{ t('dashboard.phonePromo.downloadMetacode') }}
        </a>
      </div>
    </div>
  </div>
  <div v-else class="flex justify-end">
    <a
      href="https://metacode.metask-ai.com/"
      target="_blank"
      rel="noopener noreferrer"
      class="btn btn-secondary"
    >
      <Icon name="download" size="sm" />
      {{ t('dashboard.phonePromo.downloadMetacode') }}
    </a>
  </div>
</template>

<script setup lang="ts">
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'

withDefaults(defineProps<{
  showBindAction?: boolean
}>(), {
  showBindAction: true
})

const router = useRouter()
const { t } = useI18n()
</script>
