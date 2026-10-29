import { describe, expect, it } from 'vitest'
import type { ProfitReportAccount, ProfitReportGroup, ProfitReportRow } from '@/api/admin/profitReport'
import {
  buildDetailRows,
  computeProfit,
  eachDay,
  isDiscountChanged,
  marginPct,
  resolveDiscount,
  solveDiscountFromCost,
  sortDetailRows,
  toCsv,
  totalDetailRows,
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
