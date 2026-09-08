// Subport v1 — views. Each view returns an HTML string; app.js mounts it.

const esc = (s) => String(s).replace(/[&<>"']/g, (c) => (
  { '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]
));

const pct = (n) => Math.round(n * 100) + '%';
const num = (n) => n.toLocaleString('en-US');

function loadBar(v) {
  const cls = v >= 0.75 ? ' is-bad' : v >= 0.5 ? ' is-warn' : '';
  return `<div class="bar${cls}" title="${pct(v)}"><i style="width:${Math.round(v * 100)}%"></i></div>`;
}

const STATE_PILL = {
  healthy:  ['ok',   '正常'],
  degraded: ['warn', '降级'],
  cooling:  ['warn', '冷却中'],
  paused:   ['bad',  '已暂停'],
  disabled: ['off',  '已禁用'],
};

function statePill(state) {
  const [cls, label] = STATE_PILL[state] || ['off', state];
  return `<span class="pill ${cls}">${esc(label)}</span>`;
}

/** Shown when the backend is not reachable, so nobody mistakes demo data for real. */
function demoBanner() {
  return `<div class="banner">
    <span>⚠</span>
    <div><b>演示数据</b> — 后端未连接，下面显示的是本地示例数据，不是真实状态。
    后端起来之后本页会自动切到真实数据。</div>
  </div>`;
}

// ---------------------------------------------------------------- overview

function overviewView(d) {
  const breakRate = d.requests_24h ? d.stream_breaks_24h / d.requests_24h : 0;
  return `
  <div class="stats">
    <div class="stat"><div class="k">账号池</div>
      <div class="v">${d.accounts_healthy}<small>/ ${d.accounts_total} 可用</small></div></div>
    <div class="stat"><div class="k">渠道</div><div class="v">${d.channels_total}</div></div>
    <div class="stat"><div class="k">24h 请求</div><div class="v">${num(d.requests_24h)}</div></div>
    <div class="stat"><div class="k">24h 断流</div>
      <div class="v">${d.stream_breaks_24h}<small>次 · 占 ${(breakRate * 100).toFixed(2)}%</small></div></div>
    <div class="stat"><div class="k">故障转移成功率</div><div class="v">${pct(d.failover_success_rate)}</div></div>
  </div>
  <div class="card">
    <div class="card-head"><h2>第一版说明</h2></div>
    <div class="card-body" style="color:var(--text-dim)">
      <p style="margin-top:0">这是 Subport 第一版。核心目标是把「不断」做对，具体落在两件事上：</p>
      <ul style="margin:0;padding-left:20px">
        <li><b>同档横向换账号</b> — 同一优先级里还有健康账号时，绝不降级到更差的档位。这是对 new-api
            原生「逐档下降」重试行为的修正。</li>
        <li><b>首字节分界</b> — 首字节返回<b>前</b>失败可以换账号重试；首字节返回<b>后</b>失败一律不重放，
            避免客户端看到重复内容。</li>
      </ul>
    </div>
  </div>`;
}

// ---------------------------------------------------------------- accounts

function accountsView(rows) {
  const byTier = {};
  rows.forEach((r) => { (byTier[r.tier] = byTier[r.tier] || []).push(r); });
  const tiers = Object.keys(byTier).sort((a, b) => a - b);

  const cards = tiers.map((t) => {
    const list = byTier[t];
    const up = list.filter((r) => r.state === 'healthy').length;
    return `
    <div class="card">
      <div class="card-head">
        <h2>优先级 ${esc(t)} 档</h2>
        <span class="pill ${up ? 'ok' : 'bad'}">${up} / ${list.length} 可调度</span>
        <div class="spacer"></div>
        <span style="color:var(--text-faint);font-size:12px">本档用尽才会降到下一档</span>
      </div>
      <div class="table-wrap"><table>
        <thead><tr>
          <th>账号</th><th>供应商</th><th>状态</th><th>负载</th>
          <th>冷却</th><th>最近错误</th><th></th>
        </tr></thead>
        <tbody>${list.map((r) => `
          <tr>
            <td><b>${esc(r.label)}</b></td>
            <td style="color:var(--text-dim)">${esc(r.provider)}</td>
            <td>${statePill(r.state)}</td>
            <td style="width:110px">${loadBar(r.load)}</td>
            <td class="mono" style="color:var(--text-dim)">${r.cooldown_until ? esc(r.cooldown_until) : '—'}</td>
            <td style="color:${r.last_error ? 'var(--bad)' : 'var(--text-faint)'}">${r.last_error ? esc(r.last_error) : '—'}</td>
            <td style="text-align:right"><button class="btn sm" disabled title="第一版只读">编辑</button></td>
          </tr>`).join('')}
        </tbody>
      </table></div>
    </div>`;
  }).join('');

  return cards || `<div class="card"><div class="empty">还没有账号</div></div>`;
}

// ---------------------------------------------------------------- channels

function channelsView(rows) {
  if (!rows.length) return `<div class="card"><div class="empty">还没有渠道</div></div>`;
  return `
  <div class="card">
    <div class="card-head"><h2>渠道</h2><div class="spacer"></div>
      <button class="btn sm" disabled title="第一版只读">新建渠道</button></div>
    <div class="table-wrap"><table>
      <thead><tr>
        <th>名称</th><th>供应商</th><th>分组</th><th class="num">优先级</th>
        <th class="num">账号数</th><th>健康度</th><th>模型</th><th>状态</th>
      </tr></thead>
      <tbody>${rows.map((c) => `
        <tr>
          <td><b>${esc(c.name)}</b></td>
          <td style="color:var(--text-dim)">${esc(c.provider)}</td>
          <td><code>${esc(c.group)}</code></td>
          <td class="num">${c.priority}</td>
          <td class="num">${c.accounts}</td>
          <td style="width:120px">${loadBar(c.health)}</td>
          <td style="color:var(--text-dim)">${c.models.map((m) => `<code>${esc(m)}</code>`).join(' ')}</td>
          <td>${c.enabled ? '<span class="pill ok">启用</span>' : '<span class="pill off">停用</span>'}</td>
        </tr>`).join('')}
      </tbody>
    </table></div>
  </div>`;
}

// ---------------------------------------------------------------- keys

function keysView(rows) {
  if (!rows.length) return `<div class="card"><div class="empty">还没有密钥</div></div>`;
  return `
  <div class="card">
    <div class="card-head"><h2>API 密钥</h2><div class="spacer"></div>
      <button class="btn sm primary" disabled title="第一版只读">新建密钥</button></div>
    <div class="table-wrap"><table>
      <thead><tr>
        <th>名称</th><th>密钥</th><th>分组</th><th>额度使用</th>
        <th class="num">已用 / 总额</th><th>最近调用</th><th>状态</th>
      </tr></thead>
      <tbody>${rows.map((k) => {
        const r = k.quota ? k.used / k.quota : 0;
        return `
        <tr>
          <td><b>${esc(k.name)}</b></td>
          <td><code>${esc(k.prefix)}····</code></td>
          <td><code>${esc(k.group)}</code></td>
          <td style="width:130px">${loadBar(Math.min(r, 1))}</td>
          <td class="num" style="color:var(--text-dim)">${num(k.used)} / ${num(k.quota)}</td>
          <td style="color:var(--text-faint)">${esc(k.last_used)}</td>
          <td>${k.enabled ? '<span class="pill ok">启用</span>' : '<span class="pill off">停用</span>'}</td>
        </tr>`;
      }).join('')}
      </tbody>
    </table></div>
  </div>`;
}

// ---------------------------------------------------------------- usage

function usageView(rows) {
  if (!rows.length) return `<div class="card"><div class="empty">还没有用量数据</div></div>`;

  const maxReq = Math.max(...rows.map((r) => r.requests));
  const chron = rows.slice().reverse();          // oldest -> newest, left to right

  const bars = chron.map((r) => {
    const h = Math.max(3, Math.round((r.requests / maxReq) * 100));
    const bad = r.stream_breaks / r.requests > 0.003;
    // height:100% matters: the bar below is sized in %, which needs a resolved
    // parent height or it collapses to zero.
    return `<div style="flex:1;height:100%;display:flex;flex-direction:column;justify-content:flex-end;align-items:center;gap:6px;min-width:0">
      <div title="${num(r.requests)} 次请求 / ${r.stream_breaks} 次断流"
           style="width:100%;max-width:46px;height:${h}%;border-radius:4px 4px 0 0;
                  background:${bad ? 'var(--bad)' : 'var(--accent)'};opacity:.9"></div>
      <div style="font-size:11px;color:var(--text-faint);white-space:nowrap">${esc(r.date)}</div>
    </div>`;
  }).join('');

  return `
  <div class="card">
    <div class="card-head"><h2>近 7 天请求量</h2><div class="spacer"></div>
      <span style="font-size:12px;color:var(--text-faint)">红色 = 断流率超过 0.3%</span></div>
    <div class="card-body">
      <div style="display:flex;gap:8px;align-items:flex-end;height:150px">${bars}</div>
    </div>
  </div>
  <div class="card">
    <div class="card-head"><h2>明细</h2></div>
    <div class="table-wrap"><table>
      <thead><tr>
        <th>日期</th><th class="num">请求</th><th class="num">Token</th>
        <th class="num">断流</th><th class="num">断流率</th>
        <th class="num">故障转移</th><th class="num">平均首字节</th>
      </tr></thead>
      <tbody>${rows.map((r) => {
        const br = r.stream_breaks / r.requests;
        return `
        <tr>
          <td><b>${esc(r.date)}</b></td>
          <td class="num">${num(r.requests)}</td>
          <td class="num">${num(r.tokens)}</td>
          <td class="num">${r.stream_breaks}</td>
          <td class="num" style="color:${br > 0.003 ? 'var(--bad)' : 'var(--text-dim)'}">${(br * 100).toFixed(2)}%</td>
          <td class="num">${r.failovers}</td>
          <td class="num" style="color:${r.avg_ttfb_ms > 500 ? 'var(--warn)' : 'var(--text-dim)'}">${r.avg_ttfb_ms} ms</td>
        </tr>`;
      }).join('')}
      </tbody>
    </table></div>
  </div>`;
}

// ---------------------------------------------------------------- login

function loginView(err) {
  return `
  <div class="login-wrap">
    <form class="login-card" id="login-form">
      <div class="brand"><span class="brand-dot"></span><span>Subport</span></div>
      <div class="tag">Agent 账号转 API 网关</div>
      ${err ? `<div class="login-err">${esc(err)}</div>` : ''}
      <div class="field">
        <label for="u">用户名</label>
        <input id="u" name="username" type="text" autocomplete="username" required autofocus>
      </div>
      <div class="field">
        <label for="p">密码</label>
        <input id="p" name="password" type="password" autocomplete="current-password" required>
      </div>
      <button class="btn primary" type="submit">登录</button>
      <div class="hint" style="text-align:center;margin-top:14px;color:var(--text-faint);font-size:12px">
        后端未连接时会进入演示模式
      </div>
    </form>
  </div>`;
}
