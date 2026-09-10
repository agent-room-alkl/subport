<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { adminApi, asList } from '../api/client'
import PageHeader from '../components/PageHeader.vue'
import StatusPill from '../components/StatusPill.vue'
import EmptyState from '../components/EmptyState.vue'

type ApiKey = {
  id?: string
  prefix?: string
  key_prefix?: string
  name?: string
  enabled?: boolean
  created_at?: string
  updated_at?: string
  last_used_at?: string
  expires_at?: string
}

const rows = ref<ApiKey[]>([])
const err = ref('')
const loading = ref(false)

function formatTs(v?: string) {
  if (!v) return '—'
  try {
    const d = new Date(v)
    return Number.isNaN(d.getTime()) ? v : d.toLocaleString()
  } catch {
    return v
  }
}

function prefixOf(k: ApiKey) {
  return k.prefix || k.key_prefix || '—'
}

async function load() {
  loading.value = true
  err.value = ''
  try {
    rows.value = asList<ApiKey>(await adminApi.keys(), [
      'items',
      'rows',
      'data',
      'results',
      'keys',
    ])
  } catch (e: any) {
    err.value = e.message || '加载失败（接口可能尚未就绪）'
    rows.value = []
  } finally {
    loading.value = false
  }
}

onMounted(load)
</script>

<template>
  <div>
    <PageHeader
      title="下游 API 密钥"
      subtitle="发给调用方的 sk-sp-…，不是上游 OAuth。"
    >
      <template #actions>
        <button class="btn-ghost" type="button" :disabled="loading" @click="load">刷新</button>
      </template>
    </PageHeader>

    <p v-if="err" class="mb-3 text-sm text-amber-700">{{ err }}</p>

    <EmptyState
      v-if="!loading && !err && rows.length === 0"
      title="暂无下游密钥"
      description="尚未创建 sk-sp- 密钥。创建后可发给调用方访问网关。"
    />

    <EmptyState
      v-else-if="!loading && err && rows.length === 0"
      title="无法加载密钥"
      :description="err"
    >
      <template #actions>
        <button class="btn-ghost" type="button" @click="load">重试</button>
      </template>
    </EmptyState>

    <div v-else-if="rows.length" class="table-wrap">
      <table class="data-table">
        <thead>
          <tr>
            <th>前缀</th>
            <th>名称</th>
            <th>状态</th>
            <th>创建时间</th>
            <th>最近使用</th>
            <th>过期</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="(k, i) in rows" :key="k.id || prefixOf(k) + '-' + i">
            <td class="font-mono text-sm text-teal">{{ prefixOf(k) }}</td>
            <td>{{ k.name || '—' }}</td>
            <td>
              <StatusPill
                :status="k.enabled !== false ? 'enabled' : 'disabled'"
                :label="k.enabled !== false ? '启用' : '禁用'"
              />
            </td>
            <td class="whitespace-nowrap text-xs text-slatex">{{ formatTs(k.created_at) }}</td>
            <td class="whitespace-nowrap text-xs text-slatex">{{ formatTs(k.last_used_at) }}</td>
            <td class="whitespace-nowrap text-xs text-slatex">{{ formatTs(k.expires_at) }}</td>
          </tr>
        </tbody>
      </table>
    </div>

    <div v-else-if="loading" class="card text-slatex">加载中…</div>
  </div>
</template>
