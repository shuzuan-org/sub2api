import { apiClient } from './client'

export interface PublicModelPricing {
  model: string
  input_per_mtok_u: number
  output_per_mtok_u: number
  /** 0 when the model has no cache pricing (no prompt caching, or no price for it). */
  cache_create_per_mtok_u: number
  /** 0 unless the model bills 1-hour cache writes at a separate, higher rate. */
  cache_create_1h_per_mtok_u: number
  cache_read_per_mtok_u: number
  original_input_per_mtok_u: number
  original_output_per_mtok_u: number
  original_cache_create_per_mtok_u: number
  original_cache_read_per_mtok_u: number
  discount_percent: number
}

export interface PublicGroupPricing {
  group_name: string
  platform: string
  rate_multiplier: number
  models: PublicModelPricing[]
}

export interface PublicPricingResponse {
  groups: PublicGroupPricing[]
  updated_at: string
}

export async function getPublicModelPricing(): Promise<PublicPricingResponse> {
  const { data } = await apiClient.get<PublicPricingResponse>('/pricing/models')
  return data
}
