<template>
  <AuthLayout>
    <div class="space-y-6">
      <!-- Title -->
      <div class="text-center">
        <h2 class="text-2xl font-bold text-gray-900 dark:text-white">
          {{ t('auth.createAccount') }}
        </h2>
        <p class="mt-2 text-sm text-gray-500 dark:text-dark-400">
          {{ t('auth.signUpToStart', { siteName }) }}
        </p>
      </div>

      <!-- LinuxDo Connect OAuth 登录 -->
      <LinuxDoOAuthSection v-if="linuxdoOAuthEnabled" :disabled="isLoading" />

      <!-- Registration Disabled Message -->
      <div
        v-if="!registrationEnabled && settingsLoaded"
        class="rounded-xl border border-amber-200 bg-amber-50 p-4 dark:border-amber-800/50 dark:bg-amber-900/20"
      >
        <div class="flex items-start gap-3">
          <div class="flex-shrink-0">
            <Icon name="exclamationCircle" size="md" class="text-amber-500" />
          </div>
          <p class="text-sm text-amber-700 dark:text-amber-400">
            {{ t('auth.registrationDisabled') }}
          </p>
        </div>
      </div>

      <!-- Registration Form -->
      <form v-else @submit.prevent="handleRegister" class="space-y-5">
        <!-- Registration Mode Toggle -->
        <div
          v-if="phoneRegistrationEnabled"
          class="flex rounded-lg bg-gray-100 p-1 dark:bg-dark-700"
        >
          <button
            type="button"
            :class="[
              'flex-1 rounded-md px-3 py-2 text-sm font-medium transition-colors',
              registrationMode === 'email'
                ? 'bg-white text-gray-900 shadow dark:bg-dark-600 dark:text-white'
                : 'text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-300'
            ]"
            @click="registrationMode = 'email'"
          >
            {{ t('auth.emailPasswordLogin') }}
          </button>
          <button
            type="button"
            :class="[
              'flex-1 rounded-md px-3 py-2 text-sm font-medium transition-colors',
              registrationMode === 'phone'
                ? 'bg-white text-gray-900 shadow dark:bg-dark-600 dark:text-white'
                : 'text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-300'
            ]"
            @click="registrationMode = 'phone'"
          >
            {{ t('auth.phoneLogin') }}
          </button>
        </div>

        <template v-if="registrationMode === 'email'">
          <!-- Email Input -->
          <div>
          <label for="email" class="input-label">
            {{ t('auth.emailLabel') }}
          </label>
          <div class="relative">
            <div class="pointer-events-none absolute inset-y-0 left-0 flex items-center pl-3.5">
              <Icon name="mail" size="md" class="text-gray-400 dark:text-dark-500" />
            </div>
            <input
              id="email"
              v-model="formData.email"
              type="email"
              required
              autofocus
              autocomplete="email"
              :disabled="isLoading"
              class="input pl-11"
              :class="{ 'input-error': errors.email }"
              :placeholder="t('auth.emailPlaceholder')"
            />
          </div>
          <p v-if="errors.email" class="input-error-text">
            {{ errors.email }}
          </p>
        </div>

          <!-- Password Input -->
          <div>
          <label for="password" class="input-label">
            {{ t('auth.passwordLabel') }}
          </label>
          <div class="relative">
            <div class="pointer-events-none absolute inset-y-0 left-0 flex items-center pl-3.5">
              <Icon name="lock" size="md" class="text-gray-400 dark:text-dark-500" />
            </div>
            <input
              id="password"
              v-model="formData.password"
              :type="showPassword ? 'text' : 'password'"
              required
              autocomplete="new-password"
              :disabled="isLoading"
              class="input pl-11 pr-11"
              :class="{ 'input-error': errors.password }"
              :placeholder="t('auth.createPasswordPlaceholder')"
            />
            <button
              type="button"
              @click="showPassword = !showPassword"
              class="absolute inset-y-0 right-0 flex items-center pr-3.5 text-gray-400 transition-colors hover:text-gray-600 dark:hover:text-dark-300"
            >
              <Icon v-if="showPassword" name="eyeOff" size="md" />
              <Icon v-else name="eye" size="md" />
            </button>
          </div>
          <p v-if="errors.password" class="input-error-text">
            {{ errors.password }}
          </p>
          <p v-else class="input-hint">
            {{ t('auth.passwordHint') }}
          </p>
        </div>

        </template>

        <template v-if="registrationMode === 'phone'">
          <!-- Phone Input -->
          <div>
            <label for="register_phone" class="input-label">
              {{ t('auth.phoneLabel') }}
            </label>
            <div class="relative">
              <div class="pointer-events-none absolute inset-y-0 left-0 flex items-center pl-3.5">
                <Icon name="key" size="md" class="text-gray-400 dark:text-dark-500" />
              </div>
              <input
                id="register_phone"
                v-model="phoneFormData.phone"
                type="tel"
                required
                autocomplete="tel"
                :disabled="isLoading"
                class="input pl-11"
                :class="{ 'input-error': errors.phone }"
                :placeholder="t('auth.phonePlaceholder')"
              />
            </div>
            <p v-if="errors.phone" class="input-error-text">
              {{ errors.phone }}
            </p>
          </div>

          <!-- Phone Verify Code Input -->
          <div>
            <label for="register_phone_code" class="input-label">
              {{ t('auth.verificationCode') }}
            </label>
            <div class="flex gap-2">
              <input
                id="register_phone_code"
                v-model="phoneFormData.verifyCode"
                type="text"
                required
                inputmode="numeric"
                maxlength="6"
                autocomplete="one-time-code"
                :disabled="isLoading"
                class="input"
                :class="{ 'input-error': errors.phone_verify_code }"
                :placeholder="t('auth.verificationCodeHint')"
              />
              <button
                type="button"
                :disabled="isLoading || sendPhoneCodeDisabled"
                class="btn btn-secondary whitespace-nowrap"
                @click="handleSendPhoneRegisterCode"
              >
                {{ sendPhoneCodeButtonText }}
              </button>
            </div>
            <p v-if="errors.phone_verify_code" class="input-error-text">
              {{ errors.phone_verify_code }}
            </p>
          </div>
        </template>

        <!-- Invitation Code Input (Optional when enabled) -->
        <div v-if="invitationCodeEnabled || inviteCodeFromUrl">
          <label for="invitation_code" class="input-label">
            {{ t('auth.invitationCodeLabel') }}
            <span class="ml-1 text-xs font-normal text-gray-400 dark:text-dark-500">({{ t('common.optional') }})</span>
          </label>
          <div class="relative">
            <div class="pointer-events-none absolute inset-y-0 left-0 flex items-center pl-3.5">
              <Icon name="key" size="md" :class="invitationValidation.valid ? 'text-green-500' : 'text-gray-400 dark:text-dark-500'" />
            </div>
            <input
              id="invitation_code"
              v-model="formData.invitation_code"
              type="text"
              :disabled="isLoading"
              class="input pl-11 pr-10"
              :class="{
                'border-green-500 focus:border-green-500 focus:ring-green-500': invitationValidation.valid,
                'border-red-500 focus:border-red-500 focus:ring-red-500': invitationValidation.invalid || errors.invitation_code
              }"
              :placeholder="t('auth.invitationCodePlaceholder')"
              @input="handleInvitationCodeInput"
            />
            <!-- Validation indicator -->
            <div v-if="invitationValidating" class="absolute inset-y-0 right-0 flex items-center pr-3.5">
              <svg class="h-4 w-4 animate-spin text-gray-400" fill="none" viewBox="0 0 24 24">
                <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
                <path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
              </svg>
            </div>
            <div v-else-if="invitationValidation.valid" class="absolute inset-y-0 right-0 flex items-center pr-3.5">
              <Icon name="checkCircle" size="md" class="text-green-500" />
            </div>
            <div v-else-if="invitationValidation.invalid || errors.invitation_code" class="absolute inset-y-0 right-0 flex items-center pr-3.5">
              <Icon name="exclamationCircle" size="md" class="text-red-500" />
            </div>
          </div>
          <!-- Invitation code validation result -->
          <transition name="fade">
            <div v-if="invitationValidation.valid" class="mt-2 flex items-center gap-2 rounded-lg bg-green-50 px-3 py-2 dark:bg-green-900/20">
              <Icon name="checkCircle" size="sm" class="text-green-600 dark:text-green-400" />
              <span class="text-sm text-green-700 dark:text-green-400">
                {{ t('auth.invitationCodeValid') }}
              </span>
            </div>
            <p v-else-if="invitationValidation.invalid" class="input-error-text">
              {{ invitationValidation.message }}
            </p>
            <p v-else-if="errors.invitation_code" class="input-error-text">
              {{ errors.invitation_code }}
            </p>
          </transition>
        </div>

        <!-- Turnstile Widget -->
        <div v-if="turnstileEnabled && turnstileSiteKey">
          <TurnstileWidget
            ref="turnstileRef"
            :site-key="turnstileSiteKey"
            @verify="onTurnstileVerify"
            @expire="onTurnstileExpire"
            @error="onTurnstileError"
          />
          <p v-if="errors.turnstile" class="input-error-text mt-2 text-center">
            {{ errors.turnstile }}
          </p>
        </div>

        <!-- Error Message -->
        <transition name="fade">
          <div
            v-if="errorMessage"
            class="rounded-xl border border-red-200 bg-red-50 p-4 dark:border-red-800/50 dark:bg-red-900/20"
          >
            <div class="flex items-start gap-3">
              <div class="flex-shrink-0">
                <Icon name="exclamationCircle" size="md" class="text-red-500" />
              </div>
              <p class="text-sm text-red-700 dark:text-red-400">
                {{ errorMessage }}
              </p>
            </div>
          </div>
        </transition>

        <!-- Submit Button -->
        <button
          type="submit"
          :disabled="isLoading || (turnstileEnabled && !turnstileToken)"
          class="btn btn-primary w-full"
        >
          <svg
            v-if="isLoading"
            class="-ml-1 mr-2 h-4 w-4 animate-spin text-white"
            fill="none"
            viewBox="0 0 24 24"
          >
            <circle
              class="opacity-25"
              cx="12"
              cy="12"
              r="10"
              stroke="currentColor"
              stroke-width="4"
            ></circle>
            <path
              class="opacity-75"
              fill="currentColor"
              d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"
            ></path>
          </svg>
          <Icon v-else name="userPlus" size="md" class="mr-2" />
          {{
            isLoading
              ? t('auth.processing')
              : registrationMode === 'email' && emailVerifyEnabled
                ? t('auth.continue')
                : t('auth.createAccount')
          }}
        </button>
      </form>
    </div>

    <!-- Footer -->
    <template #footer>
      <p class="text-gray-500 dark:text-dark-400">
        {{ t('auth.alreadyHaveAccount') }}
        <router-link
          to="/login"
          class="font-medium text-primary-600 transition-colors hover:text-primary-500 dark:text-primary-400 dark:hover:text-primary-300"
        >
          {{ t('auth.signIn') }}
        </router-link>
      </p>
    </template>
  </AuthLayout>
</template>

<script setup lang="ts">
import { ref, reactive, onMounted, onUnmounted, computed } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { AuthLayout } from '@/components/layout'
import LinuxDoOAuthSection from '@/components/auth/LinuxDoOAuthSection.vue'
import Icon from '@/components/icons/Icon.vue'
import TurnstileWidget from '@/components/TurnstileWidget.vue'
import { useAuthStore, useAppStore } from '@/stores'
import { getPublicSettings, validateInvitationCode, sendPhoneRegisterCode } from '@/api/auth'
import { buildAuthErrorMessage } from '@/utils/authError'
import {
  isRegistrationEmailSuffixAllowed,
  normalizeRegistrationEmailSuffixWhitelist
} from '@/utils/registrationEmailPolicy'

const { t, locale } = useI18n()

// ==================== Router & Stores ====================

const router = useRouter()
const route = useRoute()
const authStore = useAuthStore()
const appStore = useAppStore()

// ==================== State ====================

const isLoading = ref<boolean>(false)
const settingsLoaded = ref<boolean>(false)
const errorMessage = ref<string>('')
const showPassword = ref<boolean>(false)
const registrationMode = ref<'email' | 'phone'>('email')
const phoneRegistrationEnabled = ref<boolean>(false)
const inviteCodeFromUrl = ref<boolean>(false)

// Public settings
const registrationEnabled = ref<boolean>(true)
const emailVerifyEnabled = ref<boolean>(false)
const invitationCodeEnabled = ref<boolean>(false)
const turnstileEnabled = ref<boolean>(false)
const turnstileSiteKey = ref<string>('')
const siteName = ref<string>('Sub2API')
const linuxdoOAuthEnabled = ref<boolean>(false)
const registrationEmailSuffixWhitelist = ref<string[]>([])

// Turnstile
const turnstileRef = ref<InstanceType<typeof TurnstileWidget> | null>(null)
const turnstileToken = ref<string>('')

// Invitation code validation
const invitationValidating = ref<boolean>(false)
const invitationValidation = reactive({
  valid: false,
  invalid: false,
  message: ''
})
let invitationValidateTimeout: ReturnType<typeof setTimeout> | null = null

const formData = reactive({
  email: '',
  password: '',
  invitation_code: '',
  referral_code: ''
})

const phoneFormData = reactive({
  phone: '',
  verifyCode: ''
})

const errors = reactive({
  email: '',
  password: '',
  phone: '',
  phone_verify_code: '',
  turnstile: '',
  invitation_code: ''
})

const sendPhoneCodeCountdown = ref<number>(0)
const sendPhoneCodeDisabled = computed(() => sendPhoneCodeCountdown.value > 0)
const sendPhoneCodeButtonText = computed(() => {
  if (sendPhoneCodeCountdown.value > 0) {
    return t('auth.resendAfter', { seconds: sendPhoneCodeCountdown.value })
  }
  return t('auth.sendCode')
})
let sendPhoneCodeTimer: ReturnType<typeof setInterval> | null = null

// ==================== Lifecycle ====================

onMounted(async () => {
  try {
    const settings = await getPublicSettings()
    registrationEnabled.value = settings.registration_enabled
    emailVerifyEnabled.value = settings.email_verify_enabled
    invitationCodeEnabled.value = settings.invitation_code_enabled
    turnstileEnabled.value = settings.turnstile_enabled
    turnstileSiteKey.value = settings.turnstile_site_key || ''
    siteName.value = settings.site_name || 'Sub2API'
    linuxdoOAuthEnabled.value = settings.linuxdo_oauth_enabled
    phoneRegistrationEnabled.value = settings.phone_login_enabled
    registrationEmailSuffixWhitelist.value = normalizeRegistrationEmailSuffixWhitelist(
      settings.registration_email_suffix_whitelist || []
    )

    // Read invite code from URL. Channel activity/friend invite links should be
    // visible and submitted even when the global invitation-code gate is off.
    const inviteParam = route.query.invite as string
    if (inviteParam) {
      const inviteCode = inviteParam.trim()
      if (inviteCode) {
        inviteCodeFromUrl.value = true
        formData.referral_code = inviteCode
        formData.invitation_code = inviteCode
        await validateInvitationCodeDebounced(inviteCode)
      }
    }
  } catch (error) {
    console.error('Failed to load public settings:', error)
  } finally {
    settingsLoaded.value = true
  }
})

onUnmounted(() => {
  if (invitationValidateTimeout) {
    clearTimeout(invitationValidateTimeout)
  }
  if (sendPhoneCodeTimer) {
    clearInterval(sendPhoneCodeTimer)
  }
})

// ==================== Invitation Code Validation ====================

function handleInvitationCodeInput(): void {
  const code = formData.invitation_code.trim()

  // Clear previous validation
  invitationValidation.valid = false
  invitationValidation.invalid = false
  invitationValidation.message = ''
  errors.invitation_code = ''

  if (!code) {
    return
  }

  // Debounce validation
  if (invitationValidateTimeout) {
    clearTimeout(invitationValidateTimeout)
  }

  invitationValidateTimeout = setTimeout(() => {
    validateInvitationCodeDebounced(code)
  }, 500)
}

async function validateInvitationCodeDebounced(code: string): Promise<void> {
  invitationValidating.value = true

  try {
    const result = await validateInvitationCode(code)

    if (result.valid) {
      invitationValidation.valid = true
      invitationValidation.invalid = false
      invitationValidation.message = ''
    } else {
      invitationValidation.valid = false
      invitationValidation.invalid = true
      invitationValidation.message = getInvitationErrorMessage(result.error_code)
    }
  } catch {
    invitationValidation.valid = false
    invitationValidation.invalid = true
    invitationValidation.message = t('auth.invitationCodeInvalid')
  } finally {
    invitationValidating.value = false
  }
}

function getInvitationErrorMessage(errorCode?: string): string {
  switch (errorCode) {
    case 'INVITATION_CODE_NOT_FOUND':
      return t('auth.invitationCodeInvalid')
    case 'INVITATION_CODE_INVALID':
      return t('auth.invitationCodeInvalid')
    case 'INVITATION_CODE_USED':
      return t('auth.invitationCodeInvalid')
    case 'INVITATION_CODE_DISABLED':
      return t('auth.invitationCodeInvalid')
    default:
      return t('auth.invitationCodeInvalid')
  }
}

// ==================== Turnstile Handlers ====================

function onTurnstileVerify(token: string): void {
  turnstileToken.value = token
  errors.turnstile = ''
}

function onTurnstileExpire(): void {
  turnstileToken.value = ''
  errors.turnstile = t('auth.turnstileExpired')
}

function onTurnstileError(): void {
  turnstileToken.value = ''
  errors.turnstile = t('auth.turnstileFailed')
}

// ==================== Validation ====================

function validateEmail(email: string): boolean {
  const emailRegex = /^[^\s@]+@[^\s@]+\.[^\s@]+$/
  return emailRegex.test(email)
}

function buildEmailSuffixNotAllowedMessage(): string {
  const normalizedWhitelist = normalizeRegistrationEmailSuffixWhitelist(
    registrationEmailSuffixWhitelist.value
  )
  if (normalizedWhitelist.length === 0) {
    return t('auth.emailSuffixNotAllowed')
  }
  const separator = String(locale.value || '').toLowerCase().startsWith('zh') ? '、' : ', '
  return t('auth.emailSuffixNotAllowedWithAllowed', {
    suffixes: normalizedWhitelist.join(separator)
  })
}

function validateForm(): boolean {
  // Reset errors
  errors.email = ''
  errors.password = ''
  errors.phone = ''
  errors.phone_verify_code = ''
  errors.turnstile = ''
  errors.invitation_code = ''

  let isValid = true

  if (registrationMode.value === 'email') {
    // Email validation
    if (!formData.email.trim()) {
      errors.email = t('auth.emailRequired')
      isValid = false
    } else if (!validateEmail(formData.email)) {
      errors.email = t('auth.invalidEmail')
      isValid = false
    } else if (
      !isRegistrationEmailSuffixAllowed(formData.email, registrationEmailSuffixWhitelist.value)
    ) {
      errors.email = buildEmailSuffixNotAllowedMessage()
      isValid = false
    }

    // Password validation
    if (!formData.password) {
      errors.password = t('auth.passwordRequired')
      isValid = false
    } else if (formData.password.length < 6) {
      errors.password = t('auth.passwordMinLength')
      isValid = false
    }
  } else {
    if (!phoneFormData.phone.trim()) {
      errors.phone = t('auth.phoneRequired')
      isValid = false
    } else if (!/^1[3-9]\d{9}$/.test(phoneFormData.phone.trim())) {
      errors.phone = t('auth.invalidPhone')
      isValid = false
    }

    if (!/^\d{6}$/.test(phoneFormData.verifyCode.trim())) {
      errors.phone_verify_code = t('auth.invalidCode')
      isValid = false
    }
  }

  // Turnstile validation
  if (turnstileEnabled.value && !turnstileToken.value) {
    errors.turnstile = t('auth.completeVerification')
    isValid = false
  }

  return isValid
}


function startPhoneCodeCountdown(seconds: number): void {
  sendPhoneCodeCountdown.value = seconds
  if (sendPhoneCodeTimer) {
    clearInterval(sendPhoneCodeTimer)
  }
  sendPhoneCodeTimer = setInterval(() => {
    if (sendPhoneCodeCountdown.value > 0) {
      sendPhoneCodeCountdown.value--
    } else if (sendPhoneCodeTimer) {
      clearInterval(sendPhoneCodeTimer)
      sendPhoneCodeTimer = null
    }
  }, 1000)
}

async function handleSendPhoneRegisterCode(): Promise<void> {
  errors.phone = ''
  errorMessage.value = ''

  if (!/^1[3-9]\d{9}$/.test(phoneFormData.phone.trim())) {
    errors.phone = t('auth.invalidPhone')
    return
  }
  if (turnstileEnabled.value && !turnstileToken.value) {
    errors.turnstile = t('auth.completeVerification')
    return
  }

  try {
    const result = await sendPhoneRegisterCode({
      phone: phoneFormData.phone.trim(),
      turnstile_token: turnstileEnabled.value ? turnstileToken.value : undefined
    })
    startPhoneCodeCountdown(result.countdown)
    appStore.showSuccess(t('auth.sendCodeSuccess'))
  } catch (error: unknown) {
    errorMessage.value = buildAuthErrorMessage(error, {
      fallback: t('auth.sendCodeFailed')
    })
    appStore.showError(errorMessage.value)
  }
}

// ==================== Form Handlers ====================

async function handleRegister(): Promise<void> {
  // Clear previous error
  errorMessage.value = ''

  // Validate form
  if (!validateForm()) {
    return
  }

  // Check invitation code validation status only when a code is provided.
  // Empty invitation code skips all invitation logic and registers normally.
  const invitationCode = formData.invitation_code.trim()
  if (invitationCode) {
    // If still validating, wait
    if (invitationValidating.value) {
      errorMessage.value = t('auth.invitationCodeValidating')
      return
    }
    // If invitation code is invalid, block submission
    if (invitationValidation.invalid) {
      errorMessage.value = t('auth.invitationCodeInvalidCannotRegister')
      return
    }
    // If invitation code was provided but not validated yet
    if (!invitationValidation.valid) {
      errorMessage.value = t('auth.invitationCodeValidating')
      // Trigger validation
      await validateInvitationCodeDebounced(invitationCode)
      if (!invitationValidation.valid) {
        errorMessage.value = t('auth.invitationCodeInvalidCannotRegister')
        return
      }
    }
  }

  isLoading.value = true

  try {
    if (registrationMode.value === 'phone') {
      await authStore.registerWithPhoneCode({
        phone: phoneFormData.phone.trim(),
        verify_code: phoneFormData.verifyCode.trim(),
        invitation_code: formData.invitation_code || undefined,
        turnstile_token: turnstileEnabled.value ? turnstileToken.value : undefined
      })

      appStore.showSuccess(t('auth.accountCreatedSuccess', { siteName: siteName.value }))
      const redirectTo = (route.query.redirect as string) || '/dashboard'
      await router.push(redirectTo)
      return
    }

    // If email verification is enabled, redirect to verification page
    if (emailVerifyEnabled.value) {
      // Store registration data in sessionStorage
      sessionStorage.setItem(
        'register_data',
        JSON.stringify({
          email: formData.email,
          password: formData.password,
          turnstile_token: turnstileToken.value,
          invitation_code: formData.invitation_code || undefined,
          referral_code: formData.referral_code || undefined,
          redirect: (route.query.redirect as string) || undefined
        })
      )

      // Navigate to email verification page
      await router.push('/email-verify')
      return
    }

    // Otherwise, directly register
    await authStore.register({
      email: formData.email,
      password: formData.password,
      turnstile_token: turnstileEnabled.value ? turnstileToken.value : undefined,
      invitation_code: formData.invitation_code || undefined,
      referral_code: formData.referral_code || undefined
    })

    // Show success toast
    appStore.showSuccess(t('auth.accountCreatedSuccess', { siteName: siteName.value }))

    // Redirect to intended destination, or default to dashboard
    const redirectTo = (route.query.redirect as string) || '/dashboard'
    await router.push(redirectTo)
  } catch (error: unknown) {
    // Reset Turnstile on error
    if (turnstileRef.value) {
      turnstileRef.value.reset()
      turnstileToken.value = ''
    }

    // Handle registration error
    errorMessage.value = buildAuthErrorMessage(error, {
      fallback: t('auth.registrationFailed')
    })

    // Also show error toast
    appStore.showError(errorMessage.value)
  } finally {
    isLoading.value = false
  }
}
</script>

<style scoped>
.fade-enter-active,
.fade-leave-active {
  transition: all 0.3s ease;
}

.fade-enter-from,
.fade-leave-to {
  opacity: 0;
  transform: translateY(-8px);
}
</style>
