import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import * as XLSX from 'xlsx'

import type { ReconcileResponse } from '@/api/admin/profitReport'
import { TEMPLATE_HEADERS } from '@/utils/profitReconcile'
import MonthlyReconcilePanel from '../MonthlyReconcilePanel.vue'

const { getReconcile, saveSupplierBinding, replaceSupplierBills, showError, showSuccess, saveAs } = vi.hoisted(() => ({
  getReconcile: vi.fn(),
  saveSupplierBinding: vi.fn(),
  replaceSupplierBills: vi.fn(),
  showError: vi.fn(),
  showSuccess: vi.fn(),
  saveAs: vi.fn()
}))

vi.mock('@/api/admin', () => ({
  adminAPI: { profitReport: { getReconcile, saveSupplierBinding, replaceSupplierBills } }
}))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showError, showSuccess }) }))
vi.mock('file-saver', () => ({ saveAs }))
vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({ t: (key: string, params?: Record<string, unknown>) => (params ? `${key}:${JSON.stringify(params)}` : key) })
  }
})

const account = (id: number, name: string, rate = 1) => ({
  id,
  name,
  platform: 'anthropic',
  rate_multiplier: rate,
  status: 'active',
  schedulable: true,
  deleted: false,
  last_used_at: null
})

const createData = (): ReconcileResponse => ({
  generated_at: '2026-11-06T10:00:00+08:00',
  start_period: '2026-08',
  end_period: '2026-08',
  timezone: 'Asia/Shanghai',
  rows: [
    { period: '2026-08', model: 'glm-5.2', account_id: 1, requests: 10, input_tokens: 100, output_tokens: 0, cache_creation_tokens: 0, cache_read_tokens: 0, list_price: 100, revenue: 90 },
    { period: '2026-08', model: 'glm-5.2', account_id: 2, requests: 1, input_tokens: 5, output_tokens: 0, cache_creation_tokens: 0, cache_read_tokens: 0, list_price: 1, revenue: 1 }
  ],
  accounts: [account(1, 'ks-anthropic-glm-5.2', 0.5), account(2, 'yuguan-glm5.2')],
  bindings: [{ account_id: 1, supplier: '金山云', supplier_account: '2000186841' }],
  bills: [
    {
      period: '2026-08', supplier: '金山云', supplier_account: '2000186841', standard_model: 'glm-5.2', platform_models: '',
      cost_discount: null, currency: 'RMB', input_tokens: 100, output_tokens: null, cache_read_tokens: null, cache_write_tokens: null,
      other_tokens: null, resource_fee: 5, discount_amount: null, adjustment: null, tax: null, fx_rate: 1, note: ''
    },
    {
      period: '2026-08', supplier: '金山云', supplier_account: '2000186841', standard_model: '调账', platform_models: '',
      cost_discount: null, currency: 'RMB', input_tokens: null, output_tokens: null, cache_read_tokens: null, cache_write_tokens: null,
      other_tokens: null, resource_fee: null, discount_amount: null, adjustment: -1, tax: null, fx_rate: 1, note: '补退'
    }
  ]
})

const mountPanel = async () => {
  const wrapper = mount(MonthlyReconcilePanel, {
    global: { stubs: { ConfirmDialog: { props: ['show', 'message'], template: '<div v-if="show" data-test="confirm"><pre>{{ message }}</pre><button data-test="ok" @click="$emit(\'confirm\')" /></div>' }, Icon: true } }
  })
  await flushPromises()
  return wrapper
}

const xlsxFile = (aoa: unknown[][]): File => {
  const wb = XLSX.utils.book_new()
  XLSX.utils.book_append_sheet(wb, XLSX.utils.aoa_to_sheet(aoa), '收入成本看板')
  const buf = XLSX.write(wb, { type: 'array', bookType: 'xlsx' }) as ArrayBuffer
  const file = new File([buf], 'bill.xlsx')
  // jsdom 的 File 没有 arrayBuffer()
  Object.defineProperty(file, 'arrayBuffer', { value: async () => buf })
  return file
}

const chooseFile = async (wrapper: Awaited<ReturnType<typeof mountPanel>>, file: File) => {
  const input = wrapper.get('[data-test="import-file"]')
  Object.defineProperty(input.element, 'files', { value: [file], configurable: true })
  await input.trigger('change')
  await flushPromises()
  await new Promise((r) => setTimeout(r, 0))
  await flushPromises()
}

describe('MonthlyReconcilePanel', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    getReconcile.mockResolvedValue(createData())
    saveSupplierBinding.mockResolvedValue(undefined)
    replaceSupplierBills.mockResolvedValue(undefined)
  })

  it('loads last month and renders reconciled rows with statuses', async () => {
    const wrapper = await mountPanel()
    expect(getReconcile).toHaveBeenCalledTimes(1)
    const { start_period, end_period } = getReconcile.mock.calls[0][0]
    expect(start_period).toMatch(/^\d{4}-\d{2}$/)
    expect(end_period).toBe(start_period)

    const statuses = wrapper.findAll('[data-test="detail-table"] tbody tr').map((tr) => tr.attributes('data-status'))
    // 金山云 glm（估算 100×0.5×0.1=5，账单 5，token 一致）→ 一致；调账；yuguan 未绑定
    expect(statuses).toEqual(['match', 'adjustment', 'unbound'])
    expect(wrapper.get('[data-test="unbound-count"]').text()).toContain('"n":1')
    // 待处理 = 1（未绑定那行）
    expect(wrapper.get('[data-test="kpis"]').text()).toContain('"n":3')
  })

  it('filters to rows that need attention', async () => {
    const wrapper = await mountPanel()
    await wrapper.get('[data-test="status-filter"]').setValue('pending')
    const statuses = wrapper.findAll('[data-test="detail-table"] tbody tr').map((tr) => tr.attributes('data-status'))
    expect(statuses).toEqual(['unbound'])
  })

  it('saves a binding and reloads', async () => {
    const wrapper = await mountPanel()
    const row = wrapper.findAll('[data-test="bindings-table"] tbody tr').find((tr) => tr.text().includes('yuguan-glm5.2'))!
    const [sup, acc] = row.findAll('input')
    await sup.setValue('育冠')
    await acc.setValue('yg-001')
    await row.findAll('button').find((b) => b.text() === 'common.save')!.trigger('click')
    await flushPromises()
    expect(saveSupplierBinding).toHaveBeenCalledWith(2, '育冠', 'yg-001')
    expect(getReconcile).toHaveBeenCalledTimes(2)
  })

  it('exports the template as xlsx', async () => {
    const wrapper = await mountPanel()
    await wrapper.get('[data-test="export"]').trigger('click')
    await flushPromises()
    await new Promise((r) => setTimeout(r, 0))
    expect(saveAs).toHaveBeenCalledTimes(1)
    const [blob, name] = saveAs.mock.calls[0]
    expect(name).toMatch(/^收入成本对账_\d{4}-\d{2}\.xlsx$/)
    const buf = await new Promise<ArrayBuffer>((resolve) => {
      const fr = new FileReader()
      fr.onload = () => resolve(fr.result as ArrayBuffer)
      fr.readAsArrayBuffer(blob as Blob)
    })
    const wb = XLSX.read(buf, { type: 'array' })
    const rows = XLSX.utils.sheet_to_json<unknown[]>(wb.Sheets['收入成本看板'], { header: 1 })
    expect(rows[0]).toEqual([...TEMPLATE_HEADERS])
    expect(rows).toHaveLength(4)
  })

  it('previews an import and replaces the periods after confirmation', async () => {
    const wrapper = await mountPanel()
    const H = [...TEMPLATE_HEADERS] as string[]
    const row = (v: Record<string, unknown>) => H.map((h) => v[h] ?? null)
    await chooseFile(
      wrapper,
      xlsxFile([
        ['收入成本看板｜单工作表模板'],
        H,
        row({ 账期: '2026-08', 供应商: '金山云', '供应商账号/API Key': '2000186841', 标准模型: 'glm-5.2', '账单资源费(原币)': 6 }),
        row({ 账期: '2026-08', 标准模型: '调账', '调账(原币，有正负)': -2 }),
        row({ 账期: '2026-08', 标准模型: 'kimi', 后台输入Token: 1 })
      ])
    )
    const confirm = wrapper.get('[data-test="confirm"]')
    expect(confirm.text()).toContain('"n":2') // 导入 2 行
    expect(confirm.text()).toContain('"old":"2"') // 2026-08 现有 2 行
    expect(confirm.text()).toContain('importSkipped')

    await confirm.get('[data-test="ok"]').trigger('click')
    await flushPromises()
    expect(replaceSupplierBills).toHaveBeenCalledTimes(1)
    const [periods, lines] = replaceSupplierBills.mock.calls[0]
    expect(periods).toEqual(['2026-08'])
    expect(lines.map((l: { standard_model: string }) => l.standard_model)).toEqual(['glm-5.2', '调账'])
    expect(showSuccess).toHaveBeenCalled()
    expect(getReconcile).toHaveBeenCalledTimes(2)
  })

  it('refuses to import a file with bad cells', async () => {
    const wrapper = await mountPanel()
    const H = [...TEMPLATE_HEADERS] as string[]
    await chooseFile(wrapper, xlsxFile([H, H.map((h) => (h === '账期' ? '2026-08' : h === '标准模型' ? 'glm' : h === '账单资源费(原币)' ? '一百' : null))]))
    expect(wrapper.find('[data-test="confirm"]').exists()).toBe(false)
    expect(showError).toHaveBeenCalledWith(expect.stringContaining('第 2 行'))
    expect(replaceSupplierBills).not.toHaveBeenCalled()
  })
})
