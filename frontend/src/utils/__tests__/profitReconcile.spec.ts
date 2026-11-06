import { describe, expect, it } from 'vitest'
import * as XLSX from 'xlsx'
import type {
  ProfitReportAccount,
  ReconcileUsageRow,
  SupplierBillLine,
  SupplierBinding
} from '@/api/admin/profitReport'
import {
  billPayable,
  billTokenTotal,
  isBillTotalOnly,
  buildReconcileLines,
  buildTemplateRows,
  computeKpis,
  normalizePeriod,
  parseNumberCell,
  parseTemplateRows,
  splitAliases,
  STATUS_LABEL_ZH,
  suggestBinding,
  TEMPLATE_HEADERS,
  toSheetCells,
  tokensMatch
} from '../profitReconcile'

const account = (id: number, name: string, rate = 1): ProfitReportAccount => ({
  id,
  name,
  platform: 'anthropic',
  rate_multiplier: rate,
  status: 'active',
  schedulable: true,
  deleted: false,
  last_used_at: null
})

const usage = (p: Partial<ReconcileUsageRow>): ReconcileUsageRow => ({
  period: '2026-08',
  model: 'glm-5.2',
  account_id: 1,
  requests: 0,
  input_tokens: 0,
  output_tokens: 0,
  cache_creation_tokens: 0,
  cache_read_tokens: 0,
  list_price: 0,
  revenue: 0,
  ...p
})

const bill = (p: Partial<SupplierBillLine>): SupplierBillLine => ({
  period: '2026-08',
  supplier: '金山云',
  supplier_account: '2000186841',
  standard_model: 'glm-5.2',
  platform_models: '',
  cost_discount: null,
  currency: 'RMB',
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
  note: '',
  ...p
})

const KS: SupplierBinding[] = [
  { account_id: 1, supplier: '金山云', supplier_account: '2000186841' },
  { account_id: 2, supplier: '金山云', supplier_account: '2000186841' }
]

describe('billPayable', () => {
  it('returns null when every money field is blank, defaults fx to 1', () => {
    expect(billPayable(bill({}))).toBeNull()
    expect(billPayable(bill({ resource_fee: 100, discount_amount: 45, adjustment: -5, tax: 6 }))).toBeCloseTo(56)
    expect(billPayable(bill({ resource_fee: 10, fx_rate: 7.2 }))).toBeCloseTo(72)
    expect(billPayable(bill({ adjustment: -6120.65 }))).toBeCloseTo(-6120.65)
  })
})

describe('tokensMatch', () => {
  it('tolerates ±0.5%', () => {
    expect(tokensMatch(1000, 1005)).toBe(true)
    expect(tokensMatch(1000, 1006)).toBe(false)
    expect(tokensMatch(0, 0)).toBe(true)
    expect(tokensMatch(0, 1)).toBe(false)
  })
})

describe('buildReconcileLines — 2026-08 金山云', () => {
  // 后台：glm 标价 464,598.73 U（≈ ¥46,459.87），deepseek 标价 16.6 U；两个账号都绑金山云。
  const rows = [
    usage({ account_id: 1, model: 'glm-5.2', requests: 157813, input_tokens: 1_296_000_000, cache_read_tokens: 16_700_000_000, output_tokens: 102_600_000, list_price: 464598.727, revenue: 400189.8 }),
    usage({ account_id: 2, model: 'deepseek-v4-flash', requests: 89, input_tokens: 1_440_577, cache_read_tokens: 3_122_283, output_tokens: 117_830, list_price: 16.6, revenue: 16.6 })
  ]
  const accounts = [account(1, 'ks-anthropic-glm-5.2', 0.5522), account(2, 'ks-deepseek-v4-flash-anthropic', 1)]
  const bills = [
    bill({ standard_model: 'glm-5.2', input_tokens: 1_296_590_057, cache_read_tokens: 16_699_964_970, output_tokens: 102_648_169, resource_fee: 46646.86, discount_amount: 14837.76, cost_discount: 0.5522 }),
    bill({ standard_model: 'deepseek-v4-flash', input_tokens: 1_440_577, cache_read_tokens: 3_122_283, output_tokens: 117_830, resource_fee: 2.36, discount_amount: 0.6, cost_discount: 1.06 }),
    bill({ standard_model: '调账', adjustment: -6120.65, note: '8/7-8/17 折扣补退' })
  ]
  const lines = buildReconcileLines(rows, bills, KS, accounts)

  it('matches backend usage to the bill line of the same supplier account and model', () => {
    expect(lines).toHaveLength(3)
    const glm = lines.find((l) => l.standardModel === 'glm-5.2')!
    expect(glm.accountNames).toEqual(['ks-anthropic-glm-5.2'])
    expect(glm.payable).toBeCloseTo(31809.1)
    expect(glm.estimate).toBeCloseTo(464598.727 * 0.5522 * 0.1, 2)
    expect(glm.discountSource).toBe('bill')
    expect(glm.revenue).toBeCloseTo(40018.98)
    // 金额差 = 账单 31809.10 − 估算 25655.14 ≈ 6153.96，整笔调账另起一行
    expect(glm.amountDiff).toBeCloseTo(31809.1 - 25655.14, 0)
    expect(glm.status).toBe('diff')
  })

  it('keeps the adjustment as its own line and adds it back into the KPI amount diff', () => {
    const adj = lines.find((l) => l.standardModel === '调账')!
    expect(adj.status).toBe('adjustment')
    expect(adj.payable).toBeCloseTo(-6120.65)
    expect(adj.tokenDiff).toBeNull()
    expect(lines[lines.length - 1]).toBe(adj)

    const k = computeKpis(lines)
    expect(k.payable).toBeCloseTo(31809.1 + 1.76 - 6120.65, 2) // = 25690.21，与 9 月账单反推的 8 月结算一致
    expect(k.adjustments).toBeCloseTo(-6120.65)
    // 调账加回后，金额差额只剩估算误差，而不是 6000 多
    expect(Math.abs(k.amountDiff)).toBeLessThan(40)
    expect(k.margin).toBeCloseTo(k.revenue - k.payable)
  })

  it('marks the deepseek line as matching when tokens equal and money is within 0.01', () => {
    const ds = lines.find((l) => l.standardModel === 'deepseek-v4-flash')!
    expect(ds.tokenDiff).toBe(0)
    expect(ds.estimate).toBeCloseTo(16.6 * 1.06 * 0.1)
    expect(ds.payable).toBeCloseTo(1.76)
    expect(ds.status).toBe('match')
  })
})

describe('buildReconcileLines — statuses and matching', () => {
  const accounts = [account(1, 'ks-a', 0.5), account(2, 'ks-b'), account(9, 'yuguan-glm5.2', 0.82)]

  it('uses platform_models aliases to match a different billing model name', () => {
    const lines = buildReconcileLines(
      [usage({ account_id: 1, model: 'GLM-5.2-Thinking', requests: 1, input_tokens: 10, list_price: 10 })],
      [bill({ standard_model: 'glm-5.2', platform_models: 'glm-5.2-thinking， glm5.2', input_tokens: 10, resource_fee: 0.5 })],
      KS,
      accounts
    )
    expect(lines).toHaveLength(1)
    expect(lines[0].models).toEqual(['GLM-5.2-Thinking'])
    expect(lines[0].discountSource).toBe('account')
    expect(lines[0].estimate).toBeCloseTo(10 * 0.5 * 0.1)
    expect(lines[0].status).toBe('match')
  })

  it('merges two accounts bound to the same supplier account into one line', () => {
    const lines = buildReconcileLines(
      [
        usage({ account_id: 1, requests: 2, input_tokens: 10, list_price: 10 }),
        usage({ account_id: 2, requests: 3, input_tokens: 20, list_price: 10 })
      ],
      [],
      KS,
      accounts
    )
    expect(lines).toHaveLength(1)
    expect(lines[0].requests).toBe(5)
    expect(lines[0].accountIds).toEqual([1, 2])
    expect(lines[0].discountSource).toBe('account')
    expect(lines[0].discount).toBeCloseTo(0.75) // (10×0.5 + 10×1) / 20
    expect(lines[0].status).toBe('missingBill')
  })

  it('keeps unbound accounts apart and flags them', () => {
    const lines = buildReconcileLines(
      [usage({ account_id: 9, requests: 1, input_tokens: 5, list_price: 1 }), usage({ account_id: 3, requests: 1, input_tokens: 5, list_price: 1 })],
      [],
      KS,
      accounts
    )
    expect(lines).toHaveLength(2)
    expect(lines.every((l) => l.status === 'unbound')).toBe(true)
    expect(lines.map((l) => l.accountNames[0]).sort()).toEqual(['#3', 'yuguan-glm5.2'])
  })

  it('flags bill-only, no-usage and amount-pending lines', () => {
    const lines = buildReconcileLines(
      [usage({ account_id: 1, model: 'kimi', requests: 1, input_tokens: 5, list_price: 1 })],
      [
        bill({ standard_model: 'minimax-m3', input_tokens: 100, resource_fee: 1 }),
        bill({ standard_model: 'deepseek-v4-flash', input_tokens: 0, resource_fee: 0 }),
        bill({ standard_model: 'kimi', input_tokens: 5 })
      ],
      KS,
      accounts
    )
    const by = Object.fromEntries(lines.map((l) => [l.standardModel, l.status]))
    expect(by).toEqual({ 'minimax-m3': 'billOnly', 'deepseek-v4-flash': 'noUsage', kimi: 'amountPending' })
    expect(computeKpis(lines).pending).toBe(2)
  })

  it('does not match bills across periods', () => {
    const lines = buildReconcileLines(
      [usage({ period: '2026-09', account_id: 1, requests: 1, input_tokens: 5, list_price: 1 })],
      [bill({ period: '2026-08', input_tokens: 5, resource_fee: 1 })],
      KS,
      accounts
    )
    expect(lines.map((l) => [l.period, l.status])).toEqual([
      ['2026-08', 'billOnly'],
      ['2026-09', 'missingBill']
    ])
  })
})

describe('import helpers', () => {
  it('normalizes period spellings', () => {
    expect(normalizePeriod('2026-08')).toBe('2026-08')
    expect(normalizePeriod('2026/8')).toBe('2026-08')
    expect(normalizePeriod('2026年8月')).toBe('2026-08')
    expect(normalizePeriod('2026-08-01')).toBe('2026-08')
    expect(normalizePeriod(202608)).toBe('2026-08')
    expect(normalizePeriod(new Date(2026, 7, 15))).toBe('2026-08')
    expect(normalizePeriod('2026-13')).toBeNull()
    expect(normalizePeriod('八月')).toBeNull()
    expect(normalizePeriod('')).toBeNull()
  })

  it('parses number cells', () => {
    expect(parseNumberCell(null)).toBeNull()
    expect(parseNumberCell('  ')).toBeNull()
    expect(parseNumberCell('¥25,653.52')).toBeCloseTo(25653.52)
    expect(parseNumberCell(-6120.65)).toBe(-6120.65)
    expect(parseNumberCell('abc')).toBeNaN()
  })

  it('splits aliases on Chinese and ASCII separators', () => {
    expect(splitAliases('a, b，c、d;e\nf')).toEqual(['a', 'b', 'c', 'd', 'e', 'f'])
  })

  it('suggests bindings for known prefixes only', () => {
    expect(suggestBinding('ks-anthropic-glm-5.2')).toEqual({ supplier: '金山云', supplier_account: '2000186841' })
    expect(suggestBinding('parat_minimax_m3')?.supplier).toBe('并行科技')
    expect(suggestBinding('yuguan-glm5.2')).toBeNull()
  })
})

describe('parseTemplateRows', () => {
  const H = [...TEMPLATE_HEADERS] as string[]
  const rowWith = (vals: Record<string, unknown>) => H.map((h) => (h in vals ? vals[h] : null))

  it('finds the header anywhere (template puts it on row 20) and imports bill rows + adjustments only', () => {
    const aoa: unknown[][] = [
      ['收入成本看板｜单工作表模板'],
      [],
      ['账期筛选（空白=全部）'],
      H,
      rowWith({ 账期: '2026-08', 供应商: '金山云', '供应商账号/API Key': '2000186841', 标准模型: 'glm-5.2', 账号成本折扣: 0.5522, '账单资源费(原币)': '46,646.86', '折扣/券抵扣(原币)': 14837.76, '汇率(RMB/原币)': 1, 后台输入Token: 999 }),
      rowWith({ 账期: '2026-08', 标准模型: '调账', '调账(原币，有正负)': -6120.65, '备注/厂商待问事项': '补退' }),
      rowWith({ 账期: '2026-08', 标准模型: 'kimi', 后台输入Token: 5, '汇率(RMB/原币)': 1 }), // 只有后台数据 → 跳过
      rowWith({}), // 模板里预置公式的空行 → 忽略
      rowWith({ 账期: 202609, 标准模型: 'glm-5.2', '账单资源费(原币)': 11472.33 })
    ]
    const r = parseTemplateRows(aoa)
    expect(r.errors).toEqual([])
    expect(r.skipped).toBe(1)
    expect(r.periods).toEqual(['2026-08', '2026-09'])
    expect(r.lines).toHaveLength(3)
    expect(r.lines[0]).toMatchObject({ supplier_account: '2000186841', resource_fee: 46646.86, cost_discount: 0.5522, fx_rate: 1, input_tokens: null })
    expect(r.lines[1]).toMatchObject({ standard_model: '调账', adjustment: -6120.65, note: '补退' })
  })

  it('reports bad cells with the Excel row number and skips the row', () => {
    const aoa: unknown[][] = [H, rowWith({ 账期: '2026-08', 标准模型: 'glm', '账单资源费(原币)': '一百' }), rowWith({ 账期: '八月', 标准模型: 'x' })]
    const r = parseTemplateRows(aoa)
    expect(r.lines).toEqual([])
    expect(r.errors).toEqual(['第 2 行：「账单资源费(原币)」不是数字', '第 3 行：账期「八月」不是 YYYY-MM'])
  })

  it('fails clearly when the header row is missing', () => {
    expect(parseTemplateRows([['a', 'b']]).errors[0]).toContain('找不到表头')
  })
})

describe('buildTemplateRows round trip', () => {
  it('exports the template layout with formulas and re-imports the bill half', () => {
    const accounts = [account(1, 'ks-a', 0.5), account(9, 'free')]
    const lines = buildReconcileLines(
      [usage({ account_id: 1, requests: 2, input_tokens: 10, list_price: 10, revenue: 8 }), usage({ account_id: 9, model: 'x', requests: 1, list_price: 1 })],
      [bill({ input_tokens: 10, resource_fee: 0.5, note: 'n' }), bill({ standard_model: '调账', adjustment: -0.1 })],
      KS,
      accounts
    )
    const aoa = buildTemplateRows(lines)
    expect(aoa[0]).toEqual([...TEMPLATE_HEADERS])
    expect(aoa[0]).toHaveLength(35)
    const glm = aoa[1]
    expect(glm[TEMPLATE_HEADERS.indexOf('后台标价(U)')]).toBe(10)
    expect(glm[TEMPLATE_HEADERS.indexOf('账号成本折扣')]).toBe(0.5)
    expect(glm[TEMPLATE_HEADERS.indexOf('后台Token合计')]).toEqual({ f: 'SUM(G2:K2)', v: 10 })
    expect(glm[TEMPLATE_HEADERS.indexOf('后台估算成本(RMB)')]).toMatchObject({ f: 'M2*N2*O2' })
    expect(glm[TEMPLATE_HEADERS.indexOf('自动核对状态')]).toBe(STATUS_LABEL_ZH.match)
    const unbound = aoa.find((r) => r[TEMPLATE_HEADERS.indexOf('标准模型')] === 'x')!
    expect(unbound[TEMPLATE_HEADERS.indexOf('备注/厂商待问事项')]).toBe('平台账号：free')

    // 写成真正的 xlsx 再读回来，模拟财务填写后发回
    const ws = XLSX.utils.aoa_to_sheet(toSheetCells(aoa) as XLSX.CellObject[][])
    const wb = XLSX.utils.book_new()
    XLSX.utils.book_append_sheet(wb, ws, '收入成本看板')
    const buf = XLSX.write(wb, { type: 'array', bookType: 'xlsx' })
    const back = XLSX.read(buf, { type: 'array', cellFormula: true })
    const p2 = back.Sheets[back.SheetNames[0]]['P2']
    expect(p2.f).toBe('SUM(G2:K2)')
    expect(p2.t).toBe('n')
    expect(p2.v).toBe(10)
    const rows = XLSX.utils.sheet_to_json<unknown[]>(back.Sheets[back.SheetNames[0]], { header: 1, defval: null })
    const parsed = parseTemplateRows(rows)
    expect(parsed.errors).toEqual([])
    expect(parsed.skipped).toBe(1) // 未绑定账号那行只有后台数据
    expect(parsed.lines.map((l) => [l.standard_model, l.resource_fee, l.adjustment, l.note])).toEqual([
      ['glm-5.2', 0.5, null, 'n'],
      ['调账', null, -0.1, '']
    ])
  })
})

// 财务确认的最终版表头（2026-11-06），一字不差。
const FINAL_HEADERS = [
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
  '后台总Token（无分类时填）',
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
  '账单其他/总Token兜底',
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
]

describe('final template headers', () => {
  it('match the finance-approved list exactly', () => {
    expect([...TEMPLATE_HEADERS]).toEqual(FINAL_HEADERS)
  })

  it('import the fallback token column under both the new and the old name', () => {
    for (const name of ['账单其他/总Token兜底', '账单其他Token']) {
      const H = FINAL_HEADERS.map((h) => (h === '账单其他/总Token兜底' ? name : h))
      const row = H.map((h) => (h === '账期' ? '2026-08' : h === '标准模型' ? 'x' : h === name ? 1234 : null))
      const r = parseTemplateRows([H, row])
      expect(r.errors).toEqual([])
      expect(r.lines[0].other_tokens).toBe(1234)
    }
  })

  it('leaves the backend total column blank on export (backend tokens are always categorized)', () => {
    const lines = buildReconcileLines([usage({ account_id: 1, requests: 1, input_tokens: 10, list_price: 1 })], [], KS, [account(1, 'ks-a')])
    const row = buildTemplateRows(lines)[1]
    expect(row[FINAL_HEADERS.indexOf('后台总Token（无分类时填）')]).toBeNull()
    expect(row[FINAL_HEADERS.indexOf('后台Token合计')]).toEqual({ f: 'SUM(G2:K2)', v: 10 })
  })
})

describe('fallback token column semantics', () => {
  it('is added as "other" when any category is filled', () => {
    const b = bill({ input_tokens: 100, output_tokens: 10, other_tokens: 5 })
    expect(isBillTotalOnly(b)).toBe(false)
    expect(billTokenTotal(b)).toBe(115)
  })

  it('is the total when every category is blank, and the line is flagged total-only', () => {
    const b = bill({ other_tokens: 110, resource_fee: 1 })
    expect(isBillTotalOnly(b)).toBe(true)
    expect(billTokenTotal(b)).toBe(110)
    const [line] = buildReconcileLines(
      [usage({ account_id: 1, requests: 1, input_tokens: 100, output_tokens: 10, list_price: 10 })],
      [b],
      KS,
      [account(1, 'ks-a', 1)]
    )
    expect(line.billTotalOnly).toBe(true)
    expect(line.tokenDiff).toBe(0)
    expect(line.status).toBe('match')
  })

  it('does not flag adjustments or bills without any tokens', () => {
    expect(isBillTotalOnly(bill({ resource_fee: 1 }))).toBe(false)
    const [adj] = buildReconcileLines([], [bill({ standard_model: '调账', other_tokens: 1, adjustment: -1 })], KS, [])
    expect(adj.billTotalOnly).toBe(false)
  })
})
