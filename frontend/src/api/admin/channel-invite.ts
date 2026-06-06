import { apiClient } from '../client'
import type { ChannelInviteBatch, ChannelInviteCode, ChannelInviteCodeUsage } from '@/types/index'
import type { PaginatedResponse } from '@/types/index'

export async function listBatches(
  page = 1,
  pageSize = 20,
  filters?: { status?: string; search?: string }
): Promise<PaginatedResponse<ChannelInviteBatch>> {
  const { data } = await apiClient.get<PaginatedResponse<ChannelInviteBatch>>(
    '/admin/channel-invite/batches',
    { params: { page, page_size: pageSize, ...filters } }
  )
  return data
}

export async function getBatch(id: number): Promise<ChannelInviteBatch> {
  const { data } = await apiClient.get<ChannelInviteBatch>(
    `/admin/channel-invite/batches/${id}`
  )
  return data
}

export async function createBatch(input: {
  name: string
  bonus_amount: number
  max_uses_per_code?: number
  start_time?: number
  end_time?: number
  notes?: string
  created_by: number
  group_ids?: number[]
}): Promise<ChannelInviteBatch> {
  const { data } = await apiClient.post<ChannelInviteBatch>(
    '/admin/channel-invite/batches',
    input
  )
  return data
}

export async function updateBatch(
  id: number,
  input: {
    name?: string
    bonus_amount?: number
    max_uses_per_code?: number
    start_time?: number
    end_time?: number
    status?: string
    notes?: string
    group_ids?: number[]
  }
): Promise<ChannelInviteBatch> {
  const { data } = await apiClient.put<ChannelInviteBatch>(
    `/admin/channel-invite/batches/${id}`,
    input
  )
  return data
}

export async function deleteBatch(id: number): Promise<void> {
  await apiClient.delete(`/admin/channel-invite/batches/${id}`)
}

export async function generateCodes(
  batchId: number,
  count: number
): Promise<ChannelInviteCode[]> {
  const { data } = await apiClient.post<ChannelInviteCode[]>(
    `/admin/channel-invite/batches/${batchId}/generate-codes`,
    { count }
  )
  return data
}

export async function listCodes(
  batchId: number,
  page = 1,
  pageSize = 50,
  filters?: { status?: string; search?: string }
): Promise<PaginatedResponse<ChannelInviteCode>> {
  const { data } = await apiClient.get<PaginatedResponse<ChannelInviteCode>>(
    `/admin/channel-invite/batches/${batchId}/codes`,
    { params: { page, page_size: pageSize, ...filters } }
  )
  return data
}

export async function listUsages(
  batchId: number,
  page = 1,
  pageSize = 20
): Promise<PaginatedResponse<ChannelInviteCodeUsage>> {
  const { data } = await apiClient.get<PaginatedResponse<ChannelInviteCodeUsage>>(
    `/admin/channel-invite/batches/${batchId}/usages`,
    { params: { page, page_size: pageSize } }
  )
  return data
}
