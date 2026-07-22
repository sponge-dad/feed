# MQ 事件与异步落库实现方法论

> 本文档描述 Interaction 服务如何通过 RocketMQ 将 Redis 中的互动行为异步持久化到 MySQL，
> 并驱动通知等下游业务。

---

## 1. 事件模型

### 1.1 Topic 与生产者

- Topic：`interaction.event`
- 生产者：Interaction Service
- 发送时机：Redis 更新成功后
- 发送方式：异步发送，失败记录日志/本地重试，不阻塞接口返回

### 1.2 事件体字段

| 字段 | 类型 | 说明 |
|------|------|------|
| user_id | int64 | 行为用户 |
| feed_id | int64 | 目标帖子 |
| action_type | int32 / enum | LIKE / UNLIKE / COLLECT / UNCOLLECT |
| timestamp | int64 | 行为发生时间，毫秒级 Unix |

可选扩展字段（未来按需）：

- `client_ip`：反作弊
- `source`：行为来源（首页 / 详情页 / 推送）

---

## 2. 消息有序性

### 2.1 为什么需要有序

- 同一用户对同一帖子的「点赞 → 取消点赞 → 再次点赞」事件若乱序消费，MySQL 可能停留在错误状态。

### 2.2 有序方案

- 使用 RocketMQ 的消息队列选择器，按 `hash(user_id, feed_id)` 选择固定队列。
- 同一个 `user_id + feed_id` 的事件始终进入同一个 MessageQueue，由同一消费者顺序处理。

```text
queue = hash(user_id, feed_id) % queue_count
```

### 2.3 乱序兜底

- 即使保证了队列内有序，也可能因重试、消费失败导致乱序。
- Consumer 消费时携带事件 `timestamp`，仅当事件时间 >= MySQL 记录 `updated_at` 时才更新。
- 通过 `status` 字段判断当前应如何落库，进一步兜底。

---

## 3. 异步落库 Consumer

### 3.1 消费组 1：互动持久化

- 消费组：`interaction-persistence-consumer`
- 职责：将事件落到 MySQL `likes` / `collections` 表。

#### 3.1.1 点赞事件落库逻辑

```text
1. SELECT status, updated_at FROM likes WHERE user_id = ? AND feed_id = ?
2. 若不存在：
     INSERT (user_id, feed_id, status=1, created_at, updated_at)
3. 若存在且 status = 2：
     UPDATE status = 1, updated_at = event.timestamp
4. 若存在且 status = 1：
     幂等，不操作
```

#### 3.1.2 取消点赞事件落库逻辑

```text
1. SELECT status, updated_at FROM likes WHERE user_id = ? AND feed_id = ?
2. 若存在且 status = 1 且 event.timestamp >= updated_at：
     UPDATE status = 2, updated_at = event.timestamp
3. 其他情况：
     幂等或乱序，不操作
```

### 3.2 消费组 2：通知服务

- 消费组：`interaction-notification-consumer`
- 职责：给帖子作者发送「有人点赞/收藏了你的帖子」通知。
- 无需写 DB，只调用通知服务。

---

## 4. 幂等与重试

### 4.1 消息幂等

- 同一事件可能因网络重试被消费多次。
- MySQL 唯一索引 `(user_id, feed_id)` 保证不会重复 INSERT。
- UPDATE 操作以 `status` 和 `updated_at` 为条件，避免重复更新。

### 4.2 失败重试

- Consumer 内部异常（如 DB 连接超时）抛出错误，RocketMQ 自动重试。
- 重试达到上限后进入死信队列（DLQ），由人工或兜底任务处理。

### 4.3 顺序消费失败阻塞

- 顺序消费模式下，单条消息处理失败会阻塞同一队列的后续消息。
- 因此 Consumer 应尽量快速幂等，遇到 DB 异常可短暂休眠后重试；遇到业务异常（如数据格式错误）直接记录并跳过，避免阻塞。

---

## 5. 与缓存一致性的协作

- Redis 是用户可见状态的唯一数据源。
- MySQL 是持久化和审计数据源。
- MQ 是两者之间的异步桥梁：
  - 正常情况下，Redis 更新后数毫秒到数秒内落库。
  - 异常情况下，Redis 数据通过 TTL 过期 + 回源重建，最终与 MySQL 一致。
- 定时校准任务兜底：每天低峰期抽样或全量比对 `feed:stats:*` 与 MySQL COUNT。

---

## 6. 监控与告警

| 指标 | 告警阈值建议 |
|------|-------------|
| `interaction.event` 消息堆积量 | > 10 万持续 5 分钟 |
| 消费延迟 | > 30 秒持续 5 分钟 |
| 消费失败率 | > 1% 持续 1 分钟 |
| 死信队列消息数 | > 0 立即告警 |
