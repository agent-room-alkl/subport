<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { asList, consoleApi } from '../../api/client'
import PageHeader from '../../components/PageHeader.vue'
import StatusPill from '../../components/StatusPill.vue'
import EmptyState from '../../components/EmptyState.vue'

const packages = ref<any[]>([])
const mockAvailable = ref(false)
const topups = ref<any[]>([])
const lastOrder = ref<any>(null)
const err = ref('')
const ok = ref('')
const loading = ref(false)
const paying = ref(false)

function formatTs(v?: string) {
  if (!v) return '—'
  try {
    const d = new Date(v)
    return Number.isNaN(d.getTime()) ? v : d.toLocaleString()
  } catch {
    return v
  }
}

function yuan(cents?: number) {
  if (cents == null) return '—'
  return (Number(cents) / 100).toFixed(2)
}

async function load() {
  loading.value = true
  err.value = ''
  try {
    const pkg = await consoleApi.packages()
    packages.value = asList(pkg?.packages ?? pkg, ['packages', 'items', 'rows', 'data'])
    mockAvailable.value = !!(pkg?.mock_pay_available || pkg?.alipay_mock)
    topups.value = asList(await consoleApi.topups(), ['items', 'rows', 'data', 'topups'])
  } catch (e: any) {
    err.value = e?.message || '加载失败'
  } finally {
    loading.value = false
  }
}

async function recharge(packageId: string) {
  ok.value = ''
  err.value = ''
  paying.value = true
  try {
    const out = await consoleApi.recharge(packageId)
    lastOrder.value = out.order
    ok.value = '订单已创建'
    if (out.pay_url && !(out.mock_pay_available || mockAvailable.value)) {
      window.open(out.pay_url, '_blank')
    }
    await load()
  } catch (e: any) {
    err.value = e?.message || '创建订单失败'
  } finally {
    paying.value = false
  }
}

async function mockPay() {
  if (!lastOrder.value?.id) return
  err.value = ''
  ok.value = ''
  paying.value = true
  try {
    const out = await consoleApi.mockPay(lastOrder.value.id)
    ok.value = out.credited ? '已到账' : '已支付（幂等，未重复加额）'
    lastOrder.value = out.order
    await load()
  } catch (e: any) {
    err.value = e?.message || 'Mock 支付失败'
  } finally {
    paying.value = false
  }
}

function openPayUrl() {
  if (lastOrder.value?.pay_url) window.open(lastOrder.value.pay_url, '_blank')
}

onMounted(load)
</script>

<template>
  <div>
    <PageHeader title="充值" subtitle="选套餐 → 支付 → 额度到账">
      <template #actions>
        <button class="btn-ghost" type="button" :disabled="loading" @click="load">刷新</button>
      </template>
    </PageHeader>

    <div class="mb-4 rounded-lg border border-line bg-white px-4 py-3 text-sm text-slatex shadow-card">
      <ol class="flex flex-wrap gap-x-4 gap-y-1">
        <li><span class="font-semibold text-ink">1.</span> 选套餐</li>
        <li><span class="font-semibold text-ink">2.</span> 支付</li>
        <li><span class="font-semibold text-ink">3.</span> 额度到账</li>
      </ol>
    </div>

    <p v-if="err" class="mb-3 text-sm text-red-600">{{ err }}</p>
    <p v-if="ok" class="mb-3 text-sm text-teal">{{ ok }}</p>

    <div class="card space-y-3">
      <div class="font-semibold">充值套餐</div>
      <div v-if="packages.length" class="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
        <button
          v-for="p in packages"
          :key="p.id"
          class="rounded-xl border border-line bg-white p-4 text-left transition hover:border-teal hover:shadow-glow-sm disabled:opacity-50"
          type="button"
          :disabled="paying"
          @click="recharge(p.id)"
        >
          <div class="font-semibold text-ink">{{ p.label || p.id }}</div>
          <div class="mt-2 text-2xl font-semibold tabular-nums text-teal">
            ¥{{ yuan(p.amount_fiat_cents ?? p.price_cents) }}
          </div>
          <div class="mt-1 text-xs text-slatex">额度 +{{ p.quota_credit }}</div>
        </button>
      </div>
      <p v-else class="text-sm text-slatex">{{ loading ? '加载中…' : '暂无套餐' }}</p>

      <div v-if="lastOrder" class="rounded-lg bg-canvas px-3 py-3 text-sm">
        <div class="flex flex-wrap items-center gap-2">
          <span>最近订单</span>
          <code class="font-mono text-xs">{{ lastOrder.id }}</code>
          <StatusPill :status="lastOrder.status" :label="lastOrder.status" />
          <span class="text-xs text-slatex">credit {{ lastOrder.quota_credit }}</span>
        </div>
        <div class="mt-2 flex flex-wrap gap-2">
          <button
            v-if="mockAvailable && lastOrder.status === 'pending'"
            class="btn-primary"
            type="button"
            :disabled="paying"
            @click="mockPay"
          >
            Mock 支付
          </button>
          <button
            v-if="lastOrder.pay_url && lastOrder.status === 'pending'"
            class="btn-ghost"
            type="button"
            @click="openPayUrl"
          >
            打开支付页
          </button>
        </div>
      </div>
    </div>

    <div class="mt-4">
      <div class="mb-2 font-semibold">充值 / 加额历史</div>
      <EmptyState
        v-if="!loading && topups.length === 0"
        title="暂无记录"
        description="完成支付后，加额记录会出现在这里。"
      />
      <div v-else-if="topups.length" class="table-wrap">
        <table class="data-table">
          <thead>
            <tr>
              <th>时间</th>
              <th>额度</th>
              <th>来源</th>
              <th>备注</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="t in topups" :key="t.id">
              <td class="whitespace-nowrap text-xs text-slatex">{{ formatTs(t.created_at) }}</td>
              <td class="tabular-nums font-semibold text-teal">+{{ t.credit }}</td>
              <td>{{ t.source || '—' }}</td>
              <td class="text-xs text-slatex">{{ t.note || '—' }}</td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>
  </div>
</template>
