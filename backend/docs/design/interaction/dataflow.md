# Interaction 服务数据流

> 覆盖 `app/interaction/rpc/internal/logic/` 下全部 15 个 logic + 2 个 helper + 2 个 Worker 的数据流说明。

---

## interactHelper（核心辅助）

> 职责：点赞/收藏操作的通用引擎——冷 key 重建（Redis SET/ZSet/Hash 缓存预热）→ Lua 原子翻转 → MQ 下发。

### 涉及方法

- `interactHelper.add(ctx, constraint)` — 点赞/收藏
- `interactHelper.remove(ctx, constraint)` — 取消赞/取消收藏
- `interactHelper.isMember(ctx, constraint)` — 判断互动状态
- `interactHelper.page(ctx, constraint)` — 分页取互动列表

### 1. 入口与前置

- 入口：由 LikeFeed / UnlikeFeed / CollectFeed / UncollectFeed / GetUserInteractionStatus / GetUserLikedFeeds 等方法调用
- 前置：调用方已完成参数校验和权限判定

### 2. 主流程

**add（点赞/收藏）**:
1. `ensureSet(ctx, setKey)` — `EXISTS key` → 不存在则 `FindByUserId(ctx, userId)` MySQL → `SADD key member...` 回写
2. `ensureZSet(ctx, zsetKey)` — `EXISTS key` → 不存在则 `FindByUserId(ctx, userId)` MySQL → `ZADD key score member...` 回写
3. `ensureStats(ctx, statsKey)` — `EXISTS key` → 不存在则 `CountByFeedId(ctx, feedId)` MySQL → `HMSET key field value...` 回写
4. `Lua addScript` 原子执行：`SADD setKey feedId` + `ZADD zsetKey score feedId` + `HINCRBY statsKey like_count 1`（或 `collect_count`）
5. `refreshTTL(ctx, key...)` 续期
6. `publish(ctx, event)` — `Producer.SendSync(interaction.event, ...)` MQ 异步

**remove（取消赞/收藏）**:
1-3. 同 add 的 ensure 系列
4. `Lua removeScript` 原子执行：`SREM setKey feedId` + `ZREM zsetKey feedId` + `HINCRBY statsKey like_count -1`（或 `collect_count`）
5-6. 同 add

### 3. 依赖数据源清单

| 类型 | 名称 | 用途 | 关键说明 |
|------|------|------|----------|
| Redis | `SET/Lua SADD/SREM/HINCRBY/ZADD/ZREM` | 核心存储 | Lua 保证原子性 |
| MySQL | `interaction.FindBy*` | 冷启动回源 | SET/ZSet/Hash 为空时触发 |
| MQ | `interaction.event` | 异步持久化 | Worker 消费落库+累计指标 |

### 4. 失败与降级策略

| 失败点 | 策略 | 说明 |
|--------|------|------|
| ensure 回源 MySQL 失败 | 降级 | 不阻塞，Redis 写入创建空集合 |
| Redis 写失败 | 整体失败 | 核心路径不可降级 |
| MQ 发送失败 | 忽略 | 仅记日志；Worker 消费时有幂等兜底 |

### 5. 副作用

- MQ：`interaction.event` → 持久化 Worker 落库 + BehaviorWorker 累计指标。

### 6. 数据流图

**add（点赞/收藏）**:

```mermaid
sequenceDiagram
    participant Logic as Like/Collect Logic
    participant H as interactHelper
    participant Cache as Redis
    participant DB as MySQL: interaction
    participant MQ as RocketMQ

    Logic->>H: add(userId, feedId, actionType)
    H->>Cache: EXISTS like_set:{userId}
    alt 冷 key
        Cache-->>H: 0
        H->>DB: FindByUserId(userId)
        DB-->>H: [feed1, feed2, ...]
        H->>Cache: SADD like_set:{userId} (回写)
    end
    H->>Cache: EXISTS like_zset:{userId}
    alt 冷 key
        H->>Cache: ZADD like_zset:{userId} (回写)
    end
    H->>Cache: EXISTS feed:stats:{feedId}
    alt 冷 key
        H->>Cache: HMSET feed:stats:{feedId} (回写)
    end
    H->>Cache: Lua: SADD + ZADD + HINCRBY
    Cache-->>H: ok
    H->>Cache: EXPIRE (refreshTTL)
    H-->>Logic: ok
    H-)MQ: SendSync interaction.event (异步)
```

**remove（取消赞/收藏）**:

```mermaid
sequenceDiagram
    participant Logic as Unlike/Uncollect Logic
    participant H as interactHelper
    participant Cache as Redis
    participant DB as MySQL
    participant MQ as RocketMQ

    Logic->>H: remove(userId, feedId, actionType)
    H->>Cache: ensureSet/ensureZSet/ensureStats
    H->>Cache: Lua: SREM + ZREM + HINCRBY -1
    Cache-->>H: ok
    H->>Cache: EXPIRE (refreshTTL)
    H-->>Logic: ok
    H-)MQ: SendSync interaction.event (异步)
```

---

## LikeFeed / UnlikeFeed / CollectFeed / UncollectFeed

> 职责：点赞/取消赞/收藏/取消收藏——统一通过 `interactHelper.add/remove` 执行，返回操作后状态。

### 1. 入口与前置

- 入口：gRPC `Interaction.LikeFeed` / `UnlikeFeed` / `CollectFeed` / `UncollectFeed`
- 前置：Gateway 已校验 JWT，传入 userId

### 2. 参数校验

| 校验点 | 内容 | 失败错误码 |
|--------|------|-----------|
| userId / feedId | `<= 0` | `errorx.ParamError` |

### 3. 主流程

所有四个操作均为：

1. 构建 `interactionConstraint{userId, feedId, actionType:like/collect, isAdd:true/false}`
2. `interactHelper.add(ctx, c)` 或 `interactHelper.remove(ctx, c)`
3. 读回最新 `HGET feed:stats:{feedId}` → 返回 `InteractionStatus`（是否已赞/收藏 + 最新计数）

### 4. 依赖数据源清单

| 类型 | 名称 | 用途 | 关键说明 |
|------|------|------|----------|
| Redis | `interactHelper` 内部 | 核心操作 | 见上节 |
| Redis | `HGET feed:stats:{feedId}` | 返回最新计数 | — |
| MQ | `interaction.event` | 异步持久化 | 由 helper 内 publish |

### 5. 失败与降级策略

| 失败点 | 策略 | 说明 |
|--------|------|------|
| helper.add/remove 失败 | 整体失败 | — |
| 统计读失败 | 降级 | 返回 0 |

### 6. 副作用

- MQ → Worker 异步持久化。

### 7. 输出

- `pb.*Resp`：`IsLiked/IsCollected` + `LikeCount/CollectCount`

### 8. 数据流图

```mermaid
sequenceDiagram
    participant GW as Gateway
    participant I as InteractionRPC
    participant H as interactHelper
    participant Cache as Redis
    participant MQ as RocketMQ

    GW->>I: LikeFeed(userId, feedId)
    I->>I: 构建 constraint(actionType=like, isAdd=true)
    I->>H: add(constraint)
    activate H
    H->>Cache: ensureSet/ensureZSet/ensureStats
    H->>Cache: Lua: SADD + ZADD + HINCRBY
    H-)MQ: SendSync interaction.event
    deactivate H
    I->>Cache: HGETALL feed:stats:{feedId}
    Cache-->>I: stats
    I-->>GW: IsLiked + LikeCount
```

---

## GetFeedStats / BatchGetFeedStats

> 职责：获取帖子互动计数——`HGETALL feed:stats:{feedId}` Redis → 未命中回源 MySQL → `HSETNX` 回写。

### 1. 入口与前置

- 入口：gRPC `Interaction.GetFeedStats` / `BatchGetFeedStats`
- 前置：无

### 2. 主流程

**GetFeedStats**:
1. `HGETALL feed:stats:{feedId}` Redis
2. 命中 → 返回；未命中（空 Hash）→ `CountByFeedId(feedId)` MySQL
3. `HSETNX` 逐个字段回写

**BatchGetFeedStats**:
1. Pipeline `HGETALL feed:stats:{id1}` `HGETALL feed:stats:{id2}` ... Redis
2. 未命中收集 → `CountByFeedIds(missedIds)` MySQL `GROUP BY`
3. Pipeline `HSETNX` 逐个回写

### 3. 数据流图

**GetFeedStats**:

```mermaid
sequenceDiagram
    participant Caller as 调用方
    participant I as InteractionRPC
    participant Cache as Redis
    participant DB as MySQL: interaction

    Caller->>I: GetFeedStats(feedId)
    I->>Cache: HGETALL feed:stats:{feedId}
    alt 命中
        Cache-->>I: {like:10, collect:3}
    else 未命中
        Cache-->>I: {}
        I->>DB: CountByFeedId(feedId)
        DB-->>I: {like:10, collect:3}
        I-)Cache: HSETNX feed:stats:{feedId} (异步回写)
    end
    I-->>Caller: FeedStats
```

**BatchGetFeedStats**:

```mermaid
sequenceDiagram
    participant Caller as 调用方
    participant I as InteractionRPC
    participant Cache as Redis
    participant DB as MySQL: interaction

    Caller->>I: BatchGetFeedStats([id1, id2, id3])
    I->>Cache: Pipeline HGETALL id1, id2, id3
    Cache-->>I: [hit1, {}, hit3]
    Note over I: miss = [id2]
    I->>DB: CountByFeedIds([id2]) GROUP BY
    DB-->>I: {id2: {like:5, collect:1}}
    I-)Cache: Pipeline HSETNX (异步回写)
    I-->>Caller: map[id]FeedStats
```

---

## GetUserInteractionStatus / BatchGetUserInteractionStatus

> 职责：查询用户对帖子的互动状态（是否已赞/已收藏）。

### 1. 入口与前置

- 入口：gRPC `Interaction.GetUserInteractionStatus` / `Batch...`
- 前置：无

### 2. 主流程

**GetUserInteractionStatus**:
1. `SISMEMBER like_set:{userId} feedId` → `IsLiked`
2. `SISMEMBER collect_set:{userId} feedId` → `IsCollected`

**BatchGetUserInteractionStatus**:
1. Pipeline `EXISTS like_set:{userId} collect_set:{userId}` → 冷 key
2. 冷 key 走 `FindByUserIdFeedIds` MySQL 回源 → `SADD` 回写
3. Pipeline `SISMEMBER` 全部 ids

### 3. 数据流图

**GetUserInteractionStatus**:

```mermaid
sequenceDiagram
    participant Caller as 调用方
    participant I as InteractionRPC
    participant Cache as Redis

    Caller->>I: GetUserInteractionStatus(userId, feedId)
    I->>Cache: SISMEMBER like_set:{userId} feedId
    Cache-->>I: 1/0
    I->>Cache: SISMEMBER collect_set:{userId} feedId
    Cache-->>I: 1/0
    I-->>Caller: {IsLiked, IsCollected}
```

**BatchGetUserInteractionStatus**:

```mermaid
sequenceDiagram
    participant Caller as 调用方
    participant I as InteractionRPC
    participant Cache as Redis
    participant DB as MySQL: interaction

    Caller->>I: BatchGetUserInteractionStatus(userId, [id1, id2, id3])
    I->>Cache: Pipeline EXISTS like_set:{userId} collect_set:{userId}
    alt 冷 key
        Cache-->>I: [0, 0]
        I->>DB: FindByUserIdFeedIds(userId)
        DB-->>I: [feedIds...]
        I->>Cache: SADD (回写)
    end
    I->>Cache: Pipeline SISMEMBER like: id1, id2, id3
    Cache-->>I: [1, 0, 1]
    I->>Cache: Pipeline SISMEMBER collect: id1, id2, id3
    Cache-->>I: [0, 0, 0]
    I-->>Caller: map[id]InteractionStatus
```

---

## GetUserLikedFeeds / GetUserCollectedFeeds

> 职责：用户赞过/收藏过的帖子列表——`ZREVRANGEBYSCORE` 游标分页。

### 1. 入口与前置

- 入口：gRPC `Interaction.GetUserLikedFeeds` / `GetUserCollectedFeeds`
- 前置：无

### 2. 主流程

1. `interactHelper.page(ctx, constraint)` → 内部 `ensureZSet` → `ZREVRANGEBYSCORE zsetKey max cursor WITHSCORES LIMIT 0 pageSize`
2. 结果去 `FeedRpc.BatchGetFeeds` 取帖子内容
3. 组装返回

### 3. 数据流图

```mermaid
sequenceDiagram
    participant Caller as 调用方
    participant I as InteractionRPC
    participant H as interactHelper
    participant Cache as Redis
    participant F as FeedRpc
    participant DB as MySQL

    Caller->>I: GetUserLikedFeeds(userId, cursor, pageSize)
    I->>H: page(constraint, cursor, pageSize)
    H->>Cache: EXISTS like_zset:{userId}
    alt 冷 key
        H->>DB: FindByUserId(userId)
        DB-->>H: feeds
        H->>Cache: ZADD like_zset:{userId}
    end
    H->>Cache: ZREVRANGEBYSCORE like_zset:{userId}
    Cache-->>H: [(score, feedId), ...]
    H-->>I: [feedId...]
    I->>F: BatchGetFeeds(feedIds)
    F-->>I: feeds
    I-->>Caller: feeds + nextCursor
```

---

## GetFeedMetrics / BatchGetFeedMetrics

> 职责：帖子聚合指标——MySQL 按时间窗口累计（`feed_metrics_hourly` 聚合表）。

### 1. 入口与前置

- 入口：gRPC `Interaction.GetFeedMetrics` / `BatchGetFeedMetrics`
- 前置：无

### 2. 主流程

**GetFeedMetrics**:
1. `FeedRpc.GetFeed(feedId)` 获取帖子基本信息
2. `SumByFeedAndWindow(ctx, feedId, startTime, endTime)` MySQL 聚合查询
3. `metrics_calc.buildFeedMetrics(feed, sumRow)` 组装 pb 结构

**BatchGetFeedMetrics**:
1. `SumByFeedIDs(ctx, feedIds, startTime, endTime)` MySQL GROUP BY 批量聚合
2. 组装

### 3. 数据流图

**GetFeedMetrics**:

```mermaid
sequenceDiagram
    participant Caller as 调用方
    participant I as InteractionRPC
    participant F as FeedRpc
    participant DB as MySQL: feed_metrics_hourly

    Caller->>I: GetFeedMetrics(feedId, window)
    I->>F: GetFeed(feedId)
    F-->>I: feed info
    I->>DB: SumByFeedAndWindow(feedId, start, end)
    DB-->>I: {sum_like, sum_collect, ...}
    I->>I: buildFeedMetrics
    I-->>Caller: FeedMetrics
```

**BatchGetFeedMetrics**:

```mermaid
sequenceDiagram
    participant Caller as 调用方
    participant I as InteractionRPC
    participant DB as MySQL: feed_metrics_hourly

    Caller->>I: BatchGetFeedMetrics([id1, id2, id3], window)
    I->>DB: SumByFeedIDs([id1, id2, id3], start, end) GROUP BY feed_id
    DB-->>I: grouped sums
    I-->>Caller: map[id]FeedMetrics
```

---

## GetCreatorMetrics

> 职责：创作者聚合指标——汇总作者全部帖子的指标数据。

### 1. 入口与前置

- 入口：gRPC `Interaction.GetCreatorMetrics`
- 前置：Gateway 已校验 JWT

### 2. 主流程

1. `FeedRpc.GetUserFeeds(userId, ...)` 获取作者全部帖子
2. `BatchGetFeedMetrics(feedIds, window)` 批量聚合指标
3. `aggregateFeedMetrics(allFeedsMetrics)` 汇总：总曝光/点赞/收藏/评论数 + 百分位
4. 可选：创作者本人校验（getMe 场景）

### 3. 数据流图

```mermaid
sequenceDiagram
    participant GW as Gateway
    participant I as InteractionRPC
    participant F as FeedRpc
    participant DB as MySQL: feed_metrics_hourly

    GW->>I: GetCreatorMetrics(userId, window)
    I->>F: GetUserFeeds(userId)
    F-->>I: [feed1, feed2, ...]
    I->>DB: SumByFeedIDs(feedIds, start, end) GROUP BY
    DB-->>I: per-feed sums
    I->>I: aggregateFeedMetrics → 汇总+百分位
    I-->>GW: CreatorMetrics
```

---

## GetPeerAverageMetrics

> 职责：同类平均值对比——Feed 信息 → Content 画像 → ES 搜索同级内容 → 聚合。

### 1. 入口与前置

- 入口：gRPC `Interaction.GetPeerAverageMetrics`
- 前置：无

### 2. 主流程

1. `FeedRpc.GetFeed(feedId)` 基础信息
2. `ContentRpc.GetContentProfile(feedId)` 画像（分类/标签/时长）
3. `ContentRpc.SearchContent(ctx, dsl)` ES 搜索同类内容（同分类+时长区间）
4. 过滤 ES 结果中与目标帖子时长偏差过大者
5. `SumByFeedIDs(ctx, peerFeedIds, window)` MySQL 聚合同类指标
6. 计算百分位（目标值在同类中的位置）

### 3. 数据流图

```mermaid
sequenceDiagram
    participant Caller as 调用方
    participant I as InteractionRPC
    participant F as FeedRpc
    participant C as ContentRpc
    participant ES as Elasticsearch
    participant DB as MySQL: feed_metrics_hourly

    Caller->>I: GetPeerAverageMetrics(feedId, window)
    I->>F: GetFeed(feedId)
    F-->>I: feed (duration)
    I->>C: GetContentProfile(feedId)
    C-->>I: profile (category, tags)
    I->>C: SearchContent(category, duration_range, ...)
    C->>ES: Search(index, dsl)
    ES-->>C: peer feedIds
    C-->>I: peer feedIds
    I->>I: 过滤时长偏差
    I->>DB: SumByFeedIDs(peerIds, start, end) GROUP BY
    DB-->>I: per-feed sums
    I->>I: 百分位计算
    I-->>Caller: PeerAverageMetrics
```

---

## GetUserInterestProfile

> 职责：用户兴趣画像——Redis 快照 → 未命中回源 MySQL → 比例计算。

### 1. 入口与前置

- 入口：gRPC `Interaction.GetUserInterestProfile`
- 前置：无

### 2. 主流程

1. `interest.BuildSnapshot(ctx, userId)` Redis `HGETALL interest:{userId}`
2. 命中 → 直接使用；未命中 → `FindOneByUserId(ctx, userId)` MySQL
3. 计算各兴趣类目的占比

### 3. 数据流图

```mermaid
sequenceDiagram
    participant Caller as 调用方
    participant I as InteractionRPC
    participant Cache as Redis
    participant DB as MySQL: user_interest

    Caller->>I: GetUserInterestProfile(userId)
    I->>Cache: HGETALL interest:{userId}
    alt 命中
        Cache-->>I: {cat1: 30, cat2: 20, ...}
    else 未命中
        Cache-->>I: {}
        I->>DB: FindOneByUserId(userId)
        DB-->>I: interest record
    end
    I->>I: ratio calculation (each / total)
    I-->>Caller: UserInterestProfile
```

---

## 持久化 Worker

> 职责：消费 `interaction.event` MQ，将 Redis 先行写入的数据幂等持久化到 MySQL。

### 1. 入口

- 异步消费 `interaction.event` topic

### 2. 主流程

1. 反序列化事件 → `userId, feedId, actionType, isAdd`
2. `FindOneByUserIdFeedId(ctx, userId, feedId)` MySQL 查已有记录
3. 存在 → `UpdateStatusIfNewer(ctx, record, newStatus)`（幂等，仅更新更新的）
4. 不存在 → `Insert(ctx, record)`
   - 并发冲突（1062 Duplicate Key）→ 转为 Update

### 3. 数据流图

```mermaid
sequenceDiagram
    participant MQ as RocketMQ
    participant W as Persist Worker
    participant DB as MySQL: interaction

    MQ-)W: 消费 interaction.event (userId, feedId, action, isAdd)
    W->>DB: FindOneByUserIdFeedId(userId, feedId)
    alt 记录存在
        DB-->>W: record
        W->>DB: UpdateStatusIfNewer(幂等)
    else 记录不存在
        DB-->>W: nil
        W->>DB: INSERT
        alt 并发冲突
            DB-->>W: 1062 Duplicate Key
            W->>DB: Update (降级为更新)
        end
    end
```

---

## BehaviorWorker

> 职责：消费 `interaction.event` → 去重 → 行为记录落库 → 小时桶指标累计（Lua HINCRBY）。

### 1. 入口

- 异步消费 `interaction.event` topic

### 2. 主流程

**消费时**:
1. `SETNX behavior:dedup:{eventId} 1 ttl` Redis 去重
2. `ruleRejudge` 规则复审（质量/作弊检测）
3. `EXPOSE` 摘要上报（可选）
4. `Insert(ctx, &FeedBehaviorEvent{...})` MySQL 落行为明细
5. Lua `HINCRBY feed:metrics:{feedId}:{hour_bucket} like_count 1` 小时桶累计
6. `HINCRBY interest:{userId} {category} 1` 兴趣画像增量

**flushLoop（定时）**:
1. `claimDirty()` 获取脏小时桶列表
2. `HGETALL feed:metrics:{id}:{bucket}` → 检查指标合理性（quality check）
3. `Upsert(ctx, &FeedMetricsHourly{...})` 写入 MySQL 聚合表

### 3. 数据流图

```mermaid
sequenceDiagram
    participant MQ as RocketMQ
    participant W as BehaviorWorker
    participant Cache as Redis
    participant DB as MySQL

    MQ-)W: 消费 interaction.event
    W->>Cache: SETNX behavior:dedup:{eventId} (去重)
    alt 已处理
        Cache-->>W: 0 → skip
    else 首次处理
        Cache-->>W: 1
        W->>W: ruleRejudge (质量/作弊)
        W->>DB: INSERT feed_behavior_events
        W->>Cache: Lua HINCRBY feed:metrics:{id}:{bucket}
        W->>Cache: HINCRBY interest:{userId} {category}
    end

    Note over W: flushLoop (定时)
    W->>Cache: 获取脏桶 → HGETALL
    W->>W: quality check
    W->>DB: Upsert feed_metrics_hourly
```

---

## 关联文档

- [Interaction 设计总览](./00-overview.md)
- [点赞](./02-like/02-like.md)
- [收藏](./03-collect/03-collect.md)
- [缓存策略](./06-cache/06-cache.md)
- [MQ 事件](./07-mq-event/07-mq-event.md)
- [创作者指标](../agent/08-creator-metrics.md)
- [用户兴趣画像](../agent/06-user-interest.md)
- [Logic 数据流生成提示词](../../agent/logic-dataflow-guide.md)
