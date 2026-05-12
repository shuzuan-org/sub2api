/**
 * Alipay payment API endpoints
 */

import { apiClient } from './client'

export interface PaymentPackage {
  id: number
  name: string
  cny_amount: number
  usd_amount: number
}

export interface AlipayOrder {
  order_no: string
  status: 'pending' | 'paid' | 'expired' | 'refunded'
  cny_fee: number
  usd_amount: number
  paid_at: string | null
  expires_at: string
  created_at: string
}

export interface AlipayCreateOrderResponse {
  order_no: string
  qr_code: string
  expires_at: string
}

// ---- 支付宝 ----

async function alipayGetPackages(): Promise<PaymentPackage[]> {
  const { data } = await apiClient.get<PaymentPackage[]>('/payments/alipay/packages')
  return data
}

async function alipayCreateOrder(packageId: number, cnyAmount?: number): Promise<AlipayCreateOrderResponse> {
  const body: Record<string, unknown> = {}
  if (packageId > 0) {
    body.package_id = packageId
  } else if (cnyAmount && cnyAmount > 0) {
    body.cny_amount = cnyAmount
  }
  const { data } = await apiClient.post<AlipayCreateOrderResponse>('/payments/alipay/create-order', body)
  return data
}

async function alipayGetOrderStatus(orderNo: string): Promise<AlipayOrder> {
  const { data } = await apiClient.get<AlipayOrder>(`/payments/alipay/order/${orderNo}`)
  return data
}

async function alipayListOrders(page = 1, pageSize = 20): Promise<{
  items: AlipayOrder[]
  total: number
}> {
  const { data } = await apiClient.get<{ items: AlipayOrder[]; total: number }>(
    '/payments/alipay/orders',
    { params: { page, page_size: pageSize } }
  )
  return data
}

export const alipayAPI = {
  getPackages: alipayGetPackages,
  createOrder: alipayCreateOrder,
  getOrderStatus: alipayGetOrderStatus,
  listOrders: alipayListOrders
}
