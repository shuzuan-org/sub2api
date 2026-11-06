/**
 * 收入看板「月度对账」的纯计算逻辑（无 Vue 依赖，便于单测）。
 *
 * 口径来自财务的《收入成本看板_单工作表模板》：
 *   一行 = 账期 × 供应商账号 × 标准模型；整笔调账单独一行，标准模型填「调账」。
 *   后台估算成本 = 后台标价(U) × 成本折扣 × U兑人民币汇率
 *   账单应付     = (资源费 − 折扣/券 + 调账 + 未含税额) × 汇率
 *   用户用量扣费 = 后台实际扣费(U) × U兑人民币汇率
 *   Token差额    = 账单Token合计 − 后台Token合计
 *   金额差额     = 账单应付 − 后台估算成本
 *
 * 和模板不一样的地方（已与业务确认）：
 *   - Token 差额在 ±0.5% 内视为一致（两边计量不可能逐 token 相等），金额仍按 0.01 元判断；
 *   - 调账行状态单独为「调账」，只计入账单应付，不参与 Token 核对；
 *   - 账单行没填成本折扣时，用账号当前折扣估算，并标注来源。
 */

import type {
  ProfitReportAccount,
  ReconcileUsageRow,
  SupplierBillLine,
  SupplierBinding
} from '@/api/admin/profitReport'
import { U_TO_RMB } from '@/utils/format'

export const ADJUSTMENT_MODEL = '调账'
export const TOKEN_TOLERANCE = 0.005
export const MONEY_TOLERANCE = 0.01

export type ReconcileStatus =
  | 'adjustment' // 调账
  | 'noUsage' // 无用量
  | 'unbound' // 待确认上游账号
  | 'missingBill' // 后台有量/缺账单
  | 'billOnly' // 账单有量/后台无同名型号
  | 'amountPending' // 金额待补
  | 'match' // 一致
  | 'diff' // 有差异

/** 计入「待处理行数」的状态（与模板一致）。 */
export const PENDING_STATUSES: ReadonlySet<ReconcileStatus> = new Set<ReconcileStatus>([
  'diff',
  'missingBill',
  'billOnly',
  'unbound',
  'amountPending'
])

/** 状态的中文名，导出 xlsx 时写入「自动核对状态」列，与模板取值一致。 */
export const STATUS_LABEL_ZH: Record<ReconcileStatus, string> = {
  adjustment: '调账',
  noUsage: '无用量',
  unbound: '待确认上游账号',
  missingBill: '后台有量/缺账单',
  billOnly: '账单有量/后台无同名型号',
  amountPending: '金额待补',
  match: '一致',
  diff: '有差异'
}

export type DiscountSource = 'bill' | 'account' | 'mixed'

export interface ReconcileLine {
  key: string
  period: string
  supplier: string
  supplierAccount: string
  standardModel: string
  /** 后台出现过的计费模型名 */
  models: string[]
  accountIds: number[]
  accountNames: string[]
  bill: SupplierBillLine | null

  requests: number
  inputTokens: number
  outputTokens: number
  cacheReadTokens: number
  cacheWriteTokens: number
  otherTokens: number
  /** 后台 Token 合计（P） */
  backendTokens: number
  /** 用户实际扣费（U） */
  revenueU: number
  /** 后台标价（U） */
  listU: number
  /** 有效成本折扣 = 估算成本 / (标价 × 汇率)；没有后台标价时为 null */
  discount: number | null
  discountSource: DiscountSource | null

  /** 厂商 Token 合计（W）；没有账单或调账行为 null */
  billTokens: number | null
  /** 账单应付 RMB（AC） */
  payable: number | null
  /** 后台估算成本 RMB（AD）；没有后台用量为 null */
  estimate: number | null
  /** 用户用量扣费 RMB（AE） */
  revenue: number
  /** Token 差额（AF） */
  tokenDiff: number | null
  /** 金额差额（AG） */
  amountDiff: number | null
  status: ReconcileStatus
}

const lower = (s: string) => s.trim().toLowerCase()

/** 拆分「平台模型原名/别名」：逗号、中文逗号、顿号、分号、换行都算分隔符。 */
export function splitAliases(s: string): string[] {
  return s
    .split(/[,，、;；\n]+/)
    .map((x) => x.trim())
    .filter(Boolean)
}

const sumNullable = (...xs: (number | null | undefined)[]) => xs.reduce<number>((a, x) => a + (x ?? 0), 0)

export const isAdjustment = (b: Pick<SupplierBillLine, 'standard_model'>) => b.standard_model.trim() === ADJUSTMENT_MODEL

/** 账单应付（RMB）；四个金额字段全空时为 null。汇率留空按 1。 */
export function billPayable(b: SupplierBillLine): number | null {
  const parts = [b.resource_fee, b.discount_amount, b.adjustment, b.tax]
  if (parts.every((x) => x == null)) return null
  const raw = (b.resource_fee ?? 0) - (b.discount_amount ?? 0) + (b.adjustment ?? 0) + (b.tax ?? 0)
  return raw * (b.fx_rate ?? 1)
}

/** 账单 Token 合计。 */
export function billTokenTotal(b: SupplierBillLine): number {
  return sumNullable(b.input_tokens, b.output_tokens, b.cache_read_tokens, b.cache_write_tokens, b.other_tokens)
}

export function tokensMatch(backend: number, bill: number, tolerance = TOKEN_TOLERANCE): boolean {
  const base = Math.max(Math.abs(backend), Math.abs(bill))
  return base === 0 || Math.abs(bill - backend) <= base * tolerance
}

export function resolveStatus(l: Omit<ReconcileLine, 'status'>): ReconcileStatus {
  if (l.bill && isAdjustment(l.bill)) return 'adjustment'
  const hasBackend = l.backendTokens > 0 || l.requests > 0
  const hasBillTokens = (l.billTokens ?? 0) > 0
  if (!hasBackend && !hasBillTokens && (l.payable ?? 0) === 0) return 'noUsage'
  if (!l.supplierAccount) return 'unbound'
  if (hasBackend && !l.bill) return 'missingBill'
  if (!hasBackend) return 'billOnly'
  if (l.payable == null || l.estimate == null) return 'amountPending'
  return tokensMatch(l.backendTokens, l.billTokens ?? 0) && Math.abs(l.amountDiff ?? 0) < MONEY_TOLERANCE ? 'match' : 'diff'
}

interface Acc {
  key: string
  period: string
  supplier: string
  supplierAccount: string
  standardModel: string
  bill: SupplierBillLine | null
  models: Set<string>
  accountIds: Set<number>
  requests: number
  inputTokens: number
  outputTokens: number
  cacheReadTokens: number
  cacheWriteTokens: number
  revenueU: number
  listU: number
  costRmb: number
  sources: Set<'bill' | 'account'>
}

/**
 * 把后台月度用量和账单合并成对账行。
 * 后台行先按账号绑定找到供应商账号，再在同账期同供应商账号的账单里按「标准模型或别名」匹配计费模型；
 * 没绑定的账号每个账号×模型单独一行（状态为待确认上游账号），不同账号不会被合并。
 */
export function buildReconcileLines(
  rows: ReconcileUsageRow[],
  bills: SupplierBillLine[],
  bindings: SupplierBinding[],
  accounts: ProfitReportAccount[],
  rate = U_TO_RMB
): ReconcileLine[] {
  const accById = new Map(accounts.map((a) => [a.id, a]))
  const bindingById = new Map(bindings.map((b) => [b.account_id, b]))
  const lines = new Map<string, Acc>()

  const newAcc = (key: string, init: Pick<Acc, 'period' | 'supplier' | 'supplierAccount' | 'standardModel' | 'bill'>): Acc => ({
    key,
    ...init,
    models: new Set(),
    accountIds: new Set(),
    requests: 0,
    inputTokens: 0,
    outputTokens: 0,
    cacheReadTokens: 0,
    cacheWriteTokens: 0,
    revenueU: 0,
    listU: 0,
    costRmb: 0,
    sources: new Set()
  })

  // 账单先落行（没有后台用量的账单也要出现），并建别名索引。
  const aliasIdx = new Map<string, string>() // period|供应商账号|别名 → line key
  bills.forEach((b, i) => {
    const key = `bill#${i}`
    lines.set(
      key,
      newAcc(key, {
        period: b.period,
        supplier: b.supplier,
        supplierAccount: b.supplier_account.trim(),
        standardModel: b.standard_model.trim(),
        bill: b
      })
    )
    if (isAdjustment(b)) return
    const base = `${b.period}|${b.supplier_account.trim()}|`
    for (const alias of [b.standard_model, ...splitAliases(b.platform_models)]) {
      const k = base + lower(alias)
      if (!aliasIdx.has(k)) aliasIdx.set(k, key) // 同名重复时第一行优先
    }
  })

  for (const r of rows) {
    const binding = bindingById.get(r.account_id)
    const supAcc = binding?.supplier_account.trim() ?? ''
    let line: Acc | undefined
    if (supAcc) {
      const billKey = aliasIdx.get(`${r.period}|${supAcc}|${lower(r.model)}`)
      if (billKey) line = lines.get(billKey)
      if (!line) {
        const key = `${r.period}|${supAcc}|${lower(r.model)}`
        line = lines.get(key)
        if (!line) {
          line = newAcc(key, { period: r.period, supplier: binding!.supplier, supplierAccount: supAcc, standardModel: r.model, bill: null })
          lines.set(key, line)
        }
      }
    } else {
      const key = `${r.period}|#${r.account_id}|${lower(r.model)}`
      line = lines.get(key)
      if (!line) {
        line = newAcc(key, { period: r.period, supplier: '', supplierAccount: '', standardModel: r.model, bill: null })
        lines.set(key, line)
      }
    }
    const billDisc = line.bill?.cost_discount
    const acc = accById.get(r.account_id)
    const disc = billDisc ?? (acc == null || acc.rate_multiplier == null ? 1 : Number(acc.rate_multiplier))
    line.sources.add(billDisc != null ? 'bill' : 'account')
    line.models.add(r.model)
    line.accountIds.add(r.account_id)
    line.requests += Number(r.requests)
    line.inputTokens += Number(r.input_tokens)
    line.outputTokens += Number(r.output_tokens)
    line.cacheReadTokens += Number(r.cache_read_tokens)
    line.cacheWriteTokens += Number(r.cache_creation_tokens)
    line.revenueU += Number(r.revenue)
    line.listU += Number(r.list_price)
    line.costRmb += Number(r.list_price) * disc * rate
  }

  const out: ReconcileLine[] = []
  for (const a of lines.values()) {
    const hasBackend = a.accountIds.size > 0
    const adj = a.bill != null && isAdjustment(a.bill)
    const backendTokens = a.inputTokens + a.outputTokens + a.cacheReadTokens + a.cacheWriteTokens
    const billTokens = a.bill && !adj ? billTokenTotal(a.bill) : null
    const payable = a.bill ? billPayable(a.bill) : null
    const estimate = hasBackend ? a.costRmb : null
    const partial: Omit<ReconcileLine, 'status'> = {
      key: a.key,
      period: a.period,
      supplier: a.supplier,
      supplierAccount: a.supplierAccount,
      standardModel: a.standardModel,
      models: [...a.models].sort(),
      accountIds: [...a.accountIds].sort((x, y) => x - y),
      accountNames: [...a.accountIds].sort((x, y) => x - y).map((id) => accById.get(id)?.name ?? `#${id}`),
      bill: a.bill,
      requests: a.requests,
      inputTokens: a.inputTokens,
      outputTokens: a.outputTokens,
      cacheReadTokens: a.cacheReadTokens,
      cacheWriteTokens: a.cacheWriteTokens,
      otherTokens: 0,
      backendTokens,
      revenueU: a.revenueU,
      listU: a.listU,
      discount: a.listU > 0 ? a.costRmb / (a.listU * rate) : null,
      discountSource: a.sources.size === 0 ? null : a.sources.size > 1 ? 'mixed' : [...a.sources][0],
      billTokens,
      payable,
      estimate,
      revenue: a.revenueU * rate,
      tokenDiff: billTokens == null ? null : billTokens - backendTokens,
      amountDiff: payable != null && estimate != null ? payable - estimate : null
    }
    out.push({ ...partial, status: resolveStatus(partial) })
  }
  out.sort(
    (x, y) =>
      x.period.localeCompare(y.period) ||
      Number(!x.supplierAccount) - Number(!y.supplierAccount) ||
      x.supplier.localeCompare(y.supplier, 'zh') ||
      x.supplierAccount.localeCompare(y.supplierAccount) ||
      Number(x.status === 'adjustment') - Number(y.status === 'adjustment') ||
      x.standardModel.localeCompare(y.standardModel) ||
      x.key.localeCompare(y.key)
  )
  return out
}

export interface ReconcileKpis {
  revenue: number
  estimate: number
  payable: number
  margin: number
  backendTokens: number
  tokenDiff: number
  /** Σ 各行金额差额 + Σ 调账行应付：调账没有对应估算，不加回来金额差额会被整笔调账扭曲。 */
  amountDiff: number
  adjustments: number
  pending: number
  lines: number
}

export function computeKpis(lines: ReconcileLine[]): ReconcileKpis {
  const k: ReconcileKpis = {
    revenue: 0,
    estimate: 0,
    payable: 0,
    margin: 0,
    backendTokens: 0,
    tokenDiff: 0,
    amountDiff: 0,
    adjustments: 0,
    pending: 0,
    lines: lines.length
  }
  for (const l of lines) {
    k.revenue += l.revenue
    k.estimate += l.estimate ?? 0
    k.payable += l.payable ?? 0
    k.backendTokens += l.backendTokens
    k.tokenDiff += l.tokenDiff ?? 0
    k.amountDiff += l.amountDiff ?? 0
    if (l.status === 'adjustment') k.adjustments += l.payable ?? 0
    if (PENDING_STATUSES.has(l.status)) k.pending++
  }
  k.amountDiff += k.adjustments
  k.margin = k.revenue - k.payable
  return k
}

// ---------- 账号绑定建议 ----------

export interface BindingSuggestion {
  supplier: string
  supplier_account: string
}

/** 按账号名前缀给出绑定建议（只是建议，需要人工确认后保存）。 */
export function suggestBinding(accountName: string): BindingSuggestion | null {
  const n = accountName.trim()
  if (/^ks[-_]/i.test(n)) return { supplier: '金山云', supplier_account: '2000186841' }
  if (/^parat/i.test(n)) return { supplier: '并行科技', supplier_account: 'zbb@metask.ai' }
  return null
}

// ---------- 模板导出 / 导入 ----------

/** 模板第 20 行的表头，A..AI 共 35 列，顺序与财务模板完全一致。 */
export const TEMPLATE_HEADERS = [
  '账期',
  '供应商',
  '供应商账号/API Key',
  '标准模型',
  '平台模型原名/别名',
  '请求数',
  '后台输入Token',
  '后台输出Token',
  '后台缓存读Token',
  '后台缓存写Token',
  '后台其他Token',
  '用户实际扣费(U)',
  '后台标价(U)',
  '账号成本折扣',
  'U兑人民币汇率',
  '后台Token合计',
  '账单币种',
  '账单输入Token',
  '账单输出Token',
  '账单缓存读Token',
  '账单缓存写Token',
  '账单其他Token',
  '厂商Token合计',
  '账单资源费(原币)',
  '折扣/券抵扣(原币)',
  '调账(原币，有正负)',
  '税额(原币，若未含)',
  '汇率(RMB/原币)',
  '账单应付(RMB)',
  '后台估算成本(RMB)',
  '用户用量扣费(RMB)',
  'Token差额(账单−后台)',
  '金额差额(账单−估算)',
  '自动核对状态',
  '备注/厂商待问事项'
] as const

/** 导出单元格：数值 / 字符串 / 公式（f）/ 空。 */
export type TemplateCell = string | number | null | { f: string; v?: number | string }

const col = (i: number): string => {
  let s = ''
  for (let n = i + 1; n > 0; n = Math.floor((n - 1) / 26)) s = String.fromCharCode(65 + ((n - 1) % 26)) + s
  return s
}
const C = Object.fromEntries(TEMPLATE_HEADERS.map((h, i) => [h, col(i)])) as Record<(typeof TEMPLATE_HEADERS)[number], string>

/**
 * 生成导出用的行（含表头）。后台那一半由系统填好，账单那一半带出已导入的值，空着的留给财务填写；
 * 计算列写成和模板一致的公式，财务填完账单后在 Excel 里就能直接看到差额。
 * firstDataRow 是第一条数据在工作表里的行号（1 起），用于拼公式。
 */
export function buildTemplateRows(lines: ReconcileLine[], rate = U_TO_RMB, firstDataRow = 2): TemplateCell[][] {
  const out: TemplateCell[][] = [[...TEMPLATE_HEADERS]]
  lines.forEach((l, i) => {
    const r = firstDataRow + i
    const b = l.bill
    const ref = (h: (typeof TEMPLATE_HEADERS)[number]) => `${C[h]}${r}`
    const hasBackend = l.accountIds.length > 0
    const note = [b?.note ?? '', !l.supplierAccount && l.accountNames.length ? `平台账号：${l.accountNames.join('、')}` : '']
      .filter(Boolean)
      .join('；')
    const aliases = b?.platform_models ? b.platform_models : l.models.filter((m) => lower(m) !== lower(l.standardModel)).join(',')
    out.push([
      l.period,
      l.supplier,
      l.supplierAccount,
      l.standardModel,
      aliases,
      hasBackend ? l.requests : null,
      hasBackend ? l.inputTokens : null,
      hasBackend ? l.outputTokens : null,
      hasBackend ? l.cacheReadTokens : null,
      hasBackend ? l.cacheWriteTokens : null,
      hasBackend ? l.otherTokens : null,
      hasBackend ? l.revenueU : null,
      hasBackend ? l.listU : null,
      b?.cost_discount ?? (l.discount != null ? Math.round(l.discount * 1e6) / 1e6 : null),
      rate,
      { f: `SUM(${ref('后台输入Token')}:${ref('后台其他Token')})`, v: l.backendTokens },
      b?.currency || null,
      b?.input_tokens ?? null,
      b?.output_tokens ?? null,
      b?.cache_read_tokens ?? null,
      b?.cache_write_tokens ?? null,
      b?.other_tokens ?? null,
      { f: `SUM(${ref('账单输入Token')}:${ref('账单其他Token')})`, v: l.billTokens ?? 0 },
      b?.resource_fee ?? null,
      b?.discount_amount ?? null,
      b?.adjustment ?? null,
      b?.tax ?? null,
      b?.fx_rate ?? null,
      {
        f: `IF(COUNT(${ref('账单资源费(原币)')}:${ref('税额(原币，若未含)')})=0,"",(${ref('账单资源费(原币)')}-${ref('折扣/券抵扣(原币)')}+${ref('调账(原币，有正负)')}+${ref('税额(原币，若未含)')})*IF(${ref('汇率(RMB/原币)')}="",1,${ref('汇率(RMB/原币)')}))`,
        v: l.payable ?? ''
      },
      hasBackend
        ? { f: `${ref('后台标价(U)')}*${ref('账号成本折扣')}*${ref('U兑人民币汇率')}`, v: l.estimate ?? 0 }
        : null,
      hasBackend ? { f: `${ref('用户实际扣费(U)')}*${ref('U兑人民币汇率')}`, v: l.revenue } : null,
      l.billTokens == null ? null : { f: `${ref('厂商Token合计')}-${ref('后台Token合计')}`, v: l.tokenDiff ?? 0 },
      l.amountDiff == null && !(hasBackend && b)
        ? null
        : {
            f: `IF(OR(${ref('账单应付(RMB)')}="",${ref('后台估算成本(RMB)')}=""),"",${ref('账单应付(RMB)')}-${ref('后台估算成本(RMB)')})`,
            v: l.amountDiff ?? ''
          },
      STATUS_LABEL_ZH[l.status],
      note || null
    ])
  })
  return out
}

/** SheetJS 单元格（只用到的子集）。 */
export interface SheetCell {
  t: 'n' | 's'
  v: number | string
  f?: string
}

/** TemplateCell → 带类型的 SheetJS 单元格；aoa_to_sheet 不会给公式对象补类型，写出的文件会缺 t。 */
export function toSheetCells(aoa: TemplateCell[][]): (SheetCell | null)[][] {
  return aoa.map((row) =>
    row.map((c) => {
      if (c == null || c === '') return null
      if (typeof c === 'number') return { t: 'n', v: c }
      if (typeof c === 'string') return { t: 's', v: c }
      const v = c.v ?? ''
      return typeof v === 'number' ? { t: 'n', v, f: c.f } : { t: 's', v, f: c.f }
    })
  )
}

export interface ParsedImport {
  lines: SupplierBillLine[]
  periods: string[]
  errors: string[]
  /** 只有后台数据、没有任何账单数据而跳过的行数 */
  skipped: number
}

/** 把各种账期写法规整成 YYYY-MM：2026-08、2026/8、2026年8月、202608、Excel 日期。 */
export function normalizePeriod(v: unknown): string | null {
  if (v == null || v === '') return null
  if (v instanceof Date && !Number.isNaN(v.getTime())) {
    return `${v.getFullYear()}-${String(v.getMonth() + 1).padStart(2, '0')}`
  }
  if (typeof v === 'number' && Number.isInteger(v) && v >= 190001 && v <= 299912 && v % 100 >= 1 && v % 100 <= 12) {
    return `${Math.floor(v / 100)}-${String(v % 100).padStart(2, '0')}`
  }
  const m = String(v)
    .trim()
    .match(/^(\d{4})\s*[-/.年]?\s*(\d{1,2})\s*月?(?:[-/.]\d{1,2}.*)?$/)
  if (!m) return null
  const mon = Number(m[2])
  if (mon < 1 || mon > 12) return null
  return `${m[1]}-${String(mon).padStart(2, '0')}`
}

/** 单元格转数字：空 → null；容忍千分位逗号、¥、空格；非法 → NaN。 */
export function parseNumberCell(v: unknown): number | null {
  if (v == null) return null
  if (typeof v === 'number') return Number.isFinite(v) ? v : Number.NaN
  const s = String(v).replace(/[,，¥￥\s]/g, '')
  if (s === '') return null
  const n = Number(s)
  return Number.isFinite(n) ? n : Number.NaN
}

const BILL_NUMBER_FIELDS: [keyof SupplierBillLine, (typeof TEMPLATE_HEADERS)[number]][] = [
  ['input_tokens', '账单输入Token'],
  ['output_tokens', '账单输出Token'],
  ['cache_read_tokens', '账单缓存读Token'],
  ['cache_write_tokens', '账单缓存写Token'],
  ['other_tokens', '账单其他Token'],
  ['resource_fee', '账单资源费(原币)'],
  ['discount_amount', '折扣/券抵扣(原币)'],
  ['adjustment', '调账(原币，有正负)'],
  ['tax', '税额(原币，若未含)'],
  ['fx_rate', '汇率(RMB/原币)']
]

/**
 * 解析财务填好的模板（二维数组，通常来自 XLSX.utils.sheet_to_json(ws, { header: 1 })）。
 * 表头所在行不固定（模板是第 20 行，系统导出是第 1 行），按「账期」+「标准模型」定位，列按表头名对应。
 * 只导入有账单数据的行和调账行；只有后台数据的行跳过。
 */
export function parseTemplateRows(aoa: unknown[][]): ParsedImport {
  const errors: string[] = []
  const norm = (x: unknown) => String(x ?? '').replace(/\s+/g, '')
  const headerIdx = aoa.findIndex((row) => Array.isArray(row) && row.some((c) => norm(c) === '账期') && row.some((c) => norm(c) === '标准模型'))
  if (headerIdx < 0) return { lines: [], periods: [], errors: ['找不到表头行（需要包含「账期」和「标准模型」两列）'], skipped: 0 }
  const header = aoa[headerIdx].map(norm)
  const at = (h: string) => header.indexOf(norm(h))

  const lines: SupplierBillLine[] = []
  const periods = new Set<string>()
  let skipped = 0
  for (let i = headerIdx + 1; i < aoa.length; i++) {
    const row = aoa[i] ?? []
    const cell = (h: string) => {
      const j = at(h)
      return j < 0 ? null : row[j]
    }
    const text = (h: string) => String(cell(h) ?? '').trim()
    const rawPeriod = cell('账期')
    if (rawPeriod == null || String(rawPeriod).trim() === '') continue
    const excelRow = i + 1
    const period = normalizePeriod(rawPeriod)
    if (!period) {
      errors.push(`第 ${excelRow} 行：账期「${String(rawPeriod)}」不是 YYYY-MM`)
      continue
    }
    const model = text('标准模型')
    const line: SupplierBillLine = {
      period,
      supplier: text('供应商'),
      supplier_account: text('供应商账号/API Key'),
      standard_model: model,
      platform_models: text('平台模型原名/别名'),
      cost_discount: null,
      currency: text('账单币种'),
      input_tokens: null,
      output_tokens: null,
      cache_read_tokens: null,
      cache_write_tokens: null,
      other_tokens: null,
      resource_fee: null,
      discount_amount: null,
      adjustment: null,
      tax: null,
      fx_rate: null,
      note: text('备注/厂商待问事项')
    }
    let bad = false
    let hasBillData = false
    for (const [field, h] of BILL_NUMBER_FIELDS) {
      const n = parseNumberCell(cell(h))
      if (Number.isNaN(n)) {
        errors.push(`第 ${excelRow} 行：「${h}」不是数字`)
        bad = true
      } else if (n != null) {
        Object.assign(line, { [field]: n })
        if (field !== 'fx_rate') hasBillData = true
      }
    }
    const disc = parseNumberCell(cell('账号成本折扣'))
    if (Number.isNaN(disc)) {
      errors.push(`第 ${excelRow} 行：「账号成本折扣」不是数字`)
      bad = true
    } else line.cost_discount = disc
    if (bad) continue
    if (!hasBillData && model !== ADJUSTMENT_MODEL) {
      skipped++
      continue
    }
    if (!model) {
      errors.push(`第 ${excelRow} 行：有账单数据但「标准模型」为空`)
      continue
    }
    lines.push(line)
    periods.add(period)
  }
  return { lines, periods: [...periods].sort(), errors, skipped }
}
