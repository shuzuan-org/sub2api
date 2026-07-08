/**
 * Invite friends API endpoints
 * 邀请好友：获取专属邀请码、邀请链接、统计与邀请明细
 */

import { apiClient } from './client'
import type { BasePaginationResponse } from '@/types'

export interface InviteRecord {
  email: string
  nickname: string
  registered_at: string
  total_recharge: number // 占位：本期恒 0
  status: string // 占位：恒 "registered"
}

export interface InviteStats {
  invited_count: number
  recharged_count: number // 占位：本期恒 0
  total_commission: number // 占位：本期恒 0
  withdrawable: number // 占位：本期恒 0
}

export interface InviteSummary {
  code: string
  link: string
  stats: InviteStats
  records: InviteRecord[]
  total: number
  page: number
  page_size: number
}

export interface InviteSummaryParams {
  page?: number
  page_size?: number
  search?: string
}

// ==================== 渠道邀请（渠道合作方视角） ====================
// 响应形状与 admin 端 ChannelInviteBatch 不同（后端刻意精简），单独定义类型。

export interface ChannelBatchCode {
  code: string
  status: string
  max_uses: number
  used_count: number
}

export interface ChannelOwnedBatch {
  id: number
  name: string
  status: string
  is_active: boolean
  bonus_amount: number
  start_time: string | null
  end_time: string | null
  activity_copy_text: string
  code_count: number
  used_count: number
  codes: ChannelBatchCode[]
}

export interface ChannelUsageRecord {
  id: number
  user_email: string
  user_username: string
  claimed_at: string
  bonus_granted: boolean
}

/**
 * 获取当前用户的邀请概要（邀请码、链接、统计、明细分页）
 */
export async function getInviteSummary(
  params: InviteSummaryParams = {}
): Promise<InviteSummary> {
  const { data } = await apiClient.get<InviteSummary>('/invite/summary', { params })
  return data
}

/**
 * 获取当前用户作为渠道合作方名下的全部渠道活动批次（空数组 = 非合作方）
 */
export async function getChannelInviteSummary(): Promise<{ batches: ChannelOwnedBatch[] }> {
  const { data } = await apiClient.get<{ batches: ChannelOwnedBatch[] }>('/channel-invite/summary')
  return data
}

/**
 * 分页获取渠道活动批次的兑换（被邀请）记录，仅批次归属人可查
 */
export async function getChannelBatchUsages(
  batchId: number,
  params: { page?: number; page_size?: number } = {}
): Promise<BasePaginationResponse<ChannelUsageRecord>> {
  const { data } = await apiClient.get<BasePaginationResponse<ChannelUsageRecord>>(
    `/channel-invite/batches/${batchId}/usages`,
    { params }
  )
  return data
}

export const inviteAPI = {
  getInviteSummary,
  getChannelInviteSummary,
  getChannelBatchUsages
}

export default inviteAPI
