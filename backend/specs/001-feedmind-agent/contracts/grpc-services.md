# Contract — gRPC 服务契约

**Feature**: FeedMind Agent | **Source**: `docs/design/agent/11-api.md` §3~6

**Proto 规范**:遵循 `docs/agent/proto-writing-guide.md`。**新增字段只能追加字段号，禁止复用与调整已有编号。**

---

## 1. Content RPC（新增服务，:9007/ metrics 9110）

`api/proto/content/content.proto`

```protobuf
service Content {
  rpc GetContentProfile(GetContentProfileReq) returns (GetContentProfileResp);
  rpc BatchGetContentProfile(BatchGetContentProfileReq) returns (BatchGetContentProfileResp);
  rpc SearchContent(SearchContentReq) returns (SearchContentResp);
  rpc RetryContentAnalysis(RetryContentAnalysisReq) returns (RetryContentAnalysisResp);
  rpc SubmitProfileFeedback(SubmitProfileFeedbackReq) returns (SubmitProfileFeedbackResp);
}
```

| 方法 | 说明 | 权限 |
|------|------|------|
| `GetContentProfile` | `feed_id` + `viewer_id`；**字幕/OCR 全文仅作者本人或内部可见**，其它调用方只返回 `category`/`summary`/`topics`/`scenes` | 分级 |
| `BatchGetContentProfile` | ≤ **50** 个 feed_id，**仅公开字段**（供兴趣画像与解释使用） | 内部服务间 |
| `SearchContent` | 入参为**结构化条件**（见 `05-content-search.md` §4），返回带匹配原因的结果 | 登录 |
| `RetryContentAnalysis` | `feed_id`、`force`；重置状态并重新入队 | 内部用户 |
| `SubmitProfileFeedback` | 创作者纠错反馈（`feed_id`、`field`、`comment`），**只记录不改画像** | 作者本人 |

**状态语义**（未完成/失败/禁用）见 `04-content-analysis.md` §8 与 [data-model.md](../data-model.md) §3.2。

---

## 2. Agent RPC（新增服务，:9006 / metrics 9108）

`api/proto/agent/agent.proto`

```protobuf
service Agent {
  rpc CreateSession(CreateSessionReq) returns (CreateSessionResp);
  rpc SendMessage(SendMessageReq) returns (SendMessageResp);   // 异步：返回 run_id
  rpc GetRun(GetRunReq) returns (GetRunResp);
  rpc GetSessionMessages(GetSessionMessagesReq) returns (GetSessionMessagesResp);
  rpc CancelRun(CancelRunReq) returns (CancelRunResp);
  rpc ListSessions(ListSessionsReq) returns (ListSessionsResp);
}
```

**关键约束**：

- 所有请求携带 `user_id`（由 **Gateway 从 JWT 注入**），**服务端不接受其它来源的身份**
- `SendMessage` **立即返回 `run_id`**；执行在后台 goroutine，超时由 `RunTimeoutMs` 控制
- `GetRun` **优先读 Redis** `agent:run:{run_id}`，未命中回查 MySQL
- 会话归属校验：`SendMessage` / `GetSessionMessages` 必须校验 `session.user_id == ctx.user_id`
- 幂等：同 `session_id` 并发 `SendMessage` 时，若已有 RUNNING 类Run，**直接返回该 Run**（不消耗额度）

---

## 3. Feed RPC 增量（改造）

`api/proto/feed/feed.proto`——**现有 8 个方法保持不变**，新增 5 个：

```protobuf
service Feed {
  // ……现有 8 个方法保持不变

  rpc GetFeedDetail(GetFeedDetailReq) returns (GetFeedDetailResp);
  rpc GetFeedBatch(GetFeedBatchReq) returns (GetFeedBatchResp);
  rpc GetFeedSource(GetFeedSourceReq) returns (GetFeedSourceResp);
  rpc GetFeedRequestTrace(GetFeedRequestTraceReq) returns (GetFeedRequestTraceResp);
  rpc GetCreatorFeedList(GetCreatorFeedListReq) returns (GetCreatorFeedListResp);
}
```

| 方法 | 关键入参 | 权限 | 备注 |
|------|----------|------|------|
| `GetFeedDetail` | `feed_id`、`viewer_id` | 公开内容 | 详情+ 作者信息聚合；已删除返回 **12001** |
| `GetFeedBatch` | `feed_ids`（≤**100**）、`viewer_id` | 公开内容 | 结果**按请求顺序**，缺失项跳过 |
| `GetFeedSource` | `request_id`、`feed_id`、`viewer_id` | trace 归属者 | 未命中返回 `UNKNOWN` |
| `GetFeedRequestTrace` | `request_id`、`viewer_id` | **内部用户** | 越权 `Forbidden` |
| `GetCreatorFeedList` | `author_id`、`page`、`page_size`、`feed_type` | 本人或内部 | 供创作者分析选择作品 |

### FeedSource 枚举

`FeedInfo` / `FeedBrief` 追加 `FeedSource source` 字段（枚举定义见 `02-request-trace.md` §5）：

| 值 | 枚举 | 含义 |
|---:|------|------|
| 0 | `FEED_SOURCE_UNKNOWN` | 未知（**默认值，语义安全**） |
| 1 | `FEED_SOURCE_FOLLOW_INBOX` | 关注收件箱（推模式） |
| 2 | `FEED_SOURCE_VIP_OUTBOX` | 大V 发件箱（拉模式） |
| 3 | `FEED_SOURCE_RECOMMEND_POOL` | 推荐池 |
| 4 | `FEED_SOURCE_CITY_POOL` | 同城池 |
| 5 | `FEED_SOURCE_INBOX_REBUILD` | 收件箱回源重建 |

---

## 4. Interaction RPC 增量（改造）

`api/proto/interaction/interaction.proto`——**现有 10 个方法保持不变**，新增 5 个：

```protobuf
service Interaction {
  // ……现有 10 个方法保持不变

  rpc GetFeedMetrics(GetFeedMetricsReq) returns (GetFeedMetricsResp);
  rpc BatchGetFeedMetrics(BatchGetFeedMetricsReq) returns (BatchGetFeedMetricsResp);
  rpc GetCreatorMetrics(GetCreatorMetricsReq) returns (GetCreatorMetricsResp);
  rpc GetPeerAverageMetrics(GetPeerAverageMetricsReq) returns (GetPeerAverageMetricsResp);
  rpc GetUserInterestProfile(GetUserInterestProfileReq) returns (GetUserInterestProfileResp);
}
```

| 方法 | 说明 | 权限 |
|------|------|------|
| `GetFeedMetrics` | 单 feed 原子指标 + 派生率，入参含 `window` | 作者本人/内部 |
| `BatchGetFeedMetrics` | ≤ **100** 个 feed_id，仅原子指标 | 内部服务间 |
| `GetCreatorMetrics` | 含 `viewer_id` **归属校验**，返回结构见 `08-creator-metrics.md` §6 | 作者本人/内部 |
| `GetPeerAverageMetrics` | 匿名聚合统计量，**禁止返回 feed_id/author_id** | 持有该 feed 的创作者 |
| `GetUserInterestProfile` | `user_id` **必须等于** `viewer_id`（内部例外），返回占比摘要 | 仅本人 |

---

## 5. Metadata 透传

| Key | 生成 | 消费 |
|-----|------|------|
| `x-request-id` | Gateway | 所有下游服务（日志、Trace 写入） |
| `x-trace-id` | OTel（`traceparent` 为主，该键为兼容保留） | 日志关联 |
| `x-agent-run-id` | Agent RPC | Agent 调用的下游服务日志 |

- **客户端拦截器**注入，**服务端拦截器**取出并绑定 `logx` 字段（见 `02-request-trace.md` §4）
- 键名统一定义在 `common/interceptors/keys.go`，**禁止各服务硬编码**
- ⚠️ **下游服务禁止从 Metadata 读取身份类信息（如 user_id）作为鉴权依据**——身份只走业务字段并由上游校验

---

## 6. 新增错误码

`common/errorx/errorx.go` 追加（现有：User 10000+、Relation 11000+、Feed 12000+、Comment 13000+、Interaction 14000+）：

```go
// ---------- Interaction 服务补充 14000~14999 ----------
const (
    InteractionBehaviorInvalid   = 14003 // 行为事件参数非法
    InteractionBehaviorBatchOver = 14004 // 行为事件批量超限
    InteractionMetricsForbidden  = 14005 // 无权查看该作品指标
    InteractionPeerInsufficient  = 14006 // 同类样本不足
)

// ---------- Content 服务 15000~15999 ----------
const (
    ContentProfileNotFound   = 15001 // 内容画像不存在
    ContentAnalysisRunning   = 15002 // 内容分析进行中
    ContentAnalysisFailed    = 15003 // 内容分析失败
    ContentTypeUnsupported   = 15004 // 该内容类型不支持分析
    ContentMediaInvalid      = 15005 // 媒体地址非法或不可访问
    ContentSearchEmptyQuery  = 15006 // 检索条件为空
    ContentSearchUnavailable = 15007 // 检索服务不可用
    ContentProfileForbidden= 15008 // 无权查看该内容的完整画像
)

// ---------- Agent 服务 16000~16999 ----------
const (
    AgentSessionNotFound   = 16001 // 会话不存在
    AgentSessionForbidden  = 16002 // 无权访问该会话
    AgentRunNotFound       = 16003 // 执行任务不存在
    AgentRunNotCancelable  = 16004 // 任务已结束，无法取消
    AgentIntentUnsupported = 16005 // 暂不支持该类问题
    AgentToolParamInvalid  = 16006 // 工具参数非法
    AgentToolCallFailed    = 16007 // 数据获取失败
    AgentLimitExceeded     = 16008 // 本次执行超过调用上限
    AgentModelError        = 16009 // 模型服务不可用
    AgentDataForbidden     = 16010 // 只能查询本人数据
    AgentInputTooLong      = 16011 // 输入内容过长
    AgentRunConflict       = 16012 // 当前会话已有任务在执行
)
```

**同步更新** `docs/design/api-spec/README.md` 的码段表（新增 Content / Agent 两段）。

---

## 7. 兼容原则

- 新增字段一律**可选**，老客户端不受影响；`FeedSource` 默认值 `UNKNOWN` **语义安全**
- 新增 RPC 方法**不改变既有方法签名与语义**
- Gateway 新路由**独立分组注册**
- 埋点与 Agent 接口失败**不得影响刷流与发帖主链路**
