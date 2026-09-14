<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { adminApi, asList } from '../api/client'
import PageHeader from '../components/PageHeader.vue'
import StatusPill from '../components/StatusPill.vue'
import EmptyState from '../components/EmptyState.vue'

type Account = {
  id: string
  label: string
  provider: string
  base_url?: string
  tier: number
  state: string
  load?: number
  last_error?: string
  has_access_token?: boolean
  has_refresh_token?: boolean
  has_cookie?: boolean
  expires_at?: string
  consecutive_failures?: number
  consecutive_successes?: number
  cooldown_until?: string | null
  cookie_identity?: {
    email?: string
    name?: string
    org_uuid?: string
    org_name?: string
    plan?: string
    verified_at?: string
  }
}

type AvailableModel = {
  id: string
  provider: string
  model: string
  label?: string
  enabled: boolean
  sort_order?: number
}

const PROVIDERS = ['claude', 'codex', 'antigravity'] as const

const rows = ref<Account[]>([])
const models = ref<AvailableModel[]>([])
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
const testAccountRow = ref<Account | null>(null)
const testModel = ref('')
const testAuthMode = ref<'auto' | 'cookie' | 'oauth'>('auto')
const testResult = ref<any | null>(null)

const oauthAccount = ref<Account | null>(null)
const oauthSessionId = ref('')
const oauthState = ref('')
const oauthCallbackUrl = ref('')
const oauthBusy = ref(false)
const oauthPolling = ref(false)
let oauthPollTimer: number | null = null
let oauthPopup: Window | null = null

const claudeAccount = ref<Account | null>(null)
const claudeSessionKey = ref('')
const claudeBusy = ref(false)
const claudeErr = ref('')
const claudeOk = ref('')

const cookieAccount = ref<Account | null>(null)
const cookieValue = ref('')
const cookieBusy = ref(false)
const identityBusyId = ref<string | null>(null)
const cookieErr = ref('')
const cookieOk = ref('')

const showImportPanel = ref(false)
const importText = ref('')
const importBusy = ref(false)
const importResult = ref('')

const showAccountModal = ref(false)
const editingAccount = ref<Account | null>(null)
const accountForm = ref({
  id: '',
  name: '',
  provider: 'claude',
  priority: 1,
  base_url: '',
  showAdvanced: false,
})
const accountSaving = ref(false)
const confirmDelete = ref<Account | null>(null)
const manageAccount = ref<Account | null>(null)

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
  return parts.length ? parts.join('+') : '未授权'
}

function connectionLabel(a: Account) {
  if (a.provider === 'claude') {
    if (a.has_cookie && a.has_access_token) return 'Cookie + OAuth 兼容'
    if (a.has_cookie) return 'Cookie Web 通道'
    if (a.has_access_token) return 'OAuth API 通道'
    return '未配置'
  }
  return a.has_access_token ? 'OAuth 已连接' : '未授权'
}

function isUnauthorized(a: Account) {
  if (a.provider === 'claude') return !a.has_cookie && !a.has_access_token
  return isOAuthProvider(a.provider) && !a.has_access_token
}

function cookieIdentityLabel(a: Account) {
  const identity = a.cookie_identity
  if (!identity) return ''
  return identity.email || identity.name || identity.org_name || identity.org_uuid || ''
}

async function load() {
  loading.value = true
  err.value = ''
  try {
    const [acc, modelRows] = await Promise.all([
      adminApi.accounts(),
      adminApi.models().catch(() => []),
    ])
    rows.value = asList<Account>(acc)
    models.value = asList<AvailableModel>(modelRows)
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
  msg.value = ''
  try {
    const st = await adminApi.getCredentials(a.id)
    msg.value = st.has_access_token
      ? `已保存凭证（更新于 ${st.updated_at || '未知'}）`
      : '尚未保存上游 OAuth；可粘贴 access/refresh，或对 Antigravity 使用 Google 授权、对 Claude 使用「Claude 授权」。'
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

const testModels = computed(() => {
  const provider = testAccountRow.value?.provider
  return models.value
    .filter((m) => m.enabled && m.provider.toLowerCase() === provider?.toLowerCase())
    .sort((a, b) => (a.sort_order || 0) - (b.sort_order || 0))
})

function defaultTestModel(provider: string) {
  const candidates = models.value
    .filter((m) => m.enabled && m.provider.toLowerCase() === provider.toLowerCase())
    .sort((a, b) => (a.sort_order || 0) - (b.sort_order || 0))
  const preferred: Record<string, string> = {
    claude: 'claude-sonnet-5',
    antigravity: 'gemini-2.5-flash',
    codex: 'gpt-5.5',
  }
  const first = candidates.find((m) => m.model === preferred[provider]) || candidates[0]
  if (first?.model) return first.model
  if (provider === 'claude') return 'claude-sonnet-5'
  if (provider === 'antigravity') return 'gemini-2.5-flash'
  if (provider === 'codex') return 'gpt-5.5'
  return 'gpt-4o-mini'
}

function openTestAccount(a: Account) {
  testAccountRow.value = a
  testModel.value = defaultTestModel(a.provider)
  testAuthMode.value = 'auto'
  testResult.value = null
}

function closeTestAccount() {
  if (testingId.value) return
  testAccountRow.value = null
  testResult.value = null
}

async function runAccountTest() {
  const a = testAccountRow.value
  if (!a || !testModel.value.trim()) return
  testingId.value = a.id
  testResult.value = null
  try {
    testResult.value = await adminApi.testAccount(a.id, {
      model: testModel.value.trim(),
      auth_mode: a.provider === 'claude' ? testAuthMode.value : 'auto',
    })
  } catch (e: any) {
    testResult.value = { ok: false, error: e.message || '测试失败' }
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

function closeClaudePanel() {
  claudeAccount.value = null
  claudeSessionKey.value = ''
  claudeBusy.value = false
  claudeErr.value = ''
  claudeOk.value = ''
}

function startClaudeOAuth(a: Account) {
  claudeAccount.value = a
  claudeSessionKey.value = ''
  claudeErr.value = ''
  claudeOk.value = ''
  msg.value = '已打开 claude.ai；授权第二个订阅前请先退出或用无痕窗口登录目标账号，再复制该账号 sessionKey 粘贴到下方。'
  window.open('https://claude.ai', 'subport-claude-oauth', 'width=980,height=780')
}

async function submitClaudeSessionKey() {
  if (!claudeAccount.value) return
  const key = claudeSessionKey.value.trim()
  if (!key) {
    claudeErr.value = '请粘贴 sessionKey'
    claudeOk.value = ''
    return
  }
  claudeBusy.value = true
  claudeErr.value = ''
  claudeOk.value = ''
  msg.value = `正在交换 Claude sessionKey… ${claudeAccount.value.id}`
  try {
    const out = await adminApi.claudeOAuthExchange(claudeAccount.value.id, { session_key: key })
    claudeOk.value = out.message || 'Claude 授权成功'
    msg.value = claudeOk.value
    claudeSessionKey.value = ''
    await load()
  } catch (e: any) {
    const bits = [e.message || 'Claude 授权失败']
    if (e.step) bits.push(`步骤 ${e.step}`)
    if (e.upstreamStatus) bits.push(`上游 HTTP ${e.upstreamStatus}`)
    if (e.exchangeId) bits.push(`id ${e.exchangeId}`)
    claudeErr.value = bits.join(' · ')
    msg.value = claudeErr.value
  } finally {
    claudeBusy.value = false
  }
}



function closeCookiePanel() {
  cookieAccount.value = null
  cookieValue.value = ''
  cookieBusy.value = false
  cookieErr.value = ''
  cookieOk.value = ''
}

function openCookiePanel(a: Account) {
  cookieAccount.value = a
  cookieValue.value = ''
  cookieErr.value = ''
  cookieOk.value = ''
  msg.value = `维护 Cookie：${a.id}`
}

async function saveCookie() {
  if (!cookieAccount.value) return
  const cookie = cookieValue.value.trim()
  if (!cookie) {
    cookieErr.value = '请粘贴完整 Cookie 字符串'
    cookieOk.value = ''
    return
  }
  cookieBusy.value = true
  cookieErr.value = ''
  cookieOk.value = ''
  msg.value = `正在保存 Cookie… ${cookieAccount.value.id}`
  try {
    const out = await adminApi.saveClaudeCookie(cookieAccount.value.id, cookie)
    const identity = out.identity?.email || out.identity?.name || out.identity?.org_name || out.identity?.org_uuid
    cookieOk.value = `${out.message || 'Cookie 已保存'}${identity ? `：${identity}` : ''}`
    msg.value = cookieOk.value
    cookieValue.value = ''
    await load()
  } catch (e: any) {
    cookieErr.value = e.message || '保存 Cookie 失败'
    msg.value = cookieErr.value
  } finally {
    cookieBusy.value = false
  }
}

async function clearCookie() {
  if (!cookieAccount.value) return
  cookieBusy.value = true
  cookieErr.value = ''
  cookieOk.value = ''
  msg.value = `正在清除 Cookie… ${cookieAccount.value.id}`
  try {
    const out = await adminApi.clearClaudeCookie(cookieAccount.value.id)
    cookieOk.value = out.message || 'Cookie 已清除'
    msg.value = cookieOk.value
    cookieValue.value = ''
    await load()
  } catch (e: any) {
    cookieErr.value = e.message || '清除 Cookie 失败'
    msg.value = cookieErr.value
  } finally {
    cookieBusy.value = false
  }
}

async function verifyCookieIdentity(a: Account) {
  identityBusyId.value = a.id
  msg.value = `正在核对 ${a.label || a.id} 的 Cookie 身份…`
  try {
    const out = await adminApi.verifyClaudeCookieIdentity(a.id)
    const label = out.identity?.email || out.identity?.name || out.identity?.org_name || out.identity?.org_uuid
    msg.value = out.ok ? `身份核对成功${label ? `：${label}` : ''}` : (out.message || '身份核对失败')
    await load()
  } catch (e: any) {
    msg.value = e.message || '身份核对失败'
  } finally {
    identityBusyId.value = null
  }
}

function openCreateAccount() {
  editingAccount.value = null
  accountForm.value = {
    id: '',
    name: '',
    provider: 'claude',
    priority: 1,
    base_url: '',
    showAdvanced: false,
  }
  showAccountModal.value = true
  msg.value = ''
}

function openEditAccount(a: Account) {
  editingAccount.value = a
  accountForm.value = {
    id: a.id,
    name: a.label || a.id,
    provider: a.provider,
    priority: a.tier ?? 1,
    base_url: a.base_url || '',
    showAdvanced: !!(a.base_url),
  }
  showAccountModal.value = true
  msg.value = ''
}

function closeAccountModal() {
  showAccountModal.value = false
  editingAccount.value = null
}

function openManageAccount(a: Account) {
  manageAccount.value = a
}

function manageThen(action: (a: Account) => void) {
  const a = manageAccount.value
  if (!a) return
  manageAccount.value = null
  action(a)
}

async function saveAccount() {
  const name = accountForm.value.name.trim()
  if (!name) {
    msg.value = '请填写名称'
    return
  }
  if (!editingAccount.value && !accountForm.value.provider) {
    msg.value = '请选择 Provider'
    return
  }
  accountSaving.value = true
  msg.value = ''
  try {
    if (editingAccount.value) {
      const body: Record<string, unknown> = {
        name,
        priority: Number(accountForm.value.priority) || 1,
        base_url: accountForm.value.base_url.trim(),
      }
      await adminApi.patchAccount(editingAccount.value.id, body)
      msg.value = '已更新账号'
    } else {
      const body: Record<string, unknown> = {
        name,
        provider: accountForm.value.provider,
        priority: Number(accountForm.value.priority) || 1,
      }
      if (accountForm.value.id.trim()) body.id = accountForm.value.id.trim()
      if (accountForm.value.base_url.trim()) body.base_url = accountForm.value.base_url.trim()
      await adminApi.createAccount(body as any)
      msg.value = '已创建账号'
    }
    closeAccountModal()
    await load()
  } catch (e: any) {
    msg.value = e.message
  } finally {
    accountSaving.value = false
  }
}

async function doDeleteAccount() {
  if (!confirmDelete.value) return
  accountSaving.value = true
  try {
    await adminApi.deleteAccount(confirmDelete.value.id)
    msg.value = `已删除 ${confirmDelete.value.id}`
    confirmDelete.value = null
    await load()
  } catch (e: any) {
    msg.value = e.message
  } finally {
    accountSaving.value = false
  }
}


async function runImport() {
  importBusy.value = true
  importResult.value = ''
  msg.value = ''
  try {
    const parsed = JSON.parse(importText.value)
    const rows = Array.isArray(parsed) ? parsed : parsed.accounts
    if (!Array.isArray(rows) || rows.length === 0) {
      throw new Error('JSON must be a non-empty array (or { accounts: [...] })')
    }
    const out = await adminApi.importAccounts(rows)
    importResult.value = `imported=${out.imported} failed=${out.failed}`
    msg.value = importResult.value
    await load()
  } catch (e: any) {
    importResult.value = e.message || 'import failed'
    msg.value = importResult.value
  } finally {
    importBusy.value = false
  }
}

function onImportFile(ev: Event) {
  const input = ev.target as HTMLInputElement
  const file = input.files?.[0]
  if (!file) return
  const reader = new FileReader()
  reader.onload = () => {
    importText.value = String(reader.result || '')
  }
  reader.readAsText(file)
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
      subtitle="一行代表一个上游账号。Claude 优先维护 Cookie；OAuth 与手工凭证保留为兼容通道。"
    >
      <template #actions>
        <button class="btn-ghost" type="button" :disabled="loading" @click="load">刷新</button>
        <button class="btn-ghost" type="button" @click="showImportPanel = !showImportPanel">Import JSON</button>
        <button class="btn-primary" type="button" @click="openCreateAccount">新建账号</button>
      </template>
    </PageHeader>

    <p v-if="err" class="mb-3 max-h-20 overflow-auto break-words rounded-lg bg-red-50 px-3 py-2 text-sm text-red-700">{{ err }}</p>
    <p v-if="msg" class="mb-3 max-h-20 overflow-auto break-words rounded-lg bg-teal/10 px-3 py-2 text-sm text-teal">{{ msg }}</p>

    <div class="mb-4 rounded-xl border border-amber-200 bg-amber-50 px-4 py-3 text-sm text-amber-900">
      <span class="font-medium">Claude 通道说明：</span>
      Cookie Web 通道目前用于独立测通；正常 API 转发仍使用 OAuth access token。测试时可明确选择通道和模型，结果会显示实际路径。
    </div>

    <div v-if="showImportPanel" class="mb-4 rounded-xl border border-slate-200 bg-white p-4 shadow-sm space-y-3">
      <div class="flex items-center justify-between gap-2">
        <h3 class="font-semibold">Batch credential import</h3>
        <button class="btn-ghost" type="button" @click="showImportPanel = false">Close</button>
      </div>
      <p class="text-sm text-slatex">
        Paste JSON array:
        <code>[{ "provider":"claude", "name":"email@x", "cookie":"...", "access_token":"...", "refresh_token":"..." }]</code>
        — create/update by name+provider. Secrets are never logged.
      </p>
      <textarea
        v-model="importText"
        class="input min-h-[140px] font-mono text-xs"
        placeholder='[{"provider":"claude","name":"you@example.com","cookie":"..."}]'
        :disabled="importBusy"
      />
      <div class="flex flex-wrap items-center gap-2">
        <input type="file" accept="application/json,.json,text/plain" @change="onImportFile" />
        <button class="btn-primary" type="button" :disabled="importBusy || !importText.trim()" @click="runImport">
          {{ importBusy ? 'Importing…' : 'Import' }}
        </button>
      </div>
      <p v-if="importResult" class="text-sm text-teal">{{ importResult }}</p>
    </div>


    <EmptyState
      v-if="!loading && !err && rows.length === 0"
      title="暂无账号"
      description="账号池为空。点击「新建账号」添加上游订阅账号。"
    >
      <template #actions>
        <button class="btn-primary" type="button" @click="openCreateAccount">新建账号</button>
      </template>
    </EmptyState>

    <div v-else class="table-wrap">
      <table class="data-table">
        <thead>
          <tr>
            <th>账号</th>
            <th>状态</th>
            <th>连接方式</th>
            <th>过期</th>
            <th>最近错误</th>
            <th></th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="a in rows" :key="a.id">
            <td>
              <div class="font-medium">{{ a.label || a.id }}</div>
              <div class="font-mono text-xs text-slatex">{{ a.id }}</div>
              <div class="mt-1 flex items-center gap-1.5 text-[11px] text-slatex">
                <span class="rounded bg-slate-100 px-1.5 py-0.5">{{ a.provider }}</span>
                <span>优先级 {{ a.tier ?? '—' }}</span>
              </div>
              <div
                v-if="a.cooldown_until || a.consecutive_failures"
                class="mt-0.5 text-[11px] text-slatex"
              >
                <span v-if="a.consecutive_failures">连续失败 {{ a.consecutive_failures }}</span>
                <span v-if="a.cooldown_until"> · 冷却至 {{ formatTs(a.cooldown_until) }}</span>
              </div>
            </td>
            <td>
              <div class="flex flex-wrap items-center gap-1">
                <StatusPill :status="a.state" />
                <span
                  v-if="isUnauthorized(a)"
                  class="rounded-full bg-amber-100 px-2 py-0.5 text-[11px] font-medium text-amber-800"
                >未授权</span>
              </div>
            </td>
            <td class="text-xs">
              <div class="flex flex-col gap-1">
                <span
                  :class="(a.has_cookie || a.has_access_token) ? 'font-medium text-teal' : 'text-amber-700'"
                >
                  {{ connectionLabel(a) }}
                </span>
                <span
                  v-if="a.provider === 'claude'"
                  class="text-[11px] text-slatex"
                >
                  {{ a.has_cookie ? 'Cookie 已配置' : 'Cookie 未配置' }} · {{ credLabel(a) }}
                </span>
                <span
                  v-if="a.provider === 'claude' && a.has_cookie"
                  class="w-fit rounded-md px-2 py-1 text-[11px]"
                  :class="a.cookie_identity ? 'bg-emerald-50 text-emerald-800' : 'bg-amber-50 text-amber-800'"
                  :title="a.cookie_identity?.verified_at ? `核对时间：${formatTs(a.cookie_identity.verified_at)}` : ''"
                >
                  {{ a.cookie_identity ? `已核对：${cookieIdentityLabel(a)}` : '身份未核对' }}
                  <template v-if="a.cookie_identity?.org_name"> · {{ a.cookie_identity.org_name }}</template>
                </span>
              </div>
            </td>
            <td class="whitespace-nowrap text-xs text-slatex">{{ formatTs(a.expires_at) }}</td>
            <td class="max-w-[12rem] truncate text-xs text-red-600">{{ a.last_error || '—' }}</td>
            <td class="min-w-[15rem] whitespace-nowrap text-right">
              <div class="flex items-center justify-end gap-2">
                <button
                  v-if="a.provider === 'antigravity'"
                  class="btn-ghost"
                  type="button"
                  :disabled="oauthBusy"
                  @click="startGoogleOAuth(a)"
                >
                  Google 授权
                </button>
                <button
                  v-if="a.provider === 'claude'"
                  class="btn-primary"
                  type="button"
                  :disabled="cookieBusy"
                  @click="openCookiePanel(a)"
                >
                  {{ a.has_cookie ? '更新 Cookie' : '配置 Cookie' }}
                </button>
                <button
                  class="btn-primary"
                  type="button"
                  :disabled="testingId === a.id"
                  @click="openTestAccount(a)"
                >
                  {{ testingId === a.id ? '测试中…' : '测试' }}
                </button>
                <button class="btn-ghost" type="button" @click="openManageAccount(a)">管理</button>
              </div>
            </td>
          </tr>
        </tbody>
      </table>
    </div>

    <div v-if="manageAccount" class="modal-backdrop" @click.self="manageAccount = null">
      <div class="modal-panel max-w-md">
        <div class="flex items-center gap-2">
          <h3 class="font-semibold">管理账号 · {{ manageAccount.label || manageAccount.id }}</h3>
          <span class="pill-muted">{{ manageAccount.provider }}</span>
          <div class="flex-1" />
          <button class="btn-ghost" type="button" @click="manageAccount = null">关闭</button>
        </div>
        <p class="font-mono text-xs text-slatex">{{ manageAccount.id }}</p>

        <div class="grid grid-cols-1 gap-2 sm:grid-cols-2">
          <button
            v-if="manageAccount.provider === 'claude' && manageAccount.has_cookie"
            class="btn-ghost justify-start"
            type="button"
            :disabled="identityBusyId === manageAccount.id"
            @click="manageThen(verifyCookieIdentity)"
          >核对 Cookie 身份</button>
          <button
            v-if="manageAccount.provider === 'claude'"
            class="btn-ghost justify-start"
            type="button"
            @click="manageThen(startClaudeOAuth)"
          >OAuth 兼容授权</button>
          <button
            v-if="isOAuthProvider(manageAccount.provider)"
            class="btn-ghost justify-start"
            type="button"
            @click="manageThen(openCreds)"
          >高级凭证</button>
          <button
            v-if="manageAccount.has_refresh_token"
            class="btn-ghost justify-start"
            type="button"
            @click="manageThen(forceRefresh)"
          >刷新 OAuth token</button>
          <button class="btn-ghost justify-start" type="button" @click="manageThen(openEditAccount)">编辑账号</button>
          <button class="btn-ghost justify-start" type="button" @click="manageThen(toggle)">
            {{ manageAccount.state === 'healthy' ? '暂停账号' : '启用账号' }}
          </button>
        </div>

        <div class="rounded-xl border border-red-200 bg-red-50 p-3">
          <p class="text-sm font-medium text-red-800">危险操作</p>
          <p class="mt-1 text-xs text-red-700">删除后，该账号及其 Cookie/OAuth 凭证将从账号池移除。</p>
          <button
            class="mt-3 rounded-lg border border-red-300 bg-white px-3 py-2 text-sm font-medium text-red-700 hover:bg-red-100"
            type="button"
            @click="confirmDelete = manageAccount; manageAccount = null"
          >删除账号</button>
        </div>
      </div>
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

    <div v-if="claudeAccount" class="mt-4 rounded-xl border border-teal/40 bg-white p-4 shadow-sm space-y-3">
      <div class="flex items-center gap-2">
        <h3 class="font-semibold">Claude 授权 · {{ claudeAccount.label || claudeAccount.id }}</h3>
        <div class="flex-1" />
        <button class="btn-ghost" type="button" @click="closeClaudePanel">关闭</button>
      </div>
      <p class="text-xs text-slatex">
        浏览器会打开 <code>https://claude.ai</code>。登录后打开开发者工具 → Application/存储 → Cookies，
        复制 <code>sessionKey</code> 粘贴到下方并提交；后台会自动交换为 access/refresh token。
      </p>
      <p class="text-xs text-amber-800 bg-amber-50 border border-amber-200 rounded-lg px-3 py-2">
        授权第二个订阅前请先退出 claude.ai，或用无痕窗口登录目标账号，再粘贴该账号的 sessionKey。
        否则浏览器 Cookie 仍是旧账号，会导致串号授权。
      </p>
      <p class="text-xs text-slatex bg-slate-50 border border-slate-200 rounded-lg px-3 py-2">
        Azure 环境建议先为本账号保存完整 Cookie。若其中的 sessionKey 与下方输入一致，交换时会自动携带同账号完整 Cookie，提升通过 Cloudflare 校验的成功率。
      </p>
      <div>
        <label class="label">sessionKey</label>
        <textarea
          v-model="claudeSessionKey"
          class="input min-h-[88px] font-mono text-xs"
          placeholder="粘贴 claude.ai 的 sessionKey（不会回显到日志）"
          :disabled="claudeBusy"
        />
      </div>
      <div class="flex flex-wrap items-center gap-2">
        <button class="btn-primary" type="button" :disabled="claudeBusy" @click="submitClaudeSessionKey">
          {{ claudeBusy ? '交换中…' : '提交并自动交换' }}
        </button>
        <button class="btn-ghost" type="button" :disabled="claudeBusy" @click="startClaudeOAuth(claudeAccount!)">
          重新打开 claude.ai
        </button>
      </div>
      <p v-if="claudeErr" class="text-sm text-red-600">{{ claudeErr }}</p>
      <p v-if="claudeOk" class="text-sm text-teal">{{ claudeOk }}</p>
    </div>

    <div v-if="cookieAccount" class="mt-4 rounded-xl border border-teal/40 bg-white p-4 shadow-sm space-y-3">
      <div class="flex items-center gap-2">
        <h3 class="font-semibold">维护 Cookie · {{ cookieAccount.label || cookieAccount.id }}</h3>
        <span
          class="rounded-full px-2 py-0.5 text-[11px] font-medium"
          :class="cookieAccount.has_cookie ? 'bg-teal/15 text-teal' : 'bg-slate-100 text-slatex'"
        >
          {{ cookieAccount.has_cookie ? '已配置 Cookie' : '未配置 Cookie' }}
        </span>
        <div class="flex-1" />
        <button class="btn-ghost" type="button" @click="closeCookiePanel">关闭</button>
      </div>
      <p class="text-xs text-slatex">
        在 <code>https://claude.ai</code> 打开 DevTools → Network/Application → 复制完整
        <code>Cookie</code> 请求头（含 sessionKey），按账号粘贴保存。仅保存在该账号，不会串号。
      </p>
      <div>
        <label class="label">Cookie</label>
        <textarea
          v-model="cookieValue"
          class="input min-h-[100px] font-mono text-xs"
          placeholder="粘贴完整 Cookie 字符串（不会在列表接口回显）"
          :disabled="cookieBusy"
        />
      </div>
      <div class="flex flex-wrap items-center gap-2">
        <button class="btn-primary" type="button" :disabled="cookieBusy" @click="saveCookie">
          {{ cookieBusy ? '保存中…' : '保存 Cookie' }}
        </button>
        <button
          class="btn-ghost"
          type="button"
          :disabled="cookieBusy || !cookieAccount.has_cookie"
          @click="clearCookie"
        >
          清除 Cookie
        </button>
      </div>
      <p v-if="cookieErr" class="text-sm text-red-600">{{ cookieErr }}</p>
      <p v-if="cookieOk" class="text-sm text-teal">{{ cookieOk }}</p>
    </div>

    <div v-if="testAccountRow" class="modal-backdrop" @click.self="closeTestAccount">
      <div class="modal-panel max-w-lg">
        <div class="flex items-center gap-2">
          <h3 class="font-semibold">测试账号 · {{ testAccountRow.label || testAccountRow.id }}</h3>
          <span class="pill-muted">{{ testAccountRow.provider }}</span>
          <div class="flex-1" />
          <button class="btn-ghost" type="button" :disabled="!!testingId" @click="closeTestAccount">关闭</button>
        </div>

        <div v-if="testAccountRow.provider === 'claude'">
          <label class="label">认证通道</label>
          <select v-model="testAuthMode" class="input">
            <option value="auto">自动（优先 Cookie）</option>
            <option value="cookie" :disabled="!testAccountRow.has_cookie">Cookie Web 通道</option>
            <option value="oauth" :disabled="!testAccountRow.has_access_token">OAuth API 通道</option>
          </select>
          <p class="mt-1 text-xs text-slatex">Cookie 与 OAuth 是两条不同的上游协议；测试结果会标明实际使用的通道。</p>
        </div>

        <div>
          <label class="label">测试模型</label>
          <select v-if="testModels.length" v-model="testModel" class="input font-mono text-sm">
            <option v-for="m in testModels" :key="m.id" :value="m.model">
              {{ m.label || m.model }} · {{ m.model }}
            </option>
          </select>
          <input v-else v-model="testModel" class="input font-mono text-sm" placeholder="输入上游模型名" />
          <p class="mt-1 text-xs text-slatex">这里只测试该账号与所选上游模型，不经过账号池调度。</p>
        </div>

        <div
          v-if="testResult"
          class="rounded-xl border px-3 py-3 text-sm"
          :class="testResult.ok ? 'border-emerald-200 bg-emerald-50 text-emerald-900' : 'border-red-200 bg-red-50 text-red-800'"
        >
          <div class="flex flex-wrap items-center gap-x-3 gap-y-1 font-medium">
            <span>{{ testResult.ok ? '测试成功' : '测试失败' }}</span>
            <span v-if="testResult.path">通道：{{ testResult.path === 'cookie' ? 'Cookie Web' : (testResult.path === 'oauth' ? 'OAuth API' : testResult.path) }}</span>
            <span v-if="testResult.http_status">HTTP {{ testResult.http_status }}</span>
            <span v-if="testResult.latency_ms != null">{{ testResult.latency_ms }}ms</span>
          </div>
          <p class="mt-1 break-words text-xs">模型：{{ testResult.model || testModel }}</p>
          <p v-if="testResult.message_zh || testResult.error" class="mt-2 max-h-32 overflow-auto break-words text-xs">
            {{ testResult.message_zh || testResult.error }}
          </p>
        </div>

        <div class="flex justify-end gap-2 pt-1">
          <button class="btn-ghost" type="button" :disabled="!!testingId" @click="closeTestAccount">取消</button>
          <button class="btn-primary" type="button" :disabled="!!testingId || !testModel.trim()" @click="runAccountTest">
            {{ testingId ? '测试中…' : '开始测试' }}
          </button>
        </div>
      </div>
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
        <div class="flex justify-end gap-2 pt-1">
          <button class="btn-ghost" type="button" @click="closeModal">取消</button>
          <button class="btn-primary" type="button" :disabled="saving" @click="saveCreds">
            {{ saving ? '保存中…' : '保存到后台' }}
          </button>
        </div>
      </div>
    </div>

    <div
      v-if="showAccountModal"
      class="modal-backdrop"
      @click.self="closeAccountModal"
    >
      <div class="modal-panel max-w-lg">
        <div class="flex items-center gap-2">
          <h3 class="font-semibold">{{ editingAccount ? '编辑账号' : '新建账号' }}</h3>
          <div class="flex-1" />
          <button class="btn-ghost" type="button" @click="closeAccountModal">关闭</button>
        </div>
        <div v-if="!editingAccount">
          <label class="label">Provider</label>
          <select v-model="accountForm.provider" class="input">
            <option v-for="p in PROVIDERS" :key="p" :value="p">{{ p }}</option>
          </select>
        </div>
        <div v-else>
          <label class="label">Provider</label>
          <input class="input" :value="accountForm.provider" disabled />
        </div>
        <div>
          <label class="label">名称</label>
          <input v-model="accountForm.name" class="input" placeholder="显示名称" />
        </div>
        <div v-if="!editingAccount">
          <label class="label">ID（可选，留空自动生成 acct-provider-xxxx）</label>
          <input v-model="accountForm.id" class="input font-mono text-xs" placeholder="acct-claude-xxxx" />
        </div>
        <div>
          <label class="label">优先级（越小越优先）</label>
          <input v-model.number="accountForm.priority" class="input" type="number" min="0" />
        </div>
        <div>
          <button
            class="btn-ghost text-xs"
            type="button"
            @click="accountForm.showAdvanced = !accountForm.showAdvanced"
          >
            {{ accountForm.showAdvanced ? '收起高级选项' : '高级选项' }}
          </button>
        </div>
        <div v-if="accountForm.showAdvanced">
          <label class="label">Base URL（可选）</label>
          <input v-model="accountForm.base_url" class="input font-mono text-xs" placeholder="https://..." />
        </div>
        <div class="flex justify-end gap-2 pt-1">
          <button class="btn-ghost" type="button" @click="closeAccountModal">取消</button>
          <button class="btn-primary" type="button" :disabled="accountSaving" @click="saveAccount">
            {{ accountSaving ? '保存中…' : (editingAccount ? '保存' : '创建') }}
          </button>
        </div>
      </div>
    </div>

    <div
      v-if="confirmDelete"
      class="modal-backdrop"
      @click.self="confirmDelete = null"
    >
      <div class="modal-panel max-w-md">
        <h3 class="font-semibold">确认删除账号</h3>
        <p class="text-sm text-slatex">
          将删除 <span class="font-mono">{{ confirmDelete.id }}</span>
          （{{ confirmDelete.label || confirmDelete.provider }}）及其凭证，且无法从调度池恢复。此操作不可撤销。
        </p>
        <div class="flex justify-end gap-2 pt-2">
          <button class="btn-ghost" type="button" @click="confirmDelete = null">取消</button>
          <button class="btn-primary" type="button" :disabled="accountSaving" @click="doDeleteAccount">
            {{ accountSaving ? '删除中…' : '确认删除' }}
          </button>
        </div>
      </div>
    </div>

  </div>
</template>
