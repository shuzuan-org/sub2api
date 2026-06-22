<template>
  <div class="card">
    <div class="border-b border-gray-100 px-6 py-4 dark:border-dark-700">
      <h2 class="text-lg font-medium text-gray-900 dark:text-white">
        {{ t('profile.phone.title') }}
      </h2>
    </div>
    <div class="px-6 py-6">
      <!-- Bound state -->
      <div v-if="user?.phone_number" class="space-y-3">
        <div class="flex items-center gap-3 rounded-lg bg-emerald-50 p-4 dark:bg-emerald-900/20">
          <div class="rounded-lg bg-emerald-100 p-2 dark:bg-emerald-800">
            <Icon name="checkCircle" size="md" class="text-emerald-600 dark:text-emerald-400" />
          </div>
          <div>
            <p class="text-sm font-medium text-emerald-800 dark:text-emerald-200">
              {{ t('profile.phone.boundSuccess') }}
            </p>
            <p class="text-sm text-emerald-600 dark:text-emerald-400">
              {{ maskedPhone }}
            </p>
          </div>
        </div>
        <p class="text-xs text-gray-500 dark:text-gray-400">
          {{ t('profile.phone.bonusGranted') }}
        </p>
      </div>

      <!-- Unbound state -->
      <form v-else @submit.prevent="handleBind" class="space-y-4">
        <div>
          <label for="phone-number" class="input-label">
            {{ t('profile.phone.phoneNumber') }}
          </label>
          <input
            id="phone-number"
            v-model="phoneNumber"
            type="tel"
            class="input"
            :placeholder="t('profile.phone.phonePlaceholder')"
            maxlength="20"
            :disabled="sending"
          />
        </div>

        <div>
          <label for="phone-code" class="input-label">
            {{ t('profile.phone.verifyCode') }}
          </label>
          <div class="flex gap-2">
            <input
              id="phone-code"
              v-model="verifyCode"
              type="text"
              class="input flex-1"
              :placeholder="t('profile.phone.codePlaceholder')"
              maxlength="6"
              inputmode="numeric"
              autocomplete="one-time-code"
            />
            <button
              type="button"
              class="btn btn-secondary whitespace-nowrap"
              :disabled="sending || countdown > 0 || !phoneNumber.trim()"
              @click="handleSendCode"
            >
              {{ countdown > 0 ? `${countdown}s` : t('profile.phone.sendCode') }}
            </button>
          </div>
        </div>

        <div class="flex items-center gap-2 rounded-lg bg-amber-50 p-3 text-sm text-amber-700 dark:bg-amber-900/20 dark:text-amber-300">
          <Icon name="gift" size="sm" />
          <span>{{ t('profile.phone.bonusHint') }}</span>
        </div>

        <div class="flex justify-end">
          <button
            type="submit"
            class="btn btn-primary"
            :disabled="loading || !phoneNumber.trim() || !verifyCode.trim()"
          >
            {{ loading ? t('common.loading') : t('profile.phone.bindButton') }}
          </button>
        </div>
      </form>

      <!-- Error message -->
      <div v-if="errorMsg" class="mt-3 text-sm text-red-600 dark:text-red-400">
        {{ errorMsg }}
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAuthStore } from '@/stores/auth'
import { useAppStore } from '@/stores/app'
import { userAPI } from '@/api'
import Icon from '@/components/icons/Icon.vue'

const props = defineProps<{
  user: import('@/types').User | null
}>()

const { t } = useI18n()
const authStore = useAuthStore()
const appStore = useAppStore()

const phoneNumber = ref('')
const verifyCode = ref('')
const countdown = ref(0)
const sending = ref(false)
const loading = ref(false)
const errorMsg = ref('')

const maskedPhone = computed(() => {
  const p = props.user?.phone_number
  if (!p) return ''
  // Show "+86138****8000" format
  if (p.length >= 14) {
    return p.slice(0, 6) + '****' + p.slice(-4)
  }
  return p
})

let countdownTimer: ReturnType<typeof setInterval> | null = null

const startCountdown = (seconds: number) => {
  countdown.value = seconds
  countdownTimer = setInterval(() => {
    countdown.value--
    if (countdown.value <= 0) {
      clearInterval(countdownTimer!)
      countdownTimer = null
    }
  }, 1000)
}

const handleSendCode = async () => {
  errorMsg.value = ''
  sending.value = true
  try {
    const res = await userAPI.sendPhoneCode(phoneNumber.value.trim())
    startCountdown(res.countdown || 60)
    appStore.showSuccess(t('profile.phone.codeSent'))
  } catch (error: any) {
    const detail = error?.response?.data?.detail || error?.message || t('profile.phone.sendFailed')
    errorMsg.value = typeof detail === 'object' ? JSON.stringify(detail) : detail
  } finally {
    sending.value = false
  }
}

const handleBind = async () => {
  errorMsg.value = ''
  loading.value = true
  try {
    const result = await userAPI.bindPhone(phoneNumber.value.trim(), verifyCode.value.trim())
    authStore.user = result.user
    appStore.showSuccess(t('profile.phone.boundSuccessToast', { amount: result.bonus_amount }))
    // Reset form
    phoneNumber.value = ''
    verifyCode.value = ''
  } catch (error: any) {
    const detail = error?.response?.data?.detail || error?.message || t('profile.phone.bindFailed')
    errorMsg.value = typeof detail === 'object' ? JSON.stringify(detail) : detail
  } finally {
    loading.value = false
  }
}
</script>
