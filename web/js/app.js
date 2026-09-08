// Subport v1 — app shell and router. No framework, no build step.

// Globals come from api.js and views.js, loaded before this file.

const PAGES = [
  { id: 'overview', ico: '◈', label: '总览',   title: '总览',     sub: '账号池与稳定性概况' },
  { id: 'accounts', ico: '◉', label: '账号池', title: '账号池',   sub: '按优先级分档，同档内横向调度' },
  { id: 'channels', ico: '⇄', label: '渠道',   title: '渠道',     sub: '模型路由与分组' },
  { id: 'keys',     ico: '⚿', label: '密钥',   title: 'API 密钥', sub: '额度与访问控制' },
  { id: 'usage',    ico: '▤', label: '用量',   title: '用量',     sub: '请求量、断流与故障转移' },
];

const root = document.getElementById('app');
let current = 'overview';

// ---------------------------------------------------------------- theme

function initTheme() {
  let saved = null;
  try { saved = localStorage.getItem('subport_theme'); } catch (_) { /* blocked */ }
  if (saved) document.documentElement.setAttribute('data-theme', saved);
}

function toggleTheme() {
  const el = document.documentElement;
  const now = el.getAttribute('data-theme');
  const sysDark = window.matchMedia('(prefers-color-scheme: dark)').matches;
  const next = now ? (now === 'dark' ? 'light' : 'dark') : (sysDark ? 'light' : 'dark');
  el.setAttribute('data-theme', next);
  try { localStorage.setItem('subport_theme', next); } catch (_) { /* blocked */ }
}

// ---------------------------------------------------------------- login

function renderLogin(err) {
  root.innerHTML = loginView(err);
  document.getElementById('login-form').addEventListener('submit', async (e) => {
    e.preventDefault();
    const btn = e.target.querySelector('button[type=submit]');
    btn.disabled = true;
    btn.textContent = '登录中…';
    const fd = new FormData(e.target);
    try {
      await api.login(fd.get('username'), fd.get('password'));
      renderShell();
      go(current);
    } catch (ex) {
      renderLogin(ex.message || '登录失败');
    }
  });
}

// ---------------------------------------------------------------- shell

function renderShell() {
  root.innerHTML = `
  <div class="shell">
    <nav class="sidebar">
      <div class="brand"><span class="brand-dot"></span><span>Subport</span></div>
      ${PAGES.map((p) => `
        <button class="nav-item" data-go="${p.id}" aria-label="${p.label}">
          <span class="ico" aria-hidden="true">${p.ico}</span><span>${p.label}</span>
        </button>`).join('')}
      <div class="sidebar-foot">
        <button class="nav-item" id="theme-btn" aria-label="切换主题">
          <span class="ico" aria-hidden="true">◐</span><span>切换主题</span></button>
        <button class="nav-item" id="logout-btn" aria-label="退出">
          <span class="ico" aria-hidden="true">⏻</span><span>退出</span></button>
      </div>
    </nav>
    <div class="main">
      <header class="topbar">
        <h1 id="pg-title"></h1>
        <span class="sub" id="pg-sub"></span>
        <div class="spacer"></div>
        <button class="btn sm" id="refresh-btn">刷新</button>
      </header>
      <div class="content" id="content"></div>
    </div>
  </div>`;

  root.querySelectorAll('[data-go]').forEach((b) => {
    b.addEventListener('click', () => go(b.dataset.go));
  });
  document.getElementById('theme-btn').addEventListener('click', toggleTheme);
  document.getElementById('refresh-btn').addEventListener('click', () => go(current));
  document.getElementById('logout-btn').addEventListener('click', () => {
    api.logout();
    renderLogin(null);
  });
}

// ---------------------------------------------------------------- routing

async function go(id) {
  current = id;
  const page = PAGES.find((p) => p.id === id) || PAGES[0];

  document.getElementById('pg-title').textContent = page.title;
  document.getElementById('pg-sub').textContent = page.sub;
  root.querySelectorAll('[data-go]').forEach((b) => {
    if (b.dataset.go === id) b.setAttribute('aria-current', 'page');
    else b.removeAttribute('aria-current');
  });

  const box = document.getElementById('content');
  box.innerHTML = `<div class="card"><div class="empty">加载中…</div></div>`;

  try {
    let html;
    if (id === 'overview')      html = overviewView(await api.overview());
    else if (id === 'accounts') html = accountsView(await api.accounts());
    else if (id === 'channels') html = channelsView(await api.channels());
    else if (id === 'keys')     html = keysView(await api.keys());
    else                        html = usageView(await api.usage());

    // api.js sets the global usingDemo during the call above.
    box.innerHTML = (usingDemo ? demoBanner() : '') + html;
  } catch (e) {
    box.innerHTML = `<div class="card"><div class="empty" style="color:var(--bad)">
      加载失败：${e.message || e}</div></div>`;
  }
}

// ---------------------------------------------------------------- boot

initTheme();
if (getToken()) { renderShell(); go('overview'); }
else { renderLogin(null); }
