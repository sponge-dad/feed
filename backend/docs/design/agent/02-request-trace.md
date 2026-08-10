# 请求标识与 Feed 链路追踪

> 定义 `request_id` / `trace_id` / `run_id` / `event_id` 的职责边界与落地方式，并定义 Feed 返回来源标记与请求级 Trace 记录。属实施阶段一。

---

## 1. 概述与定位

当前 `common/response/response.go` 的 `Body.RequestID` 从 `ctx.Value("request_id")` 读取，但全链路没有任何位置写入该值，因此对外返回恒为空；下游服务日志也无法按业务请求聚合。本篇解决三件事：

1. 生成并贯通 `request_id`（Gateway → gRPC Metadata → 下游日志 → 响应）。
2. 给 Feed 返回结果打上**召回来源**，使「为什么推荐」有结构化依据。
3. 记录**请求级 Trace**（每个数据源读取了多少条、合并后多少条），供内部排障与 Agent 诊断。

## 2. 标识职责边界

| 标识 | 格式 | 生成点 | 传播方式 | 存储 |
|------|------|--------|----------|------|
| `request_id` | 32 位 hex（uuid v4 去横线） | Gateway 中间件 | ctx + gRPC Metadata `x-request-id` | Redis Trace（TTL）+ 日志 |
| `trace_id` | OTel TraceID | go-zero Telemetry | W3C `traceparent` | APM 后端 |
| `run_id` | Snowflake（`common/idgen`） | Agent RPC | ctx + Metadata `x-agent-run-id` | `agent_runs` |
| `event_id` | uuid v4 |事件生产者 | 消息体字段 | 消费端去重 key |

规则：

- **禁止**用 `trace_id` 替代 `request_id` 返回给客户端（采样、后端替换会导致不可控）。
- 客户端可携带 `X-Request-Id` 请求头，但 Gateway 必须**校验格式**（长度 ≤ 64、`^[A-Za-z0-9_-]+$`），非法则丢弃并重新生成，防止日志注入与伪造。
- 下游服务**不生成** `request_id`；Metadata 缺失时记录 `request_id=missing` 并告警计数，便于发现漏改的调用点。

## 3. Gateway 侧实现

新增 `app/gateway/internal/middleware/requestid.go`：

```go
// 顺序：RequestIDMiddleware → ClientIPMiddleware → 业务 handler
// 1. 取 X-Request-Id（校验合法性），否则生成
// 2. 写入 ctx：typed key + 兼容字符串 key "request_id"
// 3. 写响应头 X-Request-Id
// 4. 绑定日志字段：logx.WithFields(logx.Field("request_id", rid))
```

配套改造：

| 文件 | 改动 |
|------|------|
| `app/gateway/cmd/api/gateway.go` | `server.Use(middleware.RequestIDMiddleware)`，注册在 ClientIP 之前 |
| `common/requestid/`（新增） | `WithRequestID(ctx, id)` / `FromContext(ctx) string`，typed key 定义 |
| `common/response/response.go` | `requestID(ctx)` 先读 typed key，再回退字符串 key（保持向后兼容） |

约束：`request_id` 必须同时出现在**响应头**与**响应体**，前端可在错误提示中展示，用户反馈时可直接定位。

## 4. gRPC 透传

新增 `common/interceptors/`：

```go
// UnaryClientRequestID：从 ctx 取request_id / run_id，注入 outgoing metadata
//   x-request-id、x-agent-run-id（有则带，无则不带）
// UnaryServerRequestID：从 incoming metadata 取值→ 写入 ctx →
//   logx.WithFields 绑定，保证该请求内所有日志自带字段
```

接入方式：

- 客户端：Gateway 及各服务构造 `zrpc.Client` 时通过 `zrpc.WithUnaryClientInterceptor` 追加。
- 服务端：各 RPC `server.AddUnaryInterceptors(interceptors.UnaryServerRequestID, serverinterceptors.ErrorInterceptor)`，顺序在错误拦截器之前。
- MQ：事件体统一新增 `request_id` 字段（`feed-created`、`feed-deleted`、`feed-behavior-event`），Worker 消费时把它绑定到日志，使异步副作用可回溯到触发它的 HTTP 请求。

Metadata 键统一在 `common/interceptors/keys.go` 定义，禁止各服务硬编码字符串。

## 5. Feed 来源标记

`api/proto/feed/feed.proto` 新增枚举与字段：

```protobuf
enum FeedSource {
  FEED_SOURCE_UNKNOWN        = 0; // 未知 / 未标记（proto3 默认值）
  FEED_SOURCE_FOLLOW_INBOX   = 1; // 关注作者推入的收件箱
  FEED_SOURCE_VIP_OUTBOX     = 2; // 关注的大 V 发件箱实时拉取
  FEED_SOURCE_INBOX_REBUILD  = 3; // 收件箱缺失后回源重建
  FEED_SOURCE_CITY_POOL      = 4; // 同城池
  FEED_SOURCE_RECOMMEND_POOL = 5; // 公共推荐池
}
```

`FeedBrief` / `FeedInfo` 各新增 `FeedSource source`（`FeedBrief` 追加为下一个可用字段号，禁止复用旧号）。

打标位置：

| Logic | 来源判定|
|-------|----------|
| `getfollowtimelinelogic.go` | 候选来自 `keys.Inbox(uid)` → `FOLLOW_INBOX`；来自 `keys.Outbox(bigV)` → `VIP_OUTBOX`；同一 feed 被两路命中时以 `FOLLOW_INBOX` 优先（更贴近用户可解释语义） |
| `getrecommendtimelinelogic.go` | `RECOMMEND_POOL` |
| `getcitytimelinelogic.go` | `CITY_POOL` |
| 回源重建路径 | `INBOX_REBUILD` |

**注意（已落地 2026-08-05）**：`getfollowtimelinelogic.go` 已补齐收件箱回源重建逻辑——`inbox` 读取为空且用户有关注关系时，由 `rebuildInbox` 并行拉取各作者 `outbox` 兜底重建并回写 inbox，命中该路径的 feed 标记为 `INBOX_REBUILD`（重建失败不致命，仅记日志）。多路命中按优先级收敛：`FOLLOW_INBOX > INBOX_REBUILD > VIP_OUTBOX`。`FeedSource` 枚举与 `source` 字段（`FeedBrief.source(10)` / `FeedInfo.source(18)`）已生成并透传至 gateway `FeedCard.source`。

## 6. 请求级 Trace 记录

### 6.1 数据结构

```go
// FeedRequestTrace 一次 Timeline 请求的读取路径记录（内部诊断用）
type FeedRequestTrace struct {
    RequestID   string        `json:"request_id"`
    UserID      int64         `json:"user_id"`
    Tab         string        `json:"tab"`          // follow / recommend / city
    Cursor      string        `json:"cursor"`
    PageSize    int32         `json:"page_size"`
    Sources     []SourceStat  `json:"sources"`      // 各数据源读取量
    MergedCount int32         `json:"merged_count"` // 去重合并后候选数
    ReturnedCount int32       `json:"returned_count"`
    FilteredCount int32       `json:"filtered_count"` // 因详情缺失/状态异常被丢弃
    CostMs      int64         `json:"cost_ms"`
    Items       []TraceItem   `json:"items"`        // feed_id → source / position
}

type SourceStat struct {
    Source string `json:"source"` // FOLLOW_INBOX / VIP_OUTBOX / ...
    Count  int32  `json:"count"`
    CostMs int64  `json:"cost_ms"`
}

type TraceItem struct {
    FeedID   int64  `json:"feed_id"`
    Source   string `json:"source"`
    Position int32  `json:"position"`
    Score    int64  `json:"score"` // ZSet score（秒级时间戳）
}
```

### 6.2 存储

| Key | 结构 | 内容 | TTL |
|-----|------|------|-----|
| `feed:trace:{request_id}` | Hash | `meta` → Trace JSON（不含 Items）；`f:{feed_id}` → `source` | 24h（可配） |

- 只写 Redis，不落 MySQL：Trace 是排障数据，量大且时效短。
- 写入使用 Pipeline，且**失败不阻塞主流程**（与现有缓存策略一致，仅记日志 + 计数）。
- 采样：`TraceSampleRate` 默认 1.0（开发/测试全采），生产建议 0.1；但**来源标记不采样**，因为推荐原因依赖它。
- 容量估算：一页 20 条 → 约 21 个 field、< 2KB；QPS 200 且 TTL 24h 时约 200×86400×2KB ≈ 34GB，故生产必须降采样或将 TTL 降到 30min。

### 6.3 查询

- `GetFeedSource(request_id, feed_id, user_id)`：读 `f:{feed_id}`；命中即返回来源，未命中返回 `UNKNOWN`（由 [07](./07-recommend-reason.md) 走降级解释）。
- `GetFeedRequestTrace(request_id)`：读 `meta` + 全部 `f:*`，**仅内部用户可调用**；越权返回 `errorx.Forbidden`。
- 归属校验：Trace 的 `user_id` 必须与调用者一致（内部用户例外），防止用别人的 `request_id` 探测他人Feed。

## 7. 测试策略

| 层次 | 用例 |
|------|------|
| 单元 | `request_id` 非法输入（超长/含控制字符/含换行）被丢弃并重新生成 |
| 单元 | 来源优先级：同一 feed 同时在 inbox 与大 V outbox → 标记 `FOLLOW_INBOX` |
| 集成 | 一次 Timeline 请求：响应头/响应体 `request_id` 一致，且能在 Feed RPC 日志中检索到 |
| 集成 | Trace 写入后`GetFeedSource` 能取到正确来源；TTL 到期后返回 `UNKNOWN` |
| 集成 | 用A 用户的 `request_id`、以 B 用户身份查询 → `Forbidden` |
| 集成 | Redis 不可用时 Timeline 仍正常返回（Trace 降级） |

## 8. 演进与 TODO

- `request_id` 注入 MQ 事件后，补齐 Worker 侧日志字段与Trace 关联视图。
- Trace 增加下游 RPC 耗时明细（User/Interaction 聚合阶段）。
- 提供内部诊断页面（阶段五），按 `request_id` 展示 Gateway → Feed → Redis → 聚合层全链路。

---

## 关联文档

- [总览与定位](./00-overview.md)
- [架构与服务拆分](./01-architecture.md)
- [行为事件采集与指标聚合](./03-behavior-event.md)
- [推荐原因解释](./07-recommend-reason.md)
- [可观测性](./12-observability.md)
- [Feed 关注流设计](../feed/03-timeline-follow.md)
- [Feed 缓存策略](../feed/06-cache-strategy.md)
