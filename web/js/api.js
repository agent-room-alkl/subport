// Subport v1 — API client.
// Talks to the Go backend. When the backend is not up (or not built yet),
// falls back to a local demo dataset so the UI is reviewable on its own.

const BASE = '';           // same-origin; the Go server serves this UI
let token = null;

try { token = localStorage.getItem('subport_token'); } catch (_) { /* private mode */ }

function getToken() { return token; }

function setToken(t) {
  token = t;
  try {
    if (t) localStorage.setItem('subport_token', t);
    else localStorage.removeItem('subport_token');
  } catch (_) { /* storage blocked; session-only login still works */ }
}

/** True when we are serving demo data because the backend did not answer. */
var usingDemo = false;

/** Statuses that mean "no Subport backend here", not "your request was rejected". */
const NO_BACKEND_CODES = new Set([404, 405, 501]);

async function req(path, opts = {}) {
  const headers = Object.assign({ 'Content-Type': 'application/json' }, opts.headers || {});
  if (token) headers['Authorization'] = 'Bearer ' + token;
  const res = await fetch(BASE + path, Object.assign({}, opts, { headers }));
  if (!res.ok) {
    let msg = res.status + ' ' + res.statusText;
    try {
      const body = await res.json();
      if (body && body.error) msg = body.error;
    } catch (_) { /* non-JSON error body */ }
    const err = new Error(msg);
    err.status = res.status;
    throw err;
  }
  return res.status === 204 ? null : res.json();
}

/** Try the real backend; on network failure serve demo data and flag it. */
async function withFallback(path, demoFn) {
  try {
    const out = await req(path);
    usingDemo = false;
    return out;
  } catch (e) {
    // Same rule as login: only a genuine backend error surfaces to the user.
    if (e.status && !NO_BACKEND_CODES.has(e.status)) throw e;
    usingDemo = true;
    return demoFn();
  }
}

const api = {
  async login(username, password) {
    try {
      const out = await req('/api/auth/login', {
        method: 'POST',
        body: JSON.stringify({ username, password }),
      });
      usingDemo = false;
      setToken(out.token);
      return out;
    } catch (e) {
      // A real auth rejection (401/403) or a server fault must surface as-is.
      // Everything else means "there is no Subport backend at this origin":
      // no status at all = connection refused; 404/405/501 = the page is being
      // served by a plain static server that cannot answer POST. In those cases
      // fall back to a demo session so the UI stays reviewable on its own.
      if (e.status && !NO_BACKEND_CODES.has(e.status)) throw e;
      usingDemo = true;
      setToken('demo-session');
      return { token: 'demo-session', user: { username, role: 'admin' } };
    }
  },

  logout() { setToken(null); },

  accounts: () => withFallback('/api/accounts', () => demo.accounts),
  channels: () => withFallback('/api/channels', () => demo.channels),
  keys:     () => withFallback('/api/keys',     () => demo.keys),
  usage:    () => withFallback('/api/usage',    () => demo.usage),
  overview: () => withFallback('/api/overview', () => demo.overview),
};

// ---------------------------------------------------------------------------
// Demo dataset. Shape mirrors the v1 backend contract exactly, so swapping in
// the real API is a no-op for every view below.
// ---------------------------------------------------------------------------

const demo = {
  overview: {
    accounts_total: 12,
    accounts_healthy: 9,
    channels_total: 4,
    requests_24h: 18432,
    stream_breaks_24h: 37,
    failover_success_rate: 0.982,
  },
  accounts: [
    { id: 1,  label: 'openai-pool-01', provider: 'openai', tier: 1, state: 'healthy',   load: 0.42, cooldown_until: null,   last_error: null,               consecutive_timeouts: 0, consecutive_403: 0 },
    { id: 2,  label: 'openai-pool-02', provider: 'openai', tier: 1, state: 'healthy',   load: 0.61, cooldown_until: null,   last_error: null,               consecutive_timeouts: 0, consecutive_403: 0 },
    { id: 3,  label: 'openai-pool-03', provider: 'openai', tier: 1, state: 'cooling',   load: 0.00, cooldown_until: '4m12s', last_error: '429 rate_limit',   consecutive_timeouts: 0, consecutive_403: 0 },
    { id: 4,  label: 'openai-pool-04', provider: 'openai', tier: 2, state: 'healthy',   load: 0.18, cooldown_until: null,   last_error: null,               consecutive_timeouts: 0, consecutive_403: 0 },
    { id: 5,  label: 'claude-pool-01', provider: 'anthropic', tier: 1, state: 'healthy', load: 0.55, cooldown_until: null,  last_error: null,               consecutive_timeouts: 0, consecutive_403: 0 },
    { id: 6,  label: 'claude-pool-02', provider: 'anthropic', tier: 1, state: 'degraded', load: 0.77, cooldown_until: null, last_error: 'first-byte timeout', consecutive_timeouts: 2, consecutive_403: 0 },
    { id: 7,  label: 'claude-pool-03', provider: 'anthropic', tier: 2, state: 'healthy', load: 0.09, cooldown_until: null,  last_error: null,               consecutive_timeouts: 0, consecutive_403: 0 },
    { id: 8,  label: 'gemini-pool-01', provider: 'google',  tier: 1, state: 'healthy',   load: 0.31, cooldown_until: null,  last_error: null,               consecutive_timeouts: 0, consecutive_403: 0 },
    { id: 9,  label: 'gemini-pool-02', provider: 'google',  tier: 1, state: 'paused',    load: 0.00, cooldown_until: null,  last_error: '403 forbidden x5', consecutive_timeouts: 0, consecutive_403: 5 },
    { id: 10, label: 'gemini-pool-03', provider: 'google',  tier: 2, state: 'healthy',   load: 0.24, cooldown_until: null,  last_error: null,               consecutive_timeouts: 0, consecutive_403: 0 },
    { id: 11, label: 'grok-pool-01',   provider: 'xai',     tier: 2, state: 'healthy',   load: 0.12, cooldown_until: null,  last_error: null,               consecutive_timeouts: 0, consecutive_403: 0 },
    { id: 12, label: 'grok-pool-02',   provider: 'xai',     tier: 3, state: 'cooling',   load: 0.00, cooldown_until: '1m03s', last_error: '502 bad gateway', consecutive_timeouts: 1, consecutive_403: 0 },
  ],
  channels: [
    { id: 1, name: 'OpenAI 主通道',   provider: 'openai',    priority: 100, group: 'default', models: ['gpt-4o', 'gpt-4o-mini'],              enabled: true,  accounts: 4, health: 0.93 },
    { id: 2, name: 'Anthropic 主通道', provider: 'anthropic', priority: 100, group: 'default', models: ['claude-sonnet-4', 'claude-opus-4'],   enabled: true,  accounts: 3, health: 0.81 },
    { id: 3, name: 'Gemini 备用',     provider: 'google',    priority: 50,  group: 'default', models: ['gemini-2.5-pro'],                      enabled: true,  accounts: 3, health: 0.67 },
    { id: 4, name: 'Grok 实验',       provider: 'xai',       priority: 10,  group: 'beta',    models: ['grok-4'],                              enabled: false, accounts: 2, health: 0.50 },
  ],
  keys: [
    { id: 1, name: '生产环境',   prefix: 'sk-sp-a41f', group: 'default', quota: 1000000, used: 412300, enabled: true,  last_used: '2 分钟前' },
    { id: 2, name: '内部测试',   prefix: 'sk-sp-77c2', group: 'default', quota: 200000,  used: 189440, enabled: true,  last_used: '18 分钟前' },
    { id: 3, name: 'CI 流水线',  prefix: 'sk-sp-0b9e', group: 'default', quota: 50000,   used: 6120,   enabled: true,  last_used: '1 小时前' },
    { id: 4, name: '已停用的 key', prefix: 'sk-sp-d3a8', group: 'beta',  quota: 100000,  used: 100000, enabled: false, last_used: '3 天前' },
  ],
  usage: [
    { date: '09-08', requests: 18432, tokens: 7412000, stream_breaks: 37, failovers: 214, avg_ttfb_ms: 412 },
    { date: '09-07', requests: 17980, tokens: 7133000, stream_breaks: 52, failovers: 301, avg_ttfb_ms: 487 },
    { date: '09-06', requests: 16204, tokens: 6489000, stream_breaks: 29, failovers: 176, avg_ttfb_ms: 398 },
    { date: '09-05', requests: 19110, tokens: 7802000, stream_breaks: 88, failovers: 495, avg_ttfb_ms: 623 },
    { date: '09-04', requests: 15332, tokens: 6011000, stream_breaks: 21, failovers: 142, avg_ttfb_ms: 371 },
    { date: '09-03', requests: 14887, tokens: 5820000, stream_breaks: 18, failovers: 130, avg_ttfb_ms: 365 },
    { date: '09-02', requests: 15901, tokens: 6244000, stream_breaks: 25, failovers: 158, avg_ttfb_ms: 389 },
  ],
};
