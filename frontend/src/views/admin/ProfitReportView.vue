<template>
  <AppLayout>
    <div class="space-y-6">
      <!-- 控制条 -->
      <div class="card p-4">
        <div class="flex flex-wrap items-center gap-x-6 gap-y-3">
          <div class="flex items-center gap-2">
            <span class="text-sm font-medium text-gray-700 dark:text-gray-300">{{ t('admin.dashboard.timeRange') }}:</span>
            <DateRangePicker v-model:start-date="startDate" v-model:end-date="endDate" @change="onDateRangeChange" />
          </div>
          <div class="flex items-center gap-2">
            <span class="text-sm font-medium text-gray-700 dark:text-gray-300">{{ t('admin.profitReport.settlement') }}:</span>
            <span
              data-test="settlement"
              class="rounded-md bg-gray-100 px-2 py-1 font-mono text-sm text-gray-800 dark:bg-gray-800 dark:text-gray-200"
            >¥ · 1 U = {{ U_TO_RMB }} {{ t('admin.profitReport.yuan') }}</span>
          </div>
          <div class="ml-auto flex items-center gap-2">
            <button class="btn btn-secondary" :disabled="loading" @click="loadReport">
              <Icon name="refresh" size="sm" class="mr-1" />
              {{ t('common.refresh') }}
            </button>
          </div>
        </div>
        <p class="mt-3 text-xs text-gray-500 dark:text-gray-400">
          {{ t('admin.profitReport.rateNote') }}
          <span v-if="report" class="ml-2 font-mono" data-test="snapshot">
            {{ t('admin.profitReport.snapshot', { start: report.start_date, end: report.end_date, tz: report.timezone || 'server', time: formatDateTime(report.generated_at), rows: report.rows.length }) }}
          </span>
        </p>
      </div>

      <!-- KPI -->
      <div class="grid grid-cols-2 gap-4 md:grid-cols-3 xl:grid-cols-6">
        <div class="card p-4 min-w-0">
          <p class="kpi-label">{{ t('admin.profitReport.kpi.revenue') }}</p>
          <p class="kpi-value text-blue-600 dark:text-blue-400">{{ fmK(R.tot.rev) }}</p>
          <p class="kpi-sub">{{ t('admin.profitReport.kpi.listPrice') }} {{ fmK(R.tot.list) }}</p>
        </div>
        <div class="card p-4 min-w-0">
          <p class="kpi-label">{{ t('admin.profitReport.kpi.cost') }}</p>
          <p class="kpi-value text-orange-600 dark:text-orange-400">{{ fmK(R.tot.cost) }}</p>
          <p class="kpi-sub">{{ t('admin.profitReport.kpi.costHint') }}</p>
        </div>
        <div class="card p-4 min-w-0">
          <p class="kpi-label">{{ t('admin.profitReport.kpi.margin') }}</p>
          <p class="kpi-value" :class="signClass(totalMargin)">{{ fmK(totalMargin) }}</p>
          <p class="kpi-sub">{{ t('admin.profitReport.kpi.marginHint') }}</p>
        </div>
        <div class="card p-4 min-w-0">
          <p class="kpi-label">{{ t('admin.profitReport.kpi.marginRate') }}</p>
          <p class="kpi-value" :class="totalPct == null ? '' : signClass(totalPct)">{{ pctText(totalPct) }}</p>
          <p class="kpi-sub">{{ t('admin.profitReport.kpi.marginRateHint') }}</p>
        </div>
        <div class="card p-4 min-w-0">
          <p class="kpi-label">{{ t('admin.profitReport.kpi.requests') }}</p>
          <p class="kpi-value">{{ fi(R.tot.req) }}</p>
          <p class="kpi-sub font-mono">{{ startDate }} ~ {{ endDate }}</p>
        </div>
        <div class="card p-4 min-w-0">
          <p class="kpi-label">{{ t('admin.profitReport.kpi.tokens') }}</p>
          <p class="kpi-value">{{ formatTokensShort(R.tot.tok) }}</p>
          <p class="kpi-sub">{{ t('admin.profitReport.kpi.tokensHint') }}</p>
        </div>
      </div>

      <!-- 供应商成本折扣 -->
      <div class="card p-4">
        <div class="mb-2 flex flex-wrap items-center gap-x-4 gap-y-2">
          <h3 class="text-sm font-semibold text-gray-900 dark:text-white">{{ t('admin.profitReport.discount.title') }}</h3>
          <span class="text-xs text-gray-500 dark:text-gray-400">{{ t('admin.profitReport.discount.hint') }}</span>
          <label class="ml-auto inline-flex items-center gap-2 text-xs text-gray-600 dark:text-gray-300">
            <input v-model="showIdle" type="checkbox" class="rounded border-gray-300 dark:border-dark-600" />
            {{ t('admin.profitReport.discount.showIdle') }}
            <span class="badge badge-gray font-mono">{{ idleAccounts.length }}</span>
          </label>
          <button class="btn btn-secondary btn-sm" :disabled="!changedAccounts.length && !hasOverrides" @click="resetAll">
            {{ t('admin.profitReport.resetAll') }}
          </button>
          <button class="btn btn-primary btn-sm" :disabled="!changedAccounts.length || saving" @click="confirmSaveVisible = true">
            {{ t('admin.profitReport.discount.saveChanged', { n: changedAccounts.length }) }}
          </button>
        </div>
        <div class="table-container">
          <table class="table pr-table">
            <thead>
              <tr>
                <th>{{ t('admin.profitReport.discount.account') }}</th>
                <th>{{ t('admin.profitReport.discount.platform') }}</th>
                <th class="r">{{ t('admin.profitReport.discount.dbValue') }}</th>
                <th class="r">{{ t('admin.profitReport.discount.discount') }}</th>
                <th class="r">{{ t('admin.profitReport.columns.requests') }}</th>
                <th class="r">{{ t('admin.profitReport.columns.revenue') }}</th>
                <th class="r">{{ t('admin.profitReport.discount.costEditable') }}</th>
                <th class="r">{{ t('admin.profitReport.columns.margin') }}</th>
                <th class="r">{{ t('admin.profitReport.columns.marginRate') }}</th>
                <th>{{ t('admin.profitReport.discount.perModel') }}</th>
              </tr>
            </thead>
            <tbody>
              <template v-for="a in visibleAccounts" :key="a.id">
                <tr :class="{ 'pr-changed': isDiscountChanged(discState, a) }">
                  <td>
                    <span class="font-medium text-gray-900 dark:text-white">{{ a.name }}</span>
                    <span v-if="a.deleted" class="badge badge-danger ml-1">{{ t('admin.profitReport.discount.deleted') }}</span>
                    <span v-else-if="a.status !== 'active'" class="badge badge-warning ml-1">{{ a.status }}</span>
                    <span
                      v-if="!a.deleted && a.schedulable === false"
                      class="badge badge-gray ml-1"
                      :title="t('admin.profitReport.discount.lastUsed', { time: a.last_used_at ? formatDateTime(a.last_used_at) : '—' })"
                    >{{ t('admin.profitReport.discount.unschedulable') }}</span>
                  </td>
                  <td><span v-if="a.platform" class="badge badge-gray">{{ a.platform }}</span></td>
                  <td class="r">{{ dbRate(a).toFixed(2) }}</td>
                  <td class="r">
                    <input
                      type="number"
                      min="0"
                      max="10"
                      step="0.0001"
                      class="input pr-num w-24"
                      :value="discState.disc[a.id] ?? dbRate(a)"
                      :disabled="a.deleted"
                      :aria-label="`${a.name} ${t('admin.profitReport.discount.discount')}`"
                      @input="onDiscInput(a.id, ($event.target as HTMLInputElement).value)"
                    />
                  </td>
                  <td class="r">{{ fi(bucketOf(a.id).req) }}</td>
                  <td class="r">{{ fm(bucketOf(a.id).rev) }}</td>
                  <td class="r">
                    <input
                      type="number"
                      min="0"
                      step="0.01"
                      class="input pr-num w-32"
                      :value="(bucketOf(a.id).cost * factor).toFixed(2)"
                      :disabled="a.deleted || !(bucketOf(a.id).list > 0)"
                      :title="t('admin.profitReport.discount.costTitle', { sym: sym, list: fm(bucketOf(a.id).list) })"
                      :aria-label="`${a.name} ${t('admin.profitReport.discount.costEditable')}`"
                      @change="onCostChange(a.id, ($event.target as HTMLInputElement).value)"
                    />
                  </td>
                  <td class="r" :class="signClass(bucketOf(a.id).rev - bucketOf(a.id).cost)">{{ fm(bucketOf(a.id).rev - bucketOf(a.id).cost) }}</td>
                  <td class="r" :class="pctClass(marginPct(bucketOf(a.id).rev - bucketOf(a.id).cost, bucketOf(a.id).rev))">
                    {{ pctText(marginPct(bucketOf(a.id).rev - bucketOf(a.id).cost, bucketOf(a.id).rev)) }}
                  </td>
                  <td>
                    <button class="btn btn-secondary btn-sm" :disabled="a.deleted" @click="toggleOpen(a.id)">
                      {{ t('admin.profitReport.discount.perModelBtn') }}<span v-if="hasOverride(a.id)"> ●</span>
                      <span class="badge badge-gray ml-1 font-mono">{{ modelsOf(a.id).length }}</span>
                    </button>
                  </td>
                </tr>
                <tr v-if="openIds.has(a.id)" class="pr-ov">
                  <td colspan="10">
                    <div v-if="modelsOf(a.id).length" class="flex flex-wrap items-center gap-x-4 gap-y-2">
                      <label v-for="m in modelsOf(a.id)" :key="m" class="inline-flex items-center gap-1.5 font-mono text-xs">
                        {{ m }}
                        <input
                          type="number"
                          min="0"
                          max="10"
                          step="0.01"
                          class="input pr-num w-20"
                          :placeholder="t('admin.profitReport.discount.inherit')"
                          :value="discState.ov[a.id]?.[m] ?? ''"
                          @input="onOvInput(a.id, m, ($event.target as HTMLInputElement).value)"
                        />
                      </label>
                      <span class="text-xs text-gray-500 dark:text-gray-400">{{ t('admin.profitReport.discount.perModelNote') }}</span>
                    </div>
                    <span v-else class="text-xs text-gray-500 dark:text-gray-400">{{ t('admin.profitReport.discount.noRequests') }}</span>
                  </td>
                </tr>
              </template>
              <tr v-if="!visibleAccounts.length">
                <td colspan="10" class="py-8 text-center text-gray-500 dark:text-gray-400">{{ t('admin.profitReport.tables.noData') }}</td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>

      <!-- 趋势图 -->
      <ProfitTrendChart :points="trendPoints" :loading="loading" :format="(v) => fm(v)" :format-axis="fmAxis" />

      <!-- 明细表 -->
      <div class="card p-4">
        <div class="flex flex-wrap gap-1 border-b border-gray-200 dark:border-dark-700" role="tablist">
          <button
            v-for="tab in tabs"
            :key="tab"
            role="tab"
            :aria-selected="activeTab === tab"
            class="-mb-px border-b-2 px-3 py-2 text-sm"
            :class="activeTab === tab ? 'border-primary-500 font-semibold text-primary-600 dark:text-primary-400' : 'border-transparent text-gray-600 hover:text-gray-900 dark:text-gray-400 dark:hover:text-gray-200'"
            @click="activeTab = tab"
          >
            {{ t(`admin.profitReport.tabs.${tab}`) }}
          </button>
        </div>
        <div class="mt-3 flex flex-wrap items-center justify-between gap-3">
          <p class="text-xs text-gray-500 dark:text-gray-400">{{ t(`admin.profitReport.tabHints.${activeTab}`) }}</p>
          <div class="flex items-center gap-2">
            <input v-model="filter" type="text" class="input w-48" :placeholder="t('admin.profitReport.tables.filterPlaceholder')" />
            <button class="btn btn-secondary btn-sm" :disabled="!sortedRows.length" @click="exportCsv">{{ t('admin.profitReport.export.csv') }}</button>
            <button class="btn btn-secondary btn-sm" :disabled="!sortedRows.length" @click="copyCsv">{{ t('admin.profitReport.export.copy') }}</button>
          </div>
        </div>
        <div class="table-container mt-3">
          <table class="table pr-table">
            <thead>
              <tr>
                <th
                  v-for="col in columns"
                  :key="col.key"
                  :class="[col.right ? 'r' : '', 'cursor-pointer select-none']"
                  @click="toggleSort(col.key)"
                >
                  {{ t(`admin.profitReport.columns.${col.label}`) }}
                  <span v-if="sort.k === col.key">{{ sort.dir === 'asc' ? '▲' : '▼' }}</span>
                </th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="row in sortedRows" :key="row.key" :class="{ 'pr-multi': row.multi }">
                <td v-for="col in columns" :key="col.key" :class="col.right ? 'r' : ''" v-html="cellHtml(col.key, row)"></td>
              </tr>
              <tr v-if="!sortedRows.length">
                <td :colspan="columns.length" class="py-8 text-center text-gray-500 dark:text-gray-400">{{ t('admin.profitReport.tables.noData') }}</td>
              </tr>
            </tbody>
            <tfoot v-if="sortedRows.length">
              <tr class="font-semibold">
                <td v-for="(col, i) in columns" :key="col.key" :class="col.right ? 'r' : ''">
                  <template v-if="i === 0">{{ t('admin.profitReport.tables.total') }} ({{ sortedRows.length }})</template>
                  <template v-else-if="totalKeys.has(col.key)"><span v-html="cellHtml(col.key, totals)"></span></template>
                </td>
              </tr>
            </tfoot>
          </table>
        </div>
      </div>

      <!-- 公式说明 + 单笔核对 -->
      <div class="grid grid-cols-1 gap-6 xl:grid-cols-2">
        <div class="card p-4">
          <h3 class="text-sm font-semibold text-gray-900 dark:text-white">{{ t('admin.profitReport.formula.title') }}</h3>
          <ol class="mt-3 space-y-2 text-sm text-gray-700 dark:text-gray-300">
            <li><b>①</b> {{ t('admin.profitReport.formula.step1') }}</li>
            <li><b>②</b> {{ t('admin.profitReport.formula.step2') }}</li>
            <li><b>③</b> {{ t('admin.profitReport.formula.step3') }}</li>
            <li><b>④</b> {{ t('admin.profitReport.formula.step4') }}</li>
          </ol>
          <p class="mt-3 text-xs text-gray-500 dark:text-gray-400">{{ t('admin.profitReport.formula.note') }}</p>
        </div>
        <div class="card p-4">
          <h3 class="text-sm font-semibold text-gray-900 dark:text-white">{{ t('admin.profitReport.calc.title') }}</h3>
          <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('admin.profitReport.calc.note') }}</p>
          <div class="mt-3 grid grid-cols-2 gap-3 md:grid-cols-5">
            <label v-for="f in calcFields" :key="f.key" class="block min-w-0">
              <span class="block text-xs text-gray-500 dark:text-gray-400">{{ t(`admin.profitReport.calc.${f.key}`) }}</span>
              <input v-model.number="calc[f.key]" type="number" :step="f.step" class="input pr-num mt-1 w-full" />
            </label>
          </div>
          <div class="mt-3 grid grid-cols-2 gap-3 md:grid-cols-4">
            <div class="rounded-lg bg-gray-50 p-3 dark:bg-dark-800">
              <p class="kpi-label">{{ t('admin.profitReport.calc.listPrice') }}</p>
              <p class="font-mono text-sm font-semibold">{{ fm(calcOut.list, 4) }}</p>
            </div>
            <div class="rounded-lg bg-gray-50 p-3 dark:bg-dark-800">
              <p class="kpi-label">{{ t('admin.profitReport.calc.revenue') }}</p>
              <p class="font-mono text-sm font-semibold">{{ fm(calcOut.rev, 4) }}</p>
            </div>
            <div class="rounded-lg bg-gray-50 p-3 dark:bg-dark-800">
              <p class="kpi-label">{{ t('admin.profitReport.calc.cost') }}</p>
              <p class="font-mono text-sm font-semibold">{{ fm(calcOut.cost, 4) }}</p>
            </div>
            <div class="rounded-lg bg-gray-50 p-3 dark:bg-dark-800">
              <p class="kpi-label">{{ t('admin.profitReport.calc.margin') }}</p>
              <p class="font-mono text-sm font-semibold" :class="signClass(calcOut.margin)">
                {{ fm(calcOut.margin, 4) }}
                <span class="text-xs font-normal text-gray-500 dark:text-gray-400">{{ pctText(marginPct(calcOut.margin, calcOut.rev)) }}</span>
              </p>
            </div>
          </div>
        </div>
      </div>
    </div>
  </AppLayout>

  <ConfirmDialog
    :show="confirmSaveVisible"
    :title="t('admin.profitReport.discount.saveConfirmTitle')"
    :message="saveConfirmMessage"
    :confirm-text="t('common.save')"
    @confirm="saveChanged"
    @cancel="confirmSaveVisible = false"
  />
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { saveAs } from 'file-saver'
import { useAppStore } from '@/stores/app'
import { adminAPI } from '@/api/admin'
import { useClipboard } from '@/composables/useClipboard'
import { formatDateTime, U_TO_RMB } from '@/utils/format'
import {
  buildDetailRows,
  computeProfit,
  dbRate,
  eachDay,
  formatRmb,
  formatRmbAxis,
  formatRmbCompact,
  formatTokensShort,
  isDiscountChanged,
  marginPct,
  newBucket,
  resolveDiscount,
  rmbToU,
  solveDiscountFromCost,
  sortDetailRows,
  toCsv,
  totalDetailRows,
  uToRmb,
  type DetailRow,
  type DetailTab,
  type DiscountState,
  type SortSpec
} from '@/utils/profitReport'
import type { ProfitReportAccount, ProfitReportGroup, ProfitReportResponse } from '@/api/admin/profitReport'
import AppLayout from '@/components/layout/AppLayout.vue'
import DateRangePicker from '@/components/common/DateRangePicker.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import Icon from '@/components/icons/Icon.vue'
import ProfitTrendChart from '@/components/admin/profitReport/ProfitTrendChart.vue'

const { t } = useI18n()
const appStore = useAppStore()
const { copyToClipboard } = useClipboard()

// ---------- 本地持久化（仅本浏览器的便利：折扣草稿、tab） ----------
const LS_KEY = 'profit-report-v1'
interface PersistedState {
  disc?: Record<string, string>
  ov?: Record<string, Record<string, string>>
  tab?: DetailTab
  showIdle?: boolean
}
const loadPersisted = (): PersistedState => {
  try {
    const raw = localStorage.getItem(LS_KEY)
    return raw ? (JSON.parse(raw) as PersistedState) : {}
  } catch {
    return {}
  }
}
const persisted = loadPersisted()

// ---------- 日期范围 ----------
const formatLocalDate = (d: Date) => {
  const y = d.getFullYear()
  const m = String(d.getMonth() + 1).padStart(2, '0')
  const day = String(d.getDate()).padStart(2, '0')
  return `${y}-${m}-${day}`
}
const defaultEnd = new Date()
const defaultStart = new Date(defaultEnd.getTime() - 29 * 24 * 60 * 60 * 1000)
const startDate = ref(formatLocalDate(defaultStart))
const endDate = ref(formatLocalDate(defaultEnd))

// ---------- 数据 ----------
const report = ref<ProfitReportResponse | null>(null)
const loading = ref(false)
const saving = ref(false)

const loadReport = async () => {
  loading.value = true
  try {
    report.value = await adminAPI.profitReport.getReport({ start_date: startDate.value, end_date: endDate.value })
  } catch {
    appStore.showError(t('admin.profitReport.loadFailed'))
  } finally {
    loading.value = false
  }
}
const onDateRangeChange = () => loadReport()
onMounted(loadReport)

const accounts = computed(() => report.value?.accounts ?? [])
const accById = computed<Record<string, ProfitReportAccount>>(() =>
  Object.fromEntries(accounts.value.map((a) => [a.id, a]))
)
const grpById = computed<Record<string, ProfitReportGroup>>(() =>
  Object.fromEntries((report.value?.groups ?? []).map((g) => [g.id, g]))
)

// ---------- 结算：只用人民币。日志原始单位是 U，按系统统一汇率 U_TO_RMB 换算显示 ----------
const factor = U_TO_RMB
const sym = '¥'
const fm = (v: number, d = 2) => formatRmb(v, d)
const fmK = (v: number) => formatRmbCompact(v)
const fmAxis = (v: number) => formatRmbAxis(v)
const fi = (v: number) => Number(v).toLocaleString('zh-CN')
const pctText = (p: number | null) => (p == null ? '—' : `${p.toFixed(1)}%`)
const signClass = (v: number) => (v >= 0 ? 'text-emerald-600 dark:text-emerald-400' : 'text-red-600 dark:text-red-400')
const pctClass = (p: number | null) => (p == null ? 'text-gray-400' : signClass(p))

// ---------- 折扣状态 ----------
const discState = reactive<DiscountState>({ disc: { ...(persisted.disc ?? {}) }, ov: { ...(persisted.ov ?? {}) } })
const showIdle = ref(!!persisted.showIdle)
const openIds = reactive(new Set<number>())
const discountOf = (accountId: number, model: string) => resolveDiscount(discState, accById.value, accountId, model)

const onDiscInput = (id: number, value: string) => {
  discState.disc[id] = value
}
const onOvInput = (id: number, model: string, value: string) => {
  const overrides = discState.ov[id] ?? (discState.ov[id] = {})
  overrides[model] = value
}
const onCostChange = (id: number, value: string) => {
  const s = R.value.byAcc[id]
  if (!s) return
  const d = solveDiscountFromCost(rmbToU(Number(value) || 0), s.list)
  if (d == null) return
  discState.disc[id] = String(d)
  delete discState.ov[id]
}
const toggleOpen = (id: number) => {
  if (openIds.has(id)) openIds.delete(id)
  else openIds.add(id)
}
const hasOverride = (id: number) => Object.values(discState.ov[id] ?? {}).some((v) => v !== '' && v != null)
const hasOverrides = computed(() => Object.keys(discState.ov).some((id) => hasOverride(Number(id))))
const resetAll = () => {
  for (const k of Object.keys(discState.disc)) delete discState.disc[k]
  for (const k of Object.keys(discState.ov)) delete discState.ov[k]
}

// ---------- 计算 ----------
const R = computed(() => computeProfit(report.value?.rows ?? [], discountOf))
const totalMargin = computed(() => R.value.tot.rev - R.value.tot.cost)
const totalPct = computed(() => marginPct(totalMargin.value, R.value.tot.rev))
const bucketOf = (id: number) => R.value.byAcc[id] ?? newBucket()

const sortedAccounts = computed(() =>
  accounts.value.slice().sort((a, b) => bucketOf(b.id).rev - bucketOf(a.id).rev)
)
const idleAccounts = computed(() => sortedAccounts.value.filter((a) => !bucketOf(a.id).req))
const visibleAccounts = computed(() =>
  showIdle.value ? sortedAccounts.value : sortedAccounts.value.filter((a) => bucketOf(a.id).req > 0)
)
const modelsByAccount = computed(() => {
  const m: Record<string, Set<string>> = {}
  for (const r of R.value.rows) (m[r.account_id] ||= new Set()).add(r.model)
  return m
})
const modelsOf = (id: number) => Array.from(modelsByAccount.value[id] ?? []).sort()
const changedAccounts = computed(() => accounts.value.filter((a) => !a.deleted && isDiscountChanged(discState, a)))

const trendPoints = computed(() =>
  eachDay(report.value?.start_date ?? startDate.value, report.value?.end_date ?? endDate.value).map((day) => {
    const s = R.value.byDay[day] ?? newBucket()
    return { day, rev: s.rev, cost: s.cost, margin: s.rev - s.cost }
  })
)

// ---------- 明细表 ----------
const tabs: DetailTab[] = ['account', 'model', 'group', 'modelacc']
const activeTab = ref<DetailTab>(persisted.tab && tabs.includes(persisted.tab) ? persisted.tab : 'account')
const filter = ref('')
interface ColumnDef {
  key: string
  label: string
  right?: boolean
}
const COLS: Record<DetailTab, ColumnDef[]> = {
  account: [
    { key: 'name', label: 'account' }, { key: 'platform', label: 'platform' }, { key: 'rate', label: 'dbDiscount', right: true },
    { key: 'req', label: 'requests', right: true }, { key: 'tok', label: 'tokens', right: true }, { key: 'list', label: 'listPrice', right: true },
    { key: 'rev', label: 'revenue', right: true }, { key: 'cost', label: 'cost', right: true }, { key: 'margin', label: 'margin', right: true },
    { key: 'pct', label: 'marginRate', right: true }
  ],
  model: [
    { key: 'name', label: 'model' }, { key: 'n', label: 'accountCount', right: true }, { key: 'req', label: 'requests', right: true },
    { key: 'tok', label: 'tokens', right: true }, { key: 'list', label: 'listPrice', right: true }, { key: 'rev', label: 'revenue', right: true },
    { key: 'cost', label: 'cost', right: true }, { key: 'margin', label: 'margin', right: true }, { key: 'pct', label: 'marginRate', right: true }
  ],
  group: [
    { key: 'name', label: 'group' }, { key: 'platform', label: 'platform' }, { key: 'rate', label: 'groupRate', right: true },
    { key: 'req', label: 'requests', right: true }, { key: 'list', label: 'listPrice', right: true }, { key: 'rev', label: 'revenue', right: true },
    { key: 'applied', label: 'applied', right: true }, { key: 'cost', label: 'cost', right: true }, { key: 'margin', label: 'margin', right: true },
    { key: 'pct', label: 'marginRate', right: true }
  ],
  modelacc: [
    { key: 'name', label: 'model' }, { key: 'acc', label: 'account' }, { key: 'disc', label: 'discount', right: true },
    { key: 'req', label: 'requests', right: true }, { key: 'tok', label: 'tokens', right: true }, { key: 'list', label: 'listPrice', right: true },
    { key: 'rev', label: 'revenue', right: true }, { key: 'cost', label: 'cost', right: true }, { key: 'margin', label: 'margin', right: true },
    { key: 'pct', label: 'marginRate', right: true }
  ]
}
const columns = computed(() => COLS[activeTab.value])
const sortState = reactive<Record<DetailTab, SortSpec>>({
  account: { k: 'rev', dir: 'desc' }, model: { k: 'rev', dir: 'desc' }, group: { k: 'rev', dir: 'desc' }, modelacc: { k: 'rev', dir: 'desc' }
})
const sort = computed(() => sortState[activeTab.value])
const toggleSort = (k: string) => {
  const s = sortState[activeTab.value]
  if (s.k === k) s.dir = s.dir === 'desc' ? 'asc' : 'desc'
  else sortState[activeTab.value] = { k, dir: ['name', 'acc', 'platform'].includes(k) ? 'asc' : 'desc' }
}
const detailRows = computed(() =>
  buildDetailRows(activeTab.value, R.value, accById.value, grpById.value, discountOf, t('admin.profitReport.tables.noGroup'))
)
const sortedRows = computed(() => {
  const q = filter.value.trim().toLowerCase()
  const rows = q
    ? detailRows.value.filter((r) => r.name.toLowerCase().includes(q) || (r.acc ?? '').toLowerCase().includes(q))
    : detailRows.value
  return sortDetailRows(rows, sort.value)
})
const totals = computed(() => ({ key: '__total', name: '', ...totalDetailRows(sortedRows.value) }) as DetailRow)
const totalKeys = new Set(['req', 'tok', 'list', 'rev', 'cost', 'margin', 'pct'])

const esc = (s: unknown) =>
  String(s ?? '').replace(/[&<>"]/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;' })[c] as string)
const cellHtml = (k: string, row: DetailRow): string => {
  const v = (row as unknown as Record<string, unknown>)[k]
  if (v == null || v === '') return '<span class="text-gray-400">—</span>'
  switch (k) {
    case 'pct':
      return `<span class="${pctClass(v as number)}">${pctText(v as number)}</span>`
    case 'rate':
    case 'disc':
      return Number(v).toFixed(2)
    case 'applied':
      return Number(v).toFixed(3)
    case 'req':
    case 'n':
      return fi(v as number)
    case 'tok':
      return formatTokensShort(v as number)
    case 'list':
    case 'rev':
    case 'cost':
      return fm(v as number)
    case 'margin':
      return `<span class="${signClass(v as number)}">${fm(v as number)}</span>`
    case 'platform':
      return `<span class="badge badge-gray">${esc(v)}</span>`
    case 'name':
      return esc(v) + (row.deleted ? ` <span class="badge badge-danger">${esc(t('admin.profitReport.discount.deleted'))}</span>` : '')
    default:
      return esc(v)
  }
}

// ---------- 导出 ----------
const moneyKeys = new Set(['list', 'rev', 'cost', 'margin'])
const buildCsv = () => {
  const unit = t('admin.profitReport.export.unitRmb')
  const header = columns.value.map((c) => t(`admin.profitReport.columns.${c.label}`) + (moneyKeys.has(c.key) ? unit : ''))
  const rows = sortedRows.value.map((r) =>
    columns.value.map((c) => {
      const v = (r as unknown as Record<string, unknown>)[c.key]
      if (v == null) return ''
      if (typeof v === 'number') {
        if (moneyKeys.has(c.key)) return uToRmb(v).toFixed(4)
        if (c.key === 'pct') return v.toFixed(2)
      }
      return v
    })
  )
  return toCsv(header, rows)
}
const exportCsv = () => {
  const blob = new Blob([buildCsv()], { type: 'text/csv;charset=utf-8' })
  saveAs(blob, `profit_${activeTab.value}_${startDate.value}_${endDate.value}.csv`)
}
const copyCsv = () => copyToClipboard(buildCsv())

// ---------- 保存折扣到账号 ----------
const confirmSaveVisible = ref(false)
const saveConfirmMessage = computed(() =>
  t('admin.profitReport.discount.saveConfirmMsg', { n: changedAccounts.value.length }) +
  '\n' +
  changedAccounts.value.map((a) => `${a.name}: ${dbRate(a)} → ${Number(discState.disc[a.id])}`).join('\n')
)
const saveChanged = async () => {
  confirmSaveVisible.value = false
  const targets = changedAccounts.value.map((a) => ({ id: a.id, value: Number(discState.disc[a.id]) }))
  if (!targets.length) return
  saving.value = true
  let ok = 0
  const failed: number[] = []
  for (const tgt of targets) {
    try {
      await adminAPI.accounts.update(tgt.id, { rate_multiplier: tgt.value })
      delete discState.disc[tgt.id]
      ok++
    } catch {
      failed.push(tgt.id)
    }
  }
  saving.value = false
  if (ok) appStore.showSuccess(t('admin.profitReport.discount.saved', { n: ok }))
  if (failed.length) {
    appStore.showError(t('admin.profitReport.discount.saveFailed', { names: failed.map((id) => accById.value[id]?.name ?? `#${id}`).join(', ') }))
  }
  await loadReport()
}

// ---------- 单笔核对计算器 ----------
type CalcKey = 'inTok' | 'outTok' | 'cwTok' | 'crTok' | 'pIn' | 'pOut' | 'pCw' | 'pCr' | 'groupRate' | 'accountDiscount'
const calcFields: { key: CalcKey; step: string }[] = [
  { key: 'inTok', step: '1' }, { key: 'outTok', step: '1' }, { key: 'cwTok', step: '1' }, { key: 'crTok', step: '1' },
  { key: 'groupRate', step: '0.01' },
  { key: 'pIn', step: '0.0000001' }, { key: 'pOut', step: '0.0000001' }, { key: 'pCw', step: '0.0000001' }, { key: 'pCr', step: '0.0000001' },
  { key: 'accountDiscount', step: '0.01' }
]
const calc = reactive<Record<CalcKey, number>>({
  inTok: 9639, outTok: 1061, cwTok: 0, crTok: 11328,
  pIn: 0.000098, pOut: 0.000308, pCw: 0, pCr: 0.0000182,
  groupRate: 0.85, accountDiscount: 1
})
const calcOut = computed(() => {
  const n = (v: number) => (Number.isFinite(Number(v)) ? Number(v) : 0)
  const list = n(calc.inTok) * n(calc.pIn) + n(calc.outTok) * n(calc.pOut) + n(calc.cwTok) * n(calc.pCw) + n(calc.crTok) * n(calc.pCr)
  const rev = list * n(calc.groupRate)
  const cost = list * n(calc.accountDiscount)
  return { list, rev, cost, margin: rev - cost }
})

// ---------- 持久化 ----------
watch(
  [activeTab, showIdle, () => JSON.stringify(discState)],
  () => {
    try {
      const data: PersistedState = { tab: activeTab.value, showIdle: showIdle.value, disc: discState.disc, ov: discState.ov }
      localStorage.setItem(LS_KEY, JSON.stringify(data))
    } catch {
      /* 私密模式或禁用存储时忽略 */
    }
  },
  // immediate：挂载时就重写一次，顺带清掉旧版本留下的 currency/rate 等已废弃字段
  { deep: true, immediate: true }
)
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
.pr-table th,
.pr-table td {
  @apply whitespace-nowrap px-3 py-2;
}
.pr-table th.r,
.pr-table td.r {
  @apply text-right font-mono tabular-nums;
}
.pr-table th.r {
  @apply font-sans;
}
.pr-table tr.pr-changed td {
  @apply bg-amber-50 dark:bg-amber-900/20;
}
.pr-table tr.pr-multi td:first-child {
  box-shadow: inset 3px 0 0 theme('colors.primary.500');
}
.pr-table tr.pr-ov td {
  @apply bg-gray-50 pl-7 dark:bg-dark-800/60;
}
.pr-table tfoot td {
  @apply border-t-2 border-gray-200 dark:border-dark-600;
}
.pr-num {
  @apply py-1 text-right font-mono text-sm;
}
</style>
