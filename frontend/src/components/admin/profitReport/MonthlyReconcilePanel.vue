<template>
  <div class="space-y-6">
    <!-- 控制条 -->
    <div class="card p-4">
      <div class="flex flex-wrap items-center gap-x-6 gap-y-3">
        <label class="flex items-center gap-2 text-sm font-medium text-gray-700 dark:text-gray-300">
          {{ t('admin.profitReport.reconcile.periodRange') }}:
          <input v-model="startPeriod" type="month" class="input w-40" data-test="start-period" />
          <span>~</span>
          <input v-model="endPeriod" type="month" class="input w-40" data-test="end-period" />
        </label>
        <label class="flex items-center gap-2 text-sm font-medium text-gray-700 dark:text-gray-300">
          {{ t('admin.profitReport.reconcile.periodFilter') }}:
          <select v-model="periodFilter" class="input w-36" data-test="period-filter">
            <option value="">{{ t('admin.profitReport.reconcile.allPeriods') }}</option>
            <option v-for="p in periodsInData" :key="p" :value="p">{{ p }}</option>
          </select>
        </label>
        <div class="ml-auto flex flex-wrap items-center gap-2">
          <button class="btn btn-secondary" :disabled="loading" @click="load">
            <Icon name="refresh" size="sm" class="mr-1" />
            {{ t('common.refresh') }}
          </button>
          <button class="btn btn-secondary" :disabled="!data || exporting" data-test="export" @click="exportTemplate">
            <Icon name="download" size="sm" class="mr-1" />
            {{ t('admin.profitReport.reconcile.export') }}
          </button>
          <button class="btn btn-primary" :disabled="importing" data-test="import" @click="fileInput?.click()">
            <Icon name="upload" size="sm" class="mr-1" />
            {{ t('admin.profitReport.reconcile.import') }}
          </button>
          <input ref="fileInput" type="file" accept=".xlsx,.xls" class="hidden" data-test="import-file" @change="onFileChosen" />
        </div>
      </div>
      <p class="mt-3 text-xs text-gray-500 dark:text-gray-400">
        {{ t('admin.profitReport.reconcile.note') }}
        <span v-if="data" class="ml-2 font-mono" data-test="snapshot">
          {{ t('admin.profitReport.reconcile.snapshot', { start: data.start_period, end: data.end_period, time: formatDateTime(data.generated_at), bills: data.bills.length }) }}
        </span>
      </p>
    </div>

    <!-- KPI：与模板第 5–6 行的 8 个指标一一对应 -->
    <div class="grid grid-cols-2 gap-4 md:grid-cols-4" data-test="kpis">
      <div v-for="k in kpiCards" :key="k.key" class="card min-w-0 p-4">
        <p class="kpi-label">{{ t(`admin.profitReport.reconcile.kpi.${k.key}`) }}</p>
        <p class="kpi-value" :class="k.cls">{{ k.value }}</p>
        <p class="kpi-sub" :title="k.sub">{{ k.sub }}</p>
      </div>
    </div>

    <!-- 供应商账号绑定 -->
    <div class="card p-4">
      <div class="mb-2 flex flex-wrap items-center gap-x-4 gap-y-2">
        <button class="text-sm font-semibold text-gray-900 dark:text-white" @click="bindingsOpen = !bindingsOpen">
          {{ bindingsOpen ? '▾' : '▸' }} {{ t('admin.profitReport.reconcile.bindings.title') }}
        </button>
        <span class="text-xs text-gray-500 dark:text-gray-400">{{ t('admin.profitReport.reconcile.bindings.hint') }}</span>
        <span v-if="unboundWithUsage.length" class="badge badge-warning" data-test="unbound-count">
          {{ t('admin.profitReport.reconcile.bindings.unbound', { n: unboundWithUsage.length }) }}
        </span>
      </div>
      <div v-if="bindingsOpen" class="table-container">
        <table class="table rc-table" data-test="bindings-table">
          <thead>
            <tr>
              <th>{{ t('admin.profitReport.columns.account') }}</th>
              <th>{{ t('admin.profitReport.columns.platform') }}</th>
              <th class="r">{{ t('admin.profitReport.columns.requests') }}</th>
              <th>{{ t('admin.profitReport.reconcile.cols.supplier') }}</th>
              <th>{{ t('admin.profitReport.reconcile.cols.supplierAccount') }}</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="a in bindingAccounts" :key="a.id" :class="{ 'rc-dirty': isBindingDirty(a.id) }">
              <td>
                <span class="font-medium text-gray-900 dark:text-white">{{ a.name }}</span>
                <span v-if="a.deleted" class="badge badge-danger ml-1">{{ t('admin.profitReport.discount.deleted') }}</span>
              </td>
              <td><span v-if="a.platform" class="badge badge-gray">{{ a.platform }}</span></td>
              <td class="r">{{ fi(requestsByAccount.get(a.id) ?? 0) }}</td>
              <td>
                <input v-model="draft(a.id).supplier" class="input w-32 py-1 text-sm" list="rc-suppliers" :aria-label="`${a.name} ${t('admin.profitReport.reconcile.cols.supplier')}`" />
              </td>
              <td>
                <input v-model="draft(a.id).supplier_account" class="input w-48 py-1 font-mono text-sm" list="rc-supplier-accounts" :aria-label="`${a.name} ${t('admin.profitReport.reconcile.cols.supplierAccount')}`" />
              </td>
              <td class="space-x-1 whitespace-nowrap">
                <button
                  v-if="!bindingById.get(a.id) && suggestBinding(a.name) && !isBindingDirty(a.id)"
                  class="btn btn-secondary btn-sm"
                  :title="t('admin.profitReport.reconcile.bindings.suggestTitle')"
                  @click="applySuggestion(a)"
                >
                  {{ t('admin.profitReport.reconcile.bindings.suggest', { ...suggestBinding(a.name)! }) }}
                </button>
                <button class="btn btn-primary btn-sm" :disabled="!isBindingDirty(a.id) || savingBinding === a.id" @click="saveBinding(a.id)">
                  {{ t('common.save') }}
                </button>
                <button v-if="bindingById.get(a.id)" class="btn btn-secondary btn-sm" :disabled="savingBinding === a.id" @click="unbind(a.id)">
                  {{ t('admin.profitReport.reconcile.bindings.unbind') }}
                </button>
              </td>
            </tr>
            <tr v-if="!bindingAccounts.length">
              <td colspan="6" class="py-6 text-center text-gray-500 dark:text-gray-400">{{ t('admin.profitReport.tables.noData') }}</td>
            </tr>
          </tbody>
        </table>
        <datalist id="rc-suppliers">
          <option v-for="s in knownSuppliers" :key="s" :value="s" />
        </datalist>
        <datalist id="rc-supplier-accounts">
          <option v-for="s in knownSupplierAccounts" :key="s" :value="s" />
        </datalist>
      </div>
    </div>

    <!-- 对账明细 -->
    <div class="card p-4">
      <div class="mb-2 flex flex-wrap items-center gap-x-4 gap-y-2">
        <h3 class="text-sm font-semibold text-gray-900 dark:text-white">{{ t('admin.profitReport.reconcile.detailTitle') }}</h3>
        <span class="text-xs text-gray-500 dark:text-gray-400">{{ t('admin.profitReport.reconcile.detailHint') }}</span>
        <div class="ml-auto flex items-center gap-2">
          <select v-model="statusFilter" class="input w-44" data-test="status-filter">
            <option value="">{{ t('admin.profitReport.reconcile.allStatuses') }}</option>
            <option value="pending">{{ t('admin.profitReport.reconcile.pendingOnly') }}</option>
            <option v-for="s in statusOptions" :key="s" :value="s">{{ t(`admin.profitReport.reconcile.status.${s}`) }}</option>
          </select>
          <input v-model="filter" type="text" class="input w-48" :placeholder="t('admin.profitReport.tables.filterPlaceholder')" />
        </div>
      </div>
      <div class="table-container">
        <table class="table rc-table" data-test="detail-table">
          <thead>
            <tr>
              <th>{{ t('admin.profitReport.reconcile.cols.period') }}</th>
              <th>{{ t('admin.profitReport.reconcile.cols.supplier') }}</th>
              <th>{{ t('admin.profitReport.reconcile.cols.standardModel') }}</th>
              <th>{{ t('admin.profitReport.reconcile.cols.platformAccounts') }}</th>
              <th class="r">{{ t('admin.profitReport.columns.requests') }}</th>
              <th class="r">{{ t('admin.profitReport.reconcile.cols.backendTokens') }}</th>
              <th class="r">{{ t('admin.profitReport.reconcile.cols.billTokens') }}</th>
              <th class="r">{{ t('admin.profitReport.reconcile.cols.tokenDiff') }}</th>
              <th class="r">{{ t('admin.profitReport.reconcile.cols.revenue') }}</th>
              <th class="r">{{ t('admin.profitReport.reconcile.cols.estimate') }}</th>
              <th class="r">{{ t('admin.profitReport.reconcile.cols.payable') }}</th>
              <th class="r">{{ t('admin.profitReport.reconcile.cols.amountDiff') }}</th>
              <th>{{ t('admin.profitReport.reconcile.cols.status') }}</th>
              <th>{{ t('admin.profitReport.reconcile.cols.note') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="l in visibleLines" :key="l.key" :data-status="l.status">
              <td class="font-mono">{{ l.period }}</td>
              <td>
                <div class="text-gray-900 dark:text-white">{{ l.supplier || '—' }}</div>
                <div class="font-mono text-[11px] text-gray-400">{{ l.supplierAccount }}</div>
              </td>
              <td>
                <div class="font-mono text-gray-900 dark:text-white">{{ l.standardModel }}</div>
                <div v-if="aliasText(l)" class="font-mono text-[11px] text-gray-400" :title="aliasText(l)">{{ aliasText(l) }}</div>
              </td>
              <td class="max-w-[14rem] truncate text-xs" :title="l.accountNames.join('\n')">{{ l.accountNames.join('、') || '—' }}</td>
              <td class="r">{{ l.accountIds.length ? fi(l.requests) : '—' }}</td>
              <td class="r" :title="tokenTitle(l)">{{ l.accountIds.length ? formatTokensShort(l.backendTokens) : '—' }}</td>
              <td class="r">{{ l.billTokens == null ? '—' : formatTokensShort(l.billTokens) }}</td>
              <td class="r" :class="tokenDiffClass(l)">
                <div>{{ l.tokenDiff == null ? '—' : signedTokens(l.tokenDiff) }}</div>
                <div v-if="l.billTotalOnly" class="text-[11px] text-gray-400" data-test="total-only" :title="t('admin.profitReport.reconcile.totalOnlyTitle')">
                  {{ t('admin.profitReport.reconcile.totalOnly') }}
                </div>
              </td>
              <td class="r">{{ l.accountIds.length ? yuan(l.revenue) : '—' }}</td>
              <td class="r">
                <div>{{ l.estimate == null ? '—' : yuan(l.estimate) }}</div>
                <div v-if="l.discount != null" class="text-[11px] text-gray-400">
                  ×{{ l.discount.toFixed(4) }} · {{ t(`admin.profitReport.reconcile.discountSource.${l.discountSource}`) }}
                </div>
              </td>
              <td class="r">{{ l.payable == null ? '—' : yuan(l.payable) }}</td>
              <td class="r" :class="l.amountDiff == null ? '' : Math.abs(l.amountDiff) < MONEY_TOLERANCE ? '' : 'text-amber-600 dark:text-amber-400'">
                {{ l.amountDiff == null ? '—' : yuan(l.amountDiff, true) }}
              </td>
              <td><span class="badge" :class="statusClass(l.status)">{{ t(`admin.profitReport.reconcile.status.${l.status}`) }}</span></td>
              <td class="max-w-[16rem] truncate text-xs" :title="l.bill?.note">{{ l.bill?.note || '' }}</td>
            </tr>
            <tr v-if="!visibleLines.length">
              <td colspan="14" class="py-8 text-center text-gray-500 dark:text-gray-400">
                {{ loading ? t('common.loading') : t('admin.profitReport.tables.noData') }}
              </td>
            </tr>
          </tbody>
          <tfoot v-if="visibleLines.length">
            <tr class="font-semibold">
              <td colspan="4">{{ t('admin.profitReport.tables.total') }} ({{ visibleLines.length }})</td>
              <td class="r">{{ fi(visibleKpis.requests) }}</td>
              <td class="r">{{ formatTokensShort(visibleKpis.backendTokens) }}</td>
              <td class="r">{{ formatTokensShort(visibleKpis.billTokens) }}</td>
              <td class="r">{{ signedTokens(visibleKpis.tokenDiff) }}</td>
              <td class="r">{{ yuan(visibleKpis.revenue) }}</td>
              <td class="r">{{ yuan(visibleKpis.estimate) }}</td>
              <td class="r">{{ yuan(visibleKpis.payable) }}</td>
              <td class="r">{{ yuan(visibleKpis.amountDiff, true) }}</td>
              <td colspan="2"></td>
            </tr>
          </tfoot>
        </table>
      </div>
    </div>

    <ConfirmDialog
      :show="importPreview != null"
      :title="t('admin.profitReport.reconcile.importConfirmTitle')"
      :message="importPreviewMessage"
      :confirm-text="t('admin.profitReport.reconcile.importConfirm')"
      @confirm="confirmImport"
      @cancel="importPreview = null"
    />
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { saveAs } from 'file-saver'
import type { CellObject } from 'xlsx'
import { useAppStore } from '@/stores/app'
import { adminAPI } from '@/api/admin'
import type { ProfitReportAccount, ReconcileResponse } from '@/api/admin/profitReport'
import { formatDateTime } from '@/utils/format'
import { formatTokensShort } from '@/utils/profitReport'
import {
  buildReconcileLines,
  buildTemplateRows,
  computeKpis,
  MONEY_TOLERANCE,
  parseTemplateRows,
  PENDING_STATUSES,
  suggestBinding,
  toSheetCells,
  tokensMatch,
  type ParsedImport,
  type ReconcileLine,
  type ReconcileStatus
} from '@/utils/profitReconcile'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import Icon from '@/components/icons/Icon.vue'

const { t } = useI18n()
const appStore = useAppStore()

// ---------- 账期：默认上一个自然月 ----------
const monthOf = (d: Date) => `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}`
const lastMonth = (() => {
  const d = new Date()
  d.setDate(1)
  d.setMonth(d.getMonth() - 1)
  return monthOf(d)
})()
const startPeriod = ref(lastMonth)
const endPeriod = ref(lastMonth)
const periodFilter = ref('')
const statusFilter = ref<'' | 'pending' | ReconcileStatus>('')
const filter = ref('')

const data = ref<ReconcileResponse | null>(null)
const loading = ref(false)

const load = async () => {
  if (!startPeriod.value) return
  if (!endPeriod.value || endPeriod.value < startPeriod.value) endPeriod.value = startPeriod.value
  loading.value = true
  try {
    data.value = await adminAPI.profitReport.getReconcile({ start_period: startPeriod.value, end_period: endPeriod.value })
    resetDrafts()
    if (periodFilter.value && !periodsInData.value.includes(periodFilter.value)) periodFilter.value = ''
  } catch (e) {
    appStore.showError((e as { message?: string })?.message || t('admin.profitReport.reconcile.loadFailed'))
  } finally {
    loading.value = false
  }
}

// ---------- 合并与核对 ----------
const lines = computed<ReconcileLine[]>(() =>
  data.value ? buildReconcileLines(data.value.rows, data.value.bills, data.value.bindings, data.value.accounts) : []
)
const periodsInData = computed(() => [...new Set(lines.value.map((l) => l.period))].sort())
const periodLines = computed(() => (periodFilter.value ? lines.value.filter((l) => l.period === periodFilter.value) : lines.value))
const kpis = computed(() => computeKpis(periodLines.value))

const statusOptions: ReconcileStatus[] = ['diff', 'match', 'missingBill', 'billOnly', 'unbound', 'amountPending', 'adjustment', 'noUsage']
const visibleLines = computed(() => {
  const q = filter.value.trim().toLowerCase()
  return periodLines.value.filter((l) => {
    if (statusFilter.value === 'pending' && !PENDING_STATUSES.has(l.status)) return false
    if (statusFilter.value && statusFilter.value !== 'pending' && l.status !== statusFilter.value) return false
    if (!q) return true
    return [l.supplier, l.supplierAccount, l.standardModel, ...l.models, ...l.accountNames, l.bill?.note ?? '']
      .some((s) => s.toLowerCase().includes(q))
  })
})
const visibleKpis = computed(() => {
  const k = computeKpis(visibleLines.value)
  let requests = 0
  let billTokens = 0
  for (const l of visibleLines.value) {
    requests += l.requests
    billTokens += l.billTokens ?? 0
  }
  return { ...k, requests, billTokens }
})

// ---------- 格式化 ----------
const fi = (v: number) => Number(v).toLocaleString('zh-CN')
const yuan = (v: number, signed = false) => {
  const abs = Math.abs(v).toLocaleString('zh-CN', { minimumFractionDigits: 2, maximumFractionDigits: 2 })
  return (v < 0 ? '-' : signed && v > 0 ? '+' : '') + '¥' + abs
}
const signedTokens = (v: number) => (v > 0 ? '+' : v < 0 ? '-' : '') + formatTokensShort(Math.abs(v))
const signClass = (v: number) => (v >= 0 ? 'text-emerald-600 dark:text-emerald-400' : 'text-red-600 dark:text-red-400')
const aliasText = (l: ReconcileLine) =>
  l.models.filter((m) => m.toLowerCase() !== l.standardModel.toLowerCase()).join(', ') || ''
const tokenTitle = (l: ReconcileLine) =>
  t('admin.profitReport.reconcile.tokenBreakdown', {
    input: fi(l.inputTokens),
    output: fi(l.outputTokens),
    cacheRead: fi(l.cacheReadTokens),
    cacheWrite: fi(l.cacheWriteTokens)
  })
const tokenDiffClass = (l: ReconcileLine) =>
  l.tokenDiff == null || tokensMatch(l.backendTokens, l.billTokens ?? 0) ? '' : 'text-amber-600 dark:text-amber-400'
const statusClass = (s: ReconcileStatus) =>
  ({
    match: 'badge-success',
    diff: 'badge-warning',
    missingBill: 'badge-danger',
    billOnly: 'badge-danger',
    unbound: 'badge-danger',
    amountPending: 'badge-warning',
    adjustment: 'badge-primary',
    noUsage: 'badge-gray'
  })[s]

const kpiCards = computed(() => {
  const k = kpis.value
  return [
    { key: 'revenue', value: yuan(k.revenue), cls: 'text-blue-600 dark:text-blue-400', sub: t('admin.profitReport.reconcile.kpiHint.revenue') },
    { key: 'estimate', value: yuan(k.estimate), cls: 'text-orange-600 dark:text-orange-400', sub: t('admin.profitReport.reconcile.kpiHint.estimate') },
    { key: 'payable', value: yuan(k.payable), cls: 'text-orange-700 dark:text-orange-300', sub: t('admin.profitReport.reconcile.kpiHint.payable', { adj: yuan(k.adjustments) }) },
    { key: 'margin', value: yuan(k.margin), cls: signClass(k.margin), sub: t('admin.profitReport.reconcile.kpiHint.margin') },
    { key: 'backendTokens', value: formatTokensShort(k.backendTokens), cls: '', sub: t('admin.profitReport.reconcile.kpiHint.backendTokens') },
    { key: 'tokenDiff', value: signedTokens(k.tokenDiff), cls: '', sub: t('admin.profitReport.reconcile.kpiHint.tokenDiff') },
    { key: 'amountDiff', value: yuan(k.amountDiff, true), cls: '', sub: t('admin.profitReport.reconcile.kpiHint.amountDiff') },
    { key: 'pending', value: String(k.pending), cls: k.pending ? 'text-amber-600 dark:text-amber-400' : '', sub: t('admin.profitReport.reconcile.kpiHint.pending', { n: k.lines }) }
  ]
})

// ---------- 供应商账号绑定 ----------
const bindingsOpen = ref(true)
const bindingById = computed(() => new Map((data.value?.bindings ?? []).map((b) => [b.account_id, b])))
const requestsByAccount = computed(() => {
  const m = new Map<number, number>()
  for (const r of data.value?.rows ?? []) m.set(r.account_id, (m.get(r.account_id) ?? 0) + Number(r.requests))
  return m
})
/** 本区间有流量的账号 + 已绑定的账号；有流量未绑定的排最前。 */
const bindingAccounts = computed<ProfitReportAccount[]>(() => {
  const accs = (data.value?.accounts ?? []).filter((a) => requestsByAccount.value.has(a.id) || bindingById.value.has(a.id))
  return accs.sort(
    (a, b) =>
      Number(bindingById.value.has(a.id)) - Number(bindingById.value.has(b.id)) ||
      (requestsByAccount.value.get(b.id) ?? 0) - (requestsByAccount.value.get(a.id) ?? 0) ||
      a.id - b.id
  )
})
const unboundWithUsage = computed(() => bindingAccounts.value.filter((a) => !bindingById.value.has(a.id)))
const knownSuppliers = computed(() =>
  [...new Set([...(data.value?.bindings ?? []).map((b) => b.supplier), ...(data.value?.bills ?? []).map((b) => b.supplier), '金山云', '并行科技'])].filter(Boolean)
)
const knownSupplierAccounts = computed(() =>
  [...new Set([...(data.value?.bindings ?? []).map((b) => b.supplier_account), ...(data.value?.bills ?? []).map((b) => b.supplier_account)])].filter(Boolean)
)

const drafts = reactive<Record<number, { supplier: string; supplier_account: string }>>({})
const resetDrafts = () => {
  for (const k of Object.keys(drafts)) delete drafts[Number(k)]
}
const draft = (id: number) => {
  if (!drafts[id]) {
    const b = bindingById.value.get(id)
    drafts[id] = { supplier: b?.supplier ?? '', supplier_account: b?.supplier_account ?? '' }
  }
  return drafts[id]
}
const isBindingDirty = (id: number) => {
  const d = drafts[id]
  if (!d) return false
  const b = bindingById.value.get(id)
  return d.supplier.trim() !== (b?.supplier ?? '') || d.supplier_account.trim() !== (b?.supplier_account ?? '')
}
const applySuggestion = (a: ProfitReportAccount) => {
  const s = suggestBinding(a.name)
  if (s) Object.assign(draft(a.id), s)
}
const savingBinding = ref<number | null>(null)
const persistBinding = async (id: number, supplier: string, supplierAccount: string) => {
  savingBinding.value = id
  try {
    await adminAPI.profitReport.saveSupplierBinding(id, supplier, supplierAccount)
    appStore.showSuccess(t('admin.profitReport.reconcile.bindings.saved'))
    await load()
  } catch (e) {
    appStore.showError((e as { message?: string })?.message || t('admin.profitReport.reconcile.bindings.saveFailed'))
  } finally {
    savingBinding.value = null
  }
}
const saveBinding = (id: number) => {
  const d = draft(id)
  const sup = d.supplier.trim()
  const acc = d.supplier_account.trim()
  if (!sup || !acc) {
    appStore.showError(t('admin.profitReport.reconcile.bindings.bothRequired'))
    return
  }
  return persistBinding(id, sup, acc)
}
const unbind = (id: number) => persistBinding(id, '', '')

// ---------- 导出模板 ----------
const exporting = ref(false)
const exportTemplate = async () => {
  if (!data.value) return
  exporting.value = true
  try {
    const XLSX = await import('xlsx')
    const aoa = buildTemplateRows(periodLines.value)
    const ws = XLSX.utils.aoa_to_sheet(toSheetCells(aoa) as unknown as CellObject[][])
    ws['!cols'] = aoa[0].map((_, i) => ({ wch: i < 5 ? 18 : 14 }))
    const wb = XLSX.utils.book_new()
    XLSX.utils.book_append_sheet(wb, ws, '收入成本看板')
    const out = XLSX.write(wb, { type: 'array', bookType: 'xlsx' })
    const range = periodFilter.value || (data.value.start_period === data.value.end_period ? data.value.start_period : `${data.value.start_period}_${data.value.end_period}`)
    saveAs(new Blob([out], { type: 'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet' }), `收入成本对账_${range}.xlsx`)
  } catch (e) {
    appStore.showError((e as { message?: string })?.message || t('admin.profitReport.reconcile.exportFailed'))
  } finally {
    exporting.value = false
  }
}

// ---------- 导入账单 ----------
const fileInput = ref<HTMLInputElement | null>(null)
const importing = ref(false)
const importPreview = ref<ParsedImport | null>(null)
const existingByPeriod = ref<Record<string, number>>({})

const onFileChosen = async (ev: Event) => {
  const input = ev.target as HTMLInputElement
  const file = input.files?.[0]
  input.value = '' // 允许重复选择同一个文件
  if (!file) return
  importing.value = true
  try {
    const XLSX = await import('xlsx')
    const wb = XLSX.read(await file.arrayBuffer(), { type: 'array', cellDates: true })
    const name = wb.SheetNames.find((n) => n.includes('收入成本')) ?? wb.SheetNames[0]
    const aoa = XLSX.utils.sheet_to_json<unknown[]>(wb.Sheets[name], { header: 1, defval: null, raw: true })
    const parsed = parseTemplateRows(aoa)
    if (parsed.errors.length) {
      const more = parsed.errors.length > 5 ? `\n… +${parsed.errors.length - 5}` : ''
      appStore.showError(t('admin.profitReport.reconcile.importErrors') + '\n' + parsed.errors.slice(0, 5).join('\n') + more)
      return
    }
    if (!parsed.lines.length) {
      appStore.showError(t('admin.profitReport.reconcile.importEmpty', { skipped: parsed.skipped }))
      return
    }
    // 现有账单行数：本页已加载的区间之外的账期查不到，按「未知」处理，仍允许覆盖。
    const counts: Record<string, number> = {}
    for (const b of data.value?.bills ?? []) counts[b.period] = (counts[b.period] ?? 0) + 1
    existingByPeriod.value = counts
    importPreview.value = parsed
  } catch (e) {
    appStore.showError((e as { message?: string })?.message || t('admin.profitReport.reconcile.importFailed'))
  } finally {
    importing.value = false
  }
}

const loadedPeriods = computed(() => {
  if (!data.value) return new Set<string>()
  const s = new Set<string>()
  const [y0, m0] = data.value.start_period.split('-').map(Number)
  const [y1, m1] = data.value.end_period.split('-').map(Number)
  for (let y = y0, m = m0; y < y1 || (y === y1 && m <= m1); m === 12 ? ((y += 1), (m = 1)) : (m += 1)) {
    s.add(`${y}-${String(m).padStart(2, '0')}`)
  }
  return s
})

const importPreviewMessage = computed(() => {
  const p = importPreview.value
  if (!p) return ''
  const newCounts: Record<string, number> = {}
  for (const l of p.lines) newCounts[l.period] = (newCounts[l.period] ?? 0) + 1
  const lines = p.periods.map((period) => {
    const old = loadedPeriods.value.has(period) ? String(existingByPeriod.value[period] ?? 0) : '?'
    return t('admin.profitReport.reconcile.importPeriodLine', { period, old, n: newCounts[period] })
  })
  const head = t('admin.profitReport.reconcile.importSummary', { n: p.lines.length })
  const tail = p.skipped ? '\n' + t('admin.profitReport.reconcile.importSkipped', { n: p.skipped }) : ''
  return `${head}\n${lines.join('\n')}${tail}`
})

const confirmImport = async () => {
  const p = importPreview.value
  importPreview.value = null
  if (!p) return
  importing.value = true
  try {
    await adminAPI.profitReport.replaceSupplierBills(p.periods, p.lines)
    appStore.showSuccess(t('admin.profitReport.reconcile.imported', { n: p.lines.length, periods: p.periods.join('、') }))
    // 导入的账期不在当前区间时，自动把区间扩到能看到它们。
    const lo = p.periods[0]
    const hi = p.periods[p.periods.length - 1]
    if (lo < startPeriod.value || hi > endPeriod.value) {
      if (lo < startPeriod.value) startPeriod.value = lo
      if (hi > endPeriod.value) endPeriod.value = hi
      // 区间变化由下面的 watch 触发重新加载
    } else {
      await load()
    }
  } catch (e) {
    appStore.showError((e as { message?: string })?.message || t('admin.profitReport.reconcile.importFailed'))
  } finally {
    importing.value = false
  }
}

watch([startPeriod, endPeriod], () => {
  void load()
})
onMounted(load)

defineExpose({ load })
</script>

<style scoped>
.kpi-label {
  @apply text-[11px] font-medium uppercase tracking-wide text-gray-500 dark:text-gray-400;
}
.kpi-value {
  @apply mt-1 truncate font-mono text-xl font-semibold text-gray-900 dark:text-white;
}
.kpi-sub {
  @apply mt-1 truncate text-xs text-gray-500 dark:text-gray-400;
}
.rc-table th,
.rc-table td {
  @apply whitespace-nowrap px-3 py-2;
}
.rc-table th.r,
.rc-table td.r {
  @apply text-right font-mono tabular-nums;
}
.rc-table th.r {
  @apply font-sans;
}
.rc-table tr.rc-dirty td {
  @apply bg-amber-50 dark:bg-amber-900/20;
}
.rc-table tfoot td {
  @apply border-t-2 border-gray-200 dark:border-dark-600;
}
</style>
