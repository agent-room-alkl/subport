<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { asList, consoleApi } from '../../api/client'
import PageHeader from '../../components/PageHeader.vue'
import EmptyState from '../../components/EmptyState.vue'

type ModelRow = {
  id?: string
  provider?: string
  model?: string
  label?: string
  notes?: string
  enabled?: boolean
  sort_order?: number
}

const rows = ref<ModelRow[]>([])
const err = ref('')
const loading = ref(false)

const providerColor: Record<string, string> = {
  claude: 'bg-orange-100 text-orange-800',
  codex: 'bg-emerald-100 text-emerald-800',
  openai: 'bg-emerald-100 text-emerald-800',
  antigravity: 'bg-blue-100 text-blue-800',
}

function badgeClass(p?: string) {
  return providerColor[(p || '').toLowerCase()] || 'bg-slate-100 text-slate-700'
}

function hasCreditsNote(notes?: string) {
  return (notes || '').toLowerCase().includes('credits')
}

async function load() {
  loading.value = true
  err.value = ''
  try {
    const data = await consoleApi.models()
    rows.value = asList<ModelRow>(data, ['items', 'rows', 'data', 'models'])
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
    <PageHeader title="模型" subtitle="当前可路由的热门模型目录">
      <template #actions>
        <button class="btn-ghost" type="button" :disabled="loading" @click="load">刷新</button>
      </template>
    </PageHeader>

    <p v-if="err" class="mb-3 text-sm text-red-600">{{ err }}</p>

    <EmptyState
      v-if="!loading && !err && rows.length === 0"
      title="暂无可用模型"
      description="管理员尚未启用模型目录，请稍后再试。"
    />

    <div v-else-if="rows.length" class="grid gap-3 md:grid-cols-2">
      <div v-for="m in rows" :key="m.id || m.model" class="card space-y-2">
        <div class="flex items-center gap-2 flex-wrap">
          <span class="rounded-full px-2 py-0.5 text-xs font-medium" :class="badgeClass(m.provider)">
            {{ m.provider || '—' }}
          </span>
          <span class="font-semibold text-ink">{{ m.label || m.model }}</span>
          <span
            v-if="hasCreditsNote(m.notes)"
            class="rounded-full bg-slate-200 px-2 py-0.5 text-xs font-medium text-slate-600"
          >credits</span>
        </div>
        <div class="font-mono text-sm text-slatex">{{ m.model }}</div>
        <p class="text-sm text-slatex">{{ m.notes || '可在请求中指定此 model 字段。' }}</p>
      </div>
    </div>

    <div v-else-if="loading" class="card text-slatex">加载中…</div>
  </div>
</template>
