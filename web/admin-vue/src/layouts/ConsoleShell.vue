<script setup lang="ts">
import { computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useAuthStore } from '../stores/auth'
import logo from '../assets/logo.svg'

const route = useRoute()
const router = useRouter()
const auth = useAuthStore()

const nav = [
  { to: '/console', label: '看板', exact: true },
  { to: '/console/recharge', label: '充值' },
  { to: '/console/keys', label: '密钥' },
  { to: '/console/usage', label: '用量' },
  { to: '/console/profile', label: '我的' },
]

const title = computed(() => {
  const hit = nav.find((n) =>
    n.exact ? route.path === n.to : route.path === n.to || route.path.startsWith(n.to + '/'),
  )
  return hit?.label || '用户控制台'
})

function logout() {
  auth.logout()
  router.push({ name: 'login' })
}

function isActive(item: { to: string; exact?: boolean }) {
  if (item.exact) return route.path === item.to
  return route.path === item.to || route.path.startsWith(item.to + '/')
}

function refresh() {
  router.replace({ path: route.fullPath, query: { ...route.query, _r: String(Date.now()) } })
}
</script>

<template>
  <div class="flex min-h-screen bg-canvas text-ink">
    <aside class="flex w-60 shrink-0 flex-col border-r border-white/10 bg-navy px-3 py-4 text-white">
      <div class="mb-6 flex items-center gap-3 px-2">
        <img :src="logo" alt="SUBPORT" class="h-9 w-9 rounded-lg shadow-glow-sm" />
        <div class="min-w-0">
          <div class="text-sm font-semibold tracking-wordmark text-white">SUBPORT</div>
          <div class="text-[11px] text-slate-400">用户控制台</div>
        </div>
      </div>

      <nav class="flex flex-1 flex-col gap-0.5">
        <RouterLink
          v-for="item in nav"
          :key="item.to"
          :to="item.to"
          class="nav-link"
          :class="{ 'nav-link-active': isActive(item) }"
        >
          {{ item.label }}
        </RouterLink>
      </nav>

      <div class="mt-4 space-y-3 border-t border-white/10 pt-4">
        <p class="px-2 text-[11px] leading-relaxed text-slate-400">
          {{ auth.user?.username || 'user' }} · 额度与密钥自助管理
        </p>
        <button class="sidebar-action w-full" type="button" @click="logout">退出</button>
      </div>
    </aside>

    <main class="flex min-w-0 flex-1 flex-col">
      <header class="flex items-center gap-3 border-b border-white/10 bg-navy px-6 py-3.5 text-white">
        <div class="min-w-0">
          <h1 class="text-lg font-semibold leading-tight text-white">{{ title }}</h1>
          <p class="truncate text-xs text-slate-300">充值额度 · 管理 sk-sp- 密钥 · 查看用量</p>
        </div>
        <div class="flex-1" />
        <button class="header-action" type="button" @click="refresh">刷新</button>
        <button class="header-action" type="button" @click="logout">退出</button>
      </header>
      <div class="flex-1 overflow-auto bg-canvas p-6">
        <RouterView :key="String(route.query._r || route.fullPath)" />
      </div>
      <footer class="border-t border-line bg-white px-6 py-2 text-center text-[11px] text-slatex">
        SUBPORT · 用户控制台
      </footer>
    </main>
  </div>
</template>
