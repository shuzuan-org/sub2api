/**
 * Admin Alipay API endpoints
 */

import { apiClient } from '../client'
import type { PaymentPackage } from '../payment'

export type AlipayPackage = PaymentPackage

export interface AlipayConfigResponse {
  mode: string
  app_id: string
  seller_id: string
  notify_url: string
  enabled: boolean
  is_prod: boolean
  private_key_set: boolean
  public_key_set: boolean
  app_public_cert_set: boolean
  alipay_public_cert_set: boolean
  alipay_root_cert_set: boolean
  configured: boolean
}

export interface AlipayOrderRecord {
  id: number
  order_no: string
  user_id: number
  package_id: number
  cny_fee: number
  usd_amount: number
  status: string
  alipay_trade_no: string | null
  expires_at: string
  paid_at: string | null
  created_at: string
}

export async function getConfig(): Promise<AlipayConfigResponse> {
  const { data } = await apiClient.get<AlipayConfigResponse>('/admin/alipay/config')
  return data
}

export async function setEnabled(enabled: boolean): Promise<void> {
  await apiClient.put('/admin/alipay/enabled', { enabled })
}

export async function getPackages(): Promise<AlipayPackage[]> {
  const { data } = await apiClient.get<AlipayPackage[]>('/admin/alipay/packages')
  return data
}

export async function updatePackages(pkgs: AlipayPackage[]): Promise<void> {
  await apiClient.put('/admin/alipay/packages', pkgs)
}

export async function listOrders(page = 1, pageSize = 20, status = ''): Promise<{
  items: AlipayOrderRecord[]
  total: number
}> {
  const params: Record<string, unknown> = { page, page_size: pageSize }
  if (status) params.status = status
  const { data } = await apiClient.get<{ items: AlipayOrderRecord[]; total: number }>(
    '/admin/alipay/orders',
    { params }
  )
  return data
}

export const adminAlipayAPI = {
  getConfig,
  setEnabled,
  getPackages,
  updatePackages,
  listOrders
}

export default adminAlipayAPI
