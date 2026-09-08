# Subport

Agent 账号转 API 的网关。目标是**稳定** —— 不要总是断。

> 当前状态：**第一版开发中**。前端已可运行（`web/`），后端尚未提交。

## 这个项目是怎么来的

它不是从零设计的，而是从两个开源项目里取长补短：

| 来源 | 拿它的什么 |
|---|---|
| [new-api](https://github.com/QuantumNous/new-api) | 底座。计费、多用户、额度后台、渠道路由、错误码重映射 |
| [sub2api](https://github.com/Wei-Shaw/sub2api) | 零件。账号级健康度体系、OAuth token 主动刷新流水线、首字节超时 |
| 自研 | 把「账号」和「渠道」两套健康度焊成一张统一调度表；断流的判定与补偿 |

完整的对比分析、逐维取舍裁决和分阶段方案见仓库外的 `MERGE_REPORT.md`（分析产物，尚未纳入本仓库）。

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
web/     前端控制台 —— 零依赖、零构建步骤，详见 web/README.md
backend/ Go 网关内核 —— 调度器 + failover，单文件
```

## 跑起来

后端（需要 Go 1.27）：

```bash
cd backend && go run .
```

前端：

```bash
cd web && python -m http.server 8791
```

打开 <http://127.0.0.1:8791>。后端未连接时前端会进入演示模式，页面顶部有明确标注。

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

## 已知边界（诚实清单）

- **上游是模拟的。** `/mock/upstream` 是自带的假上游，还没有接真实模型供应商。`callUpstream` 是真实 HTTP 调用，换掉 BaseURL 就能接真的。
- **没有持久化。** 账号是 `main()` 里写死的切片，没有数据库，重启即还原。
- **没有鉴权。** 管理接口目前裸奔，不要暴露到公网。
- **前端只读。** 所有新建/编辑按钮都禁用，等后端写接口定稿。
- 计费、额度、多用户后台都还没开始 —— 那是方案里的阶段 3，目前在阶段 1。
