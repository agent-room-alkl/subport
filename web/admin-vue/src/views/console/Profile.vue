<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { consoleApi } from '../../api/client'
import { useAuthStore } from '../../stores/auth'
import PageHeader from '../../components/PageHeader.vue'

const auth = useAuthStore()
const me = ref<any>(null)
const quota = ref<any>(null)
const err = ref('')
const loading = ref(false)

async function load() {
  loading.value = true
  err.value = ''
  try {
    const [m, q] = await Promise.all([consoleApi.me(), consoleApi.quota()])
    me.value = m
    quota.value = q
    if (m) {
      try {
        localStorage.setItem('subport_user', JSON.stringify(m))
      } catch { /* ignore */ }
      auth.user = m
    }
  } catch (e: any) {
    err.value = e?.message || '加载失败'
  } finally {
    loading.value = false
  }
}

onMounted(load)
</script>

<template>
  <div>
    <PageHeader title="我的" subtitle="账号信息与额度明细">
      <template #actions>
        <button class="btn-ghost" type="button" :disabled="loading" @click="load">刷新</button>
      </template>
    </PageHeader>

    <p v-if="err" class="mb-3 text-sm text-red-600">{{ err }}</p>

    <div class="grid gap-4 lg:grid-cols-2">
      <div class="card space-y-3">
        <div class="font-semibold">账号</div>
        <dl class="space-y-2 text-sm">
          <div class="flex justify-between gap-4 border-b border-line py-1.5">
            <dt class="text-slatex">用户名</dt>
            <dd class="font-medium">{{ me?.username || auth.user?.username || '—' }}</dd>
          </div>
          <div class="flex justify-between gap-4 border-b border-line py-1.5">
            <dt class="text-slatex">角色</dt>
            <dd class="font-medium">{{ me?.role || auth.user?.role || '—' }}</dd>
          </div>
          <div class="flex justify-between gap-4 border-b border-line py-1.5">
            <dt class="text-slatex">用户 ID</dt>
            <dd class="break-all font-mono text-xs">{{ me?.id || auth.user?.id || '—' }}</dd>
          </div>
          <div class="flex justify-between gap-4 py-1.5">
            <dt class="text-slatex">注册时间</dt>
            <dd class="text-xs text-slatex">{{ me?.created_at || '—' }}</dd>
          </div>
        </dl>
      </div>

      <div class="card space-y-3">
        <div class="font-semibold">额度</div>
        <div v-if="quota || me" class="grid grid-cols-2 gap-3">
          <div class="rounded-lg bg-canvas px-3 py-3 text-center">
            <div class="label">剩余</div>
            <div class="text-xl font-semibold tabular-nums text-teal">
              {{ quota?.remaining ?? me?.remaining ?? '—' }}
            </div>
          </div>
          <div class="rounded-lg bg-canvas px-3 py-3 text-center">
            <div class="label">总额</div>
            <div class="text-xl font-semibold tabular-nums">
              {{ quota?.quota_total ?? me?.quota_total ?? '—' }}
            </div>
          </div>
          <div class="rounded-lg bg-canvas px-3 py-3 text-center">
            <div class="label">已用</div>
            <div class="text-xl font-semibold tabular-nums">
              {{ quota?.quota_used ?? me?.quota_used ?? '—' }}
            </div>
          </div>
          <div class="rounded-lg bg-canvas px-3 py-3 text-center">
            <div class="label">预留</div>
            <div class="text-xl font-semibold tabular-nums">
              {{ me?.quota_reserved ?? '—' }}
            </div>
          </div>
        </div>
        <p v-else class="text-sm text-slatex">{{ loading ? '加载中…' : '暂无数据' }}</p>
      </div>
    </div>
  </div>
</template>
