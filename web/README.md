# Subport 控制台（第一版前端）

零依赖、零构建步骤。没有 npm、没有 node_modules、没有打包器。

## 为什么不用框架

第一版的唯一标准是「可用性」。一个需要 `npm install` 才能看见的前端，
在没装 Node 的机器上等于不存在——这台开发机就没有 Node。
纯 HTML/CSS/JS 的代价是没有组件库，收益是**打开就能跑，任何地方都能跑**。

## 怎么跑

三种方式都可以：

1. **直接打开** `index.html`（双击即可，无需服务器）
2. **静态服务器**
   ```bash
   python -m http.server 8791
   ```
3. **由 Go 后端托管** — 后端把本目录作为静态资源目录即可，前端与 API 同源

前两种方式下没有后端，界面会自动进入**演示模式**并在页面顶部挂出橙色横幅，
明确标注「这是示例数据，不是真实状态」。后端起来之后自动切回真实数据，无需改代码。

## 文件

| 文件 | 作用 |
|---|---|
| `index.html` | 入口，按顺序加载三个经典脚本 |
| `css/app.css` | 全部样式，含浅色/深色双主题 |
| `js/api.js` | API 客户端 + 演示数据集（数据形状与后端契约一致） |
| `js/views.js` | 五个页面的渲染函数，每个返回 HTML 字符串 |
| `js/app.js` | 外壳、路由、主题切换 |

## 页面

登录 → 总览 → 账号池 → 渠道 → 密钥 → 用量

**账号池**是本版设计的重点：按优先级**分档展示**，每档标注「本档用尽才会降到下一档」。
这不是装饰，是把调度语义摆到台面上——同一档里还有健康账号时绝不降档，
这正是对 new-api 原生「逐档下降」重试行为的修正。

## 与后端的契约

`js/api.js` 里的演示数据集就是接口契约。后端只要按这个形状返回，前端不用改一行：

```
POST /api/auth/login  -> { token, user }
GET  /api/overview    -> { accounts_total, accounts_healthy, channels_total,
                           requests_24h, stream_breaks_24h, failover_success_rate }
GET  /api/accounts    -> [ { id, label, provider, tier, state, load,
                             cooldown_until, last_error,
                             consecutive_timeouts, consecutive_403 } ]
GET  /api/channels    -> [ { id, name, provider, priority, group, models[],
                             enabled, accounts, health } ]
GET  /api/keys        -> [ { id, name, prefix, group, quota, used,
                             enabled, last_used } ]
GET  /api/usage       -> [ { date, requests, tokens, stream_breaks,
                             failovers, avg_ttfb_ms } ]
```

`state` 取值：`healthy` / `degraded` / `cooling` / `paused` / `disabled`

401/403 会作为真实的登录失败弹出；404/405/501 和连接失败一律视为「此处没有后端」，
自动降级到演示模式——这样用静态服务器预览时不会被一条看不懂的报错挡住。

## 已知边界（第一版）

- **只读**。所有新建/编辑按钮都 `disabled`，标注「第一版只读」。写操作等后端接口定稿。
- 用量图表是手写的 CSS 柱状图，没有交互（悬停有 title 提示）。
- 未做国际化，界面为中文。
