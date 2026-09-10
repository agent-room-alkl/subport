<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { asList, consoleApi } from '../../api/client'
import PageHeader from '../../components/PageHeader.vue'
import DonutChart from '../../components/charts/DonutChart.vue'
import BarTrendChart from '../../components/charts/BarTrendChart.vue'

type UsageRow = {
  id?: string
  model?: string
  tokens?: number
  cost?: number
  status?: string
  created_at?: string
}

const router = useRouter()
const quota = ref<any>(null)
const me = ref<any>(null)
const keys = ref<any[]>([])
const topups = ref<any[]>([])
const usage = ref<UsageRow[]>([])
const err = ref('')
const loading = ref(false)

const recentTopups = computed(() => topups.value.slice(0, 3))

const used = computed(() => Number(quota.value?.quota_used ?? 0) || 0)
const remaining = computed(() => Number(quota.value?.remaining ?? 0) || 0)
const reserved = computed(() => {
  const v = Number(quota.value?.quota_reserved ?? me.value?.quota_reserved ?? 0) || 0
  return v > 0 ? v : 0
})

const donutSegments = computed(() => {
  const segs: { label: string; value: number; color: string }[] = [
    { label: '剩余', value: Math.max(0, remaining.value), color: '#0F9F73' },
    { label: '已用', value: Math.max(0, used.value), color: '#F59E0B' },
  ]
  if (reserved.value > 0) {
    segs.push({ label: '预留', value: reserved.value, color: '#64748B' })
  }
  // empty ring fallback so chart still renders
  if (segs.every((s) => s.value <= 0)) {
    return [{ label: '暂无', value: 1, color: '#E2E8F0' }]
  }
  return segs
})

const remainingPct = computed(() => {
  const total = used.value + remaining.value
  if (total <= 0) return '—'
  return `${Math.round((remaining.value / total) * 100)}%`
})

function dayKeyLocal(d: Date): string {
  const y = d.getFullYear()
  const m = String(d.getMonth() + 1).padStart(2, '0')
  const day = String(d.getDate()).padStart(2, '0')
  return `${y}-${m}-${day}`
}

function dayLabel(d: Date): string {
  return `${d.getMonth() + 1}/${d.getDate()}`
}

const last7Days = computed(() => {
  const today = new Date()
  today.setHours(0, 0, 0, 0)
  const days: { key: string; label: string; value: number }[] = []
  for (let i = 6; i >= 0; i--) {
    const d = new Date(today)
    d.setDate(today.getDate() - i)
    days.push({ key: dayKeyLocal(d), label: dayLabel(d), value: 0 })
  }
  const map = new Map(days.map((d) => [d.key, d]))
  for (const row of usage.value) {
    if (!row.created_at) continue
    const dt = new Date(row.created_at)
    if (Number.isNaN(dt.getTime())) continue
    const key = dayKeyLocal(dt)
    const bucket = map.get(key)
    if (bucket) bucket.value += Number(row.tokens) || 0
  }
  return days.map(({ label, value }) => ({ label, value }))
})

const hasUsageIn7d = computed(() => last7Days.value.some((p) => p.value > 0))

const topModels = computed(() => {
  const map = new Map<string, number>()
  for (const row of usage.value) {
    const name = (row.model || '').trim() || '未知模型'
    map.set(name, (map.get(name) || 0) + (Number(row.tokens) || 0))
  }
  return [...map.entries()]
    .map(([label, value]) => ({ label, value }))
    .filter((x) => x.value > 0)
    .sort((a, b) => b.value - a.value)
    .slice(0, 5)
})

const topModelMax = computed(() => Math.max(1, ...topModels.value.map((m) => m.value)))

function formatTs(v?: string) {
  if (!v) return '—'
  try {
    const d = new Date(v)
    return Number.isNaN(d.getTime()) ? v : d.toLocaleString()
  } catch {
    return v
  }
}

async function load() {
  loading.value = true
  err.value = ''
  try {
    const [q, k, t, u, m] = await Promise.all([
      consoleApi.quota(),
      consoleApi.keys(),
      consoleApi.topups(),
      consoleApi.usage(),
      consoleApi.me().catch(() => null),
    ])
    quota.value = q
    me.value = m
    keys.value = asList(k, ['items', 'rows', 'data', 'keys'])
    topups.value = asList(t, ['items', 'rows', 'data', 'topups'])
    usage.value = asList<UsageRow>(u, ['items', 'rows', 'data', 'results', 'usage'])
  } catch (e: any) {
    err.value = e?.message || '加载失败'
  } finally {
    loading.value = false
  }
}

onMounted(load)
</script>

<template>
  <div>
    <PageHeader title="看板" subtitle="额度概览、用量趋势与最近加额">
      <template #actions>
        <button class="btn-ghost" type="button" :disabled="loading" @click="load">刷新</button>
      </template>
    </PageHeader>

    <p v-if="err" class="mb-3 text-sm text-red-600">{{ err }}</p>

    <div class="grid gap-4 md:grid-cols-4">
      <div class="card">
        <div class="label">剩余额度</div>
        <div class="mt-1 text-2xl font-semibold tabular-nums text-teal">
          {{ quota?.remaining ?? '—' }}
        </div>
      </div>
      <div class="card">
        <div class="label">总额度</div>
        <div class="mt-1 text-2xl font-semibold tabular-nums">
          {{ quota?.quota_total ?? '—' }}
        </div>
      </div>
      <div class="card">
        <div class="label">已用额度</div>
        <div class="mt-1 text-2xl font-semibold tabular-nums">
          {{ quota?.quota_used ?? '—' }}
        </div>
      </div>
      <div class="card">
        <div class="label">密钥数量</div>
        <div class="mt-1 text-2xl font-semibold tabular-nums">{{ keys.length }}</div>
      </div>
    </div>

    <div class="mt-4 grid gap-4 lg:grid-cols-2">
      <div class="card space-y-3">
        <div class="font-semibold">额度占比</div>
        <DonutChart
          :segments="donutSegments"
          :center-text="remainingPct"
          center-sub="剩余占比"
        />
      </div>
      <div class="card space-y-3">
        <div class="flex items-center justify-between gap-2">
          <div class="font-semibold">近7日用量</div>
          <span class="text-xs text-slatex">按 Token 合计</span>
        </div>
        <BarTrendChart v-if="hasUsageIn7d || !loading" :points="last7Days" color="#0F9F73" />
        <p v-else class="py-8 text-center text-sm text-slatex">加载中…</p>
      </div>
    </div>

    <div v-if="topModels.length" class="card mt-4 space-y-3">
      <div class="font-semibold">热门模型（Token）</div>
      <ul class="space-y-2">
        <li v-for="m in topModels" :key="m.label" class="text-sm">
          <div class="mb-1 flex items-center justify-between gap-2">
            <span class="truncate font-mono text-ink">{{ m.label }}</span>
            <span class="shrink-0 tabular-nums text-slatex">{{ m.value }}</span>
          </div>
          <div class="h-2 overflow-hidden rounded-full bg-slate-100">
            <div
              class="h-full rounded-full bg-teal"
              :style="{ width: `${Math.max(4, (m.value / topModelMax) * 100)}%` }"
            />
          </div>
        </li>
      </ul>
    </div>

    <div class="mt-4 grid gap-4 lg:grid-cols-2">
      <div class="card space-y-3">
        <div class="flex items-center justify-between">
          <div class="font-semibold">快捷操作</div>
        </div>
        <div class="flex flex-wrap gap-2">
          <button class="btn-primary" type="button" @click="router.push({ name: 'console-recharge' })">
            去充值
          </button>
          <button class="btn-ghost" type="button" @click="router.push({ name: 'console-keys' })">
            管理密钥
          </button>
        </div>
        <div class="rounded-lg border border-teal-bright/40 bg-teal-bright/5 px-3 py-2 text-sm text-slatex">
          <div class="font-medium text-ink">调用提示</div>
          <p class="mt-1">
            在请求头使用
            <code class="rounded bg-white px-1 font-mono text-teal">Authorization: Bearer sk-sp-…</code>
            访问 OpenAI 兼容接口。密钥仅在创建时完整显示一次。
          </p>
        </div>
      </div>

      <div class="card space-y-2">
        <div class="font-semibold">最近加额</div>
        <div v-if="recentTopups.length" class="divide-y divide-line text-sm">
          <div
            v-for="t in recentTopups"
            :key="t.id"
            class="flex items-center justify-between py-2"
          >
            <span>
              <span class="font-semibold text-teal">+{{ t.credit }}</span>
              <span class="ml-2 text-xs text-slatex">{{ t.source || '—' }}</span>
            </span>
            <span class="text-xs text-slatex">{{ formatTs(t.created_at) }}</span>
          </div>
        </div>
        <p v-else class="text-sm text-slatex">{{ loading ? '加载中…' : '暂无加额记录' }}</p>
        <button
          class="btn-ghost mt-1"
          type="button"
          @click="router.push({ name: 'console-recharge' })"
        >
          查看全部 / 充值
        </button>
      </div>
    </div>
  </div>
</template>
