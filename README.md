# Subport

Agent 账号转 API 的网关。目标是**稳定** —— 不要总是断。

> 当前状态：**第一版开发中**。网关内核、账号故障转移、用户体系都已可运行；上游仍是模拟的，用户端 `/console` 在做。详见文末「已知边界」。

## 这个项目是怎么来的

它不是从零设计的，而是从两个开源项目里取长补短：

| 来源 | 拿它的什么 |
|---|---|
| [new-api](https://github.com/QuantumNous/new-api) | 底座。计费、多用户、额度后台、渠道路由、错误码重映射 |
| [sub2api](https://github.com/Wei-Shaw/sub2api) | 零件。账号级健康度体系、OAuth token 主动刷新流水线、首字节超时 |
| 自研 | 把「账号」和「渠道」两套健康度焊成一张统一调度表；断流的判定与补偿 |

完整的对比分析、逐维取舍裁决和分阶段方案见 [`docs/MERGE_REPORT.md`](docs/MERGE_REPORT.md)。

## 两条设计铁律

第一版就要做对的两件事，也是「不断」的根：

**1. 同档横向换账号，横向全挂才纵向降档。**
new-api 原生的重试是「逐档下降」—— 第 N 次重试直接取第 N 档优先级
（`model/ability.go` 的 `getPriority`：`priorityToUse = priorities[retry]`）。
这意味着同一档里还有 10 个健康账号，它一个都不会试，第一次失败就降级到更差的档位。
这是「明明还有好账号可用，服务却越跑越差」的直接原因，必须改掉。

**2. 首字节分界。**
首字节返回**前**失败 → 可以换账号重试。
首字节返回**后**失败 → 一律不重放。
已经吐给客户端的 token 撤不回来，硬重试只会让用户看到重复内容，比断了更糟。

## 目录

```
cmd/subport/           入口，只做装配
internal/
  model/               共享类型：User APIKey UsageLog Session Account
  store/               持久化 ← 换 SQLite 只动这一个目录
  gateway/             调度器 + failover + relay ← 项目的核心
  httpapi/             路由 + 鉴权 + 用户维度隔离的 console 接口
web/
  shared/app.css       两个端共用的样式
  admin/               管理端（运维用）
  console/             用户端（客户用，开发中）
docs/                  MERGE_REPORT.md 等分析产物
```

**三个端共用一套后端、一个域名，按路由分**，不是三个项目。拆开只会换来跨域、三套鉴权、三份部署，安全边界靠的是角色和数据过滤，不是仓库数量。

`internal/` 不是命名约定——Go 编译器会阻止外部模块 import 它。`store/` 独立成包是为了 SQLite 迁移时 `gateway/` 和 `httpapi/` 一行都不用动。

## 跑起来

需要 Go 1.27。一条命令，前后端一起起来：

```bash
go run ./cmd/subport
```

打开 <http://127.0.0.1:8080/admin/index.html>。后端会同时托管静态前端，不需要另起 web server。

| 变量 | 默认 | 说明 |
|---|---|---|
| `SUBPORT_ADDR` | `:8080` | 监听地址 |
| `SUBPORT_SELF_URL` | 由 ADDR 推导 | 演示上游的地址；不要写死端口 |
| `SUBPORT_WEB` | `web` | 静态资源目录 |

## 看 failover 真的发生

后端自带一个 `/mock/upstream` 假上游，它对 `acct-openai-1` 固定返回 500。所以一次普通请求就能看到横向切换：

```bash
curl -s -X POST localhost:8080/v1/chat/completions -H 'Content-Type: application/json' -d '{"messages":[]}'
```

返回的内容会指名 `acct-openai-2` —— 也就是**同一档里的另一个账号**，而不是降级到二档的 `acct-anthropic-1`。服务端日志同时打出每次尝试：

```
attempt=0 account=acct-openai-1 -> 500
attempt=1 account=acct-openai-2 -> 200
```

这就是第 2 节那条铁律在跑起来的样子。

## 用户体系

两类角色，两套接口，边界是硬的：

| | 谁用 | 接口 | 能看到什么 |
|---|---|---|---|
| 管理端 | 运维（`role=admin`） | `GET /api/*` | 全局：账号池、渠道、健康度 |
| 用户端 | 客户（`role=user`） | `/api/console/*` | **只有自己的**：密钥、额度、调用记录 |

**设计上的关键一点**：`/api/console/*` 的处理函数**从不从请求里读 user id**，只从已认证的 session 里取。所以「读别人的数据」这种请求在 API 形状上就不可表达，而不是靠某处的一个检查——那种检查总有一天会被人忘掉。

实测过的隔离行为（不是声称，是跑出来的）：

```
alice 列出密钥         -> 只有 alice-prod
bob   列出密钥         -> 只有 bob-prod
alice 删除 bob 的密钥  -> 404 key not found
alice 禁用 bob 的密钥  -> 404 key not found
bob 的密钥            -> 依然存在且启用
匿名访问 console      -> 401 not signed in
alice 访问 /api/accounts -> 403 admin only
admin 访问 /api/accounts -> 200
```

注意越权返回的是 **404 而不是 403**：这样连「这个 id 存不存在」都不泄露。

### 环境变量

| 变量 | 默认 | 说明 |
|---|---|---|
| `SUBPORT_DB` | `subport-data.json` | 数据文件 |
| `SUBPORT_INVITE_CODE` | `subport-invite` | 注册邀请码，设为空串则开放注册。注册接口 `invite_code` 和 `inviteCode` 两种写法都收 |
| `SUBPORT_ADMIN_PASSWORD` | `subport-admin` | 首次启动创建的 admin 密码 |

**首次启动会自动创建 admin 账号，默认密码 `subport-admin`——上线前必须改掉。**

## 网关鉴权与计费

`/v1/chat/completions` **必须带有效的 API Key**，否则 401：

| 情况 | 结果 |
|---|---|
| 不带 Authorization | 401 |
| 密钥不存在 | 401 |
| 密钥已停用 | 401 |
| 有效密钥 | 200，并按所属用户记账 |
| 额度耗尽 | 402 |

密钥不存在和已停用返回同一句话，避免被用来探测哪些密钥存在。

每次调用都会写一条 `usage_log`，归属到**密钥的所有者**——用户 id 来自密钥，
不来自请求。失败不计费但仍记录；断流按已产出计费并标记 `stream_broken`，可申诉。

## 已知边界（诚实清单）

- **上游是模拟的。** `/mock/upstream` 是自带的假上游，还没接真实模型供应商。`callUpstream` 已经是真实 HTTP 调用，换掉 BaseURL 就能接真的。
- **计费按响应字符数估算**，不是真实 token 数。接上真实上游后应改用上游返回的 usage。
- **存储是 JSON 文件，不是数据库。** 单机够用、原子写入不会写坏，但没有并发事务，也扛不住多实例。`Store` 是接口层，换 Postgres 不用动 handler。
- **密码哈希是 SHA-256 加盐，不是 bcrypt/argon2。** 标准库能做到的上限。上线前应换成 argon2id。
- **前端只读。** 新建/编辑按钮都禁用，等接口定稿。用户端 `/console` 还在做。
- 计费结算、发卡兑换、断流补偿都还没开始。
