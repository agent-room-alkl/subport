<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { adminApi, asList } from '../api/client'
import PageHeader from '../components/PageHeader.vue'

type PubUser = {
  id: string
  username: string
  role?: string
  quota_total?: number
  quota_used?: number
  remaining?: number
}

const users = ref<PubUser[]>([])
const topups = ref<any[]>([])
const orders = ref<any[]>([])
const q = ref('')
const selectedId = ref('')
const credit = ref(100000)
const note = ref('')
const err = ref('')
const ok = ref('')
const loading = ref(false)

const filtered = computed(() => {
  const s = q.value.trim().toLowerCase()
  if (!s) return users.value
  return users.value.filter(
    (u) => u.username?.toLowerCase().includes(s) || u.id?.toLowerCase().includes(s),
  )
})

const selected = computed(() => users.value.find((u) => u.id === selectedId.value) || null)

async function load() {
  loading.value = true
  err.value = ''
  try {
    users.value = asList<PubUser>(await adminApi.users())
    topups.value = asList(await adminApi.topups(50))
    orders.value = asList(await adminApi.paymentOrders(50))
  } catch (e: any) {
    err.value = e?.message || 'load failed'
  } finally {
    loading.value = false
  }
}

async function submit() {
  ok.value = ''
  err.value = ''
  if (!selectedId.value) {
    err.value = 'select a user'
    return
  }
  if (!credit.value || credit.value <= 0) {
    err.value = 'credit must be positive'
    return
  }
  try {
    await adminApi.createTopup(selectedId.value, { credit: Number(credit.value), note: note.value })
    ok.value = 'top-up applied'
    note.value = ''
    await load()
  } catch (e: any) {
    err.value = e?.message || 'top-up failed'
  }
}

onMounted(load)
</script>

<template>
  <div>
    <PageHeader title="充值" subtitle="管理员手动加额 · 支付订单审计">
      <template #actions>
        <button class="btn-ghost" type="button" :disabled="loading" @click="load">刷新</button>
      </template>
    </PageHeader>

    <p v-if="err" class="mb-3 text-sm text-red-600">{{ err }}</p>
    <p v-if="ok" class="mb-3 text-sm text-teal">{{ ok }}</p>

    <div class="grid gap-4 lg:grid-cols-2">
      <div class="card space-y-3">
        <div class="font-semibold">手动加额</div>
        <div>
          <label class="label">搜索用户</label>
          <input v-model="q" class="input" placeholder="用户名或 ID" />
        </div>
        <div>
          <label class="label">选择用户</label>
          <select v-model="selectedId" class="input">
            <option value="">—</option>
            <option v-for="u in filtered" :key="u.id" :value="u.id">
              {{ u.username }} ({{ u.id }}) · 剩余 {{ u.remaining ?? (u.quota_total ?? 0) - (u.quota_used ?? 0) }}
            </option>
          </select>
        </div>
        <div v-if="selected" class="rounded-lg bg-canvas px-3 py-2 text-xs text-slatex">
          total {{ selected.quota_total }} · used {{ selected.quota_used }} · remaining
          {{ selected.remaining }}
        </div>
        <div>
          <label class="label">Credit</label>
          <input v-model.number="credit" class="input" type="number" min="1" />
        </div>
        <div>
          <label class="label">备注</label>
          <input v-model="note" class="input" placeholder="optional" />
        </div>
        <button class="btn-primary" type="button" @click="submit">提交加额</button>
      </div>

      <div class="card space-y-2">
        <div class="font-semibold">最近加额</div>
        <div class="max-h-80 overflow-auto text-sm">
          <table class="w-full text-left">
            <thead class="text-xs text-slatex">
              <tr>
                <th class="py-1">用户</th>
                <th>credit</th>
                <th>来源</th>
                <th>时间</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="t in topups" :key="t.id" class="border-t border-line">
                <td class="py-1 font-mono text-xs">{{ t.user_id }}</td>
                <td class="tabular-nums">{{ t.credit }}</td>
                <td>{{ t.source }}</td>
                <td class="text-xs text-slatex">{{ t.created_at }}</td>
              </tr>
            </tbody>
          </table>
          <p v-if="!topups.length" class="text-slatex">暂无记录</p>
        </div>
      </div>

      <div class="card space-y-2 lg:col-span-2">
        <div class="font-semibold">支付订单</div>
        <div class="max-h-96 overflow-auto text-sm">
          <table class="w-full text-left">
            <thead class="text-xs text-slatex">
              <tr>
                <th class="py-1">订单</th>
                <th>用户</th>
                <th>套餐</th>
                <th>credit</th>
                <th>状态</th>
                <th>时间</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="o in orders" :key="o.id" class="border-t border-line">
                <td class="py-1 font-mono text-xs">{{ o.id }}</td>
                <td class="font-mono text-xs">{{ o.user_id }}</td>
                <td>{{ o.package_id }}</td>
                <td class="tabular-nums">{{ o.quota_credit }}</td>
                <td>{{ o.status }}</td>
                <td class="text-xs text-slatex">{{ o.created_at }}</td>
              </tr>
            </tbody>
          </table>
          <p v-if="!orders.length" class="text-slatex">暂无订单</p>
        </div>
      </div>
    </div>
  </div>
</template>
