# Contract — RocketMQ 事件

**Feature**: FeedMind Agent | **Source**: `docs/design/agent/03-behavior-event.md`、`04-content-analysis.md`

## 数据通道全景

```text
客户端（批量） → Gateway 埋点接口 → RocketMQ feed-behavior-event → Behavior Worker
                                                                   ├─ 明细：feed_behavior_events
                                                                   ├─ 指标：Redis 累加 + feed_metrics_hourly
                                                                   └─ 画像：user:interest:{user_id}

发帖 → Feed RPC → RocketMQ feed-created → Content Worker → 分析流水线 → feed_content_profiles + ES
```

---

## 1. `feed-behavior-event`（新增 Topic）

**契约定义位置**: `common/event/behavior/event.go`

```go
// TopicFeedBehaviorEvent Feed 行为事件 Topic。
// 注意：RocketMQ topic 仅允许 ^[%|a-zA-Z0-9_-]+$，故用连字符。
const TopicFeedBehaviorEvent = "feed-behavior-event"

// 行为类型（action_type）
const (
    ActionExpose        = "EXPOSE"         // 内容真正进入可视区域
    ActionPlay          = "PLAY"           // 起播
    ActionEffectivePlay = "EFFECTIVE_PLAY" // 有效播放
    ActionFinish        = "FINISH"         // 完播
    ActionSkip          = "SKIP"           // 快速划走
    ActionShare= "SHARE"          // 分享
)
```

**与 `interaction-event` 的关系**：**不合并、不改造**。点赞/收藏仍走原 Topic 保持写路径稳定。

### 上报接口校验规则（Gateway，7 条）

| # | 规则 | 违规处理 |
|---|------|---------|
| 1 | `events` 长度 **1~50** | 整批拒绝，返回 `14004` |
| 2 | `user_id` **一律取自 JWT**，**忽略**请求体中任何用户字段 | — |
| 3 | 字段校验：`feed_id > 0`、`action_type` 在枚举内、`0 ≤ watch_duration_ms ≤ 24h`、`position ∈ [0,1000]`、`timestamp` 与服务端偏差 ≤ 1h | **逐条**丢弃非法事件，返回 `accepted`/`rejected` 计数 |
| 4 | ⚠️ **`event_id` = 服务端生成 uuid v4**；`client_event_id` 仅作日志排查，**不作为幂等键**（防客户端伪造导致真实事件被吞） | — |
| 5 | 批量 `BatchGetFeeds` 校验 feed 存在且 `status=NORMAL`；**用真实 `author_id` 覆盖**、**用真实媒体时长覆盖** `media_duration_ms` | 不存在则丢弃 |
| 6 | 单用户限流：`behavior:rate:{user_id}` 每分钟 ≤ **300** 条（Redis INCR + EXPIRE） | 超限返回 `errorx.TooManyReq`（业务码 5） |
| 7 | 逐条 `SendSync(TopicFeedBehaviorEvent, body)` | 失败记日志 + `feed_behavior_event_total{result="send_failed"}` |

**响应**:
```json
{ "code": 0, "message": "success", "data": { "accepted": 9, "rejected": 1 }, "request_id": "9f2c1d..." }
```

### 幂等边界

**EXPOSE 的"同一 `(request_id, feed_id)` 只接受一次"由消费端**用 `behavior:expose:{request_id}:{feed_id}`（SETNX, TTL 24h）保证——避免客户端重试造成曝光虚高。

### SHARE 语义（特殊约定）

> **分享不做 owner 限制**——任何登录用户均可分享任意视频（含他人视频）。SHARE 的价值在于衡量"内容被传播的次数"；若限制为仅作者本人分享，指标将恒近 0，失去统计意义。**网关与 Worker 均不做 SHARE owner 校验。**

---

## 2. Behavior Worker 订阅

跑在 **Interaction RPC 进程内**（复用 `common/mq.Consumer`，与现有落库 consumer 并列）：

| Topic | 用途 |
|-------|------|
| `feed-behavior-event` | 明细 + 指标 + 兴趣 |
| `interaction-event`（已有） | 把 like/unlike/collect/uncollect **计入指标与兴趣**（不参与落库） |
| `comment-event`（已有） | 把 comment **计入指标与兴趣**（不参与落库） |

### 关键实现约束

**失败必须删除幂等 key**：
> SETNX 成功但后续落库失败时，若不删除 `behavior_event:{event_id}`，重试会被误判为"已处理"而**永久丢数据**。沿用 Feed Worker `handleCommentEvent` 的既有做法。

**重试与死信**：回调返回 error → `ConsumeRetryLater` → RocketMQ 重试直至死信队列。

**日志必须包含**：`topic` / `event_id` / `feed_id` / `user_id` / `action_type` / `reconsume_times`。

---

## 3. `feed-created` / `feed-deleted`（已有 Topic，Content Worker 新增订阅）

| Topic | Content Worker 行为 |
|-------|-------------------|
| `feed-created` | 若 `feed_type == 2`（视频）→ 启动分析流水线；否则置 `DISABLED` 并**直接 ACK 丢弃** |
| `feed-deleted` | 标记画像下线 + **从 ES 移除索引** |

**幂等**：`content:analysis:lock:{feed_id}`（TTL 6min）+ `uk_feed_id` 唯一键 + `media_hash + model_version` 判重跑（详见 [research.md](../research.md) R10）。

---

## 4. 全事件体必须携带 `request_id`

所有 MQ 事件体**必须包含 `request_id` 字段**，实现"发帖请求 → 异步分析 → 画像生成"的全链路串联。

| 事件 | request_id 来源 |
|------|----------------|
| `feed-behavior-event` | Timeline 请求的 request_id（客户端回传） |
| `feed-created` / `feed-deleted` | 发帖/删帖请求的 request_id |

---

## 5. 数据质量校验规则（5 条）

**Source**: `docs/design/agent/14-acceptance-test.md` §5

| # | 规则 |
|---|------|
| 1 | `play_count ≤ expose_count` |
| 2 | `finish_count ≤ play_count` |
| 3 | `effective_play_count ≤ play_count` |
| 4 | `skip_count ≤ play_count` |
| 5 | 客户端时间与服务端偏差 **P99 < 5min**，超出丢弃 |

> 客户端埋点不可控——**服务端重判阈值**（如EFFECTIVE_PLAY 的判定），不信客户端结论。

---

## 6. 采样策略

| 行为 | 落明细 | 理由 |
|------|:---:|------|
| `PLAY` / `EFFECTIVE_PLAY` / `FINISH` / `SKIP` / `SHARE` | **全量** | 量级可控，是漏斗诊断核心 |
| `EXPOSE` | **采样 10%** | 日活 10万 × 100 曝光 = 1000 万/日，全量不可承受 |

⚠️ **Redis 小时指标累加是全量的**（不采样）——指标必须精确，创作者看到的数字不能跳变。详见 [research.md](../research.md) R12。
