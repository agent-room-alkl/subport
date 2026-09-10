<script setup lang="ts">
import { onMounted, ref } from 'vue'
import {
  adminApi,
  asList,
  type Channel,
  type ChannelAccount,
} from '../api/client'
import PageHeader from '../components/PageHeader.vue'
import StatusPill from '../components/StatusPill.vue'
import EmptyState from '../components/EmptyState.vue'

type AccountOpt = { id: string; label?: string; provider?: string }

const rows = ref<Channel[]>([])
const accounts = ref<AccountOpt[]>([])
const err = ref('')
const msg = ref('')
const loading = ref(false)
const saving = ref(false)

const showModal = ref(false)
const editing = ref<Channel | null>(null)
const form = ref({
  name: '',
  provider: '',
  group_name: '',
  priority: 0,
  enabled: true,
  models_json: '',
})
const confirmDelete = ref<Channel | null>(null)

const expandedId = ref<string | null>(null)
const mappings = ref<ChannelAccount[]>([])
const mapLoading = ref(false)
const mapForm = ref({
  account_id: '',
  model_pattern: '*',
  priority: 0,
})

async function load() {
  loading.value = true
  err.value = ''
  try {
    const [ch, acc] = await Promise.all([
      adminApi.channels(),
      adminApi.accounts().catch(() => []),
    ])
    rows.value = asList<Channel>(ch)
    accounts.value = asList<AccountOpt>(acc)
  } catch (e: any) {
    err.value = e.message || '加载失败'
    rows.value = []
  } finally {
    loading.value = false
  }
}

function openCreate() {
  editing.value = null
  form.value = {
    name: '',
    provider: '',
    group_name: '',
    priority: 0,
    enabled: true,
    models_json: '',
  }
  showModal.value = true
}

function openEdit(c: Channel) {
  editing.value = c
  form.value = {
    name: c.name || '',
    provider: c.provider || '',
    group_name: c.group_name || '',
    priority: Number(c.priority) || 0,
    enabled: c.enabled !== false,
    models_json: c.models_json || '',
  }
  showModal.value = true
}

function closeModal() {
  showModal.value = false
  editing.value = null
}

async function save() {
  if (!form.value.name.trim() || !form.value.provider.trim()) {
    msg.value = '请填写名称与 provider'
    return
  }
  saving.value = true
  msg.value = ''
  try {
    const body: Partial<Channel> = {
      name: form.value.name.trim(),
      provider: form.value.provider.trim(),
      group_name: form.value.group_name.trim() || undefined,
      priority: Number(form.value.priority) || 0,
      enabled: !!form.value.enabled,
      models_json: form.value.models_json.trim() || undefined,
    }
    if (editing.value) {
      await adminApi.updateChannel(editing.value.id, body)
      msg.value = '已更新渠道'
    } else {
      await adminApi.createChannel(body)
      msg.value = '已创建渠道'
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
    await adminApi.deleteChannel(confirmDelete.value.id)
    msg.value = '已删除'
    if (expandedId.value === confirmDelete.value.id) {
      expandedId.value = null
      mappings.value = []
    }
    confirmDelete.value = null
    await load()
  } catch (e: any) {
    msg.value = e.message
  } finally {
    saving.value = false
  }
}

async function toggleExpand(c: Channel) {
  if (expandedId.value === c.id) {
    expandedId.value = null
    mappings.value = []
    return
  }
  expandedId.value = c.id
  mapForm.value = { account_id: '', model_pattern: '*', priority: 0 }
  await loadMappings(c.id)
}

async function loadMappings(channelId: string) {
  mapLoading.value = true
  try {
    mappings.value = asList<ChannelAccount>(await adminApi.channelAccounts(channelId))
  } catch (e: any) {
    msg.value = e.message
    mappings.value = []
  } finally {
    mapLoading.value = false
  }
}

async function addMapping() {
  if (!expandedId.value || !mapForm.value.account_id) {
    msg.value = '请选择账号'
    return
  }
  saving.value = true
  try {
    await adminApi.addChannelAccount(expandedId.value, {
      account_id: mapForm.value.account_id,
      model_pattern: mapForm.value.model_pattern.trim() || '*',
      priority: Number(mapForm.value.priority) || 0,
    })
    msg.value = '已添加账号映射'
    mapForm.value = { account_id: '', model_pattern: '*', priority: 0 }
    await loadMappings(expandedId.value)
  } catch (e: any) {
    msg.value = e.message
  } finally {
    saving.value = false
  }
}

async function removeMapping(m: ChannelAccount) {
  if (!expandedId.value) return
  saving.value = true
  try {
    await adminApi.removeChannelAccount(expandedId.value, m.account_id)
    msg.value = '已移除映射'
    await loadMappings(expandedId.value)
  } catch (e: any) {
    msg.value = e.message
  } finally {
    saving.value = false
  }
}

function accountLabel(id: string) {
  const a = accounts.value.find((x) => x.id === id)
  return a ? `${a.label || a.id} (${a.provider || ''})` : id
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
    <PageHeader title="渠道" subtitle="按 provider / 分组组织上游账号池">
      <template #actions>
        <button class="btn-ghost" type="button" :disabled="loading" @click="load">刷新</button>
        <button class="btn-primary" type="button" @click="openCreate">新建渠道</button>
      </template>
    </PageHeader>

    <p v-if="err" class="mb-3 text-sm text-red-600">{{ err }}</p>
    <p v-if="msg" class="mb-3 text-sm text-teal">{{ msg }}</p>

    <EmptyState
      v-if="!loading && !err && rows.length === 0"
      title="暂无渠道"
      description="尚未配置渠道。创建渠道后可将账号绑定到渠道。"
    >
      <template #actions>
        <button class="btn-primary" type="button" @click="openCreate">新建渠道</button>
      </template>
    </EmptyState>

    <div v-else-if="rows.length" class="space-y-3">
      <div class="table-wrap">
        <table class="data-table">
          <thead>
            <tr>
              <th>名称</th>
              <th>Provider</th>
              <th>分组</th>
              <th>优先级</th>
              <th>模型</th>
              <th>状态</th>
              <th>创建时间</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
            <template v-for="c in rows" :key="c.id">
              <tr>
                <td>
                  <div class="font-medium">{{ c.name }}</div>
                  <div class="font-mono text-xs text-slatex">{{ c.id }}</div>
                </td>
                <td>{{ c.provider }}</td>
                <td class="text-slatex">{{ c.group_name || '—' }}</td>
                <td class="tabular-nums">{{ c.priority }}</td>
                <td class="max-w-[10rem] truncate font-mono text-xs text-slatex">
                  {{ c.models_json || '—' }}
                </td>
                <td>
                  <StatusPill
                    :status="c.enabled ? 'enabled' : 'disabled'"
                    :label="c.enabled ? '启用' : '禁用'"
                  />
                </td>
                <td class="whitespace-nowrap text-xs text-slatex">{{ formatTs(c.created_at) }}</td>
                <td class="text-right">
                  <div class="flex justify-end gap-1.5">
                    <button class="btn-ghost" type="button" @click="toggleExpand(c)">
                      {{ expandedId === c.id ? '收起映射' : '账号映射' }}
                    </button>
                    <button class="btn-ghost" type="button" @click="openEdit(c)">编辑</button>
                    <button class="btn-danger" type="button" @click="confirmDelete = c">删除</button>
                  </div>
                </td>
              </tr>
              <tr v-if="expandedId === c.id">
                <td colspan="8" class="bg-slate-50 px-3 py-4">
                  <div class="space-y-3">
                    <div class="text-xs font-medium uppercase tracking-wide text-slatex">
                      渠道 ↔ 账号映射
                    </div>
                    <p v-if="mapLoading" class="text-sm text-slatex">加载中…</p>
                    <div v-else-if="mappings.length === 0" class="text-sm text-slatex">
                      暂无映射账号
                    </div>
                    <div v-else class="table-wrap">
                      <table class="data-table">
                        <thead>
                          <tr>
                            <th>账号</th>
                            <th>模型 Pattern</th>
                            <th>优先级</th>
                            <th></th>
                          </tr>
                        </thead>
                        <tbody>
                          <tr v-for="m in mappings" :key="`${m.channel_id}-${m.account_id}`">
                            <td>
                              <div>{{ accountLabel(m.account_id) }}</div>
                              <div class="font-mono text-xs text-slatex">{{ m.account_id }}</div>
                            </td>
                            <td class="font-mono text-sm">{{ m.model_pattern || '*' }}</td>
                            <td class="tabular-nums">{{ m.priority ?? 0 }}</td>
                            <td class="text-right">
                              <button
                                class="btn-danger"
                                type="button"
                                :disabled="saving"
                                @click="removeMapping(m)"
                              >
                                移除
                              </button>
                            </td>
                          </tr>
                        </tbody>
                      </table>
                    </div>

                    <div class="card flex flex-wrap items-end gap-3 p-3">
                      <div class="min-w-[12rem] flex-1">
                        <label class="label">账号</label>
                        <select v-model="mapForm.account_id" class="input">
                          <option value="">选择账号…</option>
                          <option v-for="a in accounts" :key="a.id" :value="a.id">
                            {{ a.label || a.id }} ({{ a.provider || '—' }})
                          </option>
                        </select>
                      </div>
                      <div class="w-40">
                        <label class="label">模型 Pattern</label>
                        <input v-model="mapForm.model_pattern" class="input font-mono" placeholder="*" />
                      </div>
                      <div class="w-28">
                        <label class="label">优先级</label>
                        <input v-model.number="mapForm.priority" class="input" type="number" />
                      </div>
                      <button
                        class="btn-primary"
                        type="button"
                        :disabled="saving"
                        @click="addMapping"
                      >
                        添加映射
                      </button>
                    </div>
                  </div>
                </td>
              </tr>
            </template>
          </tbody>
        </table>
      </div>
    </div>

    <div v-if="showModal" class="modal-backdrop" @click.self="closeModal">
      <div class="modal-panel">
        <div class="flex items-center gap-2">
          <h3 class="font-semibold">{{ editing ? '编辑渠道' : '新建渠道' }}</h3>
          <div class="flex-1" />
          <button class="btn-ghost" type="button" @click="closeModal">关闭</button>
        </div>
        <div>
          <label class="label">名称</label>
          <input v-model="form.name" class="input" placeholder="Claude 主池" />
        </div>
        <div>
          <label class="label">Provider</label>
          <input v-model="form.provider" class="input" placeholder="claude / codex" />
        </div>
        <div>
          <label class="label">分组</label>
          <input v-model="form.group_name" class="input" placeholder="可选" />
        </div>
        <div>
          <label class="label">优先级</label>
          <input v-model.number="form.priority" class="input" type="number" />
        </div>
        <div>
          <label class="label">模型 JSON</label>
          <textarea
            v-model="form.models_json"
            class="input min-h-[60px] font-mono"
            placeholder='["claude-sonnet-4"] 或留空'
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
          确定删除渠道
          <span class="font-medium text-ink">{{ confirmDelete.name }}</span>
          ？关联的账号映射也将失效。
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
