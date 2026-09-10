<script setup lang="ts">
import { onMounted, onUnmounted, ref } from 'vue'
import { adminApi, asList, type Proxy } from '../api/client'
import PageHeader from '../components/PageHeader.vue'
import StatusPill from '../components/StatusPill.vue'
import EmptyState from '../components/EmptyState.vue'

type Account = {
  id: string
  label: string
  provider: string
  tier: number
  state: string
  load?: number
  last_error?: string
  has_access_token?: boolean
  has_refresh_token?: boolean
  expires_at?: string
  proxy_id?: string | null
  consecutive_failures?: number
  consecutive_successes?: number
  cooldown_until?: string | null
}

const rows = ref<Account[]>([])
const proxies = ref<Proxy[]>([])
const err = ref('')
const msg = ref('')
const loading = ref(false)

const selected = ref<Account | null>(null)
const access = ref('')
const refresh = ref('')
const chatgptAccountId = ref('')
const projectId = ref('')
const clearAccess = ref(false)
const clearRefresh = ref(false)
const saving = ref(false)
const testingId = ref<string | null>(null)
const modalProxyId = ref('')

const oauthAccount = ref<Account | null>(null)
const oauthSessionId = ref('')
const oauthState = ref('')
const oauthCallbackUrl = ref('')
const oauthBusy = ref(false)
const oauthPolling = ref(false)
let oauthPollTimer: number | null = null
let oauthPopup: Window | null = null

function isOAuthProvider(p: string) {
  return p === 'claude' || p === 'codex' || p === 'antigravity'
}

function formatTs(v?: string | null) {
  if (!v) return '—'
  try {
    const d = new Date(v)
    if (Number.isNaN(d.getTime())) return v
    return d.toLocaleString()
  } catch {
    return v
  }
}

function credLabel(a: Account) {
  if (!isOAuthProvider(a.provider)) return '—'
  const parts: string[] = []
  if (a.has_access_token) parts.push('AT')
  if (a.has_refresh_token) parts.push('RT')
  return parts.length ? parts.join('+') : '未配置'
}

async function load() {
  loading.value = true
  err.value = ''
  try {
    const [acc, px] = await Promise.all([
      adminApi.accounts(),
      adminApi.proxies().catch(() => []),
    ])
    rows.value = asList<Account>(acc)
    proxies.value = asList<Proxy>(px)
  } catch (e: any) {
    err.value = e.message || '加载失败'
  } finally {
    loading.value = false
  }
}

async function openCreds(a: Account) {
  selected.value = a
  access.value = ''
  refresh.value = ''
  chatgptAccountId.value = ''
  projectId.value = ''
  clearAccess.value = false
  clearRefresh.value = false
  modalProxyId.value = a.proxy_id || ''
  msg.value = ''
  try {
    const st = await adminApi.getCredentials(a.id)
    msg.value = st.has_access_token
      ? `已保存凭证（更新于 ${st.updated_at || '未知'}）`
      : '尚未保存上游 OAuth；可粘贴 access/refresh，或对 Antigravity 使用 Google 授权。'
    if (st.extra?.chatgpt_account_id) chatgptAccountId.value = st.extra.chatgpt_account_id
    if (st.extra?.project_id) projectId.value = st.extra.project_id
    if (st.chatgpt_account_id) chatgptAccountId.value = st.chatgpt_account_id
    if (st.project_id) projectId.value = st.project_id
  } catch (e: any) {
    msg.value = e.message
  }
}

function closeModal() {
  selected.value = null
}

async function saveCreds() {
  if (!selected.value) return
  saving.value = true
  msg.value = ''
  try {
    const body: Record<string, string | boolean> = {}
    if (clearAccess.value) body.clear_access_token = true
    else if (access.value.trim()) body.access_token = access.value.trim()
    if (clearRefresh.value) body.clear_refresh_token = true
    else if (refresh.value.trim()) body.refresh_token = refresh.value.trim()
    if (chatgptAccountId.value.trim()) body.chatgpt_account_id = chatgptAccountId.value.trim()
    if (projectId.value.trim()) body.project_id = projectId.value.trim()
    if (Object.keys(body).length) {
      await adminApi.putCredentials(selected.value.id, body)
    }
    const nextProxy = modalProxyId.value || null
    if ((selected.value.proxy_id || null) !== nextProxy) {
      await adminApi.patchAccount(selected.value.id, { proxy_id: nextProxy })
    }
    access.value = ''
    refresh.value = ''
    clearAccess.value = false
    clearRefresh.value = false
    msg.value = '已保存'
    await load()
    const refreshed = rows.value.find((r) => r.id === selected.value?.id)
    if (refreshed) selected.value = refreshed
  } catch (e: any) {
    msg.value = e.message
  } finally {
    saving.value = false
  }
}

async function forceRefresh(a: Account) {
  msg.value = `正在强制刷新 ${a.id}…`
  try {
    const out = await adminApi.forceRefreshAccount(a.id)
    msg.value = out?.ok === false ? `刷新失败: ${out.error || 'unknown'}` : `已触发刷新 ${a.id}`
    await load()
  } catch (e: any) {
    msg.value = e.message
  }
}

async function testAccount(a: Account) {
  testingId.value = a.id
  msg.value = `正在测通 ${a.id}…`
  try {
    const out = await adminApi.testAccount(a.id)
    msg.value = out.ok
      ? `测通成功 ${out.latency_ms != null ? out.latency_ms + 'ms' : ''}`
      : `测通失败: ${out.error || 'unknown'}`
  } catch (e: any) {
    msg.value = e.message
  } finally {
    testingId.value = null
  }
}

async function toggle(a: Account) {
  try {
    await adminApi.setHealthy(a.id, a.state !== 'healthy')
    await load()
  } catch (e: any) {
    msg.value = e.message
  }
}

async function onProxyChange(a: Account, ev: Event) {
  const val = (ev.target as HTMLSelectElement).value || null
  try {
    await adminApi.patchAccount(a.id, { proxy_id: val })
    await load()
  } catch (e: any) {
    msg.value = e.message
  }
}

function stopOAuthPoll() {
  if (oauthPollTimer != null) {
    window.clearInterval(oauthPollTimer)
    oauthPollTimer = null
  }
  oauthPolling.value = false
}

function closeOAuthPanel() {
  stopOAuthPoll()
  oauthAccount.value = null
  oauthBusy.value = false
  oauthCallbackUrl.value = ''
}

function onOAuthMessage(ev: MessageEvent) {
  const data = ev.data
  if (!data || data.type !== 'subport-antigravity-oauth') return
  if (!oauthAccount.value || !oauthSessionId.value) return
  if (data.session_id && data.session_id !== oauthSessionId.value) return
  if (data.callback_url) oauthCallbackUrl.value = String(data.callback_url)
  if (data.ok) {
    void finishOAuthFromStatus()
  } else if (data.callback_url) {
    void submitOAuthCallback()
  }
}

async function finishOAuthFromStatus() {
  if (!oauthAccount.value || !oauthSessionId.value) return
  try {
    const st = await adminApi.antigravityOAuthStatus(oauthAccount.value.id, oauthSessionId.value)
    if (st.done && st.ok) {
      stopOAuthPoll()
      msg.value = `Google 授权成功${st.email ? ' · ' + st.email : ''}${st.project_id ? ' · project ' + st.project_id : ''}`
      oauthBusy.value = false
      oauthAccount.value = null
      await load()
      try {
        oauthPopup?.close()
      } catch {
        /* ignore */
      }
    } else if (st.done && st.error) {
      stopOAuthPoll()
      msg.value = `授权失败: ${st.error}`
      oauthBusy.value = false
    }
  } catch (e: any) {
    msg.value = e.message
  }
}

async function startGoogleOAuth(a: Account) {
  oauthBusy.value = true
  msg.value = '正在生成 Google 授权链接…'
  oauthCallbackUrl.value = ''
  stopOAuthPoll()
  try {
    const out = await adminApi.antigravityOAuthStart(a.id)
    oauthAccount.value = a
    oauthSessionId.value = out.session_id
    oauthState.value = out.state
    oauthPopup = window.open(out.auth_url, 'subport-antigravity-oauth', 'width=520,height=720')
    msg.value = '已打开 Google 登录。完成后会自动回调 localhost:8085；若未自动完成，请粘贴回调 URL。'
    oauthPolling.value = true
    oauthPollTimer = window.setInterval(() => {
      void finishOAuthFromStatus()
    }, 1500)
  } catch (e: any) {
    msg.value = e.message
    oauthBusy.value = false
  }
}

async function submitOAuthCallback() {
  if (!oauthAccount.value || !oauthSessionId.value) return
  const cb = oauthCallbackUrl.value.trim()
  if (!cb) {
    msg.value = '请粘贴 Google 回调后的完整 URL'
    return
  }
  oauthBusy.value = true
  try {
    const out = await adminApi.antigravityOAuthExchange(oauthAccount.value.id, {
      session_id: oauthSessionId.value,
      state: oauthState.value,
      callback_url: cb,
    })
    stopOAuthPoll()
    msg.value = `Google 授权成功${out.email ? ' · ' + out.email : ''}${out.project_id ? ' · project ' + out.project_id : ''}`
    oauthAccount.value = null
    oauthCallbackUrl.value = ''
    await load()
  } catch (e: any) {
    msg.value = e.message
  } finally {
    oauthBusy.value = false
  }
}

onMounted(() => {
  load()
  window.addEventListener('message', onOAuthMessage)
})
onUnmounted(() => {
  stopOAuthPoll()
  window.removeEventListener('message', onOAuthMessage)
})
</script>

<template>
  <div>
    <PageHeader
      title="账号池"
      subtitle="上游订阅 OAuth 在此维护；Antigravity 可用 Google 授权。与「下游密钥」无关。"
    >
      <template #actions>
        <button class="btn-ghost" type="button" :disabled="loading" @click="load">刷新</button>
      </template>
    </PageHeader>

    <p v-if="err" class="mb-3 text-sm text-red-600">{{ err }}</p>
    <p v-if="msg" class="mb-3 text-sm text-teal">{{ msg }}</p>

    <EmptyState
      v-if="!loading && !err && rows.length === 0"
      title="暂无账号"
      description="账号池为空。请在后台或配置中添加上游订阅账号。"
    />

    <div v-else class="table-wrap">
      <table class="data-table">
        <thead>
          <tr>
            <th>账号</th>
            <th>Provider</th>
            <th>档</th>
            <th>状态</th>
            <th>凭证</th>
            <th>过期</th>
            <th>代理</th>
            <th>最近错误</th>
            <th></th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="a in rows" :key="a.id">
            <td>
              <div class="font-medium">{{ a.label || a.id }}</div>
              <div class="font-mono text-xs text-slatex">{{ a.id }}</div>
              <div
                v-if="a.cooldown_until || a.consecutive_failures"
                class="mt-0.5 text-[11px] text-slatex"
              >
                <span v-if="a.consecutive_failures">连续失败 {{ a.consecutive_failures }}</span>
                <span v-if="a.cooldown_until"> · 冷却至 {{ formatTs(a.cooldown_until) }}</span>
              </div>
            </td>
            <td class="text-slatex">{{ a.provider }}</td>
            <td>{{ a.tier ?? '—' }}</td>
            <td>
              <StatusPill :status="a.state" />
            </td>
            <td class="text-xs">
              <span
                v-if="isOAuthProvider(a.provider)"
                :class="a.has_access_token ? 'text-teal' : 'text-slatex'"
              >
                {{ credLabel(a) }}
              </span>
              <span v-else class="text-slatex">—</span>
            </td>
            <td class="whitespace-nowrap text-xs text-slatex">{{ formatTs(a.expires_at) }}</td>
            <td>
              <select
                class="input max-w-[9rem] py-1 text-xs"
                :value="a.proxy_id || ''"
                @change="onProxyChange(a, $event)"
              >
                <option value="">无</option>
                <option v-for="p in proxies" :key="p.id" :value="p.id">
                  {{ p.name || p.id }}
                </option>
              </select>
            </td>
            <td class="max-w-[12rem] truncate text-xs text-red-600">{{ a.last_error || '—' }}</td>
            <td class="whitespace-nowrap text-right">
              <div class="flex flex-wrap justify-end gap-1.5">
                <button
                  v-if="a.provider === 'antigravity'"
                  class="btn-primary"
                  type="button"
                  :disabled="oauthBusy"
                  @click="startGoogleOAuth(a)"
                >
                  Google 授权
                </button>
                <button
                  v-if="isOAuthProvider(a.provider)"
                  class="btn-ghost"
                  type="button"
                  @click="openCreds(a)"
                >
                  凭证
                </button>
                <button
                  class="btn-ghost"
                  type="button"
                  :disabled="testingId === a.id"
                  @click="testAccount(a)"
                >
                  测通
                </button>
                <button
                  v-if="isOAuthProvider(a.provider)"
                  class="btn-ghost"
                  type="button"
                  @click="forceRefresh(a)"
                >
                  强制刷新
                </button>
                <button class="btn-ghost" type="button" @click="toggle(a)">
                  {{ a.state === 'healthy' ? '暂停' : '启用' }}
                </button>
              </div>
            </td>
          </tr>
        </tbody>
      </table>
    </div>

    <div v-if="oauthAccount" class="mt-4 rounded-xl border border-teal/40 bg-white p-4 shadow-sm space-y-3">
      <div class="flex items-center gap-2">
        <h3 class="font-semibold">Google 授权 · {{ oauthAccount.label || oauthAccount.id }}</h3>
        <div class="flex-1" />
        <button class="btn-ghost" type="button" @click="closeOAuthPanel">关闭</button>
      </div>
      <p class="text-xs text-slatex">
        浏览器会跳到 Google，再回到 <code>http://localhost:8085/callback</code>。
        自动交换成功后本页会刷新；若被拦截，请把回调页完整 URL 粘贴到下方。
      </p>
      <div>
        <label class="label">粘贴回调 URL</label>
        <textarea
          v-model="oauthCallbackUrl"
          class="input min-h-[72px] font-mono text-xs"
          placeholder="http://localhost:8085/callback?code=...&state=..."
        />
      </div>
      <button class="btn-primary" type="button" :disabled="oauthBusy" @click="submitOAuthCallback">
        用回调 URL 完成交换
      </button>
      <p v-if="oauthPolling" class="text-xs text-slatex">正在轮询授权状态…</p>
    </div>

    <div
      v-if="selected"
      class="modal-backdrop"
      @click.self="closeModal"
    >
      <div class="modal-panel max-w-xl">
        <div class="flex items-center gap-2">
          <h3 class="font-semibold">上游订阅凭证 · {{ selected.label || selected.id }}</h3>
          <span class="pill-muted">{{ selected.provider }}</span>
          <div class="flex-1" />
          <button class="btn-ghost" type="button" @click="closeModal">关闭</button>
        </div>
        <p class="text-xs text-slatex">
          只写入数据库；GET 不会返回明文 token。Claude/Codex/Antigravity 用 access/refresh；Codex 还可填 chatgpt_account_id；Antigravity 需 project_id。
        </p>
        <p v-if="msg" class="text-sm text-teal">{{ msg }}</p>

        <div>
          <label class="label">Access token（留空=不改）</label>
          <textarea
            v-model="access"
            class="input min-h-[80px] font-mono"
            :disabled="clearAccess"
            placeholder="粘贴 OAuth access token"
          />
          <label class="mt-2 flex items-center gap-2 text-xs text-slatex">
            <input v-model="clearAccess" type="checkbox" class="accent-teal" />
            清除 access token（clear_access_token）
          </label>
        </div>
        <div>
          <label class="label">Refresh token（留空=不改）</label>
          <textarea
            v-model="refresh"
            class="input min-h-[60px] font-mono"
            :disabled="clearRefresh"
            placeholder="可选"
          />
          <label class="mt-2 flex items-center gap-2 text-xs text-slatex">
            <input v-model="clearRefresh" type="checkbox" class="accent-teal" />
            清除 refresh token（clear_refresh_token）
          </label>
        </div>
        <div v-if="selected.provider === 'codex'">
          <label class="label">ChatGPT account id</label>
          <input v-model="chatgptAccountId" class="input font-mono" placeholder="uuid" />
        </div>
        <div v-if="selected.provider === 'antigravity'">
          <label class="label">Antigravity project id</label>
          <input v-model="projectId" class="input font-mono" placeholder="required for upstream calls" />
        </div>
        <div>
          <label class="label">绑定代理</label>
          <select v-model="modalProxyId" class="input">
            <option value="">无</option>
            <option v-for="p in proxies" :key="p.id" :value="p.id">
              {{ p.name || p.id }} ({{ p.type }})
            </option>
          </select>
        </div>
        <div class="flex justify-end gap-2 pt-1">
          <button class="btn-ghost" type="button" @click="closeModal">取消</button>
          <button class="btn-primary" type="button" :disabled="saving" @click="saveCreds">
            {{ saving ? '保存中…' : '保存到后台' }}
          </button>
        </div>
      </div>
    </div>
  </div>
</template>
