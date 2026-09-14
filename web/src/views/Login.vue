<script setup lang="ts">
import { computed, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useAuthStore } from '../stores/auth'
import logo from '../assets/logo.svg'

const auth = useAuthStore()
const router = useRouter()
const route = useRoute()

const isConsole = computed(() => {
  const from = String(route.query.from || '')
  const redirect = String(route.query.redirect || '')
  return from === 'console' || redirect.startsWith('/console')
})

const username = ref(isConsole.value ? '' : 'admin')
const password = ref('')
const err = ref('')
const loading = ref(false)

async function submit() {
  loading.value = true
  err.value = ''
  try {
    await auth.login(username.value, password.value)
    const role = auth.user?.role
    const qRedirect = route.query.redirect as string | undefined
    let redirect = qRedirect
    if (!redirect) {
      redirect = role === 'admin' ? '/overview' : '/console'
    }
    // Non-admin must not land on admin shell after console login intent
    if (role !== 'admin' && redirect && !redirect.startsWith('/console') && redirect !== '/login') {
      redirect = '/console'
    }
    if (role === 'admin' && isConsole.value && (!qRedirect || qRedirect.startsWith('/console'))) {
      // admin logging in from console entry can still go console if they want,
      // but default admin entry stays overview when not from console
      redirect = qRedirect || '/console'
    }
    router.replace(redirect)
  } catch (e: any) {
    err.value = e?.message || '登录失败'
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <div class="relative flex min-h-screen items-center justify-center overflow-hidden bg-canvas px-4 py-6 pb-[max(1.5rem,env(safe-area-inset-bottom))]">
    <div
      class="pointer-events-none absolute inset-0"
      style="background: radial-gradient(ellipse 55% 40% at 50% 32%, rgba(46,230,166,0.18), transparent 70%)"
    />
    <form class="card relative w-full max-w-sm space-y-5 border-teal-bright/30 p-6 shadow-glow" @submit.prevent="submit">
      <div class="flex flex-col items-center gap-3 text-center">
        <img :src="logo" class="h-14 w-14 rounded-2xl shadow-glow-sm" alt="SUBPORT" />
        <div>
          <h1 class="text-xl font-semibold tracking-wordmark text-ink">SUBPORT</h1>
          <p class="mt-1 text-sm text-slatex">
            {{ isConsole ? '用户控制台 · 登录' : '管理端 · 登录' }}
          </p>
        </div>
      </div>
      <div>
        <label class="label">用户名</label>
        <input v-model="username" class="input" autocomplete="username" :placeholder="isConsole ? '你的账号' : 'admin'" />
      </div>
      <div>
        <label class="label">密码</label>
        <input v-model="password" class="input" type="password" autocomplete="current-password" />
      </div>
      <p v-if="err" class="text-sm text-red-600">{{ err }}</p>
      <button class="btn-primary min-h-10 w-full" :disabled="loading" type="submit">
        {{ loading ? '登录中…' : '登录' }}
      </button>
      <p class="text-center text-[11px] text-slatex">
        <template v-if="isConsole">
          普通用户登录后进入控制台 · 管理请用管理端入口
        </template>
        <template v-else>
          管理上游订阅账号与下游 API 密钥
        </template>
      </p>
      <p class="text-center text-[11px]">
        <RouterLink
          v-if="isConsole"
          class="text-teal hover:underline"
          :to="{ name: 'login', query: { from: 'admin', redirect: '/overview' } }"
        >前往管理端登录</RouterLink>
        <RouterLink
          v-else
          class="text-teal hover:underline"
          :to="{ name: 'login', query: { from: 'console', redirect: '/console' } }"
        >前往用户控制台登录</RouterLink>
      </p>
    </form>
  </div>
</template>
