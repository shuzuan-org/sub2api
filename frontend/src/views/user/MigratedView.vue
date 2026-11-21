<template>
  <AuthLayout>
    <div class="space-y-6" data-testid="migrated">
      <div class="text-center">
        <h2 class="text-2xl font-bold text-gray-900 dark:text-white">迁移完成</h2>
        <p class="mt-2 text-sm text-gray-500 dark:text-dark-400">本站账号已停用，请到新站继续使用。</p>
      </div>

      <dl v-if="result" class="divide-y divide-gray-100 rounded-lg border border-gray-100 px-4 text-sm dark:divide-dark-700 dark:border-dark-700">
        <div class="flex items-center justify-between py-3">
          <dt class="text-gray-500 dark:text-dark-400">新站登录账号</dt>
          <dd class="font-medium text-gray-900 dark:text-white" data-testid="migrated-login">
            {{ result.phone_account ? '原手机号（验证码登录）' : result.login_email }}
          </dd>
        </div>
        <div class="flex items-center justify-between py-3">
          <dt class="text-gray-500 dark:text-dark-400">迁入余额</dt>
          <dd class="font-medium text-gray-900 dark:text-white" data-testid="migrated-credit">{{ formatMilliU(result.credited_mu) }}</dd>
        </div>
        <div v-if="result.deducted_mu > 0" class="flex items-center justify-between py-3">
          <dt class="text-gray-500 dark:text-dark-400">已在新站使用（扣除）</dt>
          <dd class="font-medium text-gray-900 dark:text-white">{{ formatMilliU(result.deducted_mu) }}</dd>
        </div>
        <div v-if="result.subscription.state !== 'none'" class="flex items-center justify-between py-3">
          <dt class="text-gray-500 dark:text-dark-400">订阅</dt>
          <dd class="text-right font-medium text-gray-900 dark:text-white">{{ subscriptionText }}</dd>
        </div>
        <div class="flex items-center justify-between py-3">
          <dt class="text-gray-500 dark:text-dark-400">新增导入的 API Key</dt>
          <dd class="font-medium text-gray-900 dark:text-white">{{ result.keys_imported }} 把</dd>
        </div>
      </dl>

      <ul class="list-disc space-y-2 pl-5 text-sm leading-6 text-gray-600 dark:text-dark-300">
        <li v-if="status?.gateway_base_url">
          API base URL 改成
          <code class="rounded bg-gray-100 px-1 py-0.5 text-xs dark:bg-dark-700">{{ status.gateway_base_url }}</code>
        </li>
        <li>原来的 API Key 不用换；模型名称改成新站「模型列表」里的名称。</li>
        <li v-if="result && !result.phone_account">用本站的邮箱和密码登录新站；忘记密码可在新站找回。</li>
      </ul>

      <a v-if="status?.site_url" :href="status.site_url" class="btn btn-primary block w-full text-center" data-testid="migrated-go">去新站登录</a>
    </div>
  </AuthLayout>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import AuthLayout from '@/components/layout/AuthLayout.vue'
import type { MigrationStatus } from '@/api/migration'
import { formatMilliU, MIGRATION_RESULT_KEY } from '@/composables/useMigrationStatus'

function read(): MigrationStatus | null {
  try {
    const raw = sessionStorage.getItem(MIGRATION_RESULT_KEY)
    return raw ? (JSON.parse(raw) as MigrationStatus) : null
  } catch {
    return null
  }
}

const status = ref<MigrationStatus | null>(read())
const result = computed(() => status.value?.result ?? null)

const subscriptionText = computed(() => {
  const sub = result.value?.subscription
  if (!sub) return ''
  const until = sub.period_end ? `，到期 ${new Date(sub.period_end).toLocaleDateString()}` : ''
  switch (sub.state) {
    case 'active':
      return (sub.unlimited ? '已迁入' : `剩余额度 ${formatMilliU(sub.remaining_mu)}`) + until
    case 'exhausted':
      return '额度已用完，未迁入'
    case 'kept':
      return '新站原有订阅保持不变'
    case 'skipped_has_subscription':
      return '新站已有生效订阅，未重复迁入'
    default:
      return ''
  }
})
</script>
