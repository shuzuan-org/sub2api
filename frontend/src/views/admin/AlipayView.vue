<template>
  <AppLayout>
    <div class="space-y-6">
      <div>
        <h1 class="text-2xl font-bold text-gray-900 dark:text-white">支付宝充值配置</h1>
        <p class="mt-1 text-sm text-gray-500 dark:text-dark-400">支付宝凭证由服务器证书目录提供；此处配置启用状态、充值套餐和订单记录</p>
      </div>

      <div class="card">
        <div class="flex items-center justify-between p-6">
          <div>
            <h2 class="text-base font-semibold text-gray-900 dark:text-white">启用支付宝支付</h2>
            <p class="mt-0.5 text-sm text-gray-500 dark:text-dark-400">开启后用户可在充值页面创建支付宝订单</p>
          </div>
          <button
            @click="toggleEnabled"
            :class="[
              'relative inline-flex h-6 w-11 items-center rounded-full transition-colors focus:outline-none',
              enabled ? 'bg-primary-600' : 'bg-gray-200 dark:bg-dark-600'
            ]"
          >
            <span
              :class="[
                'inline-block h-4 w-4 transform rounded-full bg-white transition-transform',
                enabled ? 'translate-x-6' : 'translate-x-1'
              ]"
            />
          </button>
        </div>
      </div>

      <div class="card">
        <div class="flex items-center justify-between border-b border-gray-100 px-6 py-4 dark:border-dark-700">
          <h2 class="text-base font-semibold text-gray-900 dark:text-white">充值套餐</h2>
          <button @click="addPackage" class="btn btn-secondary btn-sm">+ 添加套餐</button>
        </div>
        <div class="p-6">
          <div v-if="packages.length === 0" class="py-8 text-center text-gray-400">
            暂无套餐，点击右上角添加
          </div>
          <div v-else class="space-y-3">
            <div
              v-for="(pkg, idx) in packages"
              :key="pkg.id"
              class="flex items-center gap-3 rounded-lg border border-gray-200 p-3 dark:border-dark-600"
            >
              <div class="grid flex-1 grid-cols-3 gap-3">
                <div>
                  <label class="text-xs text-gray-500">套餐名称</label>
                  <input v-model="pkg.name" type="text" class="input mt-0.5 py-1.5 text-sm" placeholder="套餐名称" />
                </div>
                <div>
                  <label class="text-xs text-gray-500">支付金额（CNY 元）</label>
                  <input v-model.number="pkg.cny_amount" type="number" step="0.01" min="0.01" class="input mt-0.5 py-1.5 text-sm" />
                </div>
                <div>
                  <label class="text-xs text-gray-500">到账余额（U）</label>
                  <input v-model.number="pkg.usd_amount" type="number" step="0.01" min="0.01" class="input mt-0.5 py-1.5 text-sm" />
                </div>
              </div>
              <button @click="removePackage(idx)" class="text-gray-400 hover:text-red-500">
                <svg class="h-4 w-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 18L18 6M6 6l12 12" />
                </svg>
              </button>
            </div>
          </div>
          <div class="mt-4 flex justify-end">
            <button @click="savePackages" :disabled="savingPackages" class="btn btn-primary">
              {{ savingPackages ? '保存中...' : '保存套餐' }}
            </button>
          </div>
        </div>
      </div>

      <!-- 订单记录 -->
      <div class="card">
        <div class="flex items-center justify-between border-b border-gray-100 px-6 py-4 dark:border-dark-700">
          <h2 class="text-base font-semibold text-gray-900 dark:text-white">支付宝充值订单</h2>
          <div class="flex items-center gap-3">
            <select v-model="statusFilter" @change="onFilterChange" class="input py-1 text-sm">
              <option value="">全部状态</option>
              <option value="pending">待支付</option>
              <option value="paid">已支付</option>
              <option value="expired">已过期</option>
              <option value="refunded">已退款</option>
            </select>
          </div>
        </div>
        <div class="overflow-x-auto">
          <table class="w-full text-sm">
            <thead>
              <tr class="border-b border-gray-100 dark:border-dark-700">
                <th class="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase">订单号</th>
                <th class="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase">用户ID</th>
                <th class="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase">金额</th>
                <th class="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase">状态</th>
                <th class="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase">支付时间</th>
                <th class="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase">创建时间</th>
              </tr>
            </thead>
            <tbody class="divide-y divide-gray-50 dark:divide-dark-700">
              <tr v-if="loadingOrders">
                <td colspan="6" class="px-6 py-8 text-center text-gray-400">加载中...</td>
              </tr>
              <tr v-else-if="loadError">
                <td colspan="6" class="px-6 py-8 text-center text-red-400">{{ loadError }}</td>
              </tr>
              <tr v-else-if="orders.length === 0">
                <td colspan="6" class="px-6 py-8 text-center text-gray-400">暂无订单</td>
              </tr>
              <tr v-else v-for="order in orders" :key="order.order_no" class="hover:bg-gray-50 dark:hover:bg-dark-750">
                <td class="px-6 py-3 font-mono text-xs text-gray-600 dark:text-dark-300">{{ order.order_no }}</td>
                <td class="px-6 py-3 text-gray-600 dark:text-dark-300">{{ order.user_id }}</td>
                <td class="px-6 py-3 text-gray-900 dark:text-white">
                  ¥{{ (order.cny_fee / 100).toFixed(2) }} → {{ Number(order.usd_amount).toFixed(2) }} U
                </td>
                <td class="px-6 py-3">
                  <span :class="statusClass(order.status)" class="inline-flex rounded-full px-2 py-0.5 text-xs font-medium">
                    {{ statusLabel(order.status) }}
                  </span>
                </td>
                <td class="px-6 py-3 text-xs text-gray-400">{{ order.paid_at ? formatDate(order.paid_at) : '—' }}</td>
                <td class="px-6 py-3 text-xs text-gray-400">{{ formatDate(order.created_at) }}</td>
              </tr>
            </tbody>
          </table>
        </div>
        <!-- 分页 -->
        <div v-if="total > pageSize" class="flex items-center justify-between border-t border-gray-100 px-6 py-3 dark:border-dark-700">
          <span class="text-sm text-gray-500">共 {{ total }} 条</span>
          <div class="flex items-center gap-2">
            <button @click="prevPage" :disabled="page <= 1" class="btn btn-secondary py-1 text-sm disabled:opacity-40">上一页</button>
            <span class="text-sm text-gray-600 dark:text-dark-300">第 {{ page }} 页</span>
            <button @click="nextPage" :disabled="page * pageSize >= total" class="btn btn-secondary py-1 text-sm disabled:opacity-40">下一页</button>
          </div>
        </div>
      </div>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import AppLayout from '@/components/layout/AppLayout.vue'
import { adminAlipayAPI, type AlipayOrderRecord, type AlipayPackage } from '@/api/admin/alipay'

const loadingOrders = ref(false)
const loadError = ref('')
const orders = ref<AlipayOrderRecord[]>([])
const total = ref(0)
const page = ref(1)
const pageSize = 20
const statusFilter = ref('')
const enabled = ref(false)
const savingPackages = ref(false)
const packages = ref<AlipayPackage[]>([])

let nextPackageId = 100

onMounted(async () => {
  await Promise.allSettled([loadEnabled(), loadPackages(), loadOrders()])
})

async function loadEnabled() {
  try {
    const cfg = await adminAlipayAPI.getConfig()
    enabled.value = cfg.enabled ?? false
  } catch {}
}

async function toggleEnabled() {
  enabled.value = !enabled.value
  try {
    await adminAlipayAPI.setEnabled(enabled.value)
  } catch {
    enabled.value = !enabled.value
  }
}

async function loadPackages() {
  try {
    const pkgs = await adminAlipayAPI.getPackages()
    packages.value = pkgs
    nextPackageId = Math.max(nextPackageId, ...pkgs.map((p) => p.id + 1))
  } catch {}
}

async function loadOrders() {
  loadingOrders.value = true
  loadError.value = ''
  try {
    const result = await adminAlipayAPI.listOrders(page.value, pageSize, statusFilter.value)
    orders.value = result.items
    total.value = result.total
  } catch (e: any) {
    loadError.value = e?.response?.data?.message || '加载失败，请刷新重试'
  } finally {
    loadingOrders.value = false
  }
}

function onFilterChange() {
  page.value = 1
  loadOrders()
}

async function prevPage() {
  if (page.value > 1) {
    page.value--
    await loadOrders()
  }
}

async function nextPage() {
  if (page.value * pageSize < total.value) {
    page.value++
    await loadOrders()
  }
}

function addPackage() {
  packages.value.push({ id: nextPackageId++, name: '', cny_amount: 10, usd_amount: 1 })
}

function removePackage(idx: number) {
  packages.value.splice(idx, 1)
}

async function savePackages() {
  savingPackages.value = true
  try {
    await adminAlipayAPI.updatePackages(packages.value)
    alert('套餐已保存')
  } catch (e: any) {
    alert(e?.response?.data?.message || '保存失败')
  } finally {
    savingPackages.value = false
  }
}

function statusLabel(status: string) {
  const map: Record<string, string> = {
    pending: '待支付', paid: '已支付', expired: '已过期', refunded: '已退款'
  }
  return map[status] ?? status
}

function statusClass(status: string) {
  const map: Record<string, string> = {
    pending: 'bg-yellow-100 text-yellow-800 dark:bg-yellow-900/30 dark:text-yellow-400',
    paid: 'bg-green-100 text-green-800 dark:bg-green-900/30 dark:text-green-400',
    expired: 'bg-gray-100 text-gray-600 dark:bg-dark-700 dark:text-dark-400',
    refunded: 'bg-blue-100 text-blue-800 dark:bg-blue-900/30 dark:text-blue-400'
  }
  return map[status] ?? ''
}

function formatDate(iso: string) {
  return new Date(iso).toLocaleString('zh-CN')
}
</script>
