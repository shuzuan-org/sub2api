/**
 * Admin Profit Report API
 * 网关收入看板：按 日×计费模型×账号×分组 聚合的标价/实付 + 账号成本折扣维表。
 * 成本与毛利在前端按折扣实时计算，所以接口只返回原材料。
 */

import { apiClient } from '../client'

export interface ProfitReportRow {
  day: string
  model: string
  account_id: number
  group_id: number | null
  requests: number
  input_tokens: number
  output_tokens: number
  cache_creation_tokens: number
  cache_read_tokens: number
  /** Σ total_cost，模型标价（U） */
  list_price: number
  /** Σ actual_cost，用户实付（U，已含分组/用户倍率） */
  revenue: number
}

export interface ProfitReportAccount {
  id: number
  name: string
  platform: string
  /** accounts.rate_multiplier，库中成本折扣 */
  rate_multiplier: number
  status: string
  schedulable: boolean
  deleted: boolean
  last_used_at: string | null
}

export interface ProfitReportGroup {
  id: number
  name: string
  platform: string
  rate_multiplier: number
  deleted: boolean
}

export interface ProfitReportResponse {
  generated_at: string
  start_date: string
  end_date: string
  timezone: string
  rows: ProfitReportRow[]
  accounts: ProfitReportAccount[]
  groups: ProfitReportGroup[]
}

export interface ProfitReportParams {
  start_date: string
  end_date: string
}

export async function getReport(params: ProfitReportParams): Promise<ProfitReportResponse> {
  const { data } = await apiClient.get<ProfitReportResponse>('/admin/profit-report', { params })
  return data
}

export const profitReportAPI = {
  getReport
}

export default profitReportAPI
