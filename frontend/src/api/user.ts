/**
 * User API endpoints
 * Handles user profile management, password changes, and phone binding
 */

import { apiClient } from './client'
import type { User, ChangePasswordRequest } from '@/types'

/**
 * Get current user profile
 * @returns User profile data
 */
export async function getProfile(): Promise<User> {
  const { data } = await apiClient.get<User>('/user/profile')
  return data
}

/**
 * Update current user profile
 * @param profile - Profile data to update
 * @returns Updated user profile data
 */
export async function updateProfile(profile: {
  username?: string
}): Promise<User> {
  const { data } = await apiClient.put<User>('/user', profile)
  return data
}

/**
 * Change current user password
 * @param passwords - Old and new password
 * @returns Success message
 */
export async function changePassword(
  oldPassword: string,
  newPassword: string
): Promise<{ message: string }> {
  const payload: ChangePasswordRequest = {
    old_password: oldPassword,
    new_password: newPassword
  }

  const { data } = await apiClient.put<{ message: string }>('/user/password', payload)
  return data
}

/**
 * Send phone verification code
 * @param phoneNumber - Phone number in E.164 or CN format
 * @returns Message and countdown seconds
 */
export async function sendPhoneCode(phoneNumber: string): Promise<{ message: string; countdown: number }> {
  const { data } = await apiClient.post<{ message: string; countdown: number }>('/user/phone/send-code', {
    phone_number: phoneNumber
  })
  return data
}

export interface BindPhoneResponse {
  user: User
  bonus_amount: number
}

/**
 * Bind phone number with SMS verification code
 * @param phoneNumber - Phone number in E.164 or CN format
 * @param verifyCode - 6-digit SMS verification code
 * @returns Updated user profile and granted bonus amount
 */
export async function bindPhone(phoneNumber: string, verifyCode: string): Promise<BindPhoneResponse> {
  const { data } = await apiClient.post<BindPhoneResponse>('/user/phone/bind', {
    phone_number: phoneNumber,
    verify_code: verifyCode
  })
  return data
}

export const userAPI = {
  getProfile,
  updateProfile,
  changePassword,
  sendPhoneCode,
  bindPhone
}

export default userAPI
