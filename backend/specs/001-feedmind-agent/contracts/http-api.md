# Contract — Gateway HTTP 接口

**Feature**: FeedMind Agent | **Source**: `docs/design/agent/11-api.md` §2

## 约定

- 统一 `/api/v1` 前缀；响应结构与分页规范沿用 `docs/design/api-spec/README.md`
- 除注册/登录外全部需 `Authorization: Bearer <token>`，**`user_id` 一律取自 JWT**
- 响应体统一含 `code` / `message` / `data` / `request_id`
- **新路由独立分组注册**，避免影响现有 `feed` / `interaction` 组的中间件配置

> 路径收敛（决策 D3/D4）：需求文档中的 `/api/agent/...`、`/api/feed/behaviors` 在本项目收敛为 `/api/v1/agent/...`、`/api/v1/feeds/behaviors`。

## 端点清单（12 个）

| # | 方法 | 路径 | 权限 | 说明 |
|---|------|------|------|------|
| 1 | POST | `/api/v1/agent/sessions` | 登录 | 创建会话 |
| 2 | GET | `/api/v1/agent/sessions` | 登录 | 我的会话列表（**Offset** 分页） |
| 3 | POST | `/api/v1/agent/sessions/:sessionId/messages` | 会话归属者 | 发送消息，返回 `run_id` |
| 4 | GET | `/api/v1/agent/sessions/:sessionId/messages` | 会话归属者 | 会话消息列表（**Cursor** 分页） |
| 5 | GET | `/api/v1/agent/runs/:runId` | Run 归属者 | 查询执行状态与结果 |
| 6 | POST | `/api/v1/agent/runs/:runId/cancel` | Run 归属者 | 取消执行 |
| 7 | GET | `/api/v1/feeds/:feedId/content-profile` | 登录（**分级返回**） | 内容理解结果 |
| 8 | POST | `/api/v1/feeds/:feedId/content-profile/feedback` | 作者本人 | 识别纠错反馈（**只记录，不改画像**） |
| 9 | GET | `/api/v1/feeds/:feedId/recommendation-reason?request_id=` | 请求归属者 | 推荐原因 |
| 10 | POST | `/api/v1/feeds/behaviors` | 登录 | **批量**行为上报（≤ 50 条） |
| 11 | GET | `/api/v1/creator/feeds/:feedId/metrics?window=24h` | 作者本人 | 作品指标 |
| 12 | GET | `/api/v1/internal/feed-requests/:requestId/trace` | **内部用户** | Feed 请求链路诊断 |

## 关键接口示例

### 发送消息（#3）

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

**异步语义**：立即返回 `run_id`，执行在后台 goroutine 进行，客户端轮询 #5。

### 查询 Run（#5）

```json
{
  "code": 0, "message": "success",
  "data": {
    "run_id": "1893202",
    "status": "SUCCEEDED",
    "intent": "CREATOR_ANALYSIS",
    "answer": "该视频最近 24 小时获得 8,214 次曝光……",
    "references": [ { "type": "FEED", "feed_id": 88901 } ],
    "facts": { "metrics": {}, "peer": {}, "diagnosis": ["LOW_CTR", "EARLY_DROP"] },
    "tool_calls": [ { "seq": 1, "tool_name": "get_creator_metrics", "status": "SUCCESS", "cost_ms": 42 } ],
    "cost_ms": 5230,
    "error_code": ""
  },
  "request_id": "9f2c1d..."
}
```

> ⚠️ **`facts` 是后端计算出的结构化事实，前端应以 `facts` 为准**渲染图表；`answer` 只是它的自然语言表述。

### 行为上报（#10）

- 批量 ≤ **50** 条，返回 `accepted` / `rejected`
- 超频返回业务码 **5**（`TooManyReq`）；非法批量返回 **14004**
- 详细请求/响应见 `docs/design/agent/03-behavior-event.md` §3

### 推荐原因（#9）

`request_id` 缺省时**走降级解释**。响应见 `07-recommend-reason.md` §6。

## 容错要求（兼容原则）

> 埋点接口与 Agent 接口失败时，**不得影响刷流与发帖主链路**——前端需容错。

## 新增中间件

| 中间件 | 作用 |
|--------|------|
| `RequestIDMiddleware` | 生成/校验 request_id，写响应头 `X-Request-Id` 与响应体 `request_id` |

**校验规则**：客户端传入的 `X-Request-Id` 须匹配 `^[A-Za-z0-9_-]+$` 且长度 ≤ 64，否则**丢弃并重新生成**（32 位十六进制 = uuid v4 去横线）。
