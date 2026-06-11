<template>
  <div class="space-y-6">
    <!-- 搜索和操作栏 -->
    <div class="mb-4 flex flex-wrap items-center gap-3">
      <input
        v-model="searchText"
        :placeholder="t('admin.channelActivity.searchPlaceholder')"
        class="input w-64"
        @keyup.enter="loadBatches"
      />
      <select v-model="statusFilter" class="select w-32" @change="loadBatches">
        <option value="">{{ t('admin.channelActivity.allStatuses') }}</option>
        <option value="active">{{ t('admin.channelActivity.statusActive') }}</option>
        <option value="disabled">{{ t('admin.channelActivity.statusDisabled') }}</option>
      </select>
      <button class="btn btn-primary btn-sm" @click="loadBatches">
        {{ t('common.search') }}
      </button>
      <div class="flex-1"></div>
      <button class="btn btn-primary" @click="openCreateModal">
        {{ t('admin.channelActivity.createActivity') }}
      </button>
    </div>

    <!-- 活动列表表格 -->
    <div class="card overflow-hidden">
      <table class="w-full">
        <thead>
          <tr class="border-b border-gray-200 dark:border-gray-700 text-left text-xs font-medium text-gray-500 dark:text-gray-400">
            <th class="px-4 py-3">ID</th>
            <th class="px-4 py-3">{{ t('admin.channelActivity.name') }}</th>
            <th class="px-4 py-3">{{ t('admin.channelActivity.status') }}</th>
            <th class="px-4 py-3">{{ t('admin.channelActivity.timeRange') }}</th>
            <th class="px-4 py-3">{{ t('admin.channelActivity.bonus') }}</th>
            <th class="px-4 py-3">{{ t('admin.channelActivity.inviteCode') }}</th>
            <th class="px-4 py-3">{{ t('admin.channelActivity.usage') }}</th>
            <th class="px-4 py-3">{{ t('admin.channelActivity.creator') }}</th>
            <th class="px-4 py-3">{{ t('admin.channelActivity.actions') }}</th>
          </tr>
        </thead>
        <tbody>
          <tr v-if="loading" class="border-b border-gray-100 dark:border-gray-800">
            <td colspan="9" class="px-4 py-8 text-center text-gray-500">
              {{ t('common.loading') }}
            </td>
          </tr>
          <tr v-else-if="batches.length === 0" class="border-b border-gray-100 dark:border-gray-800">
            <td colspan="9" class="px-4 py-8 text-center text-gray-500">
              {{ t('admin.channelActivity.noActivities') }}
            </td>
          </tr>
          <template v-for="batch in batches" :key="batch.id">
            <tr class="border-b border-gray-100 dark:border-gray-800 hover:bg-gray-50 dark:hover:bg-gray-800/50 transition-colors">
              <td class="px-4 py-3 text-sm text-gray-600 dark:text-gray-400">{{ batch.id }}</td>
              <td class="px-4 py-3 text-sm font-medium">{{ batch.name }}</td>
              <td class="px-4 py-3">
                <span
                  :class="[
                    'badge text-xs',
                    batch.status === 'active' ? 'badge-success' : 'badge-gray'
                  ]"
                >
                  {{ batch.status === 'active' ? t('admin.channelActivity.statusActive') : t('admin.channelActivity.statusDisabled') }}
                </span>
              </td>
              <td class="px-4 py-3 text-xs text-gray-500">
                <span v-if="batch.start_time">{{ formatDate(batch.start_time) }}</span>
                <span v-else class="text-gray-400">-</span>
                <br />
                <span v-if="batch.end_time">{{ formatDate(batch.end_time) }}</span>
                <span v-else class="text-gray-400">-</span>
              </td>
              <td class="px-4 py-3 text-sm">{{ batch.bonus_amount }}U</td>
              <td class="px-4 py-3 text-xs font-mono text-blue-600 dark:text-blue-400">
                {{ (batch.codes && batch.codes.length > 0) ? batch.codes[0].code : '-' }}
              </td>
              <td class="px-4 py-3 text-xs text-gray-500">
                <div class="flex items-center gap-2">
                  <span>{{ batch.used_count }} / {{ batch.max_uses_per_code }}</span>
                  <button
                    v-if="batch.used_count > 0"
                    class="text-xs text-primary-600 hover:underline"
                    @click="toggleUsages(batch.id)"
                  >
                    {{ expandedBatchId === batch.id ? t('common.collapse') : t('admin.channelActivity.viewUsages') }}
                  </button>
                </div>
              </td>
              <td class="px-4 py-3 text-xs text-gray-500">
                {{ batch.creator?.email || '-' }}
              </td>
              <td class="px-4 py-3">
                <div class="flex gap-2">
                  <button class="btn btn-secondary btn-sm" @click="openEditModal(batch)">
                    {{ t('common.edit') }}
                  </button>
                  <button class="btn btn-danger btn-sm" @click="handleDelete(batch)">
                    {{ t('common.delete') }}
                  </button>
                </div>
              </td>
            </tr>
            <!-- 使用记录展开行 -->
            <tr v-if="expandedBatchId === batch.id" :key="'usage-'+batch.id" class="border-b border-gray-100 dark:border-gray-800 bg-gray-50 dark:bg-gray-800/50">
              <td colspan="9" class="px-4 py-3">
                <div v-if="expandedUsagesLoading" class="text-xs text-gray-500">{{ t('common.loading') }}</div>
                <div v-else-if="expandedUsages.length === 0" class="text-xs text-gray-500">{{ t('admin.channelActivity.noUsages') }}</div>
                <table v-else class="w-full text-xs">
                  <thead>
                    <tr class="text-left text-gray-500 font-medium">
                      <th class="py-1 pr-4">ID</th>
                      <th class="py-1 pr-4">{{ t('admin.channelActivity.user') }}</th>
                      <th class="py-1 pr-4">{{ t('admin.channelActivity.bonusGranted') }}</th>
                      <th class="py-1 pr-4">{{ t('admin.channelActivity.claimedAt') }}</th>
                    </tr>
                  </thead>
                  <tbody>
                    <tr v-for="u in expandedUsages" :key="u.id">
                      <td class="py-1 pr-4 text-gray-500">{{ u.id }}</td>
                      <td class="py-1 pr-4">{{ u.user?.email || '-' }}</td>
                      <td class="py-1 pr-4">
                        <span :class="['badge text-xs', u.bonus_granted ? 'badge-success' : 'badge-warning']">
                          {{ u.bonus_granted ? t('common.yes') : t('common.no') }}
                        </span>
                      </td>
                      <td class="py-1 pr-4 text-gray-500">{{ formatDate(u.claimed_at) }}</td>
                    </tr>
                  </tbody>
                </table>
              </td>
            </tr>
          </template>
        </tbody>
      </table>
    </div>

    <!-- 分页 -->
    <div v-if="totalPages > 1" class="mt-4 flex justify-center items-center gap-2">
      <button class="btn btn-secondary btn-sm" :disabled="currentPage <= 1" @click="changePage(currentPage - 1)">
        {{ t('common.previous') }}
      </button>
      <span class="text-sm text-gray-500">{{ currentPage }} / {{ totalPages }}</span>
      <button class="btn btn-secondary btn-sm" :disabled="currentPage >= totalPages" @click="changePage(currentPage + 1)">
        {{ t('common.next') }}
      </button>
    </div>

    <!-- 创建/编辑活动 Modal -->
    <BaseDialog
      :show="showFormModal"
      :title="editingBatch ? t('admin.channelActivity.editActivity') : t('admin.channelActivity.createActivity')"
      width="normal"
      @close="closeFormModal"
    >
      <form @submit.prevent="handleSubmitForm" class="space-y-5">
        <!-- 名称 -->
        <div>
          <label class="input-label">{{ t('admin.channelActivity.form.name') }} *</label>
          <input v-model="form.name" type="text" class="input w-full" required :placeholder="t('admin.channelActivity.form.namePlaceholder')" />
        </div>

        <!-- 时间段 -->
        <div class="grid grid-cols-2 gap-4">
          <div>
            <label class="input-label">{{ t('admin.channelActivity.form.startTime') }}</label>
            <input v-model="form.start_time" type="datetime-local" class="input w-full" />
          </div>
          <div>
            <label class="input-label">{{ t('admin.channelActivity.form.endTime') }}</label>
            <input v-model="form.end_time" type="datetime-local" class="input w-full" />
          </div>
        </div>

        <!-- 单码积分 -->
        <div>
          <label class="input-label">{{ t('admin.channelActivity.form.bonusAmount') }} *</label>
          <input v-model.number="form.bonus_amount" type="number" class="input w-48" min="0" step="0.01" required />
          <p class="input-hint">{{ t('admin.channelActivity.form.bonusHint') }}</p>
        </div>

        <!-- 单人邀请码数量（每码最大使用次数） -->
        <div>
          <label class="input-label">{{ t('admin.channelActivity.form.maxUsesPerCode') }}</label>
          <input v-model.number="form.max_uses_per_code" type="number" class="input w-48" min="1" />
          <p class="input-hint">{{ t('admin.channelActivity.form.maxUsesHint') }}</p>
        </div>

        <!-- 邀请人（邮箱搜索选择） -->
        <div>
          <label class="input-label">{{ t('admin.channelActivity.form.inviter') }}</label>
          <div class="relative">
            <div v-if="selectedInviter" class="mb-2 flex items-center gap-2">
              <span class="badge badge-info text-xs">{{ selectedInviter.email }}</span>
              <button type="button" class="text-xs text-red-500 hover:text-red-700" @click="clearInviter">
                {{ t('common.clear') }}
              </button>
            </div>
            <input
              v-if="!selectedInviter"
              v-model="inviterSearch"
              type="text"
              class="input w-full"
              :placeholder="t('admin.channelActivity.form.inviterPlaceholder')"
              @input="handleInviterSearch"
              @focus="handleInviterSearch"
            />
            <div
              v-if="!selectedInviter && inviterResults.length > 0"
              class="absolute z-10 mt-1 w-full bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-md shadow-lg max-h-48 overflow-y-auto"
            >
              <div
                v-for="user in inviterResults"
                :key="user.id"
                class="px-3 py-2 cursor-pointer hover:bg-gray-100 dark:hover:bg-gray-700 text-sm"
                @click="selectInviter(user)"
              >
                {{ user.email }}
              </div>
            </div>
          </div>
        </div>

        <!-- 活动文案 -->
        <div>
          <label class="input-label">{{ t('admin.channelActivity.form.copyText') }}</label>
          <textarea v-model="form.activity_copy_text" class="input w-full" rows="3" :placeholder="t('admin.channelActivity.form.copyTextPlaceholder')"></textarea>
        </div>
      </form>

      <template #footer>
        <div class="flex justify-end gap-3">
          <button class="btn btn-secondary" @click="closeFormModal">
            {{ t('common.cancel') }}
          </button>
          <button class="btn btn-primary" :disabled="saving" @click="handleSubmitForm">
            {{ saving ? t('common.saving') : t('common.save') }}
          </button>
        </div>
      </template>
    </BaseDialog>
  </div>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores'
import { adminAPI } from '@/api'
import BaseDialog from '@/components/common/BaseDialog.vue'
import type {
  ChannelInviteBatch,
  ChannelInviteCodeUsage,
  AdminUser
} from '@/types'

const { t } = useI18n()
const appStore = useAppStore()

// ---------- 活动列表 ----------
const loading = ref(false)
const batches = ref<ChannelInviteBatch[]>([])
const searchText = ref('')
const statusFilter = ref('')
const currentPage = ref(1)
const totalPages = ref(1)
const pageSize = 20

async function loadBatches() {
  loading.value = true
  try {
    const filters: { status?: string; search?: string } = {}
    if (statusFilter.value) filters.status = statusFilter.value
    if (searchText.value.trim()) filters.search = searchText.value.trim()

    const result = await adminAPI.channelActivities.listBatches(currentPage.value, pageSize, filters)
    batches.value = result.data || []
    totalPages.value = Math.ceil((result.total || 0) / pageSize)
  } catch {
    batches.value = []
  } finally {
    loading.value = false
  }
}

function changePage(page: number) {
  currentPage.value = page
  loadBatches()
}

// ---------- 使用记录展开 ----------
const expandedBatchId = ref<number | null>(null)
const expandedUsages = ref<ChannelInviteCodeUsage[]>([])
const expandedUsagesLoading = ref(false)

async function toggleUsages(batchId: number) {
  if (expandedBatchId.value === batchId) {
    expandedBatchId.value = null
    return
  }
  expandedBatchId.value = batchId
  expandedUsagesLoading.value = true
  try {
    const result = await adminAPI.channelActivities.listUsages(batchId)
    expandedUsages.value = result.data || []
  } catch {
    expandedUsages.value = []
  } finally {
    expandedUsagesLoading.value = false
  }
}

// ---------- 创建/编辑 Modal ----------
const showFormModal = ref(false)
const editingBatch = ref<ChannelInviteBatch | null>(null)
const saving = ref(false)
const form = ref({
  name: '',
  start_time: '',
  end_time: '',
  bonus_amount: 0,
  max_uses_per_code: 100,
  created_by: 0,
  activity_copy_text: '',
})
function openCreateModal() {
  editingBatch.value = null
  form.value = {
    name: '',
    start_time: '',
    end_time: '',
    bonus_amount: 0,
    max_uses_per_code: 100,
    created_by: 0,
    activity_copy_text: '',
  }
  selectedInviter.value = null
  inviterSearch.value = ''
  inviterResults.value = []
  showFormModal.value = true
}

function openEditModal(batch: ChannelInviteBatch) {
  editingBatch.value = batch
  form.value = {
    name: batch.name,
    start_time: batch.start_time ? toDatetimeLocal(batch.start_time) : '',
    end_time: batch.end_time ? toDatetimeLocal(batch.end_time) : '',
    bonus_amount: batch.bonus_amount,
    max_uses_per_code: batch.max_uses_per_code,
    created_by: batch.created_by,
    activity_copy_text: batch.activity_copy_text || '',
  }
  selectedInviter.value = batch.creator || null
  inviterSearch.value = ''
  inviterResults.value = []
  showFormModal.value = true
}

function closeFormModal() {
  showFormModal.value = false
  editingBatch.value = null
}

async function handleSubmitForm() {
  saving.value = true
  try {
    const data: any = {
      name: form.value.name,
      bonus_amount: form.value.bonus_amount,
      max_uses_per_code: form.value.max_uses_per_code,
      notes: '',
      activity_copy_text: form.value.activity_copy_text,
      created_by: form.value.created_by,
      group_ids: []
    }

    if (form.value.start_time) {
      data.start_time = Math.floor(new Date(form.value.start_time).getTime() / 1000)
    }
    if (form.value.end_time) {
      data.end_time = Math.floor(new Date(form.value.end_time).getTime() / 1000)
    }

    if (editingBatch.value) {
      await adminAPI.channelActivities.updateBatch(editingBatch.value.id, data)
    } else {
      await adminAPI.channelActivities.createBatch(data)
    }

    closeFormModal()
    loadBatches()
  } catch (err: any) {
    const msg = err?.response?.data?.detail || err?.message || t('common.error')
    appStore.showError(msg)
  } finally {
    saving.value = false
  }
}

// ---------- 邀请人搜索 ----------
const inviterSearch = ref('')
const inviterResults = ref<AdminUser[]>([])
const selectedInviter = ref<AdminUser | null>(null)
let inviterSearchTimer: ReturnType<typeof setTimeout> | null = null

async function handleInviterSearch() {
  if (inviterSearchTimer) clearTimeout(inviterSearchTimer)
  inviterSearchTimer = setTimeout(async () => {
    const query = inviterSearch.value.trim()
    if (!query || query.length < 1) {
      inviterResults.value = []
      return
    }
    try {
      const result = await adminAPI.users.list(1, 10, { search: query })
      inviterResults.value = result.data || []
    } catch {
      inviterResults.value = []
    }
  }, 300)
}

function selectInviter(user: AdminUser) {
  selectedInviter.value = user
  form.value.created_by = user.id
  inviterSearch.value = ''
  inviterResults.value = []
}

function clearInviter() {
  selectedInviter.value = null
  form.value.created_by = 0
}

// ---------- 删除 ----------
async function handleDelete(batch: ChannelInviteBatch) {
  if (!confirm(t('admin.channelActivity.deleteConfirm', { name: batch.name }))) return
  try {
    await adminAPI.channelActivities.deleteBatch(batch.id)
    loadBatches()
  } catch (err: any) {
    const msg = err?.response?.data?.detail || err?.message || t('common.error')
    appStore.showError(msg)
  }
}

// ---------- 工具函数 ----------
function formatDate(d: string): string {
  if (!d) return '-'
  return new Date(d).toLocaleString()
}

function toDatetimeLocal(d: string): string {
  const date = new Date(d)
  const year = date.getFullYear()
  const month = String(date.getMonth() + 1).padStart(2, '0')
  const day = String(date.getDate()).padStart(2, '0')
  const hours = String(date.getHours()).padStart(2, '0')
  const minutes = String(date.getMinutes()).padStart(2, '0')
  return `${year}-${month}-${day}T${hours}:${minutes}`
}

// ---------- 初始化 ----------
onMounted(() => {
  loadBatches()
})
</script>
