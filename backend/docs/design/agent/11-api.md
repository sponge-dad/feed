# 接口契约

>汇总 Gateway HTTP 接口、各服务新增 gRPC 方法、Metadata 透传约定与新增错误码段。

---

## 1. 概述与约定

- HTTP 一律带 `/api/v1` 前缀，响应结构与分页规范沿用 `docs/design/api-spec/README.md`。
- 需求文档中的 `/api/agent/...`、`/api/feed/behaviors` 在本项目收敛为 `/api/v1/agent/...`、`/api/v1/feeds/behaviors`（见 [01](./01-architecture.md) 决策 D3/D4）。
- 所有接口除注册/登录外都需 `Authorization: Bearer <token>`，`user_id` 一律取自 JWT。
- proto 编写遵循 `docs/agent/proto-writing-guide.md`；新增字段只能追加字段号，禁止复用与调整已有编号。

## 2. Gateway HTTP 接口

| 方法 | 路径 | 权限 | 说明 |
|------|------|------|------|
| POST | `/api/v1/agent/sessions` | 登录 | 创建会话 |
| GET | `/api/v1/agent/sessions` | 登录 | 我的会话列表（Offset 分页） |
| POST | `/api/v1/agent/sessions/:sessionId/messages` | 会话归属者 | 发送消息，返回 `run_id` |
| GET | `/api/v1/agent/sessions/:sessionId/messages` | 会话归属者 | 会话消息列表（Cursor 分页） |
| GET | `/api/v1/agent/runs/:runId` | Run 归属者 | 查询执行状态与结果 |
| POST | `/api/v1/agent/runs/:runId/cancel` | Run 归属者 | 取消执行 |
| GET | `/api/v1/feeds/:feedId/content-profile` | 登录（分级返回） | 内容理解结果 |
| POST | `/api/v1/feeds/:feedId/content-profile/feedback` | 作者本人 | 提交识别纠错反馈（只记录，不改画像） |
| GET | `/api/v1/feeds/:feedId/recommendation-reason?request_id=` | 请求归属者 | 推荐原因 |
| POST | `/api/v1/feeds/behaviors` | 登录 | 批量行为上报（≤ 50 条） |
| GET | `/api/v1/creator/feeds/:feedId/metrics?window=24h` | 作者本人 | 作品指标 |
| GET | `/api/v1/internal/feed-requests/:requestId/trace` | 内部用户 | Feed 请求链路诊断 |

### 2.1 发送消息

```http
POST /api/v1/agent/sessions/1893201/messages
{ "content": "分析我这条视频为什么播放效果不好，feed_id 88901" }
```

```json
{
  "code": 0, "message": "success",
  "data": { "run_id": "1893202", "status": "CREATED", "message_id": "1893203" },
  "request_id": "9f2c1d..."
}
```

### 2.2 查询 Run

```json
{
  "code": 0, "message": "success",
  "data": {
    "run_id": "1893202",
    "status": "SUCCEEDED",
    "intent": "CREATOR_ANALYSIS",
    "answer": "该视频最近 24 小时获得 8,214 次曝光……",
    "references": [ { "type": "FEED", "feed_id": 88901 } ],
    "facts": { "metrics": { }, "peer": { }, "diagnosis": ["LOW_CTR", "EARLY_DROP"] },
    "tool_calls": [ { "seq": 1, "tool_name": "get_creator_metrics", "status": "SUCCESS", "cost_ms": 42 } ],
    "cost_ms": 5230,
    "error_code": ""
  },
  "request_id": "9f2c1d..."
}
```

`facts` 是后端计算出的结构化事实，前端可独立渲染图表；`answer` 只是它的自然语言表述。前端应以 `facts` 为准。

### 2.3 行为上报

请求与响应见 [03-behavior-event.md](./03-behavior-event.md) §3。要点：批量 ≤ 50，返回 `accepted`/`rejected`，超频返回业务码 5（`TooManyReq`），非法批量返回 `14004`。

### 2.4 推荐原因

响应见 [07-recommend-reason.md](./07-recommend-reason.md) §6。`request_id` 缺省时走降级解释。

## 3. Feed RPC 新增

```protobuf
service Feed {
  //……现有 8 个方法保持不变

  rpc GetFeedDetail(GetFeedDetailReq) returns (GetFeedDetailResp);// 详情 + 作者信息聚合
  rpc GetFeedBatch(GetFeedBatchReq) returns (GetFeedBatchResp);               // 批量详情（≤100）
  rpc GetFeedSource(GetFeedSourceReq) returns (GetFeedSourceResp);            // 某请求中该 feed 的来源
  rpc GetFeedRequestTrace(GetFeedRequestTraceReq) returns (GetFeedRequestTraceResp); // 内部诊断
  rpc GetCreatorFeedList(GetCreatorFeedListReq) returns (GetCreatorFeedListResp);    // 创作者作品列表
}
```

| 方法 | 关键入参 | 权限 | 备注 |
|------|----------|------|------|
| `GetFeedDetail` | `feed_id`、`viewer_id` | 公开内容 | 已删除返回 `12001` |
| `GetFeedBatch` | `feed_ids`（≤100）、`viewer_id` | 公开内容 | 结果按请求顺序，缺失项跳过 |
| `GetFeedSource` | `request_id`、`feed_id`、`viewer_id` | trace 归属者 | 未命中返回 `UNKNOWN` |
| `GetFeedRequestTrace` | `request_id`、`viewer_id` | 内部用户 | 越权 `Forbidden` |
| `GetCreatorFeedList` | `author_id`、`page`、`page_size`、`feed_type` | 本人或内部 | 供创作者分析选择作品 |

另需在 `FeedInfo` / `FeedBrief` 追加 `FeedSource source`（枚举定义见 [02](./02-request-trace.md) §5）。

## 4. Content RPC（新增服务）

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
| `GetContentProfile` | `feed_id` + `viewer_id`；字幕/OCR 全文仅作者本人或内部可见，其它调用方只返回 `category/summary/topics/scenes` | 分级 |
| `BatchGetContentProfile` | ≤ 50 个feed_id，仅返回公开字段（供兴趣画像与解释使用） | 内部服务间 |
| `SearchContent` | 入参为结构化条件（见 [05](./05-content-search.md) §4），返回带匹配原因的结果 | 登录 |
| `RetryContentAnalysis` | `feed_id`、`force`；重置状态并重新入队 | 内部用户 |
| `SubmitProfileFeedback` | 创作者纠错反馈（`feed_id`、`field`、`comment`） | 作者本人 |

状态语义（未完成/失败/禁用）见 [04-content-analysis.md](./04-content-analysis.md) §8。

## 5. Interaction RPC 新增

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
| `BatchGetFeedMetrics` | ≤ 100 个 feed_id，仅原子指标 | 内部服务间 |
| `GetCreatorMetrics` |含 `viewer_id` 归属校验，返回结构见 [08](./08-creator-metrics.md) §6 | 作者本人/内部 |
| `GetPeerAverageMetrics` | 匿名聚合统计量，禁止返回 feed_id/author_id | 持有该 feed 的创作者 |
| `GetUserInterestProfile` | `user_id` 必须等于 `viewer_id`（内部例外），返回占比摘要 | 仅本人 |

## 6. Agent RPC（新增服务）

```protobuf
service Agent {
  rpc CreateSession(CreateSessionReq) returns (CreateSessionResp);
  rpc SendMessage(SendMessageReq) returns (SendMessageResp);       // 异步：返回 run_id
  rpc GetRun(GetRunReq) returns (GetRunResp);
  rpc GetSessionMessages(GetSessionMessagesReq) returns (GetSessionMessagesResp);
  rpc CancelRun(CancelRunReq) returns (CancelRunResp);
  rpc ListSessions(ListSessionsReq) returns (ListSessionsResp);
}
```

-所有请求携带 `user_id`（由 Gateway 从 JWT 注入），服务端不接受其它来源的身份。
- `SendMessage` 立即返回 `run_id`；执行在后台 goroutine 中进行，超时由 `RunTimeoutMs` 控制。
- `GetRun` 优先读 Redis `agent:run:{run_id}`，未命中回查 MySQL。

## 7. Metadata 透传

| Key | 生成 | 消费 |
|-----|------|------|
| `x-request-id` | Gateway | 所有下游服务（日志、Trace 写入） |
| `x-trace-id` | OTel（`traceparent` 为主，该键为兼容保留） | 日志关联 |
| `x-agent-run-id` | Agent RPC | Agent 调用的下游服务日志 |

- 客户端拦截器负责注入，服务端拦截器负责取出并绑定 `logx` 字段（见 [02](./02-request-trace.md) §4）。
- 下游服务**禁止**从 Metadata 读取身份类信息（如 user_id）作为鉴权依据，身份只走业务字段并由上游校验。

## 8. 新增错误码

在 `common/errorx/errorx.go` 追加码段（现有：User 10000+、Relation 11000+、Feed 12000+、Comment 13000+、Interaction 14000+）：

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
    ContentProfileForbidden  = 15008 // 无权查看该内容的完整画像
)

// ---------- Agent 服务 16000~16999 ----------
const (
    AgentSessionNotFound  = 16001 // 会话不存在
    AgentSessionForbidden = 16002 // 无权访问该会话
    AgentRunNotFound      = 16003 // 执行任务不存在
    AgentRunNotCancelable = 16004 // 任务已结束，无法取消
    AgentIntentUnsupported = 16005 // 暂不支持该类问题
    AgentToolParamInvalid = 16006 // 工具参数非法
    AgentToolCallFailed   = 16007 // 数据获取失败
    AgentLimitExceeded    = 16008 // 本次执行超过调用上限
    AgentModelError       = 16009 // 模型服务不可用
    AgentDataForbidden    = 16010 // 只能查询本人数据
    AgentInputTooLong     = 16011 // 输入内容过长
    AgentRunConflict      = 16012 // 当前会话已有任务在执行
)
```

同步更新 `docs/design/api-spec/README.md` 的码段表（新增 Content / Agent 两段）。

## 9. 兼容原则

- 新增字段一律可选，老客户端不受影响；`FeedSource` 默认值 `UNKNOWN` 语义安全。
- 新增 RPC 方法不改变既有方法签名与语义。
- Gateway 新路由独立分组注册，避免影响现有 `feed` / `interaction` 组的中间件配置。
- 埋点接口与 Agent 接口失败时，**不得**影响刷流与发帖主链路（前端需容错）。

---

## 关联文档

- [请求标识与 Feed 链路追踪](./02-request-trace.md)
- [行为事件采集与指标聚合](./03-behavior-event.md)
- [内容分析服务设计](./04-content-analysis.md)
- [创作者作品表现分析](./08-creator-metrics.md)
- [Agent服务设计](./09-agent-service.md)
- [REST API 设计规范](../api-spec/README.md)
- [Proto 编写规范](../../agent/proto-writing-guide.md)
