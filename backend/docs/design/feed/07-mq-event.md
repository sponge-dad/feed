# Feed MQ 事件与 Worker 设计方法论

> 本文档描述 Feed 服务如何通过 RocketMQ 与 Worker 解耦异步任务。
> 主要关注事件设计、消费流程和推拉结合在 Worker 中的落地。
> 不包含完整代码。

---

## 1. 为什么需要 MQ

Feed 服务中有两类操作不能同步完成：

1. **发帖后的粉丝推送**：普通用户发帖需要写入所有粉丝的 inbox，耗时与粉丝数成正比。
2. **删帖后的清理**：需要从推荐池、同城池、粉丝 inbox 中移除帖子。

MQ 的作用：

- 解耦发帖 RT 与推送耗时。
- 削峰：大V发帖或热点事件时，Worker 可控速消费。
- 可靠重试：消费失败可重试，不丢事件。

---

## 2. 事件定义

### 2.1 feed.created

**生产者**：Feed Service（发帖成功后）。

**消费者**：

- Feed Worker：执行推送或拉模式处理。
- 其他服务（可选）：通知服务、搜索服务、审核服务。

**事件体**：

```json
{
  "event_id": "uuid-v4",
  "event_type": "feed.created",
  "feed_id": 123456789,
  "user_id": 10001,
  "is_vip_feed": false,
  "city_code": "440300",
  "created_at": 1752998400000
}
```

> `created_at` 使用毫秒级 Unix 时间戳，与 proto 一致；写入 Redis ZSet 时转换为秒级。

### 2.2 feed.deleted

**生产者**：Feed Service（删帖成功后）。

**消费者**：

- Feed Worker：清理各池和粉丝 inbox。

**事件体**：

```json
{
  "event_id": "uuid-v4",
  "event_type": "feed.deleted",
  "feed_id": 123456789,
  "user_id": 10001,
  "city_code": "440300"
}
```

---

## 3. Feed Worker 处理 feed.created

### 3.1 判断推/拉模式

- 根据 `is_vip_feed` 决定：
  - `false`：普通用户，走推模式。
  - `true`：大V，走拉模式。

### 3.2 推模式：写入粉丝 inbox

```
1. 调用 Relation Service 获取粉丝列表（分批）。
2. 对每批粉丝：
   a. 使用 Redis Pipeline 对每个粉丝的 inbox ZADD feed_id。
   b. 检查 inbox 容量，超出则 ZREMRANGEBYRANK 删除最旧。
3. 记录推送进度，便于失败时断点续推。
4. 推送完成后，可选择发送通知给在线粉丝。
```

关键参数：

- 每批粉丝数：500。
- inbox 容量：1000。
- 推送超时：单批超时 5 秒，失败重试 3 次。

### 3.3 拉模式：只写入大V outbox

```
1. 将 feed_id ZADD 到 outbox:{user_id}。
2. 检查 outbox 容量，超出则删除最旧。
3. 将 feed_id ZADD 到推荐池和同城池（若 city_code 有效）。
```

### 3.4 推荐池与同城池更新

- 普通用户和大V都需要进入推荐池和同城池。
- 推荐池使用 score = `rand × time_decay`。
- 同城池使用 score = `created_at`。

---

## 4. Feed Worker 处理 feed.deleted

```
1. 从 feed:recommend ZREM feed_id。
2. 从 feed:city:{city_code} ZREM feed_id。
3. 从 outbox:{user_id} ZREM feed_id。
4. 若原帖是普通用户发布：
   a. 调用 Relation 获取粉丝列表（分批）。
   b. 从每个粉丝的 inbox ZREM feed_id。
5. 删除 feed:{feed_id} 缓存。
6. 删除相关 timeline 缓存。
```

---

## 5. 与 Relation 事件的关系

Relation 服务也会发送事件：

- `relation.created`：A 关注了 B。
- `relation.deleted`：A 取关了 B。

Feed Worker 可消费这些事件做增强：

### 5.1 relation.created

- 如果 B 是普通用户：无需处理，B 未来新帖会推送到 A 的 inbox。
- 如果 B 是大V：无需处理，A 的关注流会自动拉取 B 的 outbox。
- **可选增强**：将 B 的历史最近 N 条帖子回填到 A 的 inbox，提升新用户关注流内容丰富度。

### 5.2 relation.deleted

- 从 A 的 inbox 中移除 B 的 feed_id（可选，提升体验）。
- 不处理 outbox（B 的帖子仍对其他人可见）。

---

## 6. 可靠性设计

### 6.1 至少一次消费

- RocketMQ 默认至少一次消费，Worker 需要做幂等。
- 幂等方法：
  - `feed.created`：ZADD 是幂等的（member + score 相同多次执行结果一致）。
  - `feed.deleted`：ZREM 是幂等的（member 不存在时返回 0）。

### 6.2 消费失败重试

- 消费异常时返回 `RECONSUME_LATER`。
- 重试间隔：1s、5s、10s、30s、1m、2m、...、2h。
- 超过最大重试次数进入死信队列，人工介入。

### 6.3 顺序消费（可选）

- 同一用户的 feed.created 事件是否需要顺序消费？
- 推荐方案：不需要全局顺序，因为 ZADD 本身有 score，乱序消费结果一致。
- 但同一 feed 的 created 和 deleted 可能乱序到达，需要按事件时间戳判断（deleted 时间晚于 created 才执行删除）。

---

## 7. 性能优化

### 7.1 批量写入 Redis

- 每批粉丝 500，Pipeline 批量 ZADD。
- 不要逐条写 Redis，否则网络 RTT 会拖垮推送速度。

### 7.2 异步并行

- 多个 Worker 实例并行消费不同 MessageQueue。
- 单 Worker 内部可用 goroutine 池并发处理不同事件。

### 7.3 限流

- 大V发帖时，推模式粉丝数可能巨大，但大V走拉模式，不会出现推爆。
- 普通用户粉丝数上限受产品限制（如 5000），推模式可控。

---

## 8. 监控指标

| 指标 | 意义 |
|------|------|
| feed.created 消费延迟 | 发帖到推送完成的延迟 |
| feed.deleted 消费延迟 | 删帖到清理完成的延迟 |
| 单帖推送粉丝数 | 评估推模式压力 |
| 消费失败重试次数 | 发现 Worker 异常 |
| 死信队列积压 | 人工介入信号 |
