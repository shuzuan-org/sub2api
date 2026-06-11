/**
 * Admin Channel Activities (渠道活动) API endpoints
 */

import { apiClient } from '../client'
import type {
  ChannelInviteBatch,
  ChannelInviteCodeUsage,
  CreateChannelInviteBatchRequest,
  UpdateChannelInviteBatchRequest,
  BasePaginationResponse
} from '@/types'

export async function listBatches(
  page: number = 1,
  pageSize: number = 20,
  filters?: {
    status?: string
    search?: string
  }
): Promise<BasePaginationResponse<ChannelInviteBatch>> {
  const { data } = await apiClient.get<BasePaginationResponse<ChannelInviteBatch>>('/admin/channel-invite/batches', {
    params: { page, page_size: pageSize, ...filters }
  })
  return data
}

export async function getBatch(id: number): Promise<ChannelInviteBatch> {
  const { data } = await apiClient.get<ChannelInviteBatch>(`/admin/channel-invite/batches/${id}`)
  return data
}

export async function createBatch(request: CreateChannelInviteBatchRequest): Promise<ChannelInviteBatch> {
  const { data } = await apiClient.post<ChannelInviteBatch>('/admin/channel-invite/batches', request)
  return data
}

export async function updateBatch(id: number, request: UpdateChannelInviteBatchRequest): Promise<ChannelInviteBatch> {
  const { data } = await apiClient.put<ChannelInviteBatch>(`/admin/channel-invite/batches/${id}`, request)
  return data
}

export async function deleteBatch(id: number): Promise<{ message: string }> {
  const { data } = await apiClient.delete<{ message: string }>(`/admin/channel-invite/batches/${id}`)
  return data
}

export async function listUsages(
  batchId: number,
  page: number = 1,
  pageSize: number = 20
): Promise<BasePaginationResponse<ChannelInviteCodeUsage>> {
  const { data } = await apiClient.get<BasePaginationResponse<ChannelInviteCodeUsage>>(
    `/admin/channel-invite/batches/${batchId}/usages`,
    { params: { page, page_size: pageSize } }
  )
  return data
}

const channelActivitiesAPI = {
  listBatches,
  getBatch,
  createBatch,
  updateBatch,
  deleteBatch,
  listUsages
}

export default channelActivitiesAPI
