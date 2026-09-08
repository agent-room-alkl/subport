// Subport user console — views. Each returns an HTML string.

const esc = (s) => String(s).replace(/[&<>"']/g, (c) => (
  { '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]
));

const num = (n) => Number(n).toLocaleString('en-US');

function when(iso) {
  const d = new Date(iso);
  if (isNaN(d)) return esc(iso);
  const pad = (n) => String(n).padStart(2, '0');
  return `${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

function bar(ratio) {
  const r = Math.max(0, Math.min(1, ratio));
  const cls = r >= 0.9 ? ' is-bad' : r >= 0.7 ? ' is-warn' : '';
  return `<div class="bar${cls}"><i style="width:${Math.round(r * 100)}%"></i></div>`;
}

function demoBanner() {
  return `<div class="banner"><span>⚠</span>
    <div><b>演示数据</b> — 后端未连接，下面是本地示例数据，不是你的真实用量。
    后端起来之后本页会自动切到真实数据。</div></div>`;
}

// ---------------------------------------------------------------- overview

function overviewView(q, keys) {
  const used = q.quota_used || 0;
  const total = q.quota_total || 1;
  const ratio = used / total;
  const active = keys.filter((k) => k.enabled).length;

  return `
  <div class="stats">
    <div class="stat"><div class="k">剩余额度</div>
      <div class="v">${num(q.remaining != null ? q.remaining : total - used)}<small>token</small></div></div>
    <div class="stat"><div class="k">已用</div>
      <div class="v">${num(used)}<small>共 ${num(total)}</small></div></div>
    <div class="stat"><div class="k">启用中的密钥</div>
      <div class="v">${active}<small>共 ${keys.length} 个</small></div></div>
  </div>
  <div class="card">
    <div class="card-head"><h2>额度使用</h2><div class="spacer"></div>
      <span style="font-size:12px;color:var(--text-faint)">${(ratio * 100).toFixed(1)}% 已用</span></div>
    <div class="card-body">${bar(ratio)}</div>
  </div>
  <div class="card">
    <div class="card-head"><h2>怎么开始</h2></div>
    <div class="card-body" style="color:var(--text-dim)">
      <p style="margin-top:0">三步：</p>
      <ol style="margin:0;padding-left:20px;line-height:1.9">
        <li>去<b>密钥</b>页新建一个 API Key —— <b>密钥只在创建时显示一次</b>，请立刻复制保存</li>
        <li>去<b>接入指引</b>页复制那段 curl，把 <code>$SUBPORT_KEY</code> 换成你的密钥</li>
        <li>把你原来调 OpenAI 的 <code>base_url</code> 换成 Subport 的地址，其余不用改</li>
      </ol>
    </div>
  </div>`;
}

// ---------------------------------------------------------------- keys

function keysView(rows) {
  const list = rows.length ? rows.map((k) => `
    <tr>
      <td><b>${esc(k.name)}</b></td>
      <td><code>${esc(k.prefix)}····</code></td>
      <td style="color:var(--text-faint)">${esc(k.last_used || '—')}</td>
      <td>${k.enabled ? '<span class="pill ok">启用</span>' : '<span class="pill off">停用</span>'}</td>
      <td style="text-align:right;white-space:nowrap">
        <button class="btn sm" data-toggle="${esc(k.id)}" data-enabled="${k.enabled}">
          ${k.enabled ? '停用' : '启用'}</button>
        <button class="btn sm" data-del="${esc(k.id)}">删除</button>
      </td>
    </tr>`).join('') : `<tr><td colspan="5"><div class="empty">还没有密钥，点右上角新建一个</div></td></tr>`;

  return `
  <div id="new-key-out"></div>
  <div class="card">
    <div class="card-head"><h2>我的 API 密钥</h2><div class="spacer"></div>
      <button class="btn sm primary" id="new-key-btn">新建密钥</button></div>
    <div class="table-wrap"><table>
      <thead><tr><th>名称</th><th>密钥</th><th>最近调用</th><th>状态</th><th></th></tr></thead>
      <tbody>${list}</tbody>
    </table></div>
  </div>`;
}

/** Shown once, immediately after creation. The secret is never retrievable again. */
function newKeyBanner(name, secret) {
  return `
  <div class="card" style="border-color:var(--ok)">
    <div class="card-head"><h2>密钥已创建：${esc(name)}</h2></div>
    <div class="card-body">
      <div class="banner" style="background:var(--ok-bg);color:var(--ok);margin-bottom:12px">
        <span>✓</span><div><b>请立刻复制保存。</b>出于安全，这串密钥只显示这一次，
        离开本页后无法再次查看——丢了只能删掉重建。</div>
      </div>
      <div style="display:flex;gap:8px;align-items:center">
        <code class="mono" id="secret-text"
              style="flex:1;padding:10px 12px;background:var(--surface-2);border-radius:6px;
                     overflow-x:auto;white-space:nowrap">${esc(secret)}</code>
        <button class="btn primary" data-copy="${esc(secret)}">复制</button>
      </div>
    </div>
  </div>`;
}

// ---------------------------------------------------------------- usage

const STATUS_PILL = {
  success:       ['ok',   '成功'],
  failed:        ['bad',  '失败'],
  stream_broken: ['warn', '中途断流'],
};

function usageView(rows) {
  if (!rows.length) return `<div class="card"><div class="empty">还没有调用记录</div></div>`;

  const broken = rows.filter((r) => r.stream_broken).length;
  const note = broken
    ? `<div class="banner"><span>ⓘ</span><div>这段记录里有 <b>${broken}</b> 次<b>中途断流</b>。
       断流按已生成的 token 计费——你没拿到完整回答，但已产出的部分仍然产生了成本。
       如果你认为某次断流不该计费，可以拿这条记录来申诉。</div></div>`
    : '';

  return note + `
  <div class="card">
    <div class="card-head"><h2>调用记录</h2><div class="spacer"></div>
      <span style="font-size:12px;color:var(--text-faint)">最近 ${rows.length} 条</span></div>
    <div class="table-wrap"><table>
      <thead><tr>
        <th>时间</th><th>模型</th><th class="num">Token</th><th class="num">花费</th>
        <th class="num">尝试次数</th><th>结果</th>
      </tr></thead>
      <tbody>${rows.map((r) => {
        const [cls, label] = STATUS_PILL[r.status] || ['off', r.status];
        return `
        <tr>
          <td class="mono" style="color:var(--text-dim)">${when(r.created_at)}</td>
          <td><code>${esc(r.model)}</code></td>
          <td class="num">${num(r.tokens)}</td>
          <td class="num">${num(r.cost)}</td>
          <td class="num" style="color:${r.attempts > 1 ? 'var(--warn)' : 'var(--text-dim)'}">${r.attempts}</td>
          <td><span class="pill ${cls}">${esc(label)}</span></td>
        </tr>`;
      }).join('')}</tbody>
    </table></div>
  </div>`;
}

// ---------------------------------------------------------------- top-up

function topupView() {
  return `
  <div class="card">
    <div class="card-head"><h2>兑换码</h2></div>
    <div class="card-body">
      <div class="field">
        <label for="redeem">输入兑换码</label>
        <input id="redeem" type="text" placeholder="XXXX-XXXX-XXXX" disabled>
        <div class="hint">兑换功能尚未开放，界面先放在这里占位。</div>
      </div>
      <button class="btn primary" disabled>兑换</button>
    </div>
  </div>
  <div class="card">
    <div class="card-head"><h2>充值</h2></div>
    <div class="card-body" style="color:var(--text-dim)">
      在线支付还没接。现阶段请直接联系管理员为你的账号加额度。
    </div>
  </div>`;
}

// ---------------------------------------------------------------- guide

function guideView(base) {
  const curl = `curl ${base}/v1/chat/completions \\
  -H "Authorization: Bearer $SUBPORT_KEY" \\
  -H "Content-Type: application/json" \\
  -d '{
    "model": "gpt-4o",
    "messages": [{"role": "user", "content": "你好"}]
  }'`;

  const py = `from openai import OpenAI

client = OpenAI(
    api_key="$SUBPORT_KEY",
    base_url="${base}/v1",
)

resp = client.chat.completions.create(
    model="gpt-4o",
    messages=[{"role": "user", "content": "你好"}],
)
print(resp.choices[0].message.content)`;

  return `
  <div class="card">
    <div class="card-head"><h2>你的接入地址</h2></div>
    <div class="card-body">
      <div style="display:flex;gap:8px;align-items:center">
        <code class="mono" style="flex:1;padding:10px 12px;background:var(--surface-2);
              border-radius:6px;overflow-x:auto;white-space:nowrap">${esc(base)}/v1</code>
        <button class="btn" data-copy="${esc(base)}/v1">复制</button>
      </div>
      <div class="hint" style="margin-top:8px">
        Subport 兼容 OpenAI 接口。<b>把你原来的 base_url 换成上面这个，其余代码一行都不用改。</b>
      </div>
    </div>
  </div>

  <div class="card">
    <div class="card-head"><h2>curl</h2><div class="spacer"></div>
      <button class="btn sm primary" data-copy="${esc(curl)}">复制整段</button></div>
    <div class="card-body">
      <pre class="mono" style="margin:0;padding:12px;background:var(--surface-2);border-radius:6px;
           overflow-x:auto">${esc(curl)}</pre>
    </div>
  </div>

  <div class="card">
    <div class="card-head"><h2>Python（openai 官方库）</h2><div class="spacer"></div>
      <button class="btn sm" data-copy="${esc(py)}">复制整段</button></div>
    <div class="card-body">
      <pre class="mono" style="margin:0;padding:12px;background:var(--surface-2);border-radius:6px;
           overflow-x:auto">${esc(py)}</pre>
    </div>
  </div>

  <div class="card">
    <div class="card-head"><h2>关于稳定性</h2></div>
    <div class="card-body" style="color:var(--text-dim)">
      <p style="margin-top:0">两件事你不需要自己处理，网关已经做了：</p>
      <ul style="margin:0;padding-left:20px;line-height:1.9">
        <li><b>账号故障自动转移</b>——上游某个账号挂了，请求会自动换到同一档里的另一个账号，
            你这边不需要重试。</li>
        <li><b>不会给你重复内容</b>——一旦开始向你输出，网关就不再重试，
            所以你不会收到同一段回答的两个版本。</li>
      </ul>
      <p style="margin-bottom:0">代价是：<b>输出中途断掉时不会自动重来</b>，
        因为重来会让你看到重复内容。这类情况会在「调用记录」里标成「中途断流」。</p>
    </div>
  </div>`;
}

// ---------------------------------------------------------------- login

function loginView(mode, err) {
  const isReg = mode === 'register';
  return `
  <div class="login-wrap">
    <form class="login-card" id="auth-form">
      <div class="brand"><span class="brand-dot"></span><span>Subport</span></div>
      <div class="tag">${isReg ? '创建账号' : '用户登录'}</div>
      ${err ? `<div class="login-err">${esc(err)}</div>` : ''}
      <div class="field">
        <label for="u">用户名</label>
        <input id="u" name="username" type="text" autocomplete="username" required autofocus>
      </div>
      <div class="field">
        <label for="p">密码</label>
        <input id="p" name="password" type="password"
               autocomplete="${isReg ? 'new-password' : 'current-password'}" required>
        ${isReg ? '<div class="hint">至少 8 位</div>' : ''}
      </div>
      ${isReg ? `<div class="field">
        <label for="i">邀请码</label>
        <input id="i" name="invite" type="text" required>
        <div class="hint">目前是邀请制注册</div>
      </div>` : ''}
      <button class="btn primary" type="submit">${isReg ? '注册' : '登录'}</button>
      <div style="text-align:center;margin-top:14px;font-size:13px">
        <a href="#" id="switch-mode" style="color:var(--accent);text-decoration:none">
          ${isReg ? '已有账号？去登录' : '没有账号？去注册'}</a>
      </div>
    </form>
  </div>`;
}
