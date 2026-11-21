import { ref } from 'vue'
import { getMigrationStatus, type MigrationStatus } from '@/api/migration'

// 全站共用一份：侧栏入口、顶部横幅与迁移页都读它，每次登录会话只请求一次。
const status = ref<MigrationStatus | null>(null)
let pending: Promise<void> | null = null

export const MIGRATION_RESULT_KEY = 'metask_migration_result'

export function useMigrationStatus() {
  async function load(force = false): Promise<void> {
    if (status.value && !force) return
    if (!pending) {
      pending = getMigrationStatus()
        .then((s) => {
          status.value = s
        })
        .catch(() => {
          // 接口不可用时不显示入口，不打扰用户
        })
        .finally(() => {
          pending = null
        })
    }
    await pending
  }

  function set(next: MigrationStatus | null) {
    status.value = next
  }

  return { status, load, set }
}

/** milli-U → "12.35 U" */
export function formatMilliU(mu: number | null | undefined): string {
  return `${((mu ?? 0) / 1000).toFixed(2)} U`
}
