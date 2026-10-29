/**
 * 网关收入看板的纯计算逻辑（无 Vue 依赖，便于单测）。
 *
 * 口径与后端一致：
 *   标价 list  = Σ total_cost
 *   实付 rev   = Σ actual_cost（日志里真实扣掉的 U，不在前端重算分组倍率）
 *   成本 cost  = Σ list_price × 账号成本折扣（折扣可按账号或按账号×模型覆盖）
 *   毛利       = rev − cost；毛利率 = 毛利 / rev
 */

import type {
  ProfitReportAccount,
  ProfitReportGroup,
  ProfitReportRow
} from '@/api/admin/profitReport'
import { U_TO_RMB } from '@/utils/format'

// ---------- 结算币种：只用人民币 ----------
// 日志、单价、余额的原始单位是系统额度 U；对外结算一律按人民币，换算汇率是系统统一的 U_TO_RMB（1 U = 0.1 元）。
// 折扣与毛利率是比值，与换算无关；只有金额列需要换算。

/** U → 元 */
export function uToRmb(u: number, rate = U_TO_RMB): number {
  return u * rate
}

/** 元 → U（用于把用户填的人民币成本反推回 U 口径） */
export function rmbToU(rmb: number, rate = U_TO_RMB): number {
  return rate > 0 ? rmb / rate : 0
}

/** 人民币金额文本：¥1,234.56；负数写成 -¥1,234.56 */
export function formatRmb(u: number, digits = 2, rate = U_TO_RMB): string {
  const x = uToRmb(u, rate)
  const abs = Math.abs(x).toLocaleString('zh-CN', { minimumFractionDigits: digits, maximumFractionDigits: digits })
  return (x < 0 ? '-' : '') + '¥' + abs
}

/** KPI 用：金额 ≥ 10 万元时省掉小数 */
export function formatRmbCompact(u: number, rate = U_TO_RMB): string {
  return formatRmb(u, Math.abs(uToRmb(u, rate)) >= 1e5 ? 0 : 2, rate)
}

/** 坐标轴用：¥1.2M / ¥35K / ¥120 */
export function formatRmbAxis(u: number, rate = U_TO_RMB): string {
  const a = Math.abs(uToRmb(u, rate))
  const s = a >= 1e6 ? (a / 1e6).toFixed(1) + 'M' : a >= 1e3 ? (a / 1e3).toFixed(0) + 'K' : a.toFixed(0)
  return (u < 0 ? '-' : '') + '¥' + s
}

export type DetailTab = 'account' | 'model' | 'group' | 'modelacc'

/** 页面上可编辑的折扣状态：按账号，以及按账号×模型的覆盖。值用字符串保存以保留输入态。 */
export interface DiscountState {
  disc: Record<string, string>
  ov: Record<string, Record<string, string>>
}

export interface Bucket {
  req: number
  tok: number
  list: number
  rev: number
  cost: number
}

export interface ProfitComputation {
  rows: ProfitReportRow[]
  tot: Bucket
  byAcc: Record<string, Bucket>
  byModel: Record<string, Bucket>
  byGroup: Record<string, Bucket>
  byMA: Record<string, Bucket>
  byDay: Record<string, Bucket>
}

export const MA_SEP = '\u0000'

export const newBucket = (): Bucket => ({ req: 0, tok: 0, list: 0, rev: 0, cost: 0 })

const isFilled = (v: string | undefined | null): v is string =>
  v != null && v !== '' && Number.isFinite(Number(v))

export const dbRate = (a: ProfitReportAccount | undefined): number =>
  a == null || a.rate_multiplier == null ? 1 : Number(a.rate_multiplier)

/** 解析某账号 × 模型当前生效的成本折扣：模型覆盖 > 账号填写 > 库中值。 */
export function resolveDiscount(
  state: DiscountState,
  accById: Record<string, ProfitReportAccount>,
  accountId: number,
  model: string
): number {
  const ov = state.ov[accountId]
  if (ov && isFilled(ov[model])) return Number(ov[model])
  if (isFilled(state.disc[accountId])) return Number(state.disc[accountId])
  return dbRate(accById[accountId])
}

/** 账号折扣是否与库中值不同（即有待保存的改动）。 */
export function isDiscountChanged(state: DiscountState, a: ProfitReportAccount): boolean {
  const cur = state.disc[a.id]
  return isFilled(cur) && Number(cur) !== dbRate(a)
}

export function computeProfit(
  rows: ProfitReportRow[],
  discountOf: (accountId: number, model: string) => number
): ProfitComputation {
  const tot = newBucket()
  const byAcc: Record<string, Bucket> = {}
  const byModel: Record<string, Bucket> = {}
  const byGroup: Record<string, Bucket> = {}
  const byMA: Record<string, Bucket> = {}
  const byDay: Record<string, Bucket> = {}
  const add = (o: Bucket, r: ProfitReportRow, c: number) => {
    o.req += Number(r.requests)
    o.tok +=
      Number(r.input_tokens) +
      Number(r.output_tokens) +
      Number(r.cache_creation_tokens) +
      Number(r.cache_read_tokens)
    o.list += Number(r.list_price)
    o.rev += Number(r.revenue)
    o.cost += c
  }
  const get = (m: Record<string, Bucket>, k: string) => (m[k] ||= newBucket())
  for (const r of rows) {
    const c = Number(r.list_price) * discountOf(r.account_id, r.model)
    add(tot, r, c)
    add(get(byAcc, String(r.account_id)), r, c)
    add(get(byModel, r.model), r, c)
    add(get(byGroup, r.group_id == null ? 'null' : String(r.group_id)), r, c)
    add(get(byMA, r.model + MA_SEP + r.account_id), r, c)
    add(get(byDay, r.day), r, c)
  }
  return { rows, tot, byAcc, byModel, byGroup, byMA, byDay }
}

export const marginPct = (margin: number, revenue: number): number | null =>
  revenue > 0 ? (100 * margin) / revenue : null

export interface DetailRow extends Bucket {
  key: string
  name: string
  acc?: string
  platform?: string
  rate?: number | null
  n?: number
  applied?: number | null
  disc?: number
  multi?: boolean
  deleted?: boolean
  margin: number
  pct: number | null
}

export function buildDetailRows(
  tab: DetailTab,
  R: ProfitComputation,
  accById: Record<string, ProfitReportAccount>,
  grpById: Record<string, ProfitReportGroup>,
  discountOf: (accountId: number, model: string) => number,
  noGroupLabel = '(no group)'
): DetailRow[] {
  const out: DetailRow[] = []
  const finish = (s: Bucket) => ({ ...s, margin: s.rev - s.cost, pct: marginPct(s.rev - s.cost, s.rev) })
  if (tab === 'account') {
    for (const [id, s] of Object.entries(R.byAcc)) {
      const a = accById[id]
      out.push({
        key: id,
        name: a?.name ?? `#${id}`,
        platform: a?.platform ?? '',
        rate: dbRate(a),
        deleted: a?.deleted ?? false,
        ...finish(s)
      })
    }
  } else if (tab === 'model') {
    const accCount: Record<string, Set<number>> = {}
    for (const r of R.rows) (accCount[r.model] ||= new Set()).add(r.account_id)
    for (const [m, s] of Object.entries(R.byModel)) {
      out.push({ key: m, name: m, n: accCount[m]?.size ?? 0, ...finish(s) })
    }
  } else if (tab === 'group') {
    for (const [id, s] of Object.entries(R.byGroup)) {
      const g = grpById[id]
      out.push({
        key: id,
        name: g?.name ?? (id === 'null' ? noGroupLabel : `#${id}`),
        platform: g?.platform ?? '',
        rate: g == null ? null : Number(g.rate_multiplier),
        applied: s.list > 0 ? s.rev / s.list : null,
        deleted: g?.deleted ?? false,
        ...finish(s)
      })
    }
  } else {
    const cnt: Record<string, number> = {}
    for (const k of Object.keys(R.byMA)) {
      const m = k.split(MA_SEP)[0]
      cnt[m] = (cnt[m] || 0) + 1
    }
    for (const [k, s] of Object.entries(R.byMA)) {
      const [m, id] = k.split(MA_SEP)
      const a = accById[id]
      out.push({
        key: k,
        name: m,
        acc: a?.name ?? `#${id}`,
        disc: discountOf(Number(id), m),
        multi: cnt[m] > 1,
        ...finish(s)
      })
    }
  }
  return out
}

export interface SortSpec {
  k: string
  dir: 'asc' | 'desc'
}

export function sortDetailRows(rows: DetailRow[], sort: SortSpec): DetailRow[] {
  const copy = rows.slice()
  copy.sort((a, b) => {
    const x = (a as unknown as Record<string, unknown>)[sort.k]
    const y = (b as unknown as Record<string, unknown>)[sort.k]
    let r = Number(x == null) - Number(y == null)
    if (r === 0) {
      r =
        typeof x === 'number' && typeof y === 'number'
          ? x - y
          : String(x ?? '').localeCompare(String(y ?? ''), 'zh')
    }
    return sort.dir === 'asc' ? r : -r
  })
  return copy
}

export function totalDetailRows(rows: DetailRow[]): Bucket & { margin: number; pct: number | null } {
  const t = newBucket()
  for (const r of rows) {
    t.req += r.req
    t.tok += r.tok
    t.list += r.list
    t.rev += r.rev
    t.cost += r.cost
  }
  const margin = t.rev - t.cost
  return { ...t, margin, pct: marginPct(margin, t.rev) }
}

/** 从总成本反推账号折扣：成本 ÷ 标价，四舍五入到万分位，标价为 0 时返回 null。 */
export function solveDiscountFromCost(cost: number, listPrice: number): number | null {
  if (!(listPrice > 0) || !Number.isFinite(cost)) return null
  return Math.round(Math.max(0, cost / listPrice) * 10000) / 10000
}

export function formatTokensShort(v: number): string {
  if (v >= 1e9) return (v / 1e9).toFixed(2) + 'B'
  if (v >= 1e6) return (v / 1e6).toFixed(1) + 'M'
  if (v >= 1e3) return (v / 1e3).toFixed(0) + 'K'
  return String(Math.round(v))
}

export function csvEscape(v: unknown): string {
  const s = v == null ? '' : String(v)
  return /[",\n]/.test(s) ? `"${s.replace(/"/g, '""')}"` : s
}

export function toCsv(header: string[], rows: unknown[][]): string {
  return '﻿' + [header, ...rows].map((r) => r.map(csvEscape).join(',')).join('\n')
}

export function addDays(day: string, n: number): string {
  const t = new Date(day + 'T00:00:00Z')
  t.setUTCDate(t.getUTCDate() + n)
  return t.toISOString().slice(0, 10)
}

/** 列出 [from, to] 闭区间内的每一天。 */
export function eachDay(from: string, to: string): string[] {
  const out: string[] = []
  if (!from || !to || from > to) return out
  for (let d = from; d <= to && out.length < 1000; d = addDays(d, 1)) out.push(d)
  return out
}
