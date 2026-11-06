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

// ---------- 月度对账 ----------

/** usage_logs 按 北京时间自然月 × 计费模型 × 账号 的聚合行 */
export interface ReconcileUsageRow {
  period: string
  model: string
  account_id: number
  requests: number
  input_tokens: number
  output_tokens: number
  cache_creation_tokens: number
  cache_read_tokens: number
  /** Σ total_cost（U） */
  list_price: number
  /** Σ actual_cost（U） */
  revenue: number
}

export interface SupplierBinding {
  account_id: number
  supplier: string
  supplier_account: string
  updated_at?: string
}

/** 一行供应商账单；数值为 null 表示账单上留空（与 0 区分）。standard_model = '调账' 为整笔调账行。 */
export interface SupplierBillLine {
  period: string
  supplier: string
  supplier_account: string
  standard_model: string
  platform_models: string
  cost_discount: number | null
  currency: string
  input_tokens: number | null
  output_tokens: number | null
  cache_read_tokens: number | null
  cache_write_tokens: number | null
  other_tokens: number | null
  resource_fee: number | null
  discount_amount: number | null
  adjustment: number | null
  tax: number | null
  fx_rate: number | null
  note: string
  updated_at?: string
}

export interface ReconcileResponse {
  generated_at: string
  start_period: string
  end_period: string
  timezone: string
  rows: ReconcileUsageRow[]
  accounts: ProfitReportAccount[]
  bindings: SupplierBinding[]
  bills: SupplierBillLine[]
}

export async function getReconcile(params: { start_period: string; end_period: string }): Promise<ReconcileResponse> {
  const { data } = await apiClient.get<ReconcileResponse>('/admin/profit-report/reconcile', { params })
  return data
}

/** 绑定 sub2api 账号到供应商账号；两个字段都传空串即解绑。 */
export async function saveSupplierBinding(accountId: number, supplier: string, supplierAccount: string): Promise<void> {
  await apiClient.put(`/admin/profit-report/supplier-bindings/${accountId}`, { supplier, supplier_account: supplierAccount })
}

/** 导入账单：periods 内已有账单整月覆盖为 lines。 */
export async function replaceSupplierBills(periods: string[], lines: SupplierBillLine[]): Promise<void> {
  await apiClient.put('/admin/profit-report/supplier-bills', { periods, lines })
}

export const profitReportAPI = {
  getReport,
  getReconcile,
  saveSupplierBinding,
  replaceSupplierBills
}

export default profitReportAPI
