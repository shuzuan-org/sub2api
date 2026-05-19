/**
 * Invite friends API endpoints
 * 邀请好友：获取专属邀请码、邀请链接、统计与邀请明细
 */

import { apiClient } from './client'

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

/**
 * 获取当前用户的邀请概要（邀请码、链接、统计、明细分页）
 */
export async function getInviteSummary(
  params: InviteSummaryParams = {}
): Promise<InviteSummary> {
  const { data } = await apiClient.get<InviteSummary>('/invite/summary', { params })
  return data
}

export const inviteAPI = {
  getInviteSummary
}

export default inviteAPI
