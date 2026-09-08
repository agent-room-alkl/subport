// Subport user console — API client.
// Talks to /api/console/*, which is scoped server-side to the signed-in user.
// No user id is ever sent from here; the session decides whose data comes back.

var token = null;
try { token = localStorage.getItem('subport_console_token'); } catch (_) { /* private mode */ }

/** Statuses that mean "no Subport backend here", not "your request was rejected". */
const NO_BACKEND_CODES = new Set([404, 405, 501]);

/** True when demo data is being shown because no backend answered. */
var usingDemo = false;

function getToken() { return token; }

function setToken(t) {
  token = t;
  try {
    if (t) localStorage.setItem('subport_console_token', t);
    else localStorage.removeItem('subport_console_token');
  } catch (_) { /* storage blocked; session-only login still works */ }
}

async function req(path, opts = {}) {
  const headers = Object.assign({ 'Content-Type': 'application/json' }, opts.headers || {});
  if (token) headers['Authorization'] = 'Bearer ' + token;
  const res = await fetch(path, Object.assign({}, opts, { headers }));
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

async function withFallback(path, demoFn) {
  try {
    const out = await req(path);
    usingDemo = false;
    return out;
  } catch (e) {
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
      if (e.status && !NO_BACKEND_CODES.has(e.status)) throw e;
      usingDemo = true;
      setToken('demo-session');
      return { token: 'demo-session', user: { username, role: 'user' } };
    }
  },

  async register(username, password, inviteCode) {
    // Sent snake_case to match every response this API emits. The backend
    // accepts both spellings, but consistency here keeps the contract honest.
    try {
      const out = await req('/api/auth/register', {
        method: 'POST',
        body: JSON.stringify({ username, password, invite_code: inviteCode }),
      });
      usingDemo = false;
      setToken(out.token);
      return out;
    } catch (e) {
      if (e.status && !NO_BACKEND_CODES.has(e.status)) throw e;
      usingDemo = true;
      setToken('demo-session');
      return { token: 'demo-session', user: { username, role: 'user' } };
    }
  },

  logout() { setToken(null); },

  me:    () => withFallback('/api/console/me',    () => demo.me),
  quota: () => withFallback('/api/console/quota', () => demo.quota),
  keys:  () => withFallback('/api/console/keys',  () => demo.keys),
  usage: () => withFallback('/api/console/usage', () => demo.usage),

  async createKey(name) {
    try {
      const out = await req('/api/console/keys', {
        method: 'POST',
        body: JSON.stringify({ name }),
      });
      usingDemo = false;
      return out;
    } catch (e) {
      if (e.status && !NO_BACKEND_CODES.has(e.status)) throw e;
      usingDemo = true;
      const fake = 'sk-sp-' + Math.random().toString(16).slice(2).padEnd(48, '0').slice(0, 48);
      const k = {
        id: 'key_demo_' + Date.now(), name: name, prefix: fake.slice(0, 12),
        enabled: true, created_at: new Date().toISOString(), last_used: 'never',
      };
      demo.keys.unshift(k);
      return { key: k, secret: fake };
    }
  },

  async setKeyEnabled(id, enabled) {
    try {
      await req('/api/console/keys/' + encodeURIComponent(id), {
        method: 'PATCH',
        body: JSON.stringify({ enabled }),
      });
      usingDemo = false;
    } catch (e) {
      if (e.status && !NO_BACKEND_CODES.has(e.status)) throw e;
      usingDemo = true;
      const k = demo.keys.find((x) => x.id === id);
      if (k) k.enabled = enabled;
    }
  },

  async deleteKey(id) {
    try {
      await req('/api/console/keys/' + encodeURIComponent(id), { method: 'DELETE' });
      usingDemo = false;
    } catch (e) {
      if (e.status && !NO_BACKEND_CODES.has(e.status)) throw e;
      usingDemo = true;
      demo.keys = demo.keys.filter((x) => x.id !== id);
    }
  },
};

/** Base URL for the integration guide. Read from the page, never hardcoded -
 *  a copied curl that points at the wrong host is worse than no example. */
function baseURL() {
  return window.location.origin;
}

// ---------------------------------------------------------------------------
// Demo dataset. Same shape as the real /api/console/* responses, so swapping
// in the live backend changes nothing in the views.
// ---------------------------------------------------------------------------

const demo = {
  me: { id: 'usr_demo', username: 'demo', role: 'user', quota_total: 1000000, quota_used: 412300 },
  quota: { quota_total: 1000000, quota_used: 412300, remaining: 587700 },
  keys: [
    { id: 'key_1', name: '生产环境', prefix: 'sk-sp-a41f3c', enabled: true,  created_at: '2026-08-20T10:00:00Z', last_used: '2 分钟前' },
    { id: 'key_2', name: '本地开发', prefix: 'sk-sp-77c2e1', enabled: true,  created_at: '2026-08-28T09:12:00Z', last_used: '1 小时前' },
    { id: 'key_3', name: '已停用',   prefix: 'sk-sp-d3a8b0', enabled: false, created_at: '2026-07-02T14:30:00Z', last_used: '3 天前' },
  ],
  usage: [
    { id: 'u1', created_at: '2026-09-08T11:42:00Z', model: 'gpt-4o',           tokens: 1820, cost: 182, status: 'success',       attempts: 1, stream_broken: false },
    { id: 'u2', created_at: '2026-09-08T11:31:00Z', model: 'claude-sonnet-4',  tokens: 3410, cost: 341, status: 'success',       attempts: 2, stream_broken: false },
    { id: 'u3', created_at: '2026-09-08T11:08:00Z', model: 'gpt-4o',           tokens:  640, cost:  64, status: 'stream_broken', attempts: 1, stream_broken: true,  compensated: false },
    { id: 'u6', created_at: '2026-09-08T09:14:00Z', model: 'gpt-4o',           tokens:  980, cost:  98, status: 'stream_broken', attempts: 1, stream_broken: true,  compensated: true  },
    { id: 'u4', created_at: '2026-09-08T10:55:00Z', model: 'gpt-4o-mini',      tokens:  210, cost:  21, status: 'success',       attempts: 1, stream_broken: false },
    { id: 'u5', created_at: '2026-09-08T10:31:00Z', model: 'claude-opus-4',    tokens:    0, cost:   0, status: 'failed',        attempts: 3, stream_broken: false },
  ],
};
