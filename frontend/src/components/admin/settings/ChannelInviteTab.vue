<template>
  <div class="space-y-6">
    <!-- Header -->
    <div class="flex items-center justify-between">
      <div>
        <h2 class="text-lg font-semibold text-gray-900 dark:text-white">渠道邀请码管理</h2>
        <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">
          创建和管理渠道邀请码批次，配置优惠金额、目标分组和有效期
        </p>
      </div>
      <button type="button" class="btn btn-primary btn-sm" @click="openCreateDialog">
        创建批次
      </button>
    </div>

    <!-- Search & Filter -->
    <div class="flex items-center gap-3">
      <input
        v-model="searchQuery"
        type="text"
        placeholder="搜索批次名称..."
        class="input"
        @keyup.enter="onSearch"
      />
      <select v-model="statusFilter" class="input" @change="fetchBatches">
        <option value="">全部状态</option>
        <option value="active">启用</option>
        <option value="disabled">禁用</option>
      </select>
    </div>

    <!-- Batch List -->
    <div class="card overflow-hidden">
      <div class="overflow-x-auto">
      <table class="min-w-[980px] w-full text-left text-sm">
        <thead>
          <tr class="border-b border-gray-100 dark:border-dark-700">
            <th class="px-4 py-3 font-medium">批次名称</th>
            <th class="px-4 py-3 font-medium">优惠金额</th>
            <th class="px-4 py-3 font-medium">有效期</th>
            <th class="px-4 py-3 font-medium">码数量</th>
            <th class="px-4 py-3 font-medium">已使用</th>
            <th class="px-4 py-3 font-medium">状态</th>
            <th class="px-4 py-3 font-medium">操作</th>
          </tr>
        </thead>
        <tbody>
          <tr v-if="loading">
            <td colspan="7" class="px-4 py-8 text-center text-gray-400">加载中...</td>
          </tr>
          <tr v-else-if="batches.length === 0">
            <td colspan="7" class="px-4 py-8 text-center text-gray-400">暂无批次</td>
          </tr>
          <tr
            v-for="batch in batches"
            :key="batch.id"
            class="border-b border-gray-50 hover:bg-gray-50 dark:border-dark-700 dark:hover:bg-dark-800"
          >
            <td class="px-4 py-3 font-medium">{{ batch.name }}</td>
            <td class="px-4 py-3">{{ batch.bonus_amount }} U</td>
            <td class="px-4 py-3 text-xs text-gray-500">
              {{ batch.start_time ? fmtDate(batch.start_time) : '--' }}
              ~
              {{ batch.end_time ? fmtDate(batch.end_time) : '--' }}
            </td>
            <td class="px-4 py-3">{{ batch.code_count }}</td>
            <td class="px-4 py-3">{{ batch.used_count }}</td>
            <td class="px-4 py-3">
              <span
                :class="batch.status === 'active'
                  ? 'rounded-full bg-green-100 px-2 py-0.5 text-xs text-green-700'
                  : 'rounded-full bg-gray-100 px-2 py-0.5 text-xs text-gray-600'"
              >
                {{ batch.status === 'active' ? '启用' : '禁用' }}
              </span>
            </td>
            <td class="px-4 py-3">
              <div class="flex items-center gap-1">
                <button type="button" class="btn btn-ghost btn-xs" @click="openEditDialog(batch)">编辑</button>
                <button type="button" class="btn btn-ghost btn-xs" @click="openGenerateDialog(batch)">生成码</button>
                <button type="button" class="btn btn-ghost btn-xs" @click="openCodesDialog(batch)">码({{ batch.code_count }})</button>
                <button type="button" class="btn btn-ghost btn-xs" @click="openUsagesDialog(batch)">记录</button>
                <button type="button" class="btn btn-ghost btn-xs text-red-600" @click="confirmDelete(batch)">删除</button>
              </div>
            </td>
          </tr>
        </tbody>
      </table>
      </div>
      <!-- Pagination -->
      <div v-if="totalBatches > pageSize" class="flex items-center justify-between border-t px-4 py-3">
        <span class="text-sm text-gray-500">共 {{ totalBatches }} 个批次</span>
        <div class="flex items-center gap-2">
          <button
            type="button"
            class="btn btn-ghost btn-xs"
            :disabled="currentPage <= 1"
            @click="currentPage--; fetchBatches()"
          >
            上一页
          </button>
          <span class="text-sm">{{ currentPage }} / {{ Math.ceil(totalBatches / pageSize) }}</span>
          <button
            type="button"
            class="btn btn-ghost btn-xs"
            :disabled="currentPage >= Math.ceil(totalBatches / pageSize)"
            @click="currentPage++; fetchBatches()"
          >
            下一页
          </button>
        </div>
      </div>
    </div>

    <!-- Create/Edit Dialog -->
    <div v-if="showBatchDialog" class="fixed inset-0 z-50 flex items-center justify-center bg-black/50" @click.self="showBatchDialog = false">
      <div class="w-full max-w-lg rounded-lg bg-white p-6 shadow-xl dark:bg-dark-800">
        <h3 class="mb-4 text-lg font-semibold">{{ editingBatch ? '编辑批次' : '创建批次' }}</h3>
        <div class="space-y-3">
          <div>
            <label class="mb-1 block text-sm font-medium">批次名称 *</label>
            <input v-model="batchForm.name" class="input w-full" required />
          </div>
          <div>
            <label class="mb-1 block text-sm font-medium">优惠金额 (U) *</label>
            <input v-model.number="batchForm.bonus_amount" type="number" class="input w-full" min="0" required />
          </div>
          <div>
            <label class="mb-1 block text-sm font-medium">每码最大使用次数</label>
            <input v-model.number="batchForm.max_uses_per_code" type="number" class="input w-full" min="1" />
          </div>
          <div class="grid grid-cols-2 gap-3">
            <div>
              <label class="mb-1 block text-sm font-medium">开始时间</label>
              <input v-model="batchForm.start_time" type="datetime-local" class="input w-full" />
            </div>
            <div>
              <label class="mb-1 block text-sm font-medium">结束时间</label>
              <input v-model="batchForm.end_time" type="datetime-local" class="input w-full" />
            </div>
          </div>
          <div>
            <label class="mb-1 block text-sm font-medium">创建者用户 ID（默认当前管理员）*</label>
            <input v-model.number="batchForm.created_by" type="number" class="input w-full" required />
          </div>
          <div>
            <label class="mb-1 block text-sm font-medium">目标分组</label>
            <div class="max-h-32 overflow-y-auto rounded border p-2">
              <label v-for="g in groups" :key="g.id" class="flex items-center gap-2 py-1 text-sm">
                <input type="checkbox" :value="g.id" v-model="batchForm.group_ids" />
                {{ g.name }} ({{ g.platform }})
              </label>
              <span v-if="groups.length === 0" class="text-sm text-gray-400">加载分组中...</span>
            </div>
          </div>
          <div>
            <label class="mb-1 block text-sm font-medium">备注</label>
            <textarea v-model="batchForm.notes" class="input w-full" rows="2"></textarea>
          </div>
        </div>
        <div
          v-if="batchError"
          class="mt-4 rounded-lg border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700 dark:border-red-800 dark:bg-red-900/20 dark:text-red-300"
        >
          {{ batchError }}
        </div>
        <div class="mt-4 flex justify-end gap-2">
          <button type="button" class="btn btn-ghost" @click="showBatchDialog = false">取消</button>
          <button type="button" class="btn btn-primary" :disabled="batchSaving" @click="saveBatch">
            {{ batchSaving ? '保存中...' : '保存' }}
          </button>
        </div>
      </div>
    </div>

    <!-- Generate Codes Dialog -->
    <div v-if="showGenerateDialog" class="fixed inset-0 z-50 flex items-center justify-center bg-black/50" @click.self="showGenerateDialog = false">
      <div class="w-full max-w-md rounded-lg bg-white p-6 shadow-xl dark:bg-dark-800">
        <h3 class="mb-4 text-lg font-semibold">生成邀请码</h3>
        <p class="mb-3 text-sm text-gray-500">为批次 <strong>{{ generateTarget?.name }}</strong> 生成邀请码</p>
        <div class="space-y-3">
          <div>
            <label class="mb-1 block text-sm font-medium">生成数量 (1-500)</label>
            <input v-model.number="generateCount" type="number" class="input w-full" min="1" max="500" />
          </div>
          <div v-if="generatedCodes.length > 0">
            <p class="mb-1 text-sm font-medium">已生成的邀请码:</p>
            <div class="max-h-48 overflow-y-auto rounded border bg-gray-50 p-2 dark:bg-dark-700">
              <div v-for="code in generatedCodes" :key="code.id" class="flex items-center justify-between py-1 font-mono text-sm">
                <span>{{ code.code }}</span>
                <button type="button" class="btn btn-ghost btn-xs" @click="copyToClipboard(code.code)">复制</button>
              </div>
            </div>
          </div>
        </div>
        <div class="mt-4 flex justify-end gap-2">
          <button type="button" class="btn btn-ghost" @click="showGenerateDialog = false">关闭</button>
          <button type="button" class="btn btn-primary" :disabled="generating" @click="doGenerateCodes">
            {{ generating ? '生成中...' : '生成' }}
          </button>
        </div>
      </div>
    </div>

    <!-- Codes List Dialog -->
    <div v-if="showCodesDialog" class="fixed inset-0 z-50 flex items-center justify-center bg-black/50" @click.self="showCodesDialog = false">
      <div class="w-full max-w-2xl rounded-lg bg-white p-6 shadow-xl dark:bg-dark-800">
        <h3 class="mb-4 text-lg font-semibold">{{ codesTarget?.name }} 的邀请码</h3>
        <div class="overflow-x-auto">
        <table class="min-w-[620px] w-full text-left text-sm">
          <thead>
            <tr class="border-b">
              <th class="px-3 py-2 font-medium">邀请码</th>
              <th class="px-3 py-2 font-medium">状态</th>
              <th class="px-3 py-2 font-medium">已用/上限</th>
              <th class="px-3 py-2 font-medium">操作</th>
            </tr>
          </thead>
          <tbody>
            <tr v-if="codesLoading"><td colspan="4" class="px-3 py-4 text-center text-gray-400">加载中...</td></tr>
            <tr v-else-if="codes.length === 0"><td colspan="4" class="px-3 py-4 text-center text-gray-400">暂无邀请码</td></tr>
            <tr v-for="c in codes" :key="c.id" class="border-b border-gray-50">
              <td class="px-3 py-2 font-mono text-sm">{{ c.code }}</td>
              <td class="px-3 py-2">
                <span :class="c.status === 'unused' ? 'text-green-600' : 'text-gray-400'">
                  {{ c.status === 'unused' ? '未使用' : c.status === 'used' ? '已使用' : '已过期' }}
                </span>
              </td>
              <td class="px-3 py-2">{{ c.used_count }} / {{ c.max_uses }}</td>
              <td class="px-3 py-2">
                <button type="button" class="btn btn-ghost btn-xs" @click="copyToClipboard(c.code)">复制</button>
              </td>
            </tr>
          </tbody>
        </table>
        </div>
        <div class="mt-4 flex justify-end">
          <button type="button" class="btn btn-ghost" @click="showCodesDialog = false">关闭</button>
        </div>
      </div>
    </div>

    <!-- Usages Dialog -->
    <div v-if="showUsagesDialog" class="fixed inset-0 z-50 flex items-center justify-center bg-black/50" @click.self="showUsagesDialog = false">
      <div class="w-full max-w-2xl rounded-lg bg-white p-6 shadow-xl dark:bg-dark-800">
        <h3 class="mb-4 text-lg font-semibold">{{ usagesTarget?.name }} 的使用记录</h3>
        <div class="overflow-x-auto">
        <table class="min-w-[620px] w-full text-left text-sm">
          <thead>
            <tr class="border-b">
              <th class="px-3 py-2 font-medium">用户</th>
              <th class="px-3 py-2 font-medium">奖励状态</th>
              <th class="px-3 py-2 font-medium">兑换时间</th>
            </tr>
          </thead>
          <tbody>
            <tr v-if="usagesLoading"><td colspan="3" class="px-3 py-4 text-center text-gray-400">加载中...</td></tr>
            <tr v-else-if="usages.length === 0"><td colspan="3" class="px-3 py-4 text-center text-gray-400">暂无使用记录</td></tr>
            <tr v-for="u in usages" :key="u.id" class="border-b border-gray-50">
              <td class="px-3 py-2">{{ u.user?.email || u.user?.username || '#' + u.user_id }}</td>
              <td class="px-3 py-2">
                <span :class="u.bonus_granted ? 'text-green-600' : 'text-amber-600'">
                  {{ u.bonus_granted ? '已发放' : '待发放' }}
                </span>
              </td>
              <td class="px-3 py-2 text-xs">{{ fmtDate(u.claimed_at) }}</td>
            </tr>
          </tbody>
        </table>
        </div>
        <div class="mt-4 flex justify-end">
          <button type="button" class="btn btn-ghost" @click="showUsagesDialog = false">关闭</button>
        </div>
      </div>
    </div>

    <!-- Delete Confirmation -->
    <div v-if="showDeleteConfirm" class="fixed inset-0 z-50 flex items-center justify-center bg-black/50" @click.self="showDeleteConfirm = false">
      <div class="w-full max-w-sm rounded-lg bg-white p-6 shadow-xl dark:bg-dark-800">
        <h3 class="mb-2 text-lg font-semibold">确认删除</h3>
        <p class="mb-4 text-sm text-gray-500">确定要删除批次 "{{ deleteTarget?.name }}" 吗？此操作不可撤销。</p>
        <div class="flex justify-end gap-2">
          <button type="button" class="btn btn-ghost" @click="showDeleteConfirm = false">取消</button>
          <button type="button" class="btn btn-danger" :disabled="deleting" @click="doDelete">
            {{ deleting ? '删除中...' : '删除' }}
          </button>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import {
  listBatches,
  createBatch,
  updateBatch,
  deleteBatch,
  generateCodes as apiGenerateCodes,
  listCodes,
  listUsages,
} from '@/api/admin/channel-invite'
import { list as listGroups } from '@/api/admin/groups'
import type { ChannelInviteBatch, ChannelInviteCode, ChannelInviteCodeUsage, Group } from '@/types/index'
import { useAuthStore } from '@/stores/auth'

const authStore = useAuthStore()

// State
const loading = ref(true)
const batches = ref<ChannelInviteBatch[]>([])
const totalBatches = ref(0)
const currentPage = ref(1)
const pageSize = ref(20)
const searchQuery = ref('')
const statusFilter = ref('')

// Batch dialog
const showBatchDialog = ref(false)
const editingBatch = ref<ChannelInviteBatch | null>(null)
const batchSaving = ref(false)
const batchError = ref('')
const batchForm = ref({
  name: '',
  bonus_amount: 0,
  max_uses_per_code: 1,
  start_time: '',
  end_time: '',
  notes: '',
  created_by: 0,
  group_ids: [] as number[],
})

// Groups
const groups = ref<Group[]>([])

// Generate dialog
const showGenerateDialog = ref(false)
const generateTarget = ref<ChannelInviteBatch | null>(null)
const generateCount = ref(10)
const generating = ref(false)
const generatedCodes = ref<ChannelInviteCode[]>([])

// Codes dialog
const showCodesDialog = ref(false)
const codesTarget = ref<ChannelInviteBatch | null>(null)
const codes = ref<ChannelInviteCode[]>([])
const totalCodes = ref(0)
const codesLoading = ref(false)

// Usages dialog
const showUsagesDialog = ref(false)
const usagesTarget = ref<ChannelInviteBatch | null>(null)
const usages = ref<ChannelInviteCodeUsage[]>([])
const totalUsages = ref(0)
const usagesLoading = ref(false)

// Delete
const showDeleteConfirm = ref(false)
const deleteTarget = ref<ChannelInviteBatch | null>(null)
const deleting = ref(false)

function fmtDate(dateStr: string) {
  return new Date(dateStr).toLocaleString()
}

function copyToClipboard(text: string) {
  navigator.clipboard.writeText(text).catch(() => {})
}

// API calls
async function fetchBatches() {
  loading.value = true
  try {
    const result = await listBatches(currentPage.value, pageSize.value, {
      status: statusFilter.value || undefined,
      search: searchQuery.value || undefined,
    })
    batches.value = result.items
    totalBatches.value = result.total
  } catch {
    // handled by interceptor
  } finally {
    loading.value = false
  }
}

async function fetchGroups() {
  try {
    const result = await listGroups(1, 500)
    groups.value = result.items
  } catch {
    // ignore
  }
}

function onSearch() {
  currentPage.value = 1
  fetchBatches()
}

function getCurrentUserID(): number {
  if (authStore.user?.id) return authStore.user.id
  try {
    const saved = localStorage.getItem('auth_user')
    if (saved) {
      const parsed = JSON.parse(saved)
      return Number(parsed?.id || 0)
    }
  } catch {
    // ignore
  }
  return 0
}

function extractErrorMessage(error: unknown): string {
  if (error && typeof error === 'object' && 'message' in error) {
    return String((error as { message?: unknown }).message || '')
  }
  return ''
}

// Batch CRUD
function openCreateDialog() {
  editingBatch.value = null
  batchError.value = ''
  batchForm.value = {
    name: '',
    bonus_amount: 0,
    max_uses_per_code: 1,
    start_time: '',
    end_time: '',
    notes: '',
    created_by: getCurrentUserID(),
    group_ids: [],
  }
  showBatchDialog.value = true
}

function openEditDialog(batch: ChannelInviteBatch) {
  editingBatch.value = batch
  batchError.value = ''
  batchForm.value = {
    name: batch.name,
    bonus_amount: batch.bonus_amount,
    max_uses_per_code: batch.max_uses_per_code,
    start_time: batch.start_time ? batch.start_time.slice(0, 16) : '',
    end_time: batch.end_time ? batch.end_time.slice(0, 16) : '',
    notes: batch.notes || '',
    created_by: batch.created_by,
    group_ids: batch.groups?.map((g: Group) => g.id) || [],
  }
  showBatchDialog.value = true
}

async function saveBatch() {
  batchError.value = ''

  const name = batchForm.value.name.trim()
  const bonusAmount = Number(batchForm.value.bonus_amount)
  const maxUsesPerCode = Number(batchForm.value.max_uses_per_code || 1)
  const createdBy = Number(batchForm.value.created_by)

  if (!name) {
    batchError.value = '请填写批次名称'
    return
  }
  if (!Number.isFinite(bonusAmount) || bonusAmount <= 0) {
    batchError.value = '请填写大于 0 的优惠金额'
    return
  }
  if (!Number.isFinite(maxUsesPerCode) || maxUsesPerCode < 1) {
    batchError.value = '每码最大使用次数必须大于等于 1'
    return
  }
  if (!Number.isFinite(createdBy) || createdBy <= 0) {
    batchError.value = '请填写有效的创建者用户 ID'
    return
  }

  const startAt = batchForm.value.start_time
    ? Math.floor(new Date(batchForm.value.start_time).getTime() / 1000)
    : undefined
  const endAt = batchForm.value.end_time
    ? Math.floor(new Date(batchForm.value.end_time).getTime() / 1000)
    : undefined

  if (startAt && endAt && endAt <= startAt) {
    batchError.value = '结束时间必须晚于开始时间'
    return
  }

  batchSaving.value = true
  try {
    const input: any = {
      name,
      bonus_amount: bonusAmount,
      max_uses_per_code: maxUsesPerCode,
      notes: batchForm.value.notes,
      created_by: createdBy,
      group_ids: batchForm.value.group_ids,
      start_time: startAt,
      end_time: endAt,
    }

    if (editingBatch.value) {
      await updateBatch(editingBatch.value.id, input)
    } else {
      await createBatch(input)
    }
    showBatchDialog.value = false
    await fetchBatches()
  } catch (error) {
    batchError.value = extractErrorMessage(error) || '保存失败，请检查填写内容后重试'
  } finally {
    batchSaving.value = false
  }
}

// Generate codes
function openGenerateDialog(batch: ChannelInviteBatch) {
  generateTarget.value = batch
  generateCount.value = 10
  generatedCodes.value = []
  showGenerateDialog.value = true
}

async function doGenerateCodes() {
  if (!generateTarget.value) return
  generating.value = true
  try {
    const codes = await apiGenerateCodes(generateTarget.value.id, generateCount.value)
    generatedCodes.value = codes
  } catch {
    // handled
  } finally {
    generating.value = false
  }
}

// Codes list
async function openCodesDialog(batch: ChannelInviteBatch) {
  codesTarget.value = batch
  codesLoading.value = true
  showCodesDialog.value = true
  try {
    const result = await listCodes(batch.id, 1, 500)
    codes.value = result.items
    totalCodes.value = result.total
  } catch {
    //
  } finally {
    codesLoading.value = false
  }
}

// Usages
async function openUsagesDialog(batch: ChannelInviteBatch) {
  usagesTarget.value = batch
  usagesLoading.value = true
  showUsagesDialog.value = true
  try {
    const result = await listUsages(batch.id, 1, 500)
    usages.value = result.items
    totalUsages.value = result.total
  } catch {
    //
  } finally {
    usagesLoading.value = false
  }
}

// Delete
function confirmDelete(batch: ChannelInviteBatch) {
  deleteTarget.value = batch
  showDeleteConfirm.value = true
}

async function doDelete() {
  if (!deleteTarget.value) return
  deleting.value = true
  try {
    await deleteBatch(deleteTarget.value.id)
    showDeleteConfirm.value = false
    fetchBatches()
  } catch {
    //
  } finally {
    deleting.value = false
  }
}

onMounted(() => {
  fetchBatches()
  fetchGroups()
})
</script>
