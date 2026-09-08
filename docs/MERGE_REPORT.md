# sub2api × new-api 合并方案报告

**目标**：做一个 agent → API 的转换网关，带计费/多用户/额度后台，核心诉求是**稳定，不要总是断**。
**范围（Robin 已拍板）**：A = 网关 + 计费 + 多用户 + 额度后台全都要；B = Go；C = ①流式 SSE 中途断 + ②上游账号被封/限流 都要解决。

所有引用的文件路径均在两个仓库的 2026-09-08 HEAD 上实地核验过。

---

## 一、结论先行

| | 结论 |
|---|---|
| **底座选谁** | **new-api**。理由不是它代码更好，而是 Robin 要「计费+多用户+额度后台」，这块 new-api 已经是生产级完成品，从零写要几个月。 |
| **sub2api 的角色** | **不是备选底座，是零件供应商**。它的账号池健康度体系比 new-api 成熟得多，要移植过来。 |
| **最大的自研工作量** | **统一调度层**。两个项目「稳」的维度根本不同（见下），必须自己写一层把它们焊在一起。这是「不断」的根。 |
| **最容易被忽略的坑** | **断流时的计费语义**。机制是现成的，但产品决策必须 Robin 自己拍。见第五节。 |

---

## 二、两个项目的底牌

| | sub2api | new-api |
|---|---|---|
| module | `github.com/Wei-Shaw/sub2api` | `github.com/QuantumNous/new-api` |
| Go | 1.27.0 | 1.25.1 |
| 定位 | 订阅额度分发型 AI API 网关 | 下一代 LLM 网关 / AI 资产管理 |
| 结构 | `backend/`(Go, wire DI) + `frontend/`(Vue) | 扁平 Go 工程 + `web/` |
| 活跃度 | 2026-09-08 仍在提交 | 2026-09-08 仍在提交 |

两个都是活项目，选型不踩坑。

---

## 三、核心差异：「稳」的维度不一样

这是整份报告最重要的一条。

**sub2api 盯的是「账号」。** `backend/internal/repository/` 下有成体系的账号健康实现：

- `account_repo_temp_unsched_test.go` — 临时不可调度（temp unschedulable）
- `account_repo_auto_pause_test.go` — 自动暂停
- `account_repo_schedulable_projection_test.go` — 可调度账号投影
- `account_repo_upstream_billing_probe_{cas,due,update}_test.go` — 上游计费探针，带 CAS 并发控制
- `service/account_load_factor_test.go` — 负载因子

配合 `backend/cmd/server/wire_gen.go:114-124` 注入的 Redis 计数器：

```go
tempUnschedCache := repository.NewTempUnschedCache(redisClient)
timeoutCounterCache := repository.NewTimeoutCounterCache(redisClient)
openAI403CounterCache := repository.NewOpenAI403CounterCache(redisClient)
geminiTokenCache := repository.NewGeminiTokenCache(redisClient)
compositeTokenCacheInvalidator := service.NewCompositeTokenCacheInvalidator(geminiTokenCache)
rateLimitService := service.ProvideRateLimitService(accountRepository, usageLogRepository, ..., tempUnschedCache, timeoutCounterCache, openAI403CounterCache, ...)
```

语义很清楚：**某账号连续超时 / 连续 403 → 临时踢出调度池 → 冷却后自动回来**。这正是 Robin 说的「上游账号被封/限流」那一类断。

**new-api 盯的是「渠道」。**

- 选路：`model/ability.go` `getPriority()` — 按 `priority DESC` 排序，带 retry 索引，粒度是 `渠道 × 模型 × 分组`
- 启停：`service/channel.go:19 DisableChannel` / `:36 EnableChannel`
- 开关：`common/constants.go:129 AutomaticDisableChannelEnabled`
- 主动测活：`controller/channel-test.go:938`
- 批量：`model/channel.go:812 EnableChannelByTag` / `:821 DisableChannelByTag`
- 重试参数：`service/channel_select.go:38 RetryParam`（`GetRetry/SetRetry/IncreaseRetry/ResetRetryNextTry`）、`:108 CacheGetRandomSatisfiedChannel`
- 亲和性抑制：`service/channel_affinity.go:627 ShouldSkipRetryAfterChannelAffinityFailure`

**⚠️ 一个必须知道的重试语义（`model/ability.go` getPriority + getChannelQuery）**：

```go
if retry >= len(priorities) {
    priorityToUse = priorities[len(priorities)-1]  // 超出就钉在最低档
} else {
    priorityToUse = priorities[retry]              // 第 N 次重试 = 第 N 档优先级
}
```

也就是说 **new-api 的重试是「逐档下降」，不是「同档换人」**：第一次重试就掉到次高优先级渠道，重试次数超过档位数后一直钉在最低档。

这对 Robin 很关键：如果你的账号池是「同一档里有 10 个等价账号」，new-api 原生的重试**根本不会在这 10 个里轮换**，它会直接降级到更差的档位。这是必须在自研调度层里改掉的第一个行为——**同档内先横向换账号，横向都失败了才纵向降档**。

**问题**：new-api 的 ability 表**不认识「账号」这个维度**，它只有 channel；sub2api 的账号池**不认识「分组/模型/优先级」**这套路由。

而 Robin 做的是「agent 账号转 API」，**账号就是一等公民**。所以：

> **裁决：必须自研统一调度层。** 这是本项目最大的一块自研工作量。

---

## 四、逐维裁决表

| 维度 | 裁决 | 依据 |
|---|---|---|
| 1. 账号/渠道池 + 健康检查 | **自研合并层** | 两者粒度不同，见第三节 |
| 2. 重试 / 失败切换 | **取 new-api 骨架 + 移植 sub2api 账号熔断** | `channel_select.go` RetryParam 是现成的重试框架；但它只会换渠道，不会换账号 |
| 3. 限流 / 并发 | **取 sub2api**（Codex 判 neither，见注） | Redis 计数器 + load factor + `config.go:188-191` ProcessingTimeoutSeconds / FailedRetryBackoffSeconds |
| 4. SSE 断流与超时 | **自研，两边各取一半零件** | 见下 |
| 5. token/cookie 刷新与失效 | **取 sub2api**（双方一致） | `config.go:635-658 TokenRefreshConfig`，见下 |
| 6. 错误码映射与降级 | **取 new-api**（双方一致） | `relay/alpha_search_handler.go:90,100`，见下 |
| 7. 日志 / 可观测性 | **new-api 骨架 + sub2api attempt ledger** | `backend/dev-docs/upstream-error-proxy-attribution.md:1-103` per-attempt 错误归因 |
| 8. 持久化 / 状态恢复 | **取 new-api**（Codex 判 neither，见注） | PostgreSQL 为准 + Redis 只做租约/计数 |
| 9. 计费与断流 | **取 new-api 机制，但产品语义要自己定** | 见第五节 |

> **注：两个 reviewer 有分歧的两条。** dim-3 和 dim-8 我判「可以取现成的」，Codex 判「需自研」。这是同一份已核验证据上的判断差异，不是事实争议，这里如实并列而不抹平：保守做法按 Codex 的判断预留自研工时，乐观做法先直接取用、压测后再决定是否重写。建议按 Codex 的保守估计排期，做完 POC 再收敛。

### dim-5 细节：sub2api 的 token 刷新是这份报告里最值得直接抄的一块

`backend/internal/config/config.go:635-658` `TokenRefreshConfig`：

```go
Enabled                  bool    // 是否启用自动刷新
CheckIntervalMinutes     int     // 检查间隔
RefreshBeforeExpiryHours float64 // 过期前多久开始刷新 ← 主动刷新，不等失效
MaxRetries               int
RetryBackoffSeconds      int
CandidatePageSize        int     // 每次从库里取多少候选账号
ProviderConcurrency      int     // 每个平台并发刷新数
ProviderQPS              int     // 每个平台每进程刷新速率
ProviderFailureThreshold int     // 一周期内连续失败到此值就停掉该平台
AttemptTimeoutSeconds    int     // 单次上游刷新超时
CycleTimeoutSeconds      int     // 单个刷新周期总超时
```

这是一条**被治理的刷新流水线**：主动提前刷新（不是等 401 才救火）、按平台限流限并发、平台级失败阈值自动停摆、单次和整周期双重超时。对「agent 账号转 API」这种强依赖 OAuth token 的场景，这块直接抄，能省掉大量线上事故。

### dim-6 细节：new-api 的错误处理有一处做得很对

`relay/alpha_search_handler.go`：
- `:90` `RelayErrorHandler` + `ResetStatusCode(newAPIError, statusCodeMappingStr)` — 状态码可配置重映射
- `:100` `io.Copy` 失败时返回 `types.NewError(err, types.ErrorCodeDoRequestFailed, types.ErrOptionWithSkipRetry())`

第二条尤其值得学：**响应体已经开始往客户端写之后再失败，明确标记为不可重试**。这正是第四节那条铁律在 new-api 里已有的实现，直接继承。

sub2api 对应的是 `backend/internal/util/httputil/httputil.go:85-101` `ExtractUpstreamErrorCodeAndMessage`，把上游各种 JSON 错误结构（含非法 JSON）统一抽成 code/message 并截断。两者互补：new-api 管「错了怎么办」，sub2api 管「错误是什么」。

### dim-4 细节（Robin 的痛点 ①）

**new-api 有的**：`relay/common` 定义了 `StreamStatus` + `StreamEndReason` 枚举，已经区分 `StreamEndReasonClientGone` / `StreamEndReasonHandlerStop`。
用例：`relay/channel/gemini/relay-gemini.go:251`、`relay/channel/gemini/relay_responses.go:101,157`。
上游取消检测：`relay/channel/api_request.go:450` 的 `case <-c.Request.Context().Done():`
扫描器：`relay/helper/stream_scanner.go`

**sub2api 有的**（是真实现，不只是文档）：
- `backend/internal/service/openai_first_output_timeout.go` — 首字节超时，独立成文件 + 独立测试
- `backend/internal/service/openai_gateway_forward.go` — 转发主体
- `backend/internal/service/openai_gateway_passthrough_flush_test.go` / `openai_gateway_response_flush_test.go` — 专测 flush 行为
- `backend/internal/handler/openai_gateway_first_output_timeout_test.go`

**合并做法**：拿 new-api 的 `StreamEndReason` 分类作为状态机的枚举，拿 sub2api 的 first-output timeout 作为「上游假死」的判定器。关键规则：**首字节前失败可以换账号重试，首字节后失败绝不能重放**（已经吐给客户端的 token 无法撤回）。

---

## 五、必须 Robin 拍板的一件事：断流怎么计费

new-api 的额度模型是 **预扣 → Settle 差额结算 → 失败时幂等 Refund**：

`relay/common/billing.go` `BillingSettler` 接口：
- `Settle(actualQuota)` — `delta = actualQuota - preConsumedQuota`
- `Refund(c)` — 退还全部预扣，注释明确写了幂等安全，走 gopool 异步
- `NeedsRefund()` — 未结算且未退款
- `Reserve(target)` — 中途追加预扣

实现在 `service/billing_session.go:109 Refund` / `:127 NeedsRefund`、`service/quota.go:419 PostConsumeQuota` / `:424 postConsumeQuotaWithResult`。
字段在 `relay/common/relay_info.go:134 FinalPreConsumedQuota` / `:138 ForcePreConsume`。

**但退款闸门挂在 error 上**，`controller/relay.go:175-184`：

```go
defer func() {
    // Only return quota if downstream failed and quota was actually pre-consumed
    if newAPIError != nil {
        newAPIError = service.NormalizeViolationFeeError(newAPIError)
        if relayInfo.Billing != nil { relayInfo.Billing.Refund(c) }
        service.ChargeViolationFeeIfNeeded(c, relayInfo, newAPIError)
    }
}()
```

（另一处 `controller/relay.go:606-611` 走 `if !durable` 兜底。）

**含义**：上游报错 → 退款走得通。但客户端中途断开如果被判定为「正常结束」而非 error，就会走 `Settle` 按已产出 token 结算，**不退款**。

这不算 bug——已经生成的 token 本来就有成本——但它意味着**用户会看到「没拿到完整回答但扣了钱」**，这恰恰是「总是断」体感最差的地方。

**三选一，Robin 定**：
1. 断流按已生成量收费（= new-api 现状，成本最实，用户体感最差）
2. 断流全额退款（用户体感最好，会被刷）
3. **断流记一条可申诉的 attempt 记录，超过阈值自动补偿**（推荐，也是 sub2api attempt ledger 能补上的地方）

---

## 六、落地方案

### 阶段 0：立地基（1～2 周）
fork new-api，**不改它的 user / token / quota / redemption / 后台**，这些直接用。
只做一件事：把 `relay/` 抽出一个清晰的 upstream adapter 接口，为接 agent 类账号做准备。
验收：原样跑通，前后台功能不回归。

### 阶段 1：账号成为一等公民（2～3 周）
建统一健康表，字段至少：

```
channel_id, account_id, model, group,
failure_class,      -- timeout / 403 / 429 / 5xx / client_gone
cooldown_until,
health_score,
last_probe_at
```

调度器接口：`Pick(model, group) -> (channel, account)`，`Report(attempt_result)` 回写健康度。
移植 sub2api 的 temp-unsched / auto-pause / load-factor 语义；保留 new-api 的 priority 选路。
验收：干掉一个账号，请求自动落到下一个，冷却后自动回来。

### 阶段 2：治「断」（2～3 周，Robin 的核心诉求）
- 引入 first-output timeout（移植 sub2api 实现）
- 用 `StreamEndReason` 建请求 attempt 状态机
- **铁律：首字节前可重试换账号，首字节后不可重放**
- 有界指数退避 + jitter，per-account 熔断器
- attempt ledger 落库，每次尝试的错误归因都留痕
验收：拔掉上游、模拟 429、模拟假死三种场景，客户端表现符合预期且额度账目对得上。

### 阶段 3：计费语义收口（1 周）
按第五节 Robin 的选择实现断流补偿；把 attempt ledger 接进申诉/对账界面。

### 阶段 4：可观测性（1 周）
指标至少：TTFB、流截断率、重试原因分布、failover 成功率、per-account 错误率。
加 readiness / liveness + 合成探针。

**总计约 7～10 周**，其中阶段 1 和 2 是真正的自研，其余是集成。

---

## 七、一句话总结

> new-api 的账、权限、后台直接继承，不用自研；sub2api 的账号健康度体系整体移植；真正要自己写的是**把「账号」和「渠道」两套健康度焊成一张表的调度层**，以及**断流的判定与补偿**。这两块做完，「总是断」才算根治。
