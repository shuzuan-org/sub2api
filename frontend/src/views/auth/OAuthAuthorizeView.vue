<template>
  <AuthLayout>
    <div class="space-y-6">
      <div class="text-center">
        <div
          class="mx-auto mb-4 flex h-14 w-14 items-center justify-center rounded-2xl bg-primary-100 text-primary-600 dark:bg-primary-900/30 dark:text-primary-300"
        >
          <Icon name="shield" size="lg" />
        </div>
        <h2 class="text-2xl font-bold text-gray-900 dark:text-white">确认授权</h2>
        <p class="mt-2 text-sm text-gray-500 dark:text-dark-400">
          外部应用正在请求访问你的账号
        </p>
      </div>

      <div v-if="isLoading" class="space-y-3 rounded-xl bg-gray-50 p-4 dark:bg-dark-800">
        <div class="h-4 w-2/3 animate-pulse rounded bg-gray-200 dark:bg-dark-700"></div>
        <div class="h-4 w-full animate-pulse rounded bg-gray-200 dark:bg-dark-700"></div>
        <div class="h-4 w-1/2 animate-pulse rounded bg-gray-200 dark:bg-dark-700"></div>
      </div>

      <div
        v-else-if="errorMessage"
        class="rounded-xl border border-red-200 bg-red-50 p-4 dark:border-red-800/50 dark:bg-red-900/20"
      >
        <div class="flex items-start gap-3">
          <Icon name="exclamationCircle" size="md" class="mt-0.5 flex-shrink-0 text-red-500" />
          <div class="space-y-3">
            <p class="text-sm text-red-700 dark:text-red-400">{{ errorMessage }}</p>
            <router-link to="/dashboard" class="btn btn-secondary">返回控制台</router-link>
          </div>
        </div>
      </div>

      <template v-else-if="preview">
        <div class="rounded-xl border border-gray-200 bg-white p-4 dark:border-dark-700 dark:bg-dark-800">
          <p class="text-sm text-gray-500 dark:text-dark-400">应用</p>
          <p class="mt-1 text-lg font-semibold text-gray-900 dark:text-white">
            {{ preview.client_name }}
          </p>
          <p class="mt-1 break-all font-mono text-xs text-gray-500 dark:text-dark-400">
            {{ preview.client_id }}
          </p>
        </div>

        <div class="space-y-3 rounded-xl border border-gray-200 p-4 dark:border-dark-700">
          <div class="flex items-start gap-3">
            <Icon name="user" size="md" class="mt-0.5 flex-shrink-0 text-gray-400" />
            <div>
              <p class="text-sm font-medium text-gray-900 dark:text-white">授权账号</p>
              <p class="text-sm text-gray-500 dark:text-dark-400">
                {{ currentUserLabel }}
              </p>
            </div>
          </div>

          <div class="flex items-start gap-3">
            <Icon name="externalLink" size="md" class="mt-0.5 flex-shrink-0 text-gray-400" />
            <div class="min-w-0">
              <p class="text-sm font-medium text-gray-900 dark:text-white">回调地址</p>
              <p class="break-all text-sm text-gray-500 dark:text-dark-400">
                {{ preview.redirect_uri }}
              </p>
            </div>
          </div>
        </div>

        <div class="rounded-xl border border-amber-200 bg-amber-50 p-4 dark:border-amber-800/60 dark:bg-amber-900/20">
          <div class="flex gap-3">
            <Icon name="exclamationTriangle" size="md" class="mt-0.5 flex-shrink-0 text-amber-600" />
            <div>
              <p class="text-sm font-medium text-amber-900 dark:text-amber-200">请确认你信任该应用</p>
              <p class="mt-1 text-sm text-amber-800 dark:text-amber-300">
                授权后，该应用将获得下列权限对应的访问令牌。
              </p>
            </div>
          </div>
        </div>

        <div>
          <p class="mb-2 text-sm font-medium text-gray-900 dark:text-white">请求权限</p>
          <div class="flex flex-wrap gap-2">
            <span
              v-for="scope in preview.scopes"
              :key="scope"
              class="rounded-full bg-primary-50 px-3 py-1 text-xs font-medium text-primary-700 dark:bg-primary-900/30 dark:text-primary-300"
            >
              {{ scope }}
            </span>
            <span v-if="preview.scopes.length === 0" class="text-sm text-gray-500 dark:text-dark-400">
              未请求额外权限
            </span>
          </div>
        </div>

        <div class="grid grid-cols-2 gap-3">
          <button class="btn btn-secondary w-full" :disabled="isSubmitting" @click="handleDeny">
            拒绝
          </button>
          <button class="btn btn-primary w-full" :disabled="isSubmitting" @click="handleConfirm">
            <Icon v-if="!isSubmitting" name="check" size="md" class="mr-2" />
            {{ isSubmitting ? '处理中...' : '确认授权' }}
          </button>
        </div>
      </template>
    </div>
  </AuthLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import { AuthLayout } from '@/components/layout'
import Icon from '@/components/icons/Icon.vue'
import { confirmOAuthAuthorization, denyOAuthAuthorization, previewOAuthAuthorization } from '@/api/auth'
import type { OAuthAuthorizeParams, OAuthAuthorizePreview } from '@/api/auth'
import { useAppStore, useAuthStore } from '@/stores'

const route = useRoute()
const appStore = useAppStore()
const authStore = useAuthStore()

const isLoading = ref(true)
const isSubmitting = ref(false)
const errorMessage = ref('')
const preview = ref<OAuthAuthorizePreview | null>(null)

const currentUserLabel = computed(() => {
  const user = authStore.user
  if (!user) return '当前登录用户'
  return user.username || user.email || `用户 #${user.id}`
})

function getParams(): OAuthAuthorizeParams {
  return {
    client_id: String(route.query.client_id || ''),
    redirect_uri: String(route.query.redirect_uri || ''),
    response_type: String(route.query.response_type || ''),
    scope: route.query.scope ? String(route.query.scope) : undefined,
    state: route.query.state ? String(route.query.state) : undefined,
    code_challenge: route.query.code_challenge ? String(route.query.code_challenge) : undefined,
    code_challenge_method: route.query.code_challenge_method
      ? String(route.query.code_challenge_method)
      : undefined
  }
}

async function loadPreview() {
  isLoading.value = true
  errorMessage.value = ''
  try {
    preview.value = await previewOAuthAuthorization(getParams())
  } catch (error: any) {
    errorMessage.value = error?.message || '授权请求无效'
  } finally {
    isLoading.value = false
  }
}

async function redirectFrom(action: (params: OAuthAuthorizeParams) => Promise<{ redirect_url: string }>) {
  isSubmitting.value = true
  errorMessage.value = ''
  try {
    const result = await action(getParams())
    window.location.href = result.redirect_url
  } catch (error: any) {
    errorMessage.value = error?.message || '处理授权请求失败'
    appStore.showError(errorMessage.value)
  } finally {
    isSubmitting.value = false
  }
}

function handleConfirm() {
  redirectFrom(confirmOAuthAuthorization)
}

function handleDeny() {
  redirectFrom(denyOAuthAuthorization)
}

onMounted(loadPreview)
</script>

