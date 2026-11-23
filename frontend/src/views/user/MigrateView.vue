<template>
  <AppLayout>
    <div class="mx-auto max-w-3xl space-y-6">
      <div v-if="loading" class="flex justify-center py-16">
        <div class="h-8 w-8 animate-spin rounded-full border-2 border-primary-500 border-t-transparent"></div>
      </div>

      <div v-else-if="!status || !status.enabled" class="card p-6" data-testid="migrate-disabled">
        <h2 class="text-base font-semibold text-gray-900 dark:text-white">迁移尚未开放</h2>
        <p class="mt-2 text-sm text-gray-500 dark:text-dark-400">开放后这里会出现迁移入口，届时站内会有公告。</p>
      </div>

      <template v-else>
        <div class="card p-6">
          <h2 class="text-lg font-semibold text-gray-900 dark:text-white">把账号迁移到 metask-ai</h2>
          <p class="mt-2 text-sm leading-6 text-gray-600 dark:text-dark-300">
            本站即将停止服务，账号、余额、订阅额度和 API Key 可以一次迁移到新站
            <a v-if="status.site_url" :href="status.site_url" target="_blank" rel="noopener" class="text-primary-600 underline">{{ status.site_url }}</a
            >。<span v-if="status.deadline">{{ status.deadline }} 之后本站停止 API 调用，仍可登录本站完成迁移。</span>
          </p>
        </div>

        <div class="card" data-testid="migrate-preview">
          <div class="border-b border-gray-100 px-6 py-4 dark:border-dark-700">
            <h3 class="text-base font-semibold text-gray-900 dark:text-white">
              {{ status.status === 'frozen' ? '已冻结，等待新站入账' : '将要迁移的内容' }}
            </h3>
          </div>
          <dl class="divide-y divide-gray-100 px-6 dark:divide-dark-700">
            <div class="flex items-center justify-between py-3">
              <dt class="text-sm text-gray-500 dark:text-dark-400">余额</dt>
              <dd class="text-sm font-medium text-gray-900 dark:text-white" data-testid="migrate-balance">
                {{ formatMilliU(Math.max(status.balance_mu, 0)) }}
                <span v-if="status.balance_mu < 0" class="ml-1 text-xs text-gray-400">（当前为负，按 0 迁入）</span>
              </dd>
            </div>
            <div class="py-3">
              <div class="flex items-center justify-between">
                <dt class="text-sm text-gray-500 dark:text-dark-400">生效中的订阅</dt>
                <dd class="text-sm font-medium text-gray-900 dark:text-white">{{ status.subscriptions.length }} 份</dd>
              </div>
              <ul v-if="status.subscriptions.length" class="mt-2 space-y-1 text-xs text-gray-500 dark:text-dark-400">
                <li v-for="(s, i) in status.subscriptions" :key="i">
                  {{ s.plan_name || '订阅' }} · {{ s.monthly_limit_mu === null ? '不限额' : `月上限 ${formatMilliU(s.monthly_limit_mu)}` }} · 到期
                  {{ formatDate(s.expires_at) }}
                </li>
              </ul>
            </div>
            <div class="flex items-center justify-between py-3">
              <dt class="text-sm text-gray-500 dark:text-dark-400">可用的 API Key</dt>
              <dd class="text-sm font-medium text-gray-900 dark:text-white">{{ status.key_count }} 把</dd>
            </div>
          </dl>
        </div>

        <div class="card p-6">
          <h3 class="text-base font-semibold text-gray-900 dark:text-white">迁移后会发生什么</h3>
          <ul class="mt-3 list-disc space-y-2 pl-5 text-sm leading-6 text-gray-600 dark:text-dark-300">
            <li>本站余额清零、订阅终止、全部 API Key 立即停用，账号不能再登录本站。<strong>此操作不可撤销。</strong></li>
            <li>余额按点击这一刻的数额迁入新站；此前已在新站用掉的迁入额度会相应扣除。</li>
            <li>订阅的剩余额度迁入新站，到期日不变。</li>
            <li>
              原来的 API Key 在新站继续可用，具体新 apikey 和 baseurl 设置请参考
              <a href="https://www.metask-ai.com/app/access" target="_blank" rel="noopener" class="text-primary-600 underline">https://www.metask-ai.com/app/access</a>。
            </li>
            <li>在新站用本站的邮箱和密码登录；手机号账号用手机验证码登录。</li>
          </ul>
        </div>

        <div v-if="status.status === 'frozen'" class="rounded-lg border border-amber-200 bg-amber-50 p-4 text-sm text-amber-800 dark:border-amber-900/50 dark:bg-amber-900/20 dark:text-amber-200" data-testid="migrate-frozen">
          本站余额已冻结，新站入账暂未完成。系统每分钟自动重试一次，你也可以点下面的按钮立即重试；额度不会丢失。
        </div>
        <div v-else-if="!status.eligible" class="rounded-lg border border-gray-200 bg-gray-50 p-4 text-sm text-gray-600 dark:border-dark-700 dark:bg-dark-800 dark:text-dark-300">
          {{ status.reason === 'admin' ? '管理员账号不能迁移。' : '当前账号暂时不能迁移，请联系客服。' }}
        </div>

        <div v-if="status.eligible || status.status === 'frozen'" class="card space-y-4 p-6">
          <label v-if="status.status !== 'frozen'" class="flex items-start gap-3 text-sm text-gray-700 dark:text-dark-200">
            <input v-model="confirmed" type="checkbox" class="mt-1" data-testid="migrate-confirm" />
            <span>我已了解：迁移后本站账号与 API Key 立即停用，且不可撤销。</span>
          </label>
          <label class="flex items-start gap-3 text-sm text-gray-700 dark:text-dark-200">
            <input v-model="confirmedMetawork" type="checkbox" class="mt-1" data-testid="migrate-confirm-metawork" />
            <span>
              metacode 用户全面迁入 metawork，下载地址
              <a href="https://metawork.metask-ai.com/" target="_blank" rel="noopener" class="text-primary-600 underline">https://metawork.metask-ai.com/</a>。
            </span>
          </label>
          <button
            class="btn btn-primary"
            :disabled="submitting || (status.status !== 'frozen' && (!confirmed || !confirmedMetawork))"
            data-testid="migrate-submit"
            @click="submit"
          >
            {{ submitting ? '正在迁移…' : status.status === 'frozen' ? '立即重试入账' : '确认迁移到 metask-ai' }}
          </button>
        </div>
      </template>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import AppLayout from '@/components/layout/AppLayout.vue'
import { migrateToMetask } from '@/api/migration'
import { useAppStore } from '@/stores'
import { useAuthStore } from '@/stores/auth'
import { formatMilliU, MIGRATION_RESULT_KEY, useMigrationStatus } from '@/composables/useMigrationStatus'

const router = useRouter()
const appStore = useAppStore()
const authStore = useAuthStore()
const { status, load, set } = useMigrationStatus()

const loading = ref(true)
const submitting = ref(false)
const confirmed = ref(false)
const confirmedMetawork = ref(false)

function formatDate(value: string): string {
  const d = new Date(value)
  return Number.isNaN(d.getTime()) ? value : d.toLocaleDateString()
}

async function finish() {
  // 账号已在本站停用：把结果带到不需要登录的结果页，再清掉本地登录态
  try {
    sessionStorage.setItem(MIGRATION_RESULT_KEY, JSON.stringify(status.value))
  } catch {
    // 存不下也不影响迁移结果
  }
  try {
    await authStore.logout()
  } catch {
    // 账号已停用，退出接口会失败，本地状态由下面的跳转兜底
  }
  set(null)
  await router.replace('/migrated')
}

async function submit() {
  if (submitting.value) return
  submitting.value = true
  try {
    set(await migrateToMetask())
    if (status.value?.status === 'completed') {
      await finish()
    }
  } catch (e: unknown) {
    const message = (e as { message?: string })?.message || '迁移失败，请稍后重试'
    appStore.showError(message)
    await load(true)
  } finally {
    submitting.value = false
  }
}

onMounted(async () => {
  await load(true)
  loading.value = false
  if (status.value?.status === 'completed') {
    await finish()
  }
})
</script>
