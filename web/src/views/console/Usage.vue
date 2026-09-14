<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { asList, consoleApi } from '../../api/client'
import PageHeader from '../../components/PageHeader.vue'
import StatusPill from '../../components/StatusPill.vue'
import EmptyState from '../../components/EmptyState.vue'

type UsageRow = {
  id?: string
  model?: string
  tokens?: number
  cost?: number
  status?: string
  created_at?: string
  token_parts?: { input?: number; output?: number }
}

const rows = ref<UsageRow[]>([])
const err = ref('')
const loading = ref(false)

function formatTs(v?: string) {
  if (!v) return '—'
  try {
    const d = new Date(v)
    return Number.isNaN(d.getTime()) ? v : d.toLocaleString()
  } catch {
    return String(v)
  }
}

async function load() {
  loading.value = true
  err.value = ''
  try {
    rows.value = asList<UsageRow>(await consoleApi.usage(), [
      'items',
      'rows',
      'data',
      'results',
      'usage',
    ])
  } catch (e: any) {
    err.value = e?.message || '加载失败'
    rows.value = []
  } finally {
    loading.value = false
  }
}

onMounted(load)
</script>

<template>
  <div>
    <PageHeader title="用量" subtitle="本账号近期调用记录">
      <template #actions>
        <button class="btn-ghost" type="button" :disabled="loading" @click="load">刷新</button>
      </template>
    </PageHeader>

    <p v-if="err" class="mb-3 text-sm text-red-600">{{ err }}</p>

    <EmptyState
      v-if="!loading && !err && rows.length === 0"
      title="暂无用量记录"
      description="有 API 请求后，模型、Token、费用与状态会显示在这里。"
    />

    <div v-else-if="rows.length" class="table-wrap">
      <table class="data-table">
        <thead>
          <tr>
            <th>时间</th>
            <th>模型</th>
            <th>Token</th>
            <th>费用</th>
            <th>状态</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="(r, i) in rows" :key="r.id ?? i">
            <td class="whitespace-nowrap text-xs text-slatex">{{ formatTs(r.created_at) }}</td>
            <td class="font-mono text-sm">{{ r.model || '—' }}</td>
            <td class="tabular-nums">{{ r.tokens ?? '—' }}</td>
            <td class="tabular-nums">{{ r.cost ?? '—' }}</td>
            <td>
              <StatusPill :status="r.status || 'ok'" :label="r.status || 'ok'" />
            </td>
          </tr>
        </tbody>
      </table>
    </div>

    <div v-else-if="loading" class="card text-slatex">加载中…</div>
  </div>
</template>
