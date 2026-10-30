import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

import type { ProfitReportResponse } from '@/api/admin/profitReport'
import ProfitReportView from '../ProfitReportView.vue'

const { getReport, updateAccount, showError, showSuccess, copyToClipboard } = vi.hoisted(() => ({
  getReport: vi.fn(),
  updateAccount: vi.fn(),
  showError: vi.fn(),
  showSuccess: vi.fn(),
  copyToClipboard: vi.fn()
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    profitReport: { getReport },
    accounts: { update: updateAccount }
  }
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ showError, showSuccess })
}))

vi.mock('@/composables/useClipboard', () => ({
  useClipboard: () => ({ copied: false, copyToClipboard })
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string, params?: Record<string, unknown>) =>
        params ? `${key}:${JSON.stringify(params)}` : key
    })
  }
})

const formatLocalDate = (date: Date): string => {
  const year = date.getFullYear()
  const month = String(date.getMonth() + 1).padStart(2, '0')
  const day = String(date.getDate()).padStart(2, '0')
  return `${year}-${month}-${day}`
}

// 与前面核对过的真实日志同一组数字：标价 1.4776，分组 0.85 → 实付 1.2559。
const createReport = (): ProfitReportResponse => ({
  generated_at: '2026-10-29T10:00:00+08:00',
  start_date: '2026-10-26',
  end_date: '2026-10-27',
  timezone: 'Asia/Shanghai',
  rows: [
    {
      day: '2026-10-26',
      model: 'glm-5.3-flash',
      account_id: 13,
      group_id: 7,
      requests: 1,
      input_tokens: 9639,
      output_tokens: 1061,
      cache_creation_tokens: 0,
      cache_read_tokens: 11328,
      list_price: 1.4776,
      revenue: 1.2559
    },
    {
      day: '2026-10-27',
      model: 'glm-5.3-flash',
      account_id: 72,
      group_id: 7,
      requests: 3,
      input_tokens: 300,
      output_tokens: 30,
      cache_creation_tokens: 0,
      cache_read_tokens: 0,
      list_price: 1,
      revenue: 0.85
    }
  ],
  accounts: [
    { id: 13, name: 'GLM5-小豪', platform: 'openai', rate_multiplier: 1, status: 'active', schedulable: true, deleted: false, last_used_at: null },
    { id: 72, name: 'DeepSeek', platform: 'openai', rate_multiplier: 1, status: 'active', schedulable: false, deleted: false, last_used_at: null },
    { id: 99, name: 'idle', platform: 'anthropic', rate_multiplier: 1, status: 'active', schedulable: true, deleted: false, last_used_at: null }
  ],
  groups: [{ id: 7, name: 'GLM-官方', platform: 'openai', rate_multiplier: 0.85, deleted: false }]
})

const mountView = () =>
  mount(ProfitReportView, {
    global: {
      stubs: {
        AppLayout: { template: '<div><slot /></div>' },
        DateRangePicker: true,
        Select: true,
        Icon: true,
        ConfirmDialog: {
          props: ['show'],
          emits: ['confirm', 'cancel'],
          template: '<div v-if="show" data-test="confirm"><button data-test="confirm-btn" @click="$emit(\'confirm\')">ok</button></div>'
        },
        ProfitTrendChart: true
      }
    }
  })

describe('admin ProfitReportView', () => {
  beforeEach(() => {
    localStorage.clear()
    getReport.mockReset()
    updateAccount.mockReset()
    showError.mockReset()
    showSuccess.mockReset()
    copyToClipboard.mockReset()
    getReport.mockResolvedValue(createReport())
  })

  it('settles in RMB only: no unit switch, legacy U preference is ignored and not persisted', async () => {
    localStorage.setItem('profit-report-v1', JSON.stringify({ currency: 'U', rate: 7, tab: 'model' }))
    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.find('[data-test="settlement"]').text()).toBe('¥ · 1 U = 0.1 admin.profitReport.yuan')
    expect(wrapper.text()).not.toContain('admin.profitReport.unitU')
    // 金额仍按 1 U = 0.1 元显示，不受旧偏好里的 U / 汇率影响
    expect(wrapper.text()).toContain('¥0.21')
    expect(wrapper.text()).toContain('¥0.25')
    // 其它偏好（tab）照常恢复

    const saved = JSON.parse(localStorage.getItem('profit-report-v1') || '{}')
    expect(saved).not.toHaveProperty('currency')
    expect(saved).not.toHaveProperty('rate')
    expect(saved.tab).toBe('model')
  })

  it('exports money columns converted to RMB with the CNY unit in the header', async () => {
    const wrapper = mountView()
    await flushPromises()

    const copyBtn = wrapper.findAll('button').find((b) => b.text() === 'admin.profitReport.export.copy')
    expect(copyBtn).toBeTruthy()
    await copyBtn!.trigger('click')

    expect(copyToClipboard).toHaveBeenCalledTimes(1)
    const csv = copyToClipboard.mock.calls[0][0] as string
    const [header, ...body] = csv.replace(/^\uFEFF/, '').split('\n')
    expect(header).toContain('admin.profitReport.export.unitRmb')
    expect(header).not.toContain('admin.profitReport.export.unitU')
    // 账号 13：标价 1.4776 U → 0.1478 元；实付 1.2559 U → 0.1256 元
    const glm = body.find((l) => l.startsWith('GLM5-小豪'))
    expect(glm).toBeTruthy()
    expect(glm).toContain('0.1478')
    expect(glm).toContain('0.1256')
    expect(glm).not.toContain('1.4776')
  })

  it('loads the last 30 days by default', async () => {
    mountView()
    await flushPromises()

    const now = new Date()
    const start = new Date(now.getTime() - 29 * 24 * 60 * 60 * 1000)
    expect(getReport).toHaveBeenCalledTimes(1)
    expect(getReport).toHaveBeenCalledWith({ start_date: formatLocalDate(start), end_date: formatLocalDate(now) })
  })

  it('computes KPIs from logged revenue and list price × DB multiplier, shown in RMB at 0.1', async () => {
    const wrapper = mountView()
    await flushPromises()

    const text = wrapper.text()
    // 实付 2.1059 U → ¥0.21；标价 2.4776 U → 成本 ¥0.25（倍率 1.0）；毛利率 (2.1059-2.4776)/2.1059 = -17.6%
    expect(text).toContain('¥0.21')
    expect(text).toContain('¥0.25')
    expect(text).toContain('-17.6%')
    // 请求数 4，token 22,358
    expect(text).toContain('22K')
    // 页面标出接口实际返回的区间和切日时区，便于核对数字属于哪个区间
    const snapshot = wrapper.find('[data-test="snapshot"]').text()
    expect(snapshot).toContain('"start":"2026-10-26"')
    expect(snapshot).toContain('"end":"2026-10-27"')
    expect(snapshot).toContain('"tz":"Asia/Shanghai"')
  })

  it('hides idle accounts by default and recomputes when a discount is entered', async () => {
    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.text()).not.toContain('idle')

    const discInput = wrapper.find('input[aria-label="GLM5-小豪 admin.profitReport.discount.discount"]')
    expect(discInput.exists()).toBe(true)
    await discInput.setValue('0.5')
    await flushPromises()

    // 账号 13 成本 1.4776×0.5=0.7388，毛利率 41.2%
    expect(wrapper.text()).toContain('41.2%')
    expect(wrapper.find('tr.pr-changed').exists()).toBe(true)
    // 持久化到 localStorage
    const saved = JSON.parse(localStorage.getItem('profit-report-v1') || '{}')
    expect(saved.disc['13']).toBe('0.5')
  })

  it('back-solves the discount from an entered cost amount', async () => {
    const wrapper = mountView()
    await flushPromises()

    const costInput = wrapper.find('input[aria-label="GLM5-小豪 admin.profitReport.discount.costEditable"]')
    expect(costInput.exists()).toBe(true)
    // 成本框按人民币填：¥0.10 = 1 U → 折扣 1 / 1.4776 = 0.6768
    await costInput.setValue('0.10')
    await costInput.trigger('change')
    await flushPromises()

    const saved = JSON.parse(localStorage.getItem('profit-report-v1') || '{}')
    expect(Number(saved.disc['13'])).toBeCloseTo(0.6768, 3)
  })

  it('saves changed discounts through the account update API and reloads', async () => {
    updateAccount.mockResolvedValue({})
    const wrapper = mountView()
    await flushPromises()

    const discInput = wrapper.find('input[aria-label="GLM5-小豪 admin.profitReport.discount.discount"]')
    await discInput.setValue('0.3')
    await flushPromises()

    const saveBtn = wrapper.findAll('button').find((b) => b.text().startsWith('admin.profitReport.discount.saveChanged'))
    expect(saveBtn).toBeTruthy()
    expect(saveBtn!.attributes('disabled')).toBeUndefined()
    await saveBtn!.trigger('click')
    await flushPromises()

    await wrapper.find('[data-test="confirm-btn"]').trigger('click')
    await flushPromises()

    expect(updateAccount).toHaveBeenCalledTimes(1)
    expect(updateAccount).toHaveBeenCalledWith(13, { rate_multiplier: 0.3 })
    expect(showSuccess).toHaveBeenCalled()
    expect(getReport).toHaveBeenCalledTimes(2)
  })

  it('shows an error toast when loading fails', async () => {
    getReport.mockRejectedValueOnce(new Error('boom'))
    mountView()
    await flushPromises()
    expect(showError).toHaveBeenCalledWith('admin.profitReport.loadFailed')
  })
})
