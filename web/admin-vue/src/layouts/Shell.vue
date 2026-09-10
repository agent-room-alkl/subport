<script setup lang="ts">
import { computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useAuthStore } from '../stores/auth'
import logo from '../assets/logo.svg'

const route = useRoute()
const router = useRouter()
const auth = useAuthStore()

const nav = [
  { to: '/overview', label: '总览' },
  { to: '/accounts', label: '账号池' },
  { to: '/channels', label: '渠道' },
  { to: '/routes', label: '路由' },
  { to: '/proxies', label: '代理' },
  { to: '/keys', label: '密钥' },
  { to: '/usage', label: '用量' },
  { to: '/recharge', label: '充值' },
]

const title = computed(() => nav.find((n) => n.to === route.path)?.label || 'SUBPORT')

function logout() {
  auth.logout()
  router.push({ name: 'login' })
}

function isActive(to: string) {
  return route.path === to || route.path.startsWith(to + '/')
}
</script>

<template>
  <div class="flex min-h-screen bg-canvas text-ink">
    <aside class="flex w-60 shrink-0 flex-col border-r border-white/10 bg-navy px-3 py-4 text-white">
      <div class="mb-6 flex items-center gap-3 px-2">
        <img :src="logo" alt="SUBPORT" class="h-9 w-9 rounded-lg shadow-glow-sm" />
        <div class="min-w-0">
          <div class="text-sm font-semibold tracking-wordmark text-white">SUBPORT</div>
          <div class="text-[11px] text-slate-400">订阅 → API</div>
        </div>
      </div>

      <nav class="flex flex-1 flex-col gap-0.5">
        <RouterLink
          v-for="item in nav"
          :key="item.to"
          :to="item.to"
          class="nav-link"
          :class="{ 'nav-link-active': isActive(item.to) }"
        >
          {{ item.label }}
        </RouterLink>
      </nav>

      <div class="mt-4 space-y-3 border-t border-white/10 pt-4">
        <p class="px-2 text-[11px] leading-relaxed text-slate-400">
          订阅节点汇入 API 端口
        </p>
        <button class="sidebar-action w-full" type="button" @click="logout">退出</button>
      </div>
    </aside>

    <main class="flex min-w-0 flex-1 flex-col">
      <header class="flex items-center gap-3 border-b border-white/10 bg-navy px-6 py-3.5 text-white">
        <div class="min-w-0">
          <h1 class="text-lg font-semibold leading-tight text-white">{{ title }}</h1>
          <p class="truncate text-xs text-slate-300">订阅号池网关 · 上游 OAuth 与下游密钥分开管理</p>
        </div>
        <div class="flex-1" />
        <RouterLink class="header-action" :to="route.fullPath">刷新</RouterLink>
      </header>
      <div class="flex-1 overflow-auto bg-canvas p-6">
        <RouterView />
      </div>
      <footer class="border-t border-line bg-white px-6 py-2 text-center text-[11px] text-slatex">
        SUBPORT · 订阅 → API
      </footer>
    </main>
  </div>
</template>
