import { describe, expect, it } from 'vitest'
import type { ProfitReportAccount, ProfitReportGroup, ProfitReportRow } from '@/api/admin/profitReport'
import {
  buildDetailRows,
  computeProfit,
  eachDay,
  formatRmb,
  formatRmbAxis,
  formatRmbCompact,
  isDiscountChanged,
  marginPct,
  resolveDiscount,
  rmbToU,
  solveDiscountFromCost,
  sortDetailRows,
  toCsv,
  totalDetailRows,
  uToRmb,
  type DiscountState
} from '../profitReport'

const acc = (id: number, rate = 1, extra: Partial<ProfitReportAccount> = {}): ProfitReportAccount => ({
  id,
  name: `acc-${id}`,
  platform: 'openai',
  rate_multiplier: rate,
  status: 'active',
  schedulable: true,
  deleted: false,
  last_used_at: null,
  ...extra
})

const row = (p: Partial<ProfitReportRow>): ProfitReportRow => ({
  day: '2026-10-01',
  model: 'm',
  account_id: 1,
  group_id: 10,
  requests: 1,
  input_tokens: 0,
  output_tokens: 0,
  cache_creation_tokens: 0,
  cache_read_tokens: 0,
  list_price: 0,
  revenue: 0,
  ...p
})

const accById = { 1: acc(1, 1), 2: acc(2, 0.5) }
const grpById: Record<string, ProfitReportGroup> = {
  10: { id: 10, name: 'G', platform: 'openai', rate_multiplier: 0.85, deleted: false }
}

describe('resolveDiscount', () => {
  it('falls back to the DB multiplier when nothing is entered', () => {
    const state: DiscountState = { disc: {}, ov: {} }
    expect(resolveDiscount(state, accById, 2, 'm')).toBe(0.5)
    expect(resolveDiscount(state, accById, 99, 'm')).toBe(1)
  })

  it('prefers the per-model override, then the account value', () => {
    const state: DiscountState = { disc: { 1: '0.3' }, ov: { 1: { m: '0.2', other: '' } } }
    expect(resolveDiscount(state, accById, 1, 'm')).toBe(0.2)
    expect(resolveDiscount(state, accById, 1, 'other')).toBe(0.3)
  })

  it('treats empty and non-numeric input as not set', () => {
    const state: DiscountState = { disc: { 1: '', 2: 'abc' }, ov: {} }
    expect(resolveDiscount(state, accById, 1, 'm')).toBe(1)
    expect(resolveDiscount(state, accById, 2, 'm')).toBe(0.5)
    expect(isDiscountChanged(state, accById[1])).toBe(false)
    expect(isDiscountChanged({ disc: { 1: '1' }, ov: {} }, accById[1])).toBe(false)
    expect(isDiscountChanged({ disc: { 1: '0.9' }, ov: {} }, accById[1])).toBe(true)
  })
})

describe('computeProfit', () => {
  const rows = [
    row({ account_id: 1, model: 'a', list_price: 10, revenue: 8.5, input_tokens: 100, output_tokens: 10 }),
    row({ account_id: 2, model: 'a', list_price: 4, revenue: 3.4, cache_read_tokens: 50, requests: 2 }),
    row({ day: '2026-10-02', account_id: 2, model: 'b', group_id: null, list_price: 2, revenue: 2 })
  ]

  it('matches the GLM example: revenue is the logged value, cost = list × discount', () => {
    const r = computeProfit([row({ list_price: 1.4776, revenue: 1.2559 })], () => 1)
    expect(r.tot.rev).toBeCloseTo(1.2559, 6)
    expect(r.tot.cost).toBeCloseTo(1.4776, 6)
    expect(marginPct(r.tot.rev - r.tot.cost, r.tot.rev)).toBeCloseTo(-17.65, 1)
    const half = computeProfit([row({ list_price: 1.4776, revenue: 1.2559 })], () => 0.5)
    expect(marginPct(half.tot.rev - half.tot.cost, half.tot.rev)).toBeCloseTo(41.17, 1)
  })

  it('aggregates by account, model, group, model×account and day with per-row discounts', () => {
    const r = computeProfit(rows, (id) => (id === 2 ? 0.5 : 1))
    expect(r.tot.req).toBe(4)
    expect(r.tot.tok).toBe(160)
    expect(r.tot.list).toBe(16)
    expect(r.tot.rev).toBeCloseTo(13.9)
    expect(r.tot.cost).toBeCloseTo(10 + 2 + 1)
    expect(r.byAcc['2'].cost).toBeCloseTo(3)
    expect(r.byModel['a'].rev).toBeCloseTo(11.9)
    expect(r.byGroup['null'].list).toBe(2)
    expect(Object.keys(r.byMA)).toHaveLength(3)
    expect(r.byDay['2026-10-02'].rev).toBe(2)
  })

  it('builds detail rows for every tab', () => {
    const r = computeProfit(rows, (id) => (id === 2 ? 0.5 : 1))
    const disc = (id: number) => (id === 2 ? 0.5 : 1)

    const byAccount = buildDetailRows('account', r, accById, grpById, disc)
    expect(byAccount.map((x) => x.name).sort()).toEqual(['acc-1', 'acc-2'])
    expect(byAccount.find((x) => x.name === 'acc-2')?.rate).toBe(0.5)

    const byModel = buildDetailRows('model', r, accById, grpById, disc)
    expect(byModel.find((x) => x.name === 'a')?.n).toBe(2)
    expect(byModel.find((x) => x.name === 'b')?.n).toBe(1)

    const byGroup = buildDetailRows('group', r, accById, grpById, disc, '(none)')
    const g = byGroup.find((x) => x.name === 'G')
    expect(g?.rate).toBe(0.85)
    expect(g?.applied).toBeCloseTo(11.9 / 14)
    expect(byGroup.find((x) => x.name === '(none)')).toBeTruthy()

    const byMA = buildDetailRows('modelacc', r, accById, grpById, disc)
    expect(byMA.filter((x) => x.name === 'a').every((x) => x.multi)).toBe(true)
    expect(byMA.find((x) => x.name === 'b')?.multi).toBe(false)
    expect(byMA.find((x) => x.name === 'a' && x.acc === 'acc-2')?.disc).toBe(0.5)
  })
})

describe('sorting, totals, helpers', () => {
  const r = computeProfit(
    [
      row({ account_id: 1, list_price: 10, revenue: 8 }),
      row({ account_id: 2, list_price: 4, revenue: 0 })
    ],
    () => 1
  )
  const detail = buildDetailRows('account', r, accById, grpById, () => 1)

  it('sorts numbers and strings, nulls last', () => {
    const desc = sortDetailRows(detail, { k: 'rev', dir: 'desc' })
    expect(desc[0].name).toBe('acc-1')
    const byName = sortDetailRows(detail, { k: 'name', dir: 'asc' })
    expect(byName.map((x) => x.name)).toEqual(['acc-1', 'acc-2'])
    const pctAsc = sortDetailRows(detail, { k: 'pct', dir: 'asc' })
    expect(pctAsc[pctAsc.length - 1].pct).toBeNull()
  })

  it('totals detail rows', () => {
    const t = totalDetailRows(detail)
    expect(t.list).toBe(14)
    expect(t.rev).toBe(8)
    expect(t.margin).toBe(-6)
    expect(t.pct).toBeCloseTo(-75)
  })

  it('back-solves a discount from a cost amount', () => {
    expect(solveDiscountFromCost(5, 10)).toBe(0.5)
    expect(solveDiscountFromCost(1, 3)).toBe(0.3333)
    expect(solveDiscountFromCost(-1, 3)).toBe(0)
    expect(solveDiscountFromCost(5, 0)).toBeNull()
  })

  it('writes CSV with BOM and escaping', () => {
    const csv = toCsv(['a', 'b'], [['x,y', 'q"q'], [1, null]])
    expect(csv.startsWith('﻿')).toBe(true)
    expect(csv).toContain('"x,y","q""q"')
    expect(csv.endsWith('1,')).toBe(true)
  })

  it('enumerates days inclusively', () => {
    expect(eachDay('2026-10-30', '2026-11-02')).toEqual(['2026-10-30', '2026-10-31', '2026-11-01', '2026-11-02'])
    expect(eachDay('2026-11-02', '2026-10-30')).toEqual([])
  })
})

describe('RMB settlement (1 U = 0.1 CNY)', () => {
  it('converts U to CNY and back with the system-wide rate', () => {
    expect(uToRmb(1.4776)).toBeCloseTo(0.14776, 10)
    expect(uToRmb(10, 0.2)).toBe(2)
    expect(rmbToU(0.1)).toBeCloseTo(1, 10)
    expect(rmbToU(1, 0)).toBe(0)
    expect(rmbToU(uToRmb(222825.4987))).toBeCloseTo(222825.4987, 6)
  })

  it('formats CNY amounts with the yen sign, thousands separators and a leading minus', () => {
    expect(formatRmb(1.2559)).toBe('¥0.13')
    expect(formatRmb(-12345.678)).toBe('-¥1,234.57')
    expect(formatRmb(1.2559, 4)).toBe('¥0.1256')
    expect(formatRmbCompact(100)).toBe('¥10.00')
    expect(formatRmbCompact(2_000_000)).toBe('¥200,000')
    expect(formatRmbAxis(500)).toBe('¥50')
    expect(formatRmbAxis(50_000)).toBe('¥5K')
    expect(formatRmbAxis(20_000_000)).toBe('¥2.0M')
    expect(formatRmbAxis(-5_000)).toBe('-¥500')
  })

  it('reproduces the 2026-10-29 production audit (30 days, Asia/Shanghai) in CNY', () => {
    // 生产库 2026-09-30~10-29 按账号聚合（U），折扣为 accounts.rate_multiplier 当前值
    const audit: Array<[number, number, number, number]> = [
      // [account_id, discount, list_u, revenue_u]
      [13, 1, 59658.19, 50709.46],
      [72, 1, 46846.97, 46846.97],
      [44, 0.1, 34761.61, 34761.61],
      [9, 1, 40093.71, 34079.66],
      [74, 0.55, 32161.42, 31813.62],
      [82, 0.7546, 118659.95, 17806.47],
      [83, 2.3839, 37543.17, 5688.25],
      [64, 0.7, 729.99, 649.25],
      [88, 0.55, 231.05, 213.06],
      [65, 0.5, 239.33, 203.43],
      [87, 0.45, 38.87, 38.87],
      [85, 0.55, 11.84, 10.57],
      [86, 0.45, 4.81, 4.29]
    ]
    const disc = Object.fromEntries(audit.map(([id, d]) => [id, d]))
    const r = computeProfit(
      audit.map(([id, , list, rev]) => row({ account_id: id, list_price: list, revenue: rev })),
      (id) => disc[id] ?? 1
    )
    // 与 SQL 直接算出的 list 370980.93 / rev 222825.50 / cost 347587.70 / margin -124762.20 U 一致（逐行两位小数的舍入差 < 0.5）
    expect(r.tot.list).toBeCloseTo(370980.93, 0)
    expect(r.tot.rev).toBeCloseTo(222825.5, 0)
    expect(r.tot.cost).toBeCloseTo(347587.7, 0)
    const margin = r.tot.rev - r.tot.cost
    expect(margin).toBeCloseTo(-124762.2, 0)
    expect(marginPct(margin, r.tot.rev)).toBeCloseTo(-56.0, 1)
    // 人民币口径：实付 ¥22,282.55，成本 ¥34,758.77，毛利 -¥12,476.22
    expect(uToRmb(r.tot.rev)).toBeCloseTo(22282.55, 1)
    expect(uToRmb(r.tot.cost)).toBeCloseTo(34758.77, 1)
    expect(formatRmbCompact(margin)).toBe('-¥12,476.22')
    // 两个 truelisa 账号贡献了全部亏损；去掉它们后毛利为正
    const rest = Object.entries(r.byAcc).filter(([id]) => id !== '82' && id !== '83')
    const restRev = rest.reduce((a, [, b]) => a + b.rev, 0)
    const restCost = rest.reduce((a, [, b]) => a + b.cost, 0)
    expect(restRev - restCost).toBeGreaterThan(0)
    expect(uToRmb(restRev - restCost)).toBeCloseTo(3078.3, 0)
  })
})
