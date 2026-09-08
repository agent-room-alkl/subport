// Subport user console — shell and router.
// Globals come from api.js and views.js, loaded before this file.

const PAGES = [
  { id: 'overview', ico: '◈', label: '总览',   title: '总览',     sub: '额度与密钥概况' },
  { id: 'keys',     ico: '⚿', label: '密钥',   title: 'API 密钥', sub: '创建、停用、删除' },
  { id: 'usage',    ico: '▤', label: '调用记录', title: '调用记录', sub: '时间、模型、花费、结果' },
  { id: 'topup',    ico: '＋', label: '充值',   title: '充值与兑换', sub: '兑换码与额度补充' },
  { id: 'guide',    ico: '❯', label: '接入指引', title: '接入指引', sub: '一段能直接复制的示例' },
];

const root = document.getElementById('app');
let current = 'overview';
let authMode = 'login';

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

// ---------------------------------------------------------------- copy

async function copyText(text, btn) {
  const original = btn.textContent;
  try {
    await navigator.clipboard.writeText(text);
    btn.textContent = '已复制';
  } catch (_) {
    // Clipboard API needs a secure context; fall back so http:// still works.
    const ta = document.createElement('textarea');
    ta.value = text;
    ta.style.position = 'fixed';
    ta.style.opacity = '0';
    document.body.appendChild(ta);
    ta.select();
    let ok = false;
    try { ok = document.execCommand('copy'); } catch (_) { ok = false; }
    document.body.removeChild(ta);
    btn.textContent = ok ? '已复制' : '复制失败，请手动选中';
  }
  setTimeout(() => { btn.textContent = original; }, 1800);
}

/** One delegated handler, so buttons rendered later still copy. */
function wireCopy(container) {
  container.addEventListener('click', (e) => {
    const btn = e.target.closest('[data-copy]');
    if (btn) copyText(btn.getAttribute('data-copy'), btn);
  });
}

// ---------------------------------------------------------------- auth

function renderAuth(err) {
  root.innerHTML = loginView(authMode, err);

  document.getElementById('switch-mode').addEventListener('click', (e) => {
    e.preventDefault();
    authMode = authMode === 'login' ? 'register' : 'login';
    renderAuth(null);
  });

  document.getElementById('auth-form').addEventListener('submit', async (e) => {
    e.preventDefault();
    const btn = e.target.querySelector('button[type=submit]');
    btn.disabled = true;
    btn.textContent = '处理中…';
    const fd = new FormData(e.target);
    try {
      if (authMode === 'register') {
        await api.register(fd.get('username'), fd.get('password'), fd.get('invite'));
      } else {
        await api.login(fd.get('username'), fd.get('password'));
      }
      renderShell();
      go(current);
    } catch (ex) {
      renderAuth(ex.message || '操作失败');
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
    authMode = 'login';
    renderAuth(null);
  });

  wireCopy(document.getElementById('content'));
}

// ---------------------------------------------------------------- keys page

function wireKeys(box) {
  const btn = document.getElementById('new-key-btn');
  if (btn) {
    btn.addEventListener('click', async () => {
      const name = prompt('给这个密钥起个名字', '新密钥');
      if (name === null) return;
      btn.disabled = true;
      try {
        const out = await api.createKey(name.trim() || '新密钥');
        await go('keys');
        // Render the one-time secret above the table.
        const slot = document.getElementById('new-key-out');
        if (slot) {
          slot.innerHTML = newKeyBanner(out.key.name, out.secret);
          slot.scrollIntoView({ behavior: 'smooth', block: 'nearest' });
        }
      } catch (e) {
        alert('创建失败：' + (e.message || e));
        btn.disabled = false;
      }
    });
  }

  box.querySelectorAll('[data-toggle]').forEach((b) => {
    b.addEventListener('click', async () => {
      b.disabled = true;
      await api.setKeyEnabled(b.dataset.toggle, b.dataset.enabled !== 'true');
      go('keys');
    });
  });

  box.querySelectorAll('[data-del]').forEach((b) => {
    b.addEventListener('click', async () => {
      if (!confirm('删除后使用这个密钥的程序会立刻失效，确定删除？')) return;
      b.disabled = true;
      await api.deleteKey(b.dataset.del);
      go('keys');
    });
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
    if (id === 'overview') {
      const [q, keys] = await Promise.all([api.quota(), api.keys()]);
      html = overviewView(q, keys);
    } else if (id === 'keys') {
      html = keysView(await api.keys());
    } else if (id === 'usage') {
      html = usageView(await api.usage());
    } else if (id === 'topup') {
      html = topupView();
    } else {
      html = guideView(baseURL());
    }

    box.innerHTML = (usingDemo ? demoBanner() : '') + html;
    if (id === 'keys') wireKeys(box);
  } catch (e) {
    box.innerHTML = `<div class="card"><div class="empty" style="color:var(--bad)">
      加载失败：${e.message || e}</div></div>`;
  }
}

// ---------------------------------------------------------------- boot

initTheme();
if (getToken()) { renderShell(); go('overview'); }
else { renderAuth(null); }
