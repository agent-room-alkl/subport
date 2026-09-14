<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { adminApi, asList } from '../api/client'
import PageHeader from '../components/PageHeader.vue'
import StatusPill from '../components/StatusPill.vue'
import EmptyState from '../components/EmptyState.vue'
import DonutChart from '../components/charts/DonutChart.vue'

type UsageRow = {
  id?: string | number
  created_at?: string
  username?: string
  user_id?: string
  model?: string
  tokens?: number
  cost?: number
  status?: string
  key_prefix?: string
  account_id?: string
}

type UserStat = {
  user_id?: string
  username?: string
  calls?: number
  tokens?: number
  cost?: number
  success_calls?: number
  failed_calls?: number
}

type ModelStat = {
  model?: string
  calls?: number
  tokens?: number
  cost?: number
}

type UserModelStat = {
  username?: string
  model?: string
  calls?: number
  tokens?: number
  cost?: number
}

const rows = ref<UsageRow[]>([])
const perUser = ref<UserStat[]>([])
const perModel = ref<ModelStat[]>([])
const perUserModel = ref<UserModelStat[]>([])
const err = ref('')
const loading = ref(false)

const totalCalls = computed(() => perUser.value.reduce((a, u) => a + (Number(u.calls) || 0), 0))
const totalTokens = computed(() => perUser.value.reduce((a, u) => a + (Number(u.tokens) || 0), 0))
const totalCost = computed(() => perUser.value.reduce((a, u) => a + (Number(u.cost) || 0), 0))

const modelDonut = computed(() => {
  const colors = ['#0F9F73', '#F59E0B', '#3B82F6', '#8B5CF6', '#EF4444', '#64748B']
  const top = [...perModel.value].slice(0, 6)
  if (!top.length) return [{ label: '暂无', value: 1, color: '#E2E8F0' }]
  return top.map((m, i) => ({
    label: m.model || '—',
    value: Number(m.calls) || 0,
    color: colors[i % colors.length],
  }))
})

function formatTs(v?: string) {
  if (!v) return '—'
  try {
    const d = new Date(v)
    return Number.isNaN(d.getTime()) ? String(v) : d.toLocaleString()
  } catch {
    return String(v)
  }
}

async function load() {
  loading.value = true
  err.value = ''
  try {
    const [usageData, summary] = await Promise.all([
      adminApi.usage(),
      adminApi.usageSummary(20),
    ])
    rows.value = asList<UsageRow>(usageData, ['items', 'rows', 'data', 'results', 'usage', 'recent'])
    perUser.value = asList<UserStat>((summary as any)?.per_user, ['items'])
    perModel.value = asList<ModelStat>((summary as any)?.per_model, ['items'])
    perUserModel.value = asList<UserModelStat>((summary as any)?.per_user_model, ['items'])
  } catch (e: any) {
    err.value = e.message || '加载失败'
    rows.value = []
  } finally {
    loading.value = false
  }
}

onMounted(load)
</script>

<template>
  <div>
    <PageHeader title="用量" subtitle="按用户与模型汇总，含近期明细">
      <template #actions>
        <button class="btn-ghost" type="button" :disabled="loading" @click="load">刷新</button>
      </template>
    </PageHeader>

    <p v-if="err" class="mb-3 text-sm text-red-600">{{ err }}</p>

    <div class="grid gap-4 md:grid-cols-3">
      <div class="card">
        <div class="label">总调用</div>
        <div class="mt-1 text-2xl font-semibold tabular-nums">{{ totalCalls }}</div>
      </div>
      <div class="card">
        <div class="label">总 Token</div>
        <div class="mt-1 text-2xl font-semibold tabular-nums">{{ totalTokens }}</div>
      </div>
      <div class="card">
        <div class="label">总成本</div>
        <div class="mt-1 text-2xl font-semibold tabular-nums">{{ totalCost }}</div>
      </div>
    </div>

    <div class="mt-4 grid gap-4 lg:grid-cols-2">
      <div class="card space-y-3">
        <div class="font-semibold">用户用量</div>
        <EmptyState v-if="!loading && !perUser.length" title="暂无用户汇总" description="有调用后将按用户聚合。" />
        <div v-else-if="perUser.length" class="table-wrap">
          <table class="data-table">
            <thead>
              <tr>
                <th>用户名</th>
                <th>调用</th>
                <th>成功</th>
                <th>失败</th>
                <th>Token</th>
                <th>成本</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="u in perUser" :key="u.user_id || u.username">
                <td class="font-semibold">{{ u.username || u.user_id || '—' }}</td>
                <td class="tabular-nums">{{ u.calls ?? 0 }}</td>
                <td class="tabular-nums text-teal">{{ u.success_calls ?? 0 }}</td>
                <td class="tabular-nums text-red-600">{{ u.failed_calls ?? 0 }}</td>
                <td class="tabular-nums">{{ u.tokens ?? 0 }}</td>
                <td class="tabular-nums">{{ u.cost ?? 0 }}</td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>

      <div class="card space-y-3">
        <div class="font-semibold">模型分布（调用次数）</div>
        <DonutChart :segments="modelDonut" center-sub="模型" />
        <div v-if="perModel.length" class="table-wrap">
          <table class="data-table">
            <thead>
              <tr>
                <th>模型</th>
                <th>调用</th>
                <th>Token</th>
                <th>成本</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="m in perModel" :key="m.model">
                <td class="font-mono text-sm">{{ m.model || '—' }}</td>
                <td class="tabular-nums">{{ m.calls ?? 0 }}</td>
                <td class="tabular-nums">{{ m.tokens ?? 0 }}</td>
                <td class="tabular-nums">{{ m.cost ?? 0 }}</td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>
    </div>

    <div v-if="perUserModel.length" class="card mt-4 space-y-3">
      <div class="font-semibold">用户 × 模型（Top）</div>
      <div class="table-wrap">
        <table class="data-table">
          <thead>
            <tr>
              <th>用户名</th>
              <th>模型</th>
              <th>调用</th>
              <th>Token</th>
              <th>成本</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="(r, i) in perUserModel" :key="i">
              <td class="font-semibold">{{ r.username || '—' }}</td>
              <td class="font-mono text-sm">{{ r.model || '—' }}</td>
              <td class="tabular-nums">{{ r.calls ?? 0 }}</td>
              <td class="tabular-nums">{{ r.tokens ?? 0 }}</td>
              <td class="tabular-nums">{{ r.cost ?? 0 }}</td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>

    <div class="card mt-4 space-y-3">
      <div class="font-semibold">近期明细</div>
      <EmptyState
        v-if="!loading && !err && rows.length === 0"
        title="暂无用量记录"
        description="近期没有请求记录。网关有流量后将显示于此。"
      />
      <div v-else-if="rows.length" class="table-wrap">
        <table class="data-table">
          <thead>
            <tr>
              <th>时间</th>
              <th>用户名</th>
              <th>模型</th>
              <th>密钥前缀</th>
              <th>状态</th>
              <th>Token</th>
              <th>成本</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="(r, i) in rows" :key="r.id ?? i">
              <td class="whitespace-nowrap text-xs text-slatex">{{ formatTs(r.created_at) }}</td>
              <td class="font-semibold">{{ r.username || r.user_id || '—' }}</td>
              <td class="font-mono text-sm">{{ r.model || '—' }}</td>
              <td class="font-mono text-xs">{{ r.key_prefix || '—' }}</td>
              <td><StatusPill :status="String(r.status || 'ok')" /></td>
              <td class="tabular-nums">{{ r.tokens ?? '—' }}</td>
              <td class="tabular-nums">{{ r.cost ?? '—' }}</td>
            </tr>
          </tbody>
        </table>
      </div>
      <div v-else-if="loading" class="text-slatex">加载中…</div>
    </div>
  </div>
</template>
