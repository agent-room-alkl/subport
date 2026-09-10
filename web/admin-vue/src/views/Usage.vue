<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { adminApi, asList } from '../api/client'
import PageHeader from '../components/PageHeader.vue'
import StatusPill from '../components/StatusPill.vue'
import EmptyState from '../components/EmptyState.vue'

type UsageRow = {
  id?: string | number
  created_at?: string
  ts?: string
  timestamp?: string
  model?: string
  account_id?: string
  channel_id?: string
  key_prefix?: string
  api_key_prefix?: string
  status?: string | number
  status_code?: number
  tokens_in?: number
  tokens_out?: number
  prompt_tokens?: number
  completion_tokens?: number
  latency_ms?: number
  error?: string
}

const rows = ref<UsageRow[]>([])
const err = ref('')
const loading = ref(false)

function formatTs(r: UsageRow) {
  const v = r.created_at || r.ts || r.timestamp
  if (!v) return '—'
  try {
    const d = new Date(v)
    return Number.isNaN(d.getTime()) ? String(v) : d.toLocaleString()
  } catch {
    return String(v)
  }
}

function statusOf(r: UsageRow) {
  if (r.status != null) return String(r.status)
  if (r.status_code != null) return String(r.status_code)
  if (r.error) return 'error'
  return 'ok'
}

function tokensIn(r: UsageRow) {
  return r.tokens_in ?? r.prompt_tokens ?? '—'
}

function tokensOut(r: UsageRow) {
  return r.tokens_out ?? r.completion_tokens ?? '—'
}

function keyPrefix(r: UsageRow) {
  return r.key_prefix || r.api_key_prefix || '—'
}

async function load() {
  loading.value = true
  err.value = ''
  try {
    const data = await adminApi.usage()
    rows.value = asList<UsageRow>(data, [
      'items',
      'rows',
      'data',
      'results',
      'usage',
      'recent',
    ])
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
    <PageHeader title="用量" subtitle="近期请求记录">
      <template #actions>
        <button class="btn-ghost" type="button" :disabled="loading" @click="load">刷新</button>
      </template>
    </PageHeader>

    <p v-if="err" class="mb-3 text-sm text-red-600">{{ err }}</p>

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
            <th>模型</th>
            <th>账号</th>
            <th>密钥前缀</th>
            <th>状态</th>
            <th>输入 Token</th>
            <th>输出 Token</th>
            <th>延迟</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="(r, i) in rows" :key="r.id ?? i">
            <td class="whitespace-nowrap text-xs text-slatex">{{ formatTs(r) }}</td>
            <td class="font-mono text-sm">{{ r.model || '—' }}</td>
            <td class="font-mono text-xs text-slatex">{{ r.account_id || '—' }}</td>
            <td class="font-mono text-xs">{{ keyPrefix(r) }}</td>
            <td>
              <StatusPill :status="statusOf(r)" />
            </td>
            <td class="tabular-nums">{{ tokensIn(r) }}</td>
            <td class="tabular-nums">{{ tokensOut(r) }}</td>
            <td class="tabular-nums text-xs text-slatex">
              {{ r.latency_ms != null ? r.latency_ms + 'ms' : '—' }}
            </td>
          </tr>
        </tbody>
      </table>
    </div>

    <div v-else-if="loading" class="card text-slatex">加载中…</div>
  </div>
</template>
