<template>
  <AuthLayout>
    <div class="space-y-6">
      <div class="text-center">
        <div
          class="mx-auto mb-4 flex h-14 w-14 items-center justify-center rounded-2xl bg-primary-100 text-primary-600 dark:bg-primary-900/30 dark:text-primary-300"
        >
          <Icon name="terminal" size="lg" />
        </div>
        <h2 class="text-2xl font-bold text-gray-900 dark:text-white">授权设备</h2>
        <p class="mt-2 text-sm text-gray-500 dark:text-dark-400">
          输入 MetaCode CLI 显示的验证码以继续。
        </p>
      </div>

      <form v-if="!preview && !isDone" class="space-y-4" @submit.prevent="loadPreview">
        <div>
          <label for="device-user-code" class="input-label">设备验证码</label>
          <input
            id="device-user-code"
            v-model="userCode"
            class="input-field text-center font-mono text-xl tracking-widest"
            autocomplete="one-time-code"
            inputmode="text"
            maxlength="9"
            placeholder="ABCD-EFGH"
            @input="normalizeInput"
          />
        </div>
        <button class="btn btn-primary w-full" :disabled="isLoading || !userCode" type="submit">
          {{ isLoading ? '正在校验...' : '继续' }}
        </button>
      </form>

      <div
        v-if="errorMessage"
        class="rounded-xl border border-red-200 bg-red-50 p-4 dark:border-red-800/50 dark:bg-red-900/20"
      >
        <div class="flex items-start gap-3">
          <Icon name="exclamationCircle" size="md" class="mt-0.5 flex-shrink-0 text-red-500" />
          <p class="text-sm text-red-700 dark:text-red-400">{{ errorMessage }}</p>
        </div>
      </div>

      <template v-if="preview && !isDone">
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
              <p class="text-sm font-medium text-gray-900 dark:text-white">账号</p>
              <p class="text-sm text-gray-500 dark:text-dark-400">{{ currentUserLabel }}</p>
            </div>
          </div>

          <div v-if="deviceLabel" class="flex items-start gap-3">
            <Icon name="terminal" size="md" class="mt-0.5 flex-shrink-0 text-gray-400" />
            <div class="min-w-0">
              <p class="text-sm font-medium text-gray-900 dark:text-white">设备</p>
              <p class="break-all text-sm text-gray-500 dark:text-dark-400">{{ deviceLabel }}</p>
            </div>
          </div>

          <div class="flex items-start gap-3">
            <Icon name="clock" size="md" class="mt-0.5 flex-shrink-0 text-gray-400" />
            <div>
              <p class="text-sm font-medium text-gray-900 dark:text-white">过期时间</p>
              <p class="text-sm text-gray-500 dark:text-dark-400">{{ expiresAtLabel }}</p>
            </div>
          </div>
        </div>

        <div class="rounded-xl border border-amber-200 bg-amber-50 p-4 dark:border-amber-800/60 dark:bg-amber-900/20">
          <div class="flex gap-3">
            <Icon name="exclamationTriangle" size="md" class="mt-0.5 flex-shrink-0 text-amber-600" />
            <div>
              <p class="text-sm font-medium text-amber-900 dark:text-amber-200">请确认这是你的 CLI 会话</p>
              <p class="mt-1 text-sm text-amber-800 dark:text-amber-300">
                仅在验证码与你终端中显示的一致时才授权。
              </p>
            </div>
          </div>
        </div>

        <div>
          <p class="mb-2 text-sm font-medium text-gray-900 dark:text-white">请求的权限范围</p>
          <div class="flex flex-wrap gap-2">
            <span
              v-for="scope in preview.scopes"
              :key="scope"
              class="rounded-full bg-primary-50 px-3 py-1 text-xs font-medium text-primary-700 dark:bg-primary-900/30 dark:text-primary-300"
            >
              {{ scope }}
            </span>
          </div>
        </div>

        <div class="grid grid-cols-2 gap-3">
          <button class="btn btn-secondary w-full" :disabled="isSubmitting" @click="handleDeny">
            拒绝
          </button>
          <button class="btn btn-primary w-full" :disabled="isSubmitting" @click="handleConfirm">
            <Icon v-if="!isSubmitting" name="check" size="md" class="mr-2" />
            {{ isSubmitting ? '正在提交...' : '授权' }}
          </button>
        </div>
      </template>

      <div v-if="isDone" class="space-y-4 text-center">
        <div
          class="mx-auto flex h-14 w-14 items-center justify-center rounded-full"
          :class="doneApproved ? 'bg-green-100 text-green-600 dark:bg-green-900/30 dark:text-green-300' : 'bg-gray-100 text-gray-600 dark:bg-dark-800 dark:text-dark-300'"
        >
          <Icon :name="doneApproved ? 'checkCircle' : 'xCircle'" size="lg" />
        </div>
        <div>
          <h3 class="text-lg font-semibold text-gray-900 dark:text-white">
            {{ doneApproved ? '设备已授权' : '已拒绝授权' }}
          </h3>
          <p class="mt-2 text-sm text-gray-500 dark:text-dark-400">
            现在可以返回 MetaCode CLI。
          </p>
        </div>
      </div>
    </div>
  </AuthLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import { AuthLayout } from '@/components/layout'
import Icon from '@/components/icons/Icon.vue'
import {
  confirmOAuthDeviceAuthorization,
  denyOAuthDeviceAuthorization,
  previewOAuthDeviceAuthorization,
  type OAuthDeviceAuthorizationPreview
} from '@/api/auth'
import { useAppStore, useAuthStore } from '@/stores'

const route = useRoute()
const appStore = useAppStore()
const authStore = useAuthStore()

const userCode = ref('')
const preview = ref<OAuthDeviceAuthorizationPreview | null>(null)
const isLoading = ref(false)
const isSubmitting = ref(false)
const errorMessage = ref('')
const isDone = ref(false)
const doneApproved = ref(false)

const currentUserLabel = computed(() => {
  const user = authStore.user
  if (!user) return '当前登录用户'
  return user.username || user.email || `用户 #${user.id}`
})

const deviceLabel = computed(() => {
  if (!preview.value) return ''
  return [preview.value.device_name, preview.value.platform, preview.value.cli_version]
    .filter(Boolean)
    .join(' · ')
})

const expiresAtLabel = computed(() => {
  if (!preview.value?.expires_at) return ''
  const date = new Date(preview.value.expires_at)
  if (Number.isNaN(date.getTime())) return preview.value.expires_at
  return date.toLocaleString()
})

function normalizeInput() {
  const compact = userCode.value.toUpperCase().replace(/[^A-Z0-9]/g, '').slice(0, 8)
  userCode.value = compact.length > 4 ? `${compact.slice(0, 4)}-${compact.slice(4)}` : compact
}

async function loadPreview() {
  normalizeInput()
  if (!userCode.value) return
  isLoading.value = true
  errorMessage.value = ''
  try {
    preview.value = await previewOAuthDeviceAuthorization(userCode.value)
    userCode.value = preview.value.user_code
  } catch (error: any) {
    preview.value = null
    errorMessage.value = error?.message || '设备验证码无效或已过期。'
  } finally {
    isLoading.value = false
  }
}

async function handleConfirm() {
  if (!preview.value) return
  isSubmitting.value = true
  errorMessage.value = ''
  try {
    await confirmOAuthDeviceAuthorization(preview.value.user_code)
    doneApproved.value = true
    isDone.value = true
    appStore.showSuccess('设备已授权')
  } catch (error: any) {
    errorMessage.value = error?.message || '设备授权失败。'
    appStore.showError(errorMessage.value)
  } finally {
    isSubmitting.value = false
  }
}

async function handleDeny() {
  const code = preview.value?.user_code || userCode.value
  if (!code) return
  isSubmitting.value = true
  errorMessage.value = ''
  try {
    await denyOAuthDeviceAuthorization(code)
    doneApproved.value = false
    isDone.value = true
    appStore.showSuccess('已拒绝授权')
  } catch (error: any) {
    errorMessage.value = error?.message || '拒绝授权失败。'
    appStore.showError(errorMessage.value)
  } finally {
    isSubmitting.value = false
  }
}

onMounted(() => {
  const queryCode = route.query.user_code || route.query.code
  if (queryCode) {
    userCode.value = String(queryCode)
    loadPreview()
  }
})
</script>
