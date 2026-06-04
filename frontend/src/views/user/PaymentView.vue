<template>
  <AppLayout>
    <div class="mx-auto max-w-4xl space-y-6">
      <!-- Tab 栏 -->
      <div class="flex border-b border-gray-200 dark:border-dark-700">
        <button
          @click="activeTab = 'recharge'"
          :class="[
            'px-4 py-3 text-sm font-medium border-b-2 transition-colors',
            activeTab === 'recharge'
              ? 'border-blue-500 text-blue-600 dark:text-blue-400'
              : 'border-transparent text-gray-500 hover:text-gray-700 dark:text-dark-400 dark:hover:text-dark-200'
          ]"
        >充值</button>
        <button
          @click="activeTab = 'orders'"
          :class="[
            'px-4 py-3 text-sm font-medium border-b-2 transition-colors',
            activeTab === 'orders'
              ? 'border-blue-500 text-blue-600 dark:text-blue-400'
              : 'border-transparent text-gray-500 hover:text-gray-700 dark:text-dark-400 dark:hover:text-dark-200'
          ]"
        >充值记录</button>
      </div>

      <!-- 充值 Tab -->
      <template v-if="activeTab === 'recharge'">
        <!-- 余额卡片 -->
        <div class="card overflow-hidden">
          <div class="bg-gradient-to-br from-primary-500 to-primary-600 px-6 py-8 text-center">
            <div class="mb-4 inline-flex h-16 w-16 items-center justify-center rounded-2xl bg-white/20 backdrop-blur-sm">
              <Icon name="creditCard" size="xl" class="text-white" />
            </div>
            <p class="text-sm font-medium text-primary-100">当前余额</p>
            <p class="mt-2 text-4xl font-bold text-white">
              {{ formatBalanceU(user?.balance) }} U
            </p>
          </div>
        </div>

        <!-- 套餐选择 -->
        <div class="card">
          <!-- 支付宝标题 -->
          <div class="flex items-center gap-2 border-b border-gray-100 px-6 py-4 dark:border-dark-700">
            <svg class="h-5 w-5 text-blue-500" viewBox="0 0 24 24" fill="currentColor">
              <path d="M21.4 15.6c-.3-.1-2-.5-3.7-1 1.3-1.6 2.1-3.6 2.1-5.8C19.8 4.3 15.5 1 12 1S4.2 4.3 4.2 8.8c0 2.8 1.5 5.3 3.9 6.8-1.2.5-2.1.9-2.5 1.1-1.3.6-1.8 1.9-1.1 3 .5.8 1.5 1.3 2.7 1.3.7 0 1.4-.2 2.1-.5 1.1-.5 2.9-1.5 4.7-2.7 1.9.5 3.8.8 5 .8 2.1 0 3-.8 3-2 0-.5-.2-1-.6-1zm-9.4-.1c-1.3 0-2.5-.3-3.5-.7 1.3-1.2 2.7-2.6 3.9-4.1.5.6 1 1.3 1.4 2 .6 1.1 1 2.3 1.1 3.4-1 .3-2 .4-2.9.4z"/>
            </svg>
            <span class="text-sm font-medium text-blue-600 dark:text-blue-400">支付宝充值</span>
          </div>

          <div class="p-6">
            <!-- 套餐列表 -->
            <div v-if="loadingPackages" class="flex justify-center py-8">
              <svg class="h-6 w-6 animate-spin text-blue-500" fill="none" viewBox="0 0 24 24">
                <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4" />
                <path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4z" />
              </svg>
            </div>
            <div v-else-if="packages.length === 0 && !loadingPackages" class="py-8 text-center text-gray-500">
              暂无可用套餐，请联系管理员
            </div>
            <div v-else class="grid grid-cols-2 gap-4 sm:grid-cols-3">
              <!-- 固定套餐 -->
              <button
                v-for="pkg in packages"
                :key="pkg.id"
                @click="selectPackage(pkg)"
                :class="[
                  'relative rounded-xl border-2 p-4 text-center transition-all',
                  selectedPackage?.id === pkg.id && !customMode
                    ? 'border-blue-500 bg-blue-50 dark:bg-blue-900/20'
                    : 'border-gray-200 hover:border-blue-300 dark:border-dark-600'
                ]"
              >
                <div v-if="selectedPackage?.id === pkg.id && !customMode" class="absolute right-2 top-2">
                  <div class="flex h-5 w-5 items-center justify-center rounded-full bg-blue-500">
                    <svg class="h-3 w-3 text-white" fill="currentColor" viewBox="0 0 20 20">
                      <path fill-rule="evenodd" d="M16.707 5.293a1 1 0 010 1.414l-8 8a1 1 0 01-1.414 0l-4-4a1 1 0 011.414-1.414L8 12.586l7.293-7.293a1 1 0 011.414 0z" clip-rule="evenodd" />
                    </svg>
                  </div>
                </div>
                <p class="text-xl font-bold text-gray-900 dark:text-white">¥{{ pkg.cny_amount }}</p>
                <p class="mt-1 text-sm text-blue-600 dark:text-blue-400">到账 {{ formatBalanceU(pkg.usd_amount) }} U</p>
              </button>

              <!-- 自定义金额卡片 -->
              <button
                @click="selectCustom"
                :class="[
                  'relative rounded-xl border-2 p-4 text-center transition-all',
                  customMode
                    ? 'border-blue-500 bg-blue-50 dark:bg-blue-900/20'
                    : 'border-gray-200 hover:border-blue-300 dark:border-dark-600'
                ]"
              >
                <div v-if="customMode" class="absolute right-2 top-2">
                  <div class="flex h-5 w-5 items-center justify-center rounded-full bg-blue-500">
                    <svg class="h-3 w-3 text-white" fill="currentColor" viewBox="0 0 20 20">
                      <path fill-rule="evenodd" d="M16.707 5.293a1 1 0 010 1.414l-8 8a1 1 0 01-1.414 0l-4-4a1 1 0 011.414-1.414L8 12.586l7.293-7.293a1 1 0 011.414 0z" clip-rule="evenodd" />
                    </svg>
                  </div>
                </div>
                <p class="text-xl font-bold text-gray-900 dark:text-white">自定义</p>
                <p class="mt-1 text-sm text-blue-600 dark:text-blue-400">任意金额</p>
              </button>
            </div>

            <!-- 自定义金额输入框 -->
            <div v-if="customMode" class="mt-4">
              <div class="flex items-center gap-2 rounded-xl border-2 border-blue-500 bg-blue-50 px-4 py-3 dark:bg-blue-900/20">
                <span class="text-lg font-medium text-gray-600 dark:text-gray-300">¥</span>
                <input
                  ref="customInput"
                  v-model="customAmountStr"
                  type="number"
                  min="1"
                  max="50000"
                  step="0.01"
                  placeholder="输入充值金额（¥1 ~ ¥50000）"
                  class="flex-1 bg-transparent text-lg font-bold text-gray-900 outline-none placeholder:text-gray-400 dark:text-white"
                />
              </div>
              <p v-if="customAmountStr && customAmount > 0 && customAmount <= 50000" class="mt-1 text-sm text-blue-600 dark:text-blue-400">
                到账 {{ (customAmount * RMB_TO_U).toFixed(2) }} U
              </p>
              <p v-else-if="customAmountStr && customAmount > 50000" class="mt-1 text-sm text-red-500">
                单次充值上限 ¥50000
              </p>
            </div>

            <button
              v-if="canSubmit"
              @click="createOrder"
              :disabled="creatingOrder"
              class="mt-6 w-full rounded-lg bg-blue-500 py-3 font-medium text-white transition-colors hover:bg-blue-600 disabled:opacity-50"
            >
              <svg v-if="creatingOrder" class="-ml-1 mr-2 inline-block h-5 w-5 animate-spin" fill="none" viewBox="0 0 24 24">
                <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4" />
                <path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4z" />
              </svg>
              {{ creatingOrder ? '生成中...' : submitLabel }}
            </button>
          </div>
        </div>

        <!-- 二维码弹窗 -->
        <div v-if="showQRCode" class="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4">
          <div class="w-full max-w-sm rounded-2xl bg-white p-6 shadow-2xl dark:bg-dark-800">
            <div class="flex items-center justify-between mb-4">
              <h3 class="text-lg font-semibold text-gray-900 dark:text-white">支付宝扫码支付</h3>
              <button @click="closeQRCode" class="text-gray-400 hover:text-gray-600">
                <svg class="h-5 w-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 18L18 6M6 6l12 12" />
                </svg>
              </button>
            </div>

            <div v-if="orderStatus === 'paid'" class="text-center py-4">
              <div class="mx-auto mb-3 flex h-16 w-16 items-center justify-center rounded-full bg-green-100">
                <svg class="h-8 w-8 text-green-500" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M5 13l4 4L19 7" />
                </svg>
              </div>
              <p class="text-lg font-semibold text-gray-900 dark:text-white">支付成功！</p>
              <p class="mt-1 text-sm text-gray-500">余额已到账，页面将自动刷新</p>
            </div>

            <div v-else-if="orderStatus === 'expired'" class="text-center py-4">
              <p class="text-gray-500">订单已过期，请重新下单</p>
              <button @click="closeQRCode" class="btn btn-primary mt-4 w-full">关闭</button>
            </div>

            <div v-else class="text-center">
              <div class="mb-3 flex justify-center">
                <img v-if="qrCodeDataUrl" :src="qrCodeDataUrl" alt="支付宝二维码" class="h-48 w-48 rounded-lg" />
                <div v-else class="flex h-48 w-48 items-center justify-center rounded-lg bg-gray-100">
                  <svg class="h-8 w-8 animate-spin text-gray-400" fill="none" viewBox="0 0 24 24">
                    <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4" />
                    <path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4z" />
                  </svg>
                </div>
              </div>
              <p class="text-sm text-gray-500">请使用支付宝扫描上方二维码完成支付</p>
              <p class="mt-1 text-lg font-bold text-gray-900 dark:text-white">¥{{ pendingCnyAmount }}</p>
              <p class="text-sm text-blue-600">到账 {{ (pendingCnyAmount * RMB_TO_U).toFixed(2) }} U</p>
              <div class="mt-3 flex items-center justify-center gap-1 text-xs text-gray-400">
                <svg class="h-3 w-3 animate-pulse text-green-500" fill="currentColor" viewBox="0 0 20 20">
                  <circle cx="10" cy="10" r="10" />
                </svg>
                等待支付中...（{{ remainingSeconds }}s）
              </div>
            </div>
          </div>
        </div>
      </template>

      <!-- 充值记录 Tab -->
      <template v-if="activeTab === 'orders'">
        <div class="card">
          <div class="overflow-x-auto">
            <table class="w-full table-auto text-sm">
              <thead>
                <tr class="border-b border-gray-100 dark:border-dark-700">
                  <th class="whitespace-nowrap px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">订单号</th>
                  <th class="whitespace-nowrap px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">金额</th>
                  <th class="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">状态</th>
                  <th class="whitespace-nowrap px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">支付时间</th>
                  <th class="whitespace-nowrap px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">创建时间</th>
                </tr>
              </thead>
              <tbody class="divide-y divide-gray-50 dark:divide-dark-700">
                <tr v-if="ordersLoading">
                  <td colspan="5" class="px-4 py-8 text-center text-gray-400">加载中...</td>
                </tr>
                <tr v-else-if="ordersError">
                  <td colspan="5" class="px-4 py-8 text-center text-red-400">{{ ordersError }}</td>
                </tr>
                <tr v-else-if="orders.length === 0">
                  <td colspan="5" class="px-4 py-8 text-center text-gray-400">暂无充值记录</td>
                </tr>
                <tr v-else v-for="order in orders" :key="order.order_no" class="hover:bg-gray-50 dark:hover:bg-dark-750">
                  <td class="whitespace-nowrap px-4 py-3 font-mono text-xs text-gray-600 dark:text-dark-300">{{ order.order_no }}</td>
                  <td class="whitespace-nowrap px-4 py-3 text-gray-900 dark:text-white">
                    ¥{{ (order.cny_fee / 100).toFixed(2) }} → {{ Number(order.usd_amount).toFixed(2) }} U
                  </td>
                  <td class="whitespace-nowrap px-4 py-3">
                    <span :class="orderStatusClass(order.status)" class="inline-flex rounded-full px-2 py-0.5 text-xs font-medium">
                      {{ orderStatusLabel(order.status) }}
                    </span>
                  </td>
                  <td class="whitespace-nowrap px-4 py-3 text-xs text-gray-400">{{ order.paid_at ? formatOrderDate(order.paid_at) : '—' }}</td>
                  <td class="whitespace-nowrap px-4 py-3 text-xs text-gray-400">{{ formatOrderDate(order.created_at) }}</td>
                </tr>
              </tbody>
            </table>
          </div>
          <!-- 分页 -->
          <div v-if="ordersTotal > ordersPageSize" class="flex items-center justify-between border-t border-gray-100 px-4 py-3 dark:border-dark-700">
            <span class="text-sm text-gray-500">共 {{ ordersTotal }} 条</span>
            <div class="flex items-center gap-2">
              <button @click="ordersPrevPage" :disabled="ordersPage <= 1" class="btn btn-secondary py-1 text-sm disabled:opacity-40">上一页</button>
              <span class="text-sm text-gray-600 dark:text-dark-300">第 {{ ordersPage }} 页</span>
              <button @click="ordersNextPage" :disabled="ordersPage * ordersPageSize >= ordersTotal" class="btn btn-secondary py-1 text-sm disabled:opacity-40">下一页</button>
            </div>
          </div>
        </div>
      </template>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted, nextTick } from 'vue'
import { useAuthStore } from '@/stores/auth'
import { alipayAPI, type PaymentPackage, type AlipayOrder } from '@/api/payment'
import AppLayout from '@/components/layout/AppLayout.vue'
import { RMB_TO_U } from '@/utils/format'
import Icon from '@/components/icons/Icon.vue'
import QRCode from 'qrcode'
import { formatBalanceU } from '@/utils/format'

const authStore = useAuthStore()
const user = computed(() => authStore.user)

// Tab 状态
const activeTab = ref<'recharge' | 'orders'>('recharge')

// ---- 充值 ----
const packages = ref<PaymentPackage[]>([])
const selectedPackage = ref<PaymentPackage | null>(null)
const loadingPackages = ref(false)
const creatingOrder = ref(false)

// 自定义金额
const customMode = ref(false)
const customAmountStr = ref('')
const customInput = ref<HTMLInputElement | null>(null)
const customAmount = computed(() => {
  const v = parseFloat(customAmountStr.value)
  return isNaN(v) || v <= 0 ? 0 : v
})

const canSubmit = computed(() => {
  if (customMode.value) return customAmount.value >= 1 && customAmount.value <= 50000
  return selectedPackage.value !== null
})

const submitLabel = computed(() => {
  if (customMode.value) return `立即充值 ¥${customAmount.value}`
  return `立即充值 ¥${selectedPackage.value?.cny_amount}`
})

// 弹窗状态
const showQRCode = ref(false)
const qrCodeDataUrl = ref('')
const currentOrderNo = ref('')
const orderStatus = ref<'pending' | 'paid' | 'expired' | 'refunded'>('pending')
const remainingSeconds = ref(0)
const pendingCnyAmount = ref(0)

let pollTimer: ReturnType<typeof setInterval> | null = null
let countdownTimer: ReturnType<typeof setInterval> | null = null
let pollCount = 0
const MAX_POLL_COUNT = 200

// ---- 充值记录 ----
const ordersLoading = ref(false)
const ordersError = ref('')
const orders = ref<AlipayOrder[]>([])
const ordersTotal = ref(0)
const ordersPage = ref(1)
const ordersPageSize = 20

onMounted(async () => {
  loadingPackages.value = true
  try {
    packages.value = await alipayAPI.getPackages()
  } catch {
    // 未启用时静默处理
  } finally {
    loadingPackages.value = false
  }
})

onUnmounted(() => {
  stopPolling()
})

function selectPackage(pkg: PaymentPackage) {
  selectedPackage.value = pkg
  customMode.value = false
  customAmountStr.value = ''
}

async function selectCustom() {
  customMode.value = true
  selectedPackage.value = null
  await nextTick()
  customInput.value?.focus()
}

async function createOrder() {
  if (!canSubmit.value) return
  creatingOrder.value = true
  try {
    const resp = customMode.value
      ? await alipayAPI.createOrder(0, customAmount.value)
      : await alipayAPI.createOrder(selectedPackage.value!.id)

    pendingCnyAmount.value = customMode.value
      ? customAmount.value
      : selectedPackage.value!.cny_amount

    currentOrderNo.value = resp.order_no
    orderStatus.value = 'pending'

    const expiresAtMs = new Date(resp.expires_at).getTime()
    remainingSeconds.value = Math.max(0, Math.floor((expiresAtMs - Date.now()) / 1000))

    qrCodeDataUrl.value = await QRCode.toDataURL(resp.qr_code, { width: 192, margin: 1 })
    showQRCode.value = true
    startPolling()
    startCountdown(expiresAtMs)
  } catch (e: any) {
    alert(e?.response?.data?.message || '创建订单失败，请稍后重试')
  } finally {
    creatingOrder.value = false
  }
}

function startPolling() {
  pollCount = 0
  pollTimer = setInterval(async () => {
    pollCount++
    if (pollCount >= MAX_POLL_COUNT) {
      stopPolling()
      if (orderStatus.value === 'pending') orderStatus.value = 'expired'
      return
    }
    try {
      const order = await alipayAPI.getOrderStatus(currentOrderNo.value)
      orderStatus.value = order.status as any
      if (order.status === 'paid') {
        stopPolling()
        setTimeout(async () => {
          await authStore.refreshUser()
          closeQRCode()
        }, 2000)
      } else if (order.status === 'expired') {
        stopPolling()
      }
    } catch {
      // 忽略轮询错误
    }
  }, 3000)
}

function startCountdown(expiresAt: number) {
  countdownTimer = setInterval(() => {
    const remaining = Math.floor((expiresAt - Date.now()) / 1000)
    remainingSeconds.value = Math.max(0, remaining)
    if (remaining <= 0) {
      clearInterval(countdownTimer!)
      countdownTimer = null
      if (orderStatus.value === 'pending') {
        orderStatus.value = 'expired'
        stopPolling()
      }
    }
  }, 1000)
}

function stopPolling() {
  if (pollTimer) { clearInterval(pollTimer); pollTimer = null }
  if (countdownTimer) { clearInterval(countdownTimer); countdownTimer = null }
}

function closeQRCode() {
  stopPolling()
  showQRCode.value = false
  qrCodeDataUrl.value = ''
  currentOrderNo.value = ''
  orderStatus.value = 'pending'
}

// ---- 充值记录方法 ----
async function loadOrders() {
  ordersLoading.value = true
  ordersError.value = ''
  try {
    const result = await alipayAPI.listOrders(ordersPage.value, ordersPageSize)
    orders.value = result.items
    ordersTotal.value = result.total
  } catch (e: any) {
    ordersError.value = e?.response?.data?.message || '加载失败，请刷新重试'
  } finally {
    ordersLoading.value = false
  }
}

async function ordersPrevPage() {
  if (ordersPage.value > 1) {
    ordersPage.value--
    await loadOrders()
  }
}

async function ordersNextPage() {
  if (ordersPage.value * ordersPageSize < ordersTotal.value) {
    ordersPage.value++
    await loadOrders()
  }
}

function orderStatusLabel(status: string) {
  const map: Record<string, string> = {
    pending: '待支付', paid: '已支付', expired: '已过期', refunded: '已退款'
  }
  return map[status] ?? status
}

function orderStatusClass(status: string) {
  const map: Record<string, string> = {
    pending: 'bg-yellow-100 text-yellow-800 dark:bg-yellow-900/30 dark:text-yellow-400',
    paid: 'bg-green-100 text-green-800 dark:bg-green-900/30 dark:text-green-400',
    expired: 'bg-gray-100 text-gray-600 dark:bg-dark-700 dark:text-dark-400',
    refunded: 'bg-blue-100 text-blue-800 dark:bg-blue-900/30 dark:text-blue-400'
  }
  return map[status] ?? ''
}

function formatOrderDate(iso: string) {
  return new Date(iso).toLocaleString('zh-CN')
}

// 切换 Tab 时加载充值记录
import { watch } from 'vue'
watch(activeTab, (tab) => {
  if (tab === 'orders' && orders.value.length === 0) {
    loadOrders()
  }
})
</script>
