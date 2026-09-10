<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { adminApi, asList, type Proxy } from '../api/client'
import PageHeader from '../components/PageHeader.vue'
import StatusPill from '../components/StatusPill.vue'
import EmptyState from '../components/EmptyState.vue'

const PROXY_TYPES = ['http', 'socks5', 'socks5h'] as const

const rows = ref<Proxy[]>([])
const err = ref('')
const msg = ref('')
const loading = ref(false)
const saving = ref(false)

const showModal = ref(false)
const editing = ref<Proxy | null>(null)
const form = ref({
  name: '',
  type: 'http' as string,
  url: '',
  enabled: true,
})
const confirmDelete = ref<Proxy | null>(null)

async function load() {
  loading.value = true
  err.value = ''
  try {
    rows.value = asList<Proxy>(await adminApi.proxies())
  } catch (e: any) {
    err.value = e.message || '加载失败'
    rows.value = []
  } finally {
    loading.value = false
  }
}

function openCreate() {
  editing.value = null
  form.value = { name: '', type: 'http', url: '', enabled: true }
  showModal.value = true
}

function openEdit(p: Proxy) {
  editing.value = p
  form.value = {
    name: p.name || '',
    type: p.type || 'http',
    url: p.url || '',
    enabled: p.enabled !== false,
  }
  showModal.value = true
}

function closeModal() {
  showModal.value = false
  editing.value = null
}

async function save() {
  if (!form.value.name.trim() || !form.value.url.trim()) {
    msg.value = '请填写名称与 URL'
    return
  }
  saving.value = true
  msg.value = ''
  try {
    const body = {
      name: form.value.name.trim(),
      type: form.value.type,
      url: form.value.url.trim(),
      enabled: !!form.value.enabled,
    }
    if (editing.value) {
      await adminApi.updateProxy(editing.value.id, body)
      msg.value = '已更新代理'
    } else {
      await adminApi.createProxy(body)
      msg.value = '已创建代理'
    }
    closeModal()
    await load()
  } catch (e: any) {
    msg.value = e.message
  } finally {
    saving.value = false
  }
}

async function toggleEnabled(p: Proxy) {
  try {
    await adminApi.updateProxy(p.id, { ...p, enabled: !p.enabled })
    await load()
  } catch (e: any) {
    msg.value = e.message
  }
}

async function doDelete() {
  if (!confirmDelete.value) return
  saving.value = true
  try {
    await adminApi.deleteProxy(confirmDelete.value.id)
    msg.value = '已删除'
    confirmDelete.value = null
    await load()
  } catch (e: any) {
    msg.value = e.message
  } finally {
    saving.value = false
  }
}

function formatTs(v?: string) {
  if (!v) return '—'
  try {
    const d = new Date(v)
    return Number.isNaN(d.getTime()) ? v : d.toLocaleString()
  } catch {
    return v
  }
}

onMounted(load)
</script>

<template>
  <div>
    <PageHeader title="代理" subtitle="上游请求出口代理（HTTP / SOCKS5）">
      <template #actions>
        <button class="btn-ghost" type="button" :disabled="loading" @click="load">刷新</button>
        <button class="btn-primary" type="button" @click="openCreate">新建代理</button>
      </template>
    </PageHeader>

    <p v-if="err" class="mb-3 text-sm text-red-600">{{ err }}</p>
    <p v-if="msg" class="mb-3 text-sm text-teal">{{ msg }}</p>

    <EmptyState
      v-if="!loading && !err && rows.length === 0"
      title="暂无代理"
      description="尚未配置代理。账号可绑定代理以走指定出口。"
    >
      <template #actions>
        <button class="btn-primary" type="button" @click="openCreate">新建代理</button>
      </template>
    </EmptyState>

    <div v-else-if="rows.length" class="table-wrap">
      <table class="data-table">
        <thead>
          <tr>
            <th>名称</th>
            <th>类型</th>
            <th>URL</th>
            <th>状态</th>
            <th>创建时间</th>
            <th></th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="p in rows" :key="p.id">
            <td>
              <div class="font-medium">{{ p.name }}</div>
              <div class="font-mono text-xs text-slatex">{{ p.id }}</div>
            </td>
            <td><span class="pill-info">{{ p.type }}</span></td>
            <td class="max-w-[16rem] truncate font-mono text-xs text-slatex">{{ p.url }}</td>
            <td>
              <StatusPill
                :status="p.enabled ? 'enabled' : 'disabled'"
                :label="p.enabled ? '启用' : '禁用'"
              />
            </td>
            <td class="whitespace-nowrap text-xs text-slatex">{{ formatTs(p.created_at) }}</td>
            <td class="text-right">
              <div class="flex justify-end gap-1.5">
                <button class="btn-ghost" type="button" @click="toggleEnabled(p)">
                  {{ p.enabled ? '禁用' : '启用' }}
                </button>
                <button class="btn-ghost" type="button" @click="openEdit(p)">编辑</button>
                <button class="btn-danger" type="button" @click="confirmDelete = p">删除</button>
              </div>
            </td>
          </tr>
        </tbody>
      </table>
    </div>

    <div v-if="showModal" class="modal-backdrop" @click.self="closeModal">
      <div class="modal-panel">
        <div class="flex items-center gap-2">
          <h3 class="font-semibold">{{ editing ? '编辑代理' : '新建代理' }}</h3>
          <div class="flex-1" />
          <button class="btn-ghost" type="button" @click="closeModal">关闭</button>
        </div>
        <div>
          <label class="label">名称</label>
          <input v-model="form.name" class="input" placeholder="办公出口" />
        </div>
        <div>
          <label class="label">类型</label>
          <select v-model="form.type" class="input">
            <option v-for="t in PROXY_TYPES" :key="t" :value="t">{{ t }}</option>
          </select>
        </div>
        <div>
          <label class="label">URL</label>
          <input
            v-model="form.url"
            class="input font-mono"
            placeholder="http://user:pass@host:port"
          />
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
          确定删除代理
          <span class="font-medium text-ink">{{ confirmDelete.name }}</span>
          ？绑定此代理的账号将失去出口配置。
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
