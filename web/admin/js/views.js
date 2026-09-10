// Subport v1 â€” views. Each view returns an HTML string; app.js mounts it.

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
  healthy:  ['ok',   'æ­£å¸¸'],
  degraded: ['warn', 'é™çº§'],
  cooling:  ['warn', 'å†·å´ä¸­'],
  paused:   ['bad',  'å·²æš‚åœ'],
  disabled: ['off',  'å·²ç¦ç”¨'],
};

function statePill(state) {
  const [cls, label] = STATE_PILL[state] || ['off', state];
  return `<span class="pill ${cls}">${esc(label)}</span>`;
}

/** Shown when the backend is not reachable, so nobody mistakes demo data for real. */
function demoBanner() {
  return `<div class="banner">
    <span>âš </span>
    <div><b>æ¼”ç¤ºæ•°æ®</b> â€” åŽç«¯æœªè¿žæŽ¥ï¼Œä¸‹é¢æ˜¾ç¤ºçš„æ˜¯æœ¬åœ°ç¤ºä¾‹æ•°æ®ï¼Œä¸æ˜¯çœŸå®žçŠ¶æ€ã€‚
    åŽç«¯èµ·æ¥ä¹‹åŽæœ¬é¡µä¼šè‡ªåŠ¨åˆ‡åˆ°çœŸå®žæ•°æ®ã€‚</div>
  </div>`;
}

// ---------------------------------------------------------------- overview

function overviewView(d) {
  const breakRate = d.requests_24h ? d.stream_breaks_24h / d.requests_24h : 0;
  return `
  <div class="stats">
    <div class="stat"><div class="k">è´¦å·æ± </div>
      <div class="v">${d.accounts_healthy}<small>/ ${d.accounts_total} å¯ç”¨</small></div></div>
    <div class="stat"><div class="k">æ¸ é“</div><div class="v">${d.channels_total}</div></div>
    <div class="stat"><div class="k">24h è¯·æ±‚</div><div class="v">${num(d.requests_24h)}</div></div>
    <div class="stat"><div class="k">24h æ–­æµ</div>
      <div class="v">${d.stream_breaks_24h}<small>æ¬¡ Â· å  ${(breakRate * 100).toFixed(2)}%</small></div></div>
    <div class="stat"><div class="k">æ•…éšœè½¬ç§»æˆåŠŸçŽ‡</div><div class="v">${pct(d.failover_success_rate)}</div></div>
  </div>
  <div class="card">
    <div class="card-head"><h2>ç¬¬ä¸€ç‰ˆè¯´æ˜Ž</h2></div>
    <div class="card-body" style="color:var(--text-dim)">
      <p style="margin-top:0">è¿™æ˜¯ Subport ç¬¬ä¸€ç‰ˆã€‚æ ¸å¿ƒç›®æ ‡æ˜¯æŠŠã€Œä¸æ–­ã€åšå¯¹ï¼Œå…·ä½“è½åœ¨ä¸¤ä»¶äº‹ä¸Šï¼š</p>
      <ul style="margin:0;padding-left:20px">
        <li><b>åŒæ¡£æ¨ªå‘æ¢è´¦å·</b> â€” åŒä¸€ä¼˜å…ˆçº§é‡Œè¿˜æœ‰å¥åº·è´¦å·æ—¶ï¼Œç»ä¸é™çº§åˆ°æ›´å·®çš„æ¡£ä½ã€‚è¿™æ˜¯å¯¹ new-api
            åŽŸç”Ÿã€Œé€æ¡£ä¸‹é™ã€é‡è¯•è¡Œä¸ºçš„ä¿®æ­£ã€‚</li>
        <li><b>é¦–å­—èŠ‚åˆ†ç•Œ</b> â€” é¦–å­—èŠ‚è¿”å›ž<b>å‰</b>å¤±è´¥å¯ä»¥æ¢è´¦å·é‡è¯•ï¼›é¦–å­—èŠ‚è¿”å›ž<b>åŽ</b>å¤±è´¥ä¸€å¾‹ä¸é‡æ”¾ï¼Œ
            é¿å…å®¢æˆ·ç«¯çœ‹åˆ°é‡å¤å†…å®¹ã€‚</li>
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
        <h2>ä¼˜å…ˆçº§ ${esc(t)} æ¡£</h2>
        <span class="pill ${up ? 'ok' : 'bad'}">${up} / ${list.length} å¯è°ƒåº¦</span>
        <div class="spacer"></div>
        <span style="color:var(--text-faint);font-size:12px">æœ¬æ¡£ç”¨å°½æ‰ä¼šé™åˆ°ä¸‹ä¸€æ¡£</span>
      </div>
      <div class="table-wrap"><table>
        <thead><tr>
          <th>è´¦å·</th><th>ä¾›åº”å•†</th><th>çŠ¶æ€</th><th>è´Ÿè½½</th>
          <th>å†·å´</th><th>æœ€è¿‘é”™è¯¯</th><th></th>
        </tr></thead>
        <tbody>${list.map((r) => `
          <tr>
            <td><b>${esc(r.label)}</b></td>
            <td style="color:var(--text-dim)">${esc(r.provider)}</td>
            <td>${statePill(r.state)}</td>
            <td style="width:110px">${loadBar(r.load)}</td>
            <td class="mono" style="color:var(--text-dim)">${r.cooldown_until ? esc(r.cooldown_until) : 'â€”'}</td>
            <td style="color:${r.last_error ? 'var(--bad)' : 'var(--text-faint)'}">${r.last_error ? esc(r.last_error) : 'â€”'}</td>
            <td style="text-align:right"><button class="btn sm" disabled title="ç¬¬ä¸€ç‰ˆåªè¯»">ç¼–è¾‘</button></td>
          </tr>`).join('')}
        </tbody>
      </table></div>
    </div>`;
  }).join('');

  return cards || `<div class="card"><div class="empty">è¿˜æ²¡æœ‰è´¦å·</div></div>`;
}

// ---------------------------------------------------------------- channels

function channelsView(rows) {
  if (!rows.length) return `<div class="card"><div class="empty">è¿˜æ²¡æœ‰æ¸ é“</div></div>`;
  return `
  <div class="card">
    <div class="card-head"><h2>æ¸ é“</h2><div class="spacer"></div>
      <button class="btn sm" disabled title="ç¬¬ä¸€ç‰ˆåªè¯»">æ–°å»ºæ¸ é“</button></div>
    <div class="table-wrap"><table>
      <thead><tr>
        <th>åç§°</th><th>ä¾›åº”å•†</th><th>åˆ†ç»„</th><th class="num">ä¼˜å…ˆçº§</th>
        <th class="num">è´¦å·æ•°</th><th>å¥åº·åº¦</th><th>æ¨¡åž‹</th><th>çŠ¶æ€</th>
      </tr></thead>
      <tbody>${rows.map((c) => `
        <tr>
          <td><b>${esc(c.name)}</b></td>
          <td style="color:var(--text-dim)">${esc(c.provider)}</td>
          <td><code>${esc(c.group)}</code></td>
          <td class="num">${c.priority}</td>
          <td class="num">${c.accounts}</td>
          <td style="width:120px">${loadBar(c.health)}</td>
          <td style="color:var(--text-dim)">${(c.models||[]).map((m) => `<code>${esc(m)}</code>`).join(' ')}</td>
          <td>${c.enabled ? '<span class="pill ok">å¯ç”¨</span>' : '<span class="pill off">åœç”¨</span>'}</td>
        </tr>`).join('')}
      </tbody>
    </table></div>
  </div>`;
}

// ---------------------------------------------------------------- keys

function keysView(rows) {
  if (!rows.length) return `<div class="card"><div class="empty">è¿˜æ²¡æœ‰å¯†é’¥</div></div>`;
  return `
  <div class="card">
    <div class="card-head"><h2>API å¯†é’¥</h2><div class="spacer"></div>
      <button class="btn sm primary" disabled title="ç¬¬ä¸€ç‰ˆåªè¯»">æ–°å»ºå¯†é’¥</button></div>
    <div class="table-wrap"><table>
      <thead><tr>
        <th>åç§°</th><th>å¯†é’¥</th><th>åˆ†ç»„</th><th>é¢åº¦ä½¿ç”¨</th>
        <th class="num">å·²ç”¨ / æ€»é¢</th><th>æœ€è¿‘è°ƒç”¨</th><th>çŠ¶æ€</th>
      </tr></thead>
      <tbody>${rows.map((k) => {
        const r = k.quota ? k.used / k.quota : 0;
        return `
        <tr>
          <td><b>${esc(k.name)}</b></td>
          <td><code>${esc(k.prefix)}Â·Â·Â·Â·</code></td>
          <td><code>${esc(k.group)}</code></td>
          <td style="width:130px">${loadBar(Math.min(r, 1))}</td>
          <td class="num" style="color:var(--text-dim)">${num(k.used)} / ${num(k.quota)}</td>
          <td style="color:var(--text-faint)">${esc(k.last_used)}</td>
          <td>${k.enabled ? '<span class="pill ok">å¯ç”¨</span>' : '<span class="pill off">åœç”¨</span>'}</td>
        </tr>`;
      }).join('')}
      </tbody>
    </table></div>
  </div>`;
}

// ---------------------------------------------------------------- usage

function usageView(rows) {
  if (!rows.length) return `<div class="card"><div class="empty">è¿˜æ²¡æœ‰ç”¨é‡æ•°æ®</div></div>`;

  const maxReq = Math.max(...rows.map((r) => r.requests));
  const chron = rows.slice().reverse();          // oldest -> newest, left to right

  const bars = chron.map((r) => {
    const h = Math.max(3, Math.round((r.requests / maxReq) * 100));
    const bad = r.stream_breaks / r.requests > 0.003;
    // height:100% matters: the bar below is sized in %, which needs a resolved
    // parent height or it collapses to zero.
    return `<div style="flex:1;height:100%;display:flex;flex-direction:column;justify-content:flex-end;align-items:center;gap:6px;min-width:0">
      <div title="${num(r.requests)} æ¬¡è¯·æ±‚ / ${r.stream_breaks} æ¬¡æ–­æµ"
           style="width:100%;max-width:46px;height:${h}%;border-radius:4px 4px 0 0;
                  background:${bad ? 'var(--bad)' : 'var(--accent)'};opacity:.9"></div>
      <div style="font-size:11px;color:var(--text-faint);white-space:nowrap">${esc(r.date)}</div>
    </div>`;
  }).join('');

  return `
  <div class="card">
    <div class="card-head"><h2>è¿‘ 7 å¤©è¯·æ±‚é‡</h2><div class="spacer"></div>
      <span style="font-size:12px;color:var(--text-faint)">çº¢è‰² = æ–­æµçŽ‡è¶…è¿‡ 0.3%</span></div>
    <div class="card-body">
      <div style="display:flex;gap:8px;align-items:flex-end;height:150px">${bars}</div>
    </div>
  </div>
  <div class="card">
    <div class="card-head"><h2>æ˜Žç»†</h2></div>
    <div class="table-wrap"><table>
      <thead><tr>
        <th>æ—¥æœŸ</th><th class="num">è¯·æ±‚</th><th class="num">Token</th>
        <th class="num">æ–­æµ</th><th class="num">æ–­æµçŽ‡</th>
        <th class="num">æ•…éšœè½¬ç§»</th><th class="num">å¹³å‡é¦–å­—èŠ‚</th>
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
      <div class="tag">Agent è´¦å·è½¬ API ç½‘å…³</div>
      ${err ? `<div class="login-err">${esc(err)}</div>` : ''}
      <div class="field">
        <label for="u">ç”¨æˆ·å</label>
        <input id="u" name="username" type="text" autocomplete="username" required autofocus>
      </div>
      <div class="field">
        <label for="p">å¯†ç </label>
        <input id="p" name="password" type="password" autocomplete="current-password" required>
      </div>
      <button class="btn primary" type="submit">ç™»å½•</button>
      <div class="hint" style="text-align:center;margin-top:14px;color:var(--text-faint);font-size:12px">
        åŽç«¯æœªè¿žæŽ¥æ—¶ä¼šè¿›å…¥æ¼”ç¤ºæ¨¡å¼
      </div>
    </form>
  </div>`;
}
