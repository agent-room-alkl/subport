const TOKEN_KEY = 'subport_token'

export function getToken(): string | null {
  try { return localStorage.getItem(TOKEN_KEY) } catch { return null }
}

export function setToken(t: string | null) {
  try {
    if (t) localStorage.setItem(TOKEN_KEY, t)
    else localStorage.removeItem(TOKEN_KEY)
  } catch { /* ignore */ }
}

export class ApiError extends Error {
  status: number
  code?: string
  step?: string
  upstreamStatus?: number
  exchangeId?: string
  constructor(status: number, message: string, extra?: { code?: string; step?: string; upstreamStatus?: number; exchangeId?: string }) {
    super(message)
    this.status = status
    if (extra) {
      this.code = extra.code
      this.step = extra.step
      this.upstreamStatus = extra.upstreamStatus
      this.exchangeId = extra.exchangeId
    }
  }
}

export async function api<T = any>(path: string, opts: RequestInit = {}): Promise<T> {
  const headers: Record<string, string> = {
    'Content-Type': 'application/json',
    ...(opts.headers as Record<string, string> | undefined),
  }
  const token = getToken()
  if (token) headers.Authorization = `Bearer ${token}`
  const res = await fetch(path, { ...opts, headers })
  if (!res.ok) {
    let msg = `${res.status} ${res.statusText}`
    let extra: { code?: string; step?: string; upstreamStatus?: number; exchangeId?: string } | undefined
    try {
      const body = await res.json()
      if (body?.error) msg = body.error
      if (body && (body.code || body.step || body.upstream_status || body.exchange_id)) {
        extra = {
          code: body.code,
          step: body.step,
          upstreamStatus: body.upstream_status,
          exchangeId: body.exchange_id,
        }
      }
    } catch { /* ignore */ }
    throw new ApiError(res.status, msg, extra)
  }
  if (res.status === 204) return null as T
  const text = await res.text()
  if (!text) return null as T
  try {
    return JSON.parse(text) as T
  } catch {
    return text as T
  }
}

/** Normalize list payloads that may be bare arrays or wrapped objects. */
export function asList<T = any>(data: unknown, keys: string[] = ['items', 'rows', 'data', 'results']): T[] {
  if (Array.isArray(data)) return data as T[]
  if (data && typeof data === 'object') {
    for (const k of keys) {
      const v = (data as Record<string, unknown>)[k]
      if (Array.isArray(v)) return v as T[]
    }
  }
  return []
}

export const authApi = {
  login: (username: string, password: string) =>
    api<{ token: string; user: any }>('/api/auth/login', {
      method: 'POST',
      body: JSON.stringify({ username, password }),
    }),
}

export type ModelRoute = {
  id: string
  pattern: string
  provider: string
  priority: number
  enabled: boolean
}

export type Channel = {
  id: string
  name: string
  provider: string
  group_name?: string
  priority: number
  enabled: boolean
  models_json?: string
  created_at?: string
}

export type ChannelAccount = {
  channel_id: string
  account_id: string
  model_pattern?: string
  priority?: number
}

export const adminApi = {
  overview: () => api('/api/overview'),

  accounts: () => api<any[]>('/api/accounts'),
  createAccount: (body: {
    id?: string
    name?: string
    label?: string
    provider: string
    base_url?: string
    priority?: number
    healthy?: boolean
  }) =>
    api<any>('/api/accounts', { method: 'POST', body: JSON.stringify(body) }),
  deleteAccount: (id: string) =>
    api(`/api/accounts/${encodeURIComponent(id)}`, { method: 'DELETE' }),
  getCredentials: (id: string) => api(`/api/accounts/${encodeURIComponent(id)}/credentials`),
  putCredentials: (id: string, body: Record<string, string | boolean>) =>
    api(`/api/accounts/${encodeURIComponent(id)}/credentials`, {
      method: 'PUT',
      body: JSON.stringify(body),
    }),
  testAccount: (id: string, body: { model?: string; auth_mode?: 'auto' | 'cookie' | 'oauth' } = {}) =>
    api(`/api/accounts/${encodeURIComponent(id)}/test`, {
      method: 'POST',
      body: JSON.stringify(body),
    }),

  importAccounts: (rows: Array<Record<string, unknown>>) =>
    api<{ ok: boolean; imported: number; failed: number; results: any[] }>('/api/accounts/import', {
      method: 'POST',
      body: JSON.stringify(rows),
    }),
  getModelAliases: () => api<any>('/api/model-aliases'),
  putModelAliases: (body: Record<string, unknown>) =>
    api('/api/model-aliases', { method: 'PUT', body: JSON.stringify(body) }),

  setHealthy: (id: string, healthy: boolean) =>
    api(`/api/accounts/${encodeURIComponent(id)}`, {
      method: 'PATCH',
      body: JSON.stringify({ healthy }),
    }),
  antigravityOAuthStart: (id: string) =>
    api<{ auth_url: string; session_id: string; state: string }>(
      `/api/accounts/${encodeURIComponent(id)}/antigravity/oauth/start`,
      { method: 'POST', body: '{}' },
    ),
  antigravityOAuthExchange: (
    id: string,
    body: { session_id: string; state?: string; code?: string; callback_url?: string },
  ) =>
    api(`/api/accounts/${encodeURIComponent(id)}/antigravity/oauth/exchange`, {
      method: 'POST',
      body: JSON.stringify(body),
    }),
  antigravityOAuthStatus: (id: string, sessionId: string) =>
    api(
      `/api/accounts/${encodeURIComponent(id)}/antigravity/oauth/status?session_id=${encodeURIComponent(sessionId)}`,
    ),
  claudeOAuthExchange: (id: string, body: { session_key: string; org_uuid?: string }) =>
    api<{
      ok: boolean
      message?: string
      expires_at?: string
      expires_in?: number
      credentials?: Record<string, unknown>
    }>(`/api/accounts/${encodeURIComponent(id)}/claude/oauth/exchange`, {
      method: 'POST',
      body: JSON.stringify(body),
    }),
  saveClaudeCookie: (id: string, cookie: string) =>
    api<{ ok: boolean; message?: string; has_cookie?: boolean; identity_verified?: boolean; identity?: Record<string, string>; credentials?: Record<string, unknown> }>(
      `/api/accounts/${encodeURIComponent(id)}/claude/cookie`,
      { method: 'PUT', body: JSON.stringify({ cookie }) },
    ),
  clearClaudeCookie: (id: string) =>
    api<{ ok: boolean; message?: string; has_cookie?: boolean; credentials?: Record<string, unknown> }>(
      `/api/accounts/${encodeURIComponent(id)}/claude/cookie`,
      { method: 'DELETE' },
    ),
  verifyClaudeCookieIdentity: (id: string) =>
    api<{ ok: boolean; identity_verified: boolean; identity?: Record<string, string>; message?: string }>(
      `/api/accounts/${encodeURIComponent(id)}/claude/identity`,
      { method: 'POST', body: '{}' },
    ),
  patchAccount: (id: string, body: Record<string, unknown>) =>
    api(`/api/accounts/${encodeURIComponent(id)}`, {
      method: 'PATCH',
      body: JSON.stringify(body),
    }),

  modelRoutes: () => api<ModelRoute[] | { items?: ModelRoute[] }>('/api/model-routes'),
  createModelRoute: (body: Partial<ModelRoute>) =>
    api<ModelRoute>('/api/model-routes', { method: 'POST', body: JSON.stringify(body) }),
  updateModelRoute: (id: string, body: Partial<ModelRoute>) =>
    api<ModelRoute>(`/api/model-routes/${encodeURIComponent(id)}`, {
      method: 'PUT',
      body: JSON.stringify(body),
    }),
  deleteModelRoute: (id: string) =>
    api(`/api/model-routes/${encodeURIComponent(id)}`, { method: 'DELETE' }),

  channels: () => api<Channel[] | { items?: Channel[] }>('/api/channels'),
  createChannel: (body: Partial<Channel>) =>
    api<Channel>('/api/channels', { method: 'POST', body: JSON.stringify(body) }),
  updateChannel: (id: string, body: Partial<Channel>) =>
    api<Channel>(`/api/channels/${encodeURIComponent(id)}`, {
      method: 'PUT',
      body: JSON.stringify(body),
    }),
  deleteChannel: (id: string) =>
    api(`/api/channels/${encodeURIComponent(id)}`, { method: 'DELETE' }),

  channelAccounts: (channelId: string) =>
    api<ChannelAccount[] | { items?: ChannelAccount[] }>(
      `/api/channels/${encodeURIComponent(channelId)}/accounts`,
    ),
  addChannelAccount: (channelId: string, body: Partial<ChannelAccount>) =>
    api<ChannelAccount>(`/api/channels/${encodeURIComponent(channelId)}/accounts`, {
      method: 'POST',
      body: JSON.stringify(body),
    }),
  removeChannelAccount: (channelId: string, accountId: string) =>
    api(
      `/api/channels/${encodeURIComponent(channelId)}/accounts/${encodeURIComponent(accountId)}`,
      { method: 'DELETE' },
    ),

  keys: () => api('/api/keys'),
  setKeyEnabled: (id: string, enabled: boolean) =>
    api(`/api/keys/${encodeURIComponent(id)}`, {
      method: 'PATCH',
      body: JSON.stringify({ enabled }),
    }),
  deleteKey: (id: string) =>
    api(`/api/keys/${encodeURIComponent(id)}`, { method: 'DELETE' }),
  usage: () => api('/api/usage'),
  usageSummary: (top = 20) => api(`/api/usage/summary?top=${top}`),
  models: () => api<any[]>('/api/models'),
  setModelEnabled: (id: string, enabled: boolean) =>
    api(`/api/models/${encodeURIComponent(id)}`, {
      method: 'PATCH',
      body: JSON.stringify({ enabled }),
    }),
  tokenRefreshStatus: () => api('/api/token-refresh/status'),
  forceRefreshAccount: (id: string) =>
    api(`/api/accounts/${encodeURIComponent(id)}/refresh`, { method: 'POST', body: '{}' }),

  users: () => api<any[]>('/api/users'),
  createUser: (body: { username: string; password: string; role?: string; quota_total?: number }) =>
    api('/api/users', { method: 'POST', body: JSON.stringify(body) }),
  patchUser: (id: string, body: { role?: string; quota_total?: number; note?: string }) =>
    api(`/api/users/${encodeURIComponent(id)}`, {
      method: 'PATCH',
      body: JSON.stringify(body),
    }),
  createTopup: (userId: string, body: { credit: number; note?: string }) =>
    api(`/api/users/${encodeURIComponent(userId)}/topups`, {
      method: 'POST',
      body: JSON.stringify(body),
    }),
  userTopups: (userId: string) =>
    api(`/api/users/${encodeURIComponent(userId)}/topups`),
  topups: (limit = 100) => api(`/api/topups?limit=${limit}`),
  paymentOrders: (limit = 100) => api(`/api/payment-orders?limit=${limit}`),
}

export const consoleApi = {
  me: () => api('/api/console/me'),
  quota: () => api('/api/console/quota'),
  packages: () => api<any>('/api/console/packages'),
  recharge: (package_id: string, idempotency_key?: string) =>
    api<any>('/api/console/recharge', {
      method: 'POST',
      body: JSON.stringify({ package_id, idempotency_key }),
    }),
  getRecharge: (id: string) => api(`/api/console/recharge/${encodeURIComponent(id)}`),
  mockPay: (id: string) =>
    api(`/api/console/recharge/${encodeURIComponent(id)}/mock-pay`, { method: 'POST', body: '{}' }),
  topups: () => api<any[]>('/api/console/topups'),
  keys: () => api<any[]>('/api/console/keys'),
  createKey: (name: string) =>
    api<{ key: any; secret: string }>('/api/console/keys', {
      method: 'POST',
      body: JSON.stringify({ name }),
    }),
  deleteKey: (id: string) =>
    api(`/api/console/keys/${encodeURIComponent(id)}`, { method: 'DELETE' }),
  setKeyEnabled: (id: string, enabled: boolean) =>
    api(`/api/console/keys/${encodeURIComponent(id)}`, {
      method: 'PATCH',
      body: JSON.stringify({ enabled }),
    }),
  usage: () => api('/api/console/usage'),
  models: () => api<any[]>('/api/console/models'),
}
