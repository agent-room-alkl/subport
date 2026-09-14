<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { asList, consoleApi } from '../../api/client'
import PageHeader from '../../components/PageHeader.vue'
import StatusPill from '../../components/StatusPill.vue'
import EmptyState from '../../components/EmptyState.vue'

type ApiKey = {
  id: string
  name?: string
  prefix?: string
  enabled?: boolean
  created_at?: string
  last_used?: string
}

const rows = ref<ApiKey[]>([])
const keyName = ref('')
const secretOnce = ref('')
const err = ref('')
const ok = ref('')
const loading = ref(false)
const creating = ref(false)
const copied = ref(false)

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
    rows.value = asList<ApiKey>(await consoleApi.keys(), [
      'items',
      'rows',
      'data',
      'keys',
    ])
  } catch (e: any) {
    err.value = e?.message || '加载失败'
    rows.value = []
  } finally {
    loading.value = false
  }
}

async function createKey() {
  secretOnce.value = ''
  ok.value = ''
  err.value = ''
  creating.value = true
  copied.value = false
  try {
    const name = keyName.value.trim() || '未命名密钥'
    const out = await consoleApi.createKey(name)
    secretOnce.value = out.secret
    ok.value = '密钥已创建（完整密钥仅显示一次，请立即保存）'
    keyName.value = ''
    await load()
  } catch (e: any) {
    err.value = e?.message || '创建失败'
  } finally {
    creating.value = false
  }
}

async function copySecret() {
  if (!secretOnce.value) return
  try {
    await navigator.clipboard.writeText(secretOnce.value)
    copied.value = true
    setTimeout(() => {
      copied.value = false
    }, 2000)
  } catch {
    err.value = '复制失败，请手动选中复制'
  }
}

async function removeKey(k: ApiKey) {
  if (!k.id) return
  const label = k.name || k.prefix || k.id
  if (!confirm(`确定删除密钥「${label}」？此操作不可恢复。`)) return
  err.value = ''
  ok.value = ''
  try {
    await consoleApi.deleteKey(k.id)
    ok.value = '密钥已删除'
    if (secretOnce.value) secretOnce.value = ''
    await load()
  } catch (e: any) {
    err.value = e?.message || '删除失败'
  }
}

async function toggleEnabled(k: ApiKey) {
  if (!k.id) return
  const next = !(k.enabled !== false)
  err.value = ''
  ok.value = ''
  try {
    await consoleApi.setKeyEnabled(k.id, next)
    ok.value = next ? '已启用' : '已禁用'
    await load()
  } catch (e: any) {
    err.value = e?.message || '更新状态失败（接口可能不可用）'
  }
}

onMounted(load)
</script>

<template>
  <div>
    <PageHeader title="密钥" subtitle="创建与管理 sk-sp- 下游 API 密钥；完整密钥仅在创建时显示一次。">
      <template #actions>
        <button class="btn-ghost" type="button" :disabled="loading" @click="load">刷新</button>
      </template>
    </PageHeader>

    <p v-if="err" class="mb-3 text-sm text-red-600">{{ err }}</p>
    <p v-if="ok" class="mb-3 text-sm text-teal">{{ ok }}</p>

    <div class="card mb-4 space-y-3">
      <div class="font-semibold">创建密钥</div>
      <div class="flex flex-wrap gap-2">
        <input
          v-model="keyName"
          class="input max-w-md flex-1"
          placeholder="密钥名称，例如：本机开发"
          @keyup.enter="createKey"
        />
        <button class="btn-primary" type="button" :disabled="creating" @click="createKey">
          {{ creating ? '创建中…' : '创建' }}
        </button>
      </div>
      <div
        v-if="secretOnce"
        class="flex flex-col gap-2 rounded-lg border border-amber-200 bg-amber-50 px-3 py-3 text-amber-950 sm:flex-row sm:items-start"
      >
        <div class="min-w-0 flex-1">
          <div class="text-sm font-semibold">请立即保存完整密钥（仅显示一次）</div>
          <code class="mt-1 block break-all font-mono text-xs sm:text-sm">{{ secretOnce }}</code>
        </div>
        <button class="btn-ghost shrink-0 border-amber-300 bg-white" type="button" @click="copySecret">
          {{ copied ? '已复制' : '复制' }}
        </button>
      </div>
    </div>

    <EmptyState
      v-if="!loading && rows.length === 0"
      title="暂无密钥"
      description="创建后可在 Authorization: Bearer sk-sp-… 中使用。"
    />

    <div v-else-if="rows.length" class="table-wrap">
      <table class="data-table">
        <thead>
          <tr>
            <th>名称</th>
            <th>前缀</th>
            <th>状态</th>
            <th>创建时间</th>
            <th>操作</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="k in rows" :key="k.id">
            <td class="font-medium">{{ k.name || '—' }}</td>
            <td class="font-mono text-sm text-teal">{{ k.prefix || '—' }}</td>
            <td>
              <StatusPill
                :status="k.enabled !== false ? 'enabled' : 'disabled'"
                :label="k.enabled !== false ? '启用' : '禁用'"
              />
            </td>
            <td class="whitespace-nowrap text-xs text-slatex">{{ formatTs(k.created_at) }}</td>
            <td>
              <div class="flex flex-wrap gap-2">
                <button class="btn-ghost" type="button" @click="toggleEnabled(k)">
                  {{ k.enabled !== false ? '禁用' : '启用' }}
                </button>
                <button class="btn-danger" type="button" @click="removeKey(k)">删除</button>
              </div>
            </td>
          </tr>
        </tbody>
      </table>
    </div>

    <div v-else-if="loading" class="card text-slatex">加载中…</div>
  </div>
</template>
