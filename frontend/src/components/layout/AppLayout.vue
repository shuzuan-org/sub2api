<template>
  <div class="min-h-screen bg-gray-50 dark:bg-dark-950">
    <!-- Background Decoration -->
    <div class="pointer-events-none fixed inset-0 bg-mesh-gradient"></div>

    <!-- Sidebar -->
    <AppSidebar />

    <!-- Main Content Area -->
    <div
      class="relative min-h-screen transition-all duration-300"
      :class="[sidebarCollapsed ? 'lg:ml-[72px]' : 'lg:ml-64']"
    >
      <!-- Header -->
      <AppHeader />

      <!-- 迁移到 metask-ai：开放期间每个页面顶部都提示 -->
      <div
        v-if="showMigrationBanner"
        class="mx-4 mt-4 flex flex-wrap items-center justify-between gap-3 rounded-lg border border-amber-200 bg-amber-50 px-4 py-3 text-sm text-amber-900 dark:border-amber-900/50 dark:bg-amber-900/20 dark:text-amber-100 md:mx-6 lg:mx-8"
        data-testid="migration-banner"
      >
        <span>
          本站即将停止服务，本站已停止充值与兑换。请把账号、余额和 API Key 迁移到 metask-ai<template v-if="migrationStatus?.deadline">（{{ migrationStatus.deadline }} 之后本站停止 API 调用）</template>。
        </span>
        <router-link to="/migrate" class="btn btn-primary btn-sm">去迁移</router-link>
      </div>

      <!-- Main Content -->
      <main class="p-4 md:p-6 lg:p-8">
        <slot />
      </main>
    </div>
  </div>
</template>

<script setup lang="ts">
import '@/styles/onboarding.css'
import { computed, onMounted } from 'vue'
import { useRoute } from 'vue-router'
import { useMigrationStatus } from '@/composables/useMigrationStatus'
import { useAppStore } from '@/stores'
import { useAuthStore } from '@/stores/auth'
import { useOnboardingTour } from '@/composables/useOnboardingTour'
import { useOnboardingStore } from '@/stores/onboarding'
import AppSidebar from './AppSidebar.vue'
import AppHeader from './AppHeader.vue'

const appStore = useAppStore()
const authStore = useAuthStore()
const sidebarCollapsed = computed(() => appStore.sidebarCollapsed)
const isAdmin = computed(() => authStore.user?.role === 'admin')

const { replayTour } = useOnboardingTour({
  storageKey: isAdmin.value ? 'admin_guide' : 'user_guide',
  autoStart: true
})

const onboardingStore = useOnboardingStore()

const route = useRoute()
const { status: migrationStatus, load: loadMigrationStatus } = useMigrationStatus()
const showMigrationBanner = computed(
  () =>
    !!migrationStatus.value?.enabled &&
    migrationStatus.value.status !== 'completed' &&
    (migrationStatus.value.eligible || migrationStatus.value.status === 'frozen') &&
    route.path !== '/migrate'
)

onMounted(() => {
  onboardingStore.setReplayCallback(replayTour)
  void loadMigrationStatus()
})

defineExpose({ replayTour })
</script>
