<script setup lang="ts">
import { ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useAuthStore } from '../stores/auth'
import logo from '../assets/logo.svg'

const auth = useAuthStore()
const router = useRouter()
const route = useRoute()
const username = ref('admin')
const password = ref('')
const err = ref('')
const loading = ref(false)

async function submit() {
  loading.value = true
  err.value = ''
  try {
    await auth.login(username.value, password.value)
    const role = auth.user?.role
    const redirect = (route.query.redirect as string) || (role === 'admin' ? '/overview' : '/console')
    router.replace(redirect)
  } catch (e: any) {
    err.value = e?.message || '登录失败'
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <div class="relative flex min-h-screen items-center justify-center overflow-hidden bg-canvas px-4">
    <div
      class="pointer-events-none absolute inset-0"
      style="background: radial-gradient(ellipse 55% 40% at 50% 32%, rgba(46,230,166,0.18), transparent 70%)"
    />
    <form class="card relative w-full max-w-sm space-y-5 border-teal-bright/30 p-6 shadow-glow" @submit.prevent="submit">
      <div class="flex flex-col items-center gap-3 text-center">
        <img :src="logo" class="h-14 w-14 rounded-2xl shadow-glow-sm" alt="SUBPORT" />
        <div>
          <h1 class="text-xl font-semibold tracking-wordmark text-ink">SUBPORT</h1>
          <p class="mt-1 text-sm text-slatex">Admin · 订阅 → API</p>
        </div>
      </div>
      <div>
        <label class="label">用户名</label>
        <input v-model="username" class="input" autocomplete="username" />
      </div>
      <div>
        <label class="label">密码</label>
        <input v-model="password" class="input" type="password" autocomplete="current-password" />
      </div>
      <p v-if="err" class="text-sm text-red-600">{{ err }}</p>
      <button class="btn-primary w-full" :disabled="loading" type="submit">
        {{ loading ? '登录中…' : '登录' }}
      </button>
      <p class="text-center text-[11px] text-slatex">管理上游订阅凭证与下游 API 密钥</p>
    </form>
  </div>
</template>
