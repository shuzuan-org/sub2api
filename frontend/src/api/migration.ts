/**
 * 迁移到 metask-ai
 * 金额单位 milli-U（1 U = 1000）。
 */

import { apiClient } from './client'

export interface MigrationSubscription {
  plan_name?: string
  /** null 表示套餐没有月上限 */
  monthly_limit_mu: number | null
  expires_at: string
}

/** metask-ai 入账接口的返回 */
export interface MigrationResult {
  login_email: string
  phone_account: boolean
  created_user: boolean
  old_balance_mu: number
  deducted_mu: number
  credited_mu: number
  keys_imported: number
  subscription: {
    state: 'none' | 'active' | 'kept' | 'exhausted' | 'skipped_has_subscription'
    period_end: string | null
    unlimited: boolean
    remaining_mu: number | null
  }
}

export interface MigrationStatus {
  enabled: boolean
  eligible: boolean
  reason?: string
  status: 'none' | 'frozen' | 'completed'
  site_url: string
  gateway_base_url: string
  deadline?: string
  balance_mu: number
  subscriptions: MigrationSubscription[]
  key_count: number
  result?: MigrationResult
}

export async function getMigrationStatus(): Promise<MigrationStatus> {
  const { data } = await apiClient.get<MigrationStatus>('/user/migration')
  return data
}

/** 确认迁移；可重复调用（已冻结未入账时会再试一次入账）。 */
export async function migrateToMetask(): Promise<MigrationStatus> {
  const { data } = await apiClient.post<MigrationStatus>('/user/migration')
  return data
}

export const migrationAPI = { getMigrationStatus, migrateToMetask }

export default migrationAPI
