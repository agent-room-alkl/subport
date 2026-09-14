<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { adminApi, asList } from '../api/client'
import PageHeader from '../components/PageHeader.vue'
import StatusPill from '../components/StatusPill.vue'
import EmptyState from '../components/EmptyState.vue'

type PubUser = {
  id: string
  username: string
  role?: string
  quota_total?: number
  quota_used?: number
  quota_reserved?: number
  remaining?: number
  created_at?: string
}

const rows = ref<PubUser[]>([])
const q = ref('')
const err = ref('')
const ok = ref('')
const loading = ref(false)
const creating = ref(false)

const form = ref({
  username: '',
  password: '',
  role: 'user',
  quota_total: 1_000_000,
})

const editId = ref('')
const editRole = ref('user')
const editQuota = ref(0)

const filtered = computed(() => {
  const s = q.value.trim().toLowerCase()
  if (!s) return rows.value
  return rows.value.filter(
    (u) => u.username?.toLowerCase().includes(s) || u.id?.toLowerCase().includes(s),
  )
})

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
    rows.value = asList<PubUser>(await adminApi.users())
  } catch (e: any) {
    err.value = e?.message || '加载失败'
    rows.value = []
  } finally {
    loading.value = false
  }
}

async function createUser() {
  err.value = ''
  ok.value = ''
  if (!form.value.username.trim() || !form.value.password) {
    err.value = '请填写用户名和密码'
    return
  }
  creating.value = true
  try {
    await adminApi.createUser({
      username: form.value.username.trim(),
      password: form.value.password,
      role: form.value.role,
      quota_total: Number(form.value.quota_total) || 0,
    })
    ok.value = '用户已创建'
    form.value = { username: '', password: '', role: 'user', quota_total: 1_000_000 }
    await load()
  } catch (e: any) {
    err.value = e?.message || '创建失败'
  } finally {
    creating.value = false
  }
}

function startEdit(u: PubUser) {
  editId.value = u.id
  editRole.value = u.role === 'admin' ? 'admin' : 'user'
  editQuota.value = Number(u.quota_total) || 0
}

function cancelEdit() {
  editId.value = ''
}

async function saveEdit(u: PubUser) {
  err.value = ''
  ok.value = ''
  try {
    await adminApi.patchUser(u.id, {
      role: editRole.value,
      quota_total: Number(editQuota.value),
    })
    ok.value = '已更新角色与额度'
    editId.value = ''
    await load()
  } catch (e: any) {
    err.value = e?.message || '更新失败'
  }
}

onMounted(load)
</script>

<template>
  <div>
    <PageHeader title="用户管理" subtitle="创建用户、调整角色与额度配额。">
      <template #actions>
        <button class="btn-ghost" type="button" :disabled="loading" @click="load">刷新</button>
      </template>
    </PageHeader>

    <p v-if="err" class="mb-3 text-sm text-red-600">{{ err }}</p>
    <p v-if="ok" class="mb-3 text-sm text-teal">{{ ok }}</p>

    <div class="card mb-4 space-y-3">
      <div class="font-semibold">创建用户</div>
      <div class="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
        <div>
          <label class="label">用户名</label>
          <input v-model="form.username" class="input" placeholder="username" @keyup.enter="createUser" />
        </div>
        <div>
          <label class="label">密码</label>
          <input v-model="form.password" class="input" type="password" placeholder="password" @keyup.enter="createUser" />
        </div>
        <div>
          <label class="label">角色</label>
          <select v-model="form.role" class="input">
            <option value="user">user</option>
            <option value="admin">admin</option>
          </select>
        </div>
        <div>
          <label class="label">额度配额</label>
          <input v-model.number="form.quota_total" class="input" type="number" min="0" step="1000" />
        </div>
      </div>
      <button class="btn-primary" type="button" :disabled="creating" @click="createUser">
        {{ creating ? '创建中…' : '创建' }}
      </button>
    </div>

    <div class="mb-3">
      <input v-model="q" class="input max-w-md" placeholder="搜索用户名或 ID" />
    </div>

    <EmptyState
      v-if="!loading && filtered.length === 0"
      title="暂无用户"
      description="创建后可在此调整角色与额度。"
    />

    <div v-else-if="filtered.length" class="table-wrap">
      <table class="data-table">
        <thead>
          <tr>
            <th>用户名</th>
            <th>角色</th>
            <th>额度</th>
            <th>已用 / 预留</th>
            <th>剩余</th>
            <th>创建时间</th>
            <th>操作</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="u in filtered" :key="u.id">
            <td>
              <div class="font-medium">{{ u.username }}</div>
              <div class="font-mono text-[11px] text-slatex">{{ u.id }}</div>
            </td>
            <td>
              <template v-if="editId === u.id">
                <select v-model="editRole" class="input">
                  <option value="user">user</option>
                  <option value="admin">admin</option>
                </select>
              </template>
              <StatusPill
                v-else
                :status="u.role === 'admin' ? 'enabled' : 'idle'"
                :label="u.role || 'user'"
              />
            </td>
            <td>
              <input
                v-if="editId === u.id"
                v-model.number="editQuota"
                class="input w-32"
                type="number"
                min="0"
                step="1000"
              />
              <span v-else class="tabular-nums">{{ u.quota_total ?? 0 }}</span>
            </td>
            <td class="tabular-nums text-xs text-slatex">
              {{ u.quota_used ?? 0 }} / {{ u.quota_reserved ?? 0 }}
            </td>
            <td class="tabular-nums font-medium">{{ u.remaining ?? ((u.quota_total ?? 0) - (u.quota_used ?? 0)) }}</td>
            <td class="whitespace-nowrap text-xs text-slatex">{{ formatTs(u.created_at) }}</td>
            <td>
              <div class="flex flex-wrap gap-2">
                <template v-if="editId === u.id">
                  <button class="btn-primary" type="button" @click="saveEdit(u)">保存</button>
                  <button class="btn-ghost" type="button" @click="cancelEdit">取消</button>
                </template>
                <button v-else class="btn-ghost" type="button" @click="startEdit(u)">编辑</button>
              </div>
            </td>
          </tr>
        </tbody>
      </table>
    </div>

    <div v-else-if="loading" class="card text-slatex">加载中…</div>
  </div>
</template>
