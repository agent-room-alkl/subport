<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { adminApi, asList, type ModelRoute } from '../api/client'
import PageHeader from '../components/PageHeader.vue'
import StatusPill from '../components/StatusPill.vue'
import EmptyState from '../components/EmptyState.vue'

const rows = ref<ModelRoute[]>([])
const err = ref('')
const msg = ref('')
const loading = ref(false)
const saving = ref(false)

const showModal = ref(false)
const editing = ref<ModelRoute | null>(null)
const form = ref({
  pattern: '',
  provider: '',
  priority: 0,
  enabled: true,
})

const confirmDelete = ref<ModelRoute | null>(null)

async function load() {
  loading.value = true
  err.value = ''
  try {
    rows.value = asList<ModelRoute>(await adminApi.modelRoutes())
  } catch (e: any) {
    err.value = e.message || '加载失败'
    rows.value = []
  } finally {
    loading.value = false
  }
}

function openCreate() {
  editing.value = null
  form.value = { pattern: '', provider: '', priority: 0, enabled: true }
  showModal.value = true
}

function openEdit(r: ModelRoute) {
  editing.value = r
  form.value = {
    pattern: r.pattern || '',
    provider: r.provider || '',
    priority: Number(r.priority) || 0,
    enabled: r.enabled !== false,
  }
  showModal.value = true
}

function closeModal() {
  showModal.value = false
  editing.value = null
}

async function save() {
  if (!form.value.pattern.trim() || !form.value.provider.trim()) {
    msg.value = '请填写 pattern 与 provider'
    return
  }
  saving.value = true
  msg.value = ''
  try {
    const body = {
      pattern: form.value.pattern.trim(),
      provider: form.value.provider.trim(),
      priority: Number(form.value.priority) || 0,
      enabled: !!form.value.enabled,
    }
    if (editing.value) {
      await adminApi.updateModelRoute(editing.value.id, body)
      msg.value = '已更新路由'
    } else {
      await adminApi.createModelRoute(body)
      msg.value = '已创建路由'
    }
    closeModal()
    await load()
  } catch (e: any) {
    msg.value = e.message
  } finally {
    saving.value = false
  }
}

async function doDelete() {
  if (!confirmDelete.value) return
  saving.value = true
  try {
    await adminApi.deleteModelRoute(confirmDelete.value.id)
    msg.value = '已删除'
    confirmDelete.value = null
    await load()
  } catch (e: any) {
    msg.value = e.message
  } finally {
    saving.value = false
  }
}

onMounted(load)
</script>

<template>
  <div>
    <PageHeader
      title="模型路由"
      subtitle="将下游模型名映射到上游 provider / 渠道"
    >
      <template #actions>
        <button class="btn-ghost" type="button" :disabled="loading" @click="load">刷新</button>
        <button class="btn-primary" type="button" @click="openCreate">新建路由</button>
      </template>
    </PageHeader>

    <p v-if="err" class="mb-3 text-sm text-red-600">{{ err }}</p>
    <p v-if="msg" class="mb-3 text-sm text-teal">{{ msg }}</p>

    <EmptyState
      v-if="!loading && !err && rows.length === 0"
      title="暂无路由"
      description="尚未配置模型路由。点击「新建路由」添加 pattern → provider 映射。"
    >
      <template #actions>
        <button class="btn-primary" type="button" @click="openCreate">新建路由</button>
      </template>
    </EmptyState>

    <div v-else-if="rows.length" class="table-wrap">
      <table class="data-table">
        <thead>
          <tr>
            <th>Pattern</th>
            <th>Provider</th>
            <th>优先级</th>
            <th>状态</th>
            <th></th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="r in rows" :key="r.id">
            <td class="font-mono text-sm">{{ r.pattern }}</td>
            <td>{{ r.provider }}</td>
            <td class="tabular-nums">{{ r.priority }}</td>
            <td>
              <StatusPill
                :status="r.enabled ? 'enabled' : 'disabled'"
                :label="r.enabled ? '启用' : '禁用'"
              />
            </td>
            <td class="text-right">
              <div class="flex justify-end gap-1.5">
                <button class="btn-ghost" type="button" @click="openEdit(r)">编辑</button>
                <button class="btn-danger" type="button" @click="confirmDelete = r">删除</button>
              </div>
            </td>
          </tr>
        </tbody>
      </table>
    </div>

    <div v-if="showModal" class="modal-backdrop" @click.self="closeModal">
      <div class="modal-panel">
        <div class="flex items-center gap-2">
          <h3 class="font-semibold">{{ editing ? '编辑路由' : '新建路由' }}</h3>
          <div class="flex-1" />
          <button class="btn-ghost" type="button" @click="closeModal">关闭</button>
        </div>
        <div>
          <label class="label">Pattern</label>
          <input v-model="form.pattern" class="input font-mono" placeholder="claude-*" />
        </div>
        <div>
          <label class="label">Provider</label>
          <input v-model="form.provider" class="input" placeholder="claude / codex / …" />
        </div>
        <div>
          <label class="label">优先级</label>
          <input v-model.number="form.priority" class="input" type="number" />
        </div>
        <label class="flex items-center gap-2 text-sm">
          <input v-model="form.enabled" type="checkbox" class="accent-teal" />
          启用
        </label>
        <div class="flex justify-end gap-2">
          <button class="btn-ghost" type="button" @click="closeModal">取消</button>
          <button class="btn-primary" type="button" :disabled="saving" @click="save">
            {{ saving ? '保存中…' : '保存' }}
          </button>
        </div>
      </div>
    </div>

    <div v-if="confirmDelete" class="modal-backdrop" @click.self="confirmDelete = null">
      <div class="modal-panel">
        <h3 class="font-semibold">确认删除</h3>
        <p class="text-sm text-slatex">
          确定删除路由
          <span class="font-mono text-ink">{{ confirmDelete.pattern }}</span>
          ？此操作不可撤销。
        </p>
        <div class="flex justify-end gap-2">
          <button class="btn-ghost" type="button" @click="confirmDelete = null">取消</button>
          <button class="btn-danger" type="button" :disabled="saving" @click="doDelete">
            {{ saving ? '删除中…' : '删除' }}
          </button>
        </div>
      </div>
    </div>
  </div>
</template>
