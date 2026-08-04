import { apiClient } from './client'

export interface PublicModelPricing {
  model: string
  input_per_mtok_u: number
  output_per_mtok_u: number
  /**
   * Highest rate a cache write can bill at — models with per-TTL cache-write rates are
   * quoted at their dearest. 0 when the model has no cache pricing at all.
   */
  cache_create_per_mtok_u: number
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
