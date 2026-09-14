<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { adminApi } from '../api/client'
import PageHeader from '../components/PageHeader.vue'
import DonutChart from '../components/charts/DonutChart.vue'
import BarTrendChart from '../components/charts/BarTrendChart.vue'

const data = ref<Record<string, any> | null>(null)
const refresh = ref<Record<string, any> | null>(null)
const err = ref('')
const refreshErr = ref('')
const loading = ref(true)

const accountsHealthy = computed(() => data.value?.accounts_healthy ?? data.value?.healthy_accounts ?? '—')
const accountsTotal = computed(() => data.value?.accounts_total ?? data.value?.total_accounts ?? '—')
const channelsTotal = computed(() => data.value?.channels_total ?? data.value?.channels ?? '—')
const keysTotal = computed(() => data.value?.keys_total ?? '—')
const requests24h = computed(() => data.value?.requests_24h ?? data.value?.request_count_24h ?? '—')
const usersTotal = computed(() => data.value?.users_total ?? '—')
const adminsTotal = computed(() => data.value?.admins_total ?? '—')

const MODEL_COLORS = ['#0F9F73', '#2563EB', '#F59E0B', '#EC4899', '#8B5CF6', '#14B8A6', '#EF4444', '#64748B']

const dayPoints = computed(() => {
  const rows = Array.isArray(data.value?.usage_by_day) ? data.value!.usage_by_day : []
  return rows.map((r: any) => {
    const day = String(r.day || '')
    const label = day.length >= 10 ? day.slice(5) : day
    return { label, value: Number(r.requests ?? r.tokens ?? 0) || 0 }
  })
})

const modelSegments = computed(() => {
  const rows = Array.isArray(data.value?.usage_by_model) ? data.value!.usage_by_model : []
  return rows.map((r: any, i: number) => ({
    label: String(r.model || 'unknown'),
    value: Number(r.tokens ?? r.requests ?? 0) || 0,
    color: MODEL_COLORS[i % MODEL_COLORS.length],
  }))
})

const modelCenter = computed(() => {
  const total = modelSegments.value.reduce((s, x) => s + x.value, 0)
  return total > 0 ? String(total) : '0'
})

async function load() {
  loading.value = true
  err.value = ''
  refreshErr.value = ''
  try {
    data.value = (await adminApi.overview()) || {}
  } catch (e: any) {
    err.value = e.message
    data.value = null
  }
  try {
    refresh.value = await adminApi.tokenRefreshStatus()
  } catch (e: any) {
    refreshErr.value = e.message
    refresh.value = null
  } finally {
    loading.value = false
  }
}

onMounted(load)
</script>

<template>
  <div>
    <PageHeader
      title="总览"
      subtitle="订阅转 API · Claude / Codex OAuth → OpenAI 兼容网关"
    >
      <template #actions>
        <button class="btn-ghost" type="button" :disabled="loading" @click="load">刷新</button>
      </template>
    </PageHeader>

    <div class="grid gap-4 md:grid-cols-3">
      <div class="card-glow md:col-span-3 bg-gradient-to-r from-white to-teal-bright/10">
        <div class="text-xs font-medium uppercase tracking-wide text-teal">产品主线</div>
        <div class="mt-1 text-lg font-semibold">订阅转 API · 上游 OAuth 汇入统一网关</div>
        <p class="mt-2 text-sm text-slatex">
          上游凭证在「账号池」维护；下游调用方使用「下游密钥」(sk-sp-…)。
        </p>
      </div>

      <div v-if="err" class="card md:col-span-3 text-red-600">{{ err }}</div>

      <template v-else-if="data">
        <div class="card">
          <div class="label">账号</div>
          <div class="mt-1 text-2xl font-semibold tabular-nums">
            {{ accountsHealthy }}
            <span class="text-sm font-normal text-slatex">/ {{ accountsTotal }}</span>
          </div>
          <p class="mt-1 text-xs text-slatex">健康 / 总数</p>
        </div>
        <div class="card">
          <div class="label">渠道</div>
          <div class="mt-1 text-2xl font-semibold tabular-nums">{{ channelsTotal }}</div>
          <p class="mt-1 text-xs text-slatex">渠道总数</p>
        </div>
        <div class="card">
          <div class="label">24h 请求</div>
          <div class="mt-1 text-2xl font-semibold tabular-nums">{{ requests24h }}</div>
          <p class="mt-1 text-xs text-slatex">密钥 {{ keysTotal }} · 用户 {{ usersTotal }}（管理员 {{ adminsTotal }}）</p>
        </div>

        <div class="card md:col-span-2">
          <div class="mb-2 font-semibold">近 7 日请求</div>
          <BarTrendChart :points="dayPoints" />
        </div>
        <div class="card">
          <div class="mb-2 font-semibold">按模型用量</div>
          <DonutChart
            :segments="modelSegments"
            :center-text="modelCenter"
            center-sub="tokens"
          />
        </div>
      </template>

      <div v-else class="card md:col-span-3 text-slatex">加载中…</div>

      <div class="card md:col-span-3">
        <div class="flex flex-wrap items-center gap-2">
          <div class="font-semibold">Token 自动刷新</div>
          <span class="text-xs text-slatex">后台约每 3 分钟巡检一次</span>
        </div>
        <p v-if="refreshErr" class="mt-2 text-sm text-amber-700">暂无状态接口：{{ refreshErr }}</p>
        <div v-else-if="refresh" class="mt-3 grid gap-3 sm:grid-cols-3 text-sm">
          <div>
            <div class="label">上次运行</div>
            <div class="font-mono text-slatex">{{ refresh.last_run_at || refresh.updated_at || '—' }}</div>
          </div>
          <div>
            <div class="label">刷新成功</div>
            <div class="tabular-nums text-teal">{{ refresh.refreshed ?? refresh.ok ?? '—' }}</div>
          </div>
          <div>
            <div class="label">跳过 / 失败</div>
            <div class="tabular-nums text-slatex">{{ refresh.skipped ?? 0 }} / {{ refresh.errors ?? refresh.failed ?? 0 }}</div>
          </div>
        </div>
        <p v-else class="mt-2 text-sm text-slatex">尚未采集到刷新快照。</p>
      </div>
    </div>
  </div>
</template>
