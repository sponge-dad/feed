# 行为事件采集与指标聚合

> 定义 `feed-behavior-event` 事件契约、上报接口、幂等消费、明细落库与小时级指标聚合。属实施阶段一。

---

## 1. 概述与定位

现有系统只有点赞/收藏/评论三类显式互动（`interaction-event`、`comment-event`），缺少曝光与播放类隐式行为，导致创作者分析与兴趣画像无原料。本篇新增一条独立数据通道：

```text
客户端（批量） → Gateway 埋点接口 → RocketMQ feed-behavior-event → Behavior Worker
                                     ├─ 明细：feed_behavior_events
                                                                     ├─ 指标：Redis 累加 + feed_metrics_hourly
                                                                     └─ 画像：user:interest:{user_id}
```

与 `interaction-event`的关系：**不合并、不改造**。点赞/收藏仍走原Topic 保持写路径稳定；Behavior Worker 额外订阅 `interaction-event` 与 `comment-event`，只用于把 like/collect/comment 计入指标与兴趣画像，不参与落库。

## 2. 事件契约

新增 `common/event/behavior/event.go`：

```go
// TopicFeedBehaviorEvent Feed 行为事件 Topic。
// 注意：RocketMQ topic 仅允许^[%|a-zA-Z0-9_-]+$，故用连字符。
const TopicFeedBehaviorEvent = "feed-behavior-event"

// 行为类型（action_type）
const (
    ActionExpose        = "EXPOSE"         // 内容真正进入可视区域
    ActionPlay          = "PLAY"           // 起播
    ActionEffectivePlay = "EFFECTIVE_PLAY" // 有效播放
    ActionFinish        = "FINISH"         // 完播
    ActionSkip          = "SKIP"           // 快速划走
    ActionShare         = "SHARE"          // 分享
)

// FeedBehaviorEvent 单条行为事件。
type FeedBehaviorEvent struct {
    EventID         string `json:"event_id"`          // uuid v4，消费端幂等依据
    RequestID       string `json:"request_id"`        // 来源 Timeline 请求
    UserID          int64  `json:"user_id"`           // 由JWT 注入，不信任客户端
    FeedID          int64  `json:"feed_id"`
    AuthorID        int64  `json:"author_id"`         // 服务端从 Feed 详情校正
    ActionType      string `json:"action_type"`
    Position        int32  `json:"position"`          // 在本次 Feed 结果中的位置，从 0 开始
    WatchDurationMs int64  `json:"watch_duration_ms"`
    MediaDurationMs int64  `json:"media_duration_ms"`
    Timestamp       int64  `json:"timestamp"`         // 客户端行为时间（毫秒）
    ServerTime      int64  `json:"server_time"`       // 服务端接收时间（毫秒），用于纠偏
}
```

### 2.1 行为口径

| 行为 | 判定口径 | 客户端上报时机 | 服务端校验 |
|------|----------|----------------|------------|
| `EXPOSE` | 卡片可见面积 ≥ 50% 且持续 ≥ 500ms | 真实可视后| 接口返回不算曝光；同一 `(request_id, feed_id)` 只接受一次 |
| `PLAY` | 首帧渲染成功 | 起播 |无 `EXPOSE` 的 `PLAY` 仍接受，但打标`abnormal` 供数据质量监控 |
| `EFFECTIVE_PLAY` | `watch_duration_ms ≥ 3000` **或** `watch_duration_ms ≥ 0.5 × media_duration_ms` | 达阈值时一次 | 服务端**重新判定**，客户端结论不作准 |
| `FINISH` | `watch_duration_ms ≥ 0.95 × media_duration_ms` | 播放结束 | 同上重新判定 |
| `SKIP` | 起播后 `watch_duration_ms < 3000` 即离开 | 离开时 | 必须携带真实 `watch_duration_ms`，否则丢弃 |
| `SHARE` | 分享面板成功回调 | 触发时 | - |

服务端重新判定的意义：客户端可被篡改；把阈值放在服务端，口径变更也无需发版。阈值集中在配置 `BehaviorRule` 中（`EffectivePlayMs=3000`、`EffectivePlayRatio=0.5`、`FinishRatio=0.95`、`SkipMs=3000`）。

## 3. 上报接口

`POST /api/v1/feeds/behaviors`（JWT 必需），请求体：

```json
{
  "events": [
    {
      "client_event_id": "b1f2...",
      "request_id": "9f2c1d...",
      "feed_id": 88901,
      "action_type": "EFFECTIVE_PLAY",
      "position": 3,
      "watch_duration_ms": 5200,
      "media_duration_ms": 31000,
      "timestamp": 1785302400000
    }
  ]
}
```

服务端处理（Gateway logic，同步部分必须轻）：

| 步骤 | 规则 | 失败处理 |
|------|------|----------|
| 1 | `events` 长度 1~50，超限返回 `14004` | 整批拒绝 |
| 2 | `user_id` 一律取自 JWT，**忽略**请求体中的任何用户字段 | - |
| 3 | 字段校验：`feed_id > 0`、`action_type` 在枚举内、`0 ≤ watch_duration_ms ≤ 24h`、`position∈ [0, 1000]`、`timestamp` 与服务端时间偏差 ≤ 1h | 逐条丢弃非法事件，返回 `accepted` / `rejected` 计数 |
| 4 | `event_id =服务端生成 uuid v4`；`client_event_id` 仅作日志排查，**不作为幂等键**（防止客户端伪造导致真实事件被吞） | - |
| 5 | 批量 `BatchGetFeeds` 校验 feed 存在且 `status=NORMAL`，用真实 `user_id` 覆盖 `author_id`，用真实媒体时长覆盖 `media_duration_ms`（若Feed 侧有该字段） | 不存在则丢弃 |
| 6 | 单用户限流：`behavior:rate:{user_id}` 每分钟 ≤ 300 条事件（Redis INCR + EXPIRE） | 超限返回 `errorx.TooManyReq` |
| 7 | 逐条 `SendSync(TopicFeedBehaviorEvent, body)` | 发送失败记日志并计入 `feed_behavior_event_total{result="send_failed"}` |

响应：

```json
{ "code": 0, "message": "success", "data": { "accepted": 9, "rejected": 1 }, "request_id": "9f2c1d..." }
```

**幂等边界**：EXPOSE 的「同一 `(request_id, feed_id)` 只接受一次」由消费端用 `behavior:expose:{request_id}:{feed_id}`（SETNX，TTL 24h）保证，避免客户端重试造成曝光虚高。

## 4. Behavior Worker 消费

订阅（复用 `common/mq.Consumer`，跑在 Interaction RPC 进程内，与现有落库 consumer 并列）：

| Topic | 用途 |
|-------|------|
| `feed-behavior-event` | 明细 + 指标 + 兴趣 |
| `interaction-event` | 把 like/unlike/collect/uncollect 计入指标与兴趣 |
| `comment-event` | 把 comment 计入指标与兴趣 |

处理流程：

```text
1. json.Unmarshal 失败 → 记日志 + 返回 nil（消息体损坏不可恢复，避免死信堆积，与 Feed Worker 现有约定一致）
2. 幂等：SETNX behavior_event:{event_id}（TTL 24h）；已存在 → 直接返回 nil
3. 规则重判：EFFECTIVE_PLAY / FINISH / SKIP 按服务端阈值校正action_type
4. EXPOSE 去重：SETNX behavior:expose:{request_id}:{feed_id}
5. 明细落库（按采样策略）feed_behavior_events（唯一键 uk_event_id 兜底幂等）
6. 指标累加：HINCRBY feed:metrics:h:{feed_id}:{stat_hour} 对应字段
7. 兴趣更新：见 06-user-interest.md（需先取内容画像标签）
8. 任一持久化步骤失败 → DEL 幂等 key 后返回 error（RocketMQ 重试）
```

**为什么失败要删幂等 key**：SETNX 成功但后续落库失败时，若不删除，重试会被误判为「已处理」而永久丢数据。此处沿用 Feed Worker `handleCommentEvent` 的既有做法。

**重试与死信**：`common/mq.Consumer` 在回调返回 error 时返回 `ConsumeRetryLater`，由 RocketMQ 重试直至进入死信队列。日志必须包含 `topic / event_id / feed_id / user_id / action_type / reconsume_times`（见 [12](./12-observability.md)）。

## 5. 明细与采样

| 行为 | 落明细| 理由 |
|------|:---:|------|
| `PLAY` / `EFFECTIVE_PLAY` / `FINISH` / `SKIP` / `SHARE` | 全量 | 量级可控，是漏斗诊断核心 |
| `EXPOSE` | 采样（默认 10%，配置 `ExposeSampleRate`） | 曝光量最大，且指标聚合已在 Redis 累加，明细仅用于抽样核对 |

- 明细表只保留 30 天（`event_time` 定时清理），聚合结果长期保存。
- 明细表**不存**任何内容原文、IP、UA 等隐私字段。

## 6. 指标聚合

### 6.1 Redis 实时累加

| Key | 结构 | 字段 | TTL |
|-----|------|------|-----|
| `feed:metrics:h:{feed_id}:{yyyyMMddHH}` | Hash | `expose/play/effective_play/finish/skip/share/like/collect/comment/watch_ms` | 50h |
| `feed:metrics:dirty` | Set | 待flush 的 `{feed_id}:{stat_hour}` | 无（flush 后 SREM） |

### 6.2 落库

定时任务（默认 60s 一轮，`MetricsFlushIntervalSec`）：

```text
SPOP feed:metrics:dirty（批量，每轮上限 500）
  → HGETALL 对应 Hash
  → UPSERT feed_metrics_hourly：ON DUPLICATE KEY UPDATE expose_count = VALUES(expose_count) ...
```

**关键设计**：写入的是 Redis 里的**绝对值**而非增量。即使 flush 重复执行、或事件被重复消费（已被 `event_id` 拦截），小时表也不会重复累加——这是需求「行为事件重复消费不导致指标重复累加」的双重保证。

Redis 与 MySQL 的一致性窗口 ≤ 一个 flush 周期；Redis 丢失时可从 `feed_behavior_events` 明细重算（EXPOSE 因采样只能估算，故EXPOSE 以小时表为准且不重算）。

### 6.3 查询侧

- `GetFeedMetrics(feed_id, from, to)`：小时表求和；当 `to` 落在当前小时内时，叠加 Redis 当前小时 Hash，保证「最近 24 小时」口径不缺尾部数据。
- `BatchGetFeedMetrics`：单批 ≤ 100 个 feed_id。
- 指标定义与派生率见 [08-creator-metrics.md](./08-creator-metrics.md)。

## 7. 测试策略

| 层次 | 用例 |
|------|------|
| 单元 | 阈值重判：`watch=2900ms, media=4000ms` → 有效播放（比例 0.5 命中）；`watch=2000ms, media=30000ms` → SKIP |
| 单元 | 非法事件逐条丢弃且不影响同批合法事件 |
| 集成 | 同一事件重复投递 3 次 → 明细 1 条、指标只累加一次 |
| 集成 | 同一 `(request_id, feed_id)` 上报 5 次 EXPOSE → 曝光只+1 |
| 集成 | 落库失败注入→ 幂等 key 被删除，重试后成功且计数正确 |
| 集成 | flush 重复执行两次 → 小时表数值不变 |
| 压测 | 单实例 2000 events/s 下消费 lag 稳定，`feed_behavior_consume_lag` 不持续增长 |

## 8. 演进与 TODO

- 明细迁移到 ClickHouse，支持任意维度下钻。
- 曝光去重从 Redis SETNX 升级为布隆过滤器，降低内存。
- 引入设备维度反作弊（同设备高频曝光、播放时长异常分布）。
- `media_duration_ms` 由 Content Worker 回填到 `feeds`，彻底摆脱客户端上报值。

---

## 关联文档

- [请求标识与 Feed 链路追踪](./02-request-trace.md)
- [用户兴趣画像](./06-user-interest.md)
- [创作者作品表现分析](./08-creator-metrics.md)
- [数据模型](./10-data-model.md)
- [接口契约](./11-api.md)
- [Interaction 服务设计（含 MQ 事件）](../interaction/README.md)
