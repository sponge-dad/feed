# 收藏与取消收藏实现方法论

> 本文档描述 Interaction 服务中「收藏 / 取消收藏」模块的实现方法论。
> 收藏与点赞在业务语义和产品入口上不同，但技术实现模式完全同构，
> 因此重点放在「如何复用点赞模式、同时保持数据隔离」。

---

## 1. CollectFeed（收藏）

### 1.1 接口职责边界

`CollectFeed` 只做三件事：

1. 参数与权限校验。
2. 更新 Redis（用户可见状态 + 计数）。
3. 发送 `interaction.event` 事件到 RocketMQ。

**它不应该做**：

- 不直接写 MySQL（由 MQ Consumer 异步完成）。
- 不发通知（通知服务消费 MQ 后处理）。
- 不维护 Feed 服务自己的计数宽表。

### 1.2 参数校验与鉴权

| 层级 | 位置 | 示例 |
|------|------|------|
| 语法校验 | go-zero 自动生成的 request validate | `feed_id > 0` |
| 业务校验 | logic 层手写 | 是否允许重复收藏？按产品需求，通常幂等 |
| 安全校验 | 用户 ID 来源 | 必须从 RPC metadata / JWT 中取 `user_id` |

### 1.3 幂等：已收藏再次收藏返回成功

- 使用 `SADD collect:feed:{feed_id} {user_id}` 的原子返回值判断：
  - 返回 `1`：新增收藏，计数 +1。
  - 返回 `0`：已经存在，幂等返回成功，计数不变。

### 1.4 Redis 更新顺序

```text
1. SADD    collect:feed:{feed_id}       {user_id}
2. ZADD    user:collects:{user_id}      {now_sec} {feed_id}
3. HINCRBY feed:stats:{feed_id}         collect_count 1   // 仅当 SADD 返回 1 时
```

- 三条命令使用 Pipeline 批量发送。
- `HINCRBY` 必须受 `SADD` 返回值保护。

---

## 2. UncollectFeed（取消收藏）

### 2.1 幂等：未收藏再次取消返回成功

- 使用 `SREM collect:feed:{feed_id} {user_id}` 的原子返回值判断：
  - 返回 `1`：确实取消，计数 -1。
  - 返回 `0`：本来就没收藏，幂等返回成功，计数不变。

### 2.2 Redis 更新顺序

```text
1. SREM    collect:feed:{feed_id}       {user_id}
2. ZREM    user:collects:{user_id}      {feed_id}
3. HINCRBY feed:stats:{feed_id}         collect_count -1  // 仅当 SREM 返回 1 时
```

- 同样用 Pipeline。
- `HINCRBY -1` 必须受 `SREM` 返回值保护，必要时先 `HGET` 防止负数。

---

## 3. 与点赞模块的复用关系

| 维度 | 点赞 | 收藏 |
|------|------|------|
| 接口 | `LikeFeed` / `UnlikeFeed` | `CollectFeed` / `UncollectFeed` |
| MySQL 表 | `likes` | `collections` |
| Redis Set | `like:feed:{feed_id}` | `collect:feed:{feed_id}` |
| Redis ZSet | `user:likes:{user_id}` | `user:collects:{user_id}` |
| Hash 字段 | `like_count` | `collect_count` |
| MQ action_type | `LIKE` / `UNLIKE` | `COLLECT` / `UNCOLLECT` |

**建议的代码组织**：

- 可以在 logic 层抽象一个 `interactionWriter` 工具结构，传入 key 前缀和 action_type 即可复用主流程。
- 但禁止把点赞、收藏的数据写入同一张表或同一个 Redis key，避免后续产品扩展时互相耦合。

---

## 4. 异常场景方法论

| 场景 | 处理原则 |
|------|---------|
| Redis SADD/SREM 成功，MQ 发送失败 | 返回成功，MQ 失败记录 error log |
| Redis 更新失败 | 直接返回错误，不继续发 MQ |
| 高并发重复收藏 | Redis Set 天然幂等 |
| 取消收藏时计数已为 0 | 不执行 HINCRBY -1，避免负数 |

---

## 5. 与下游的协作

- **Redis**：`collect:feed:{feed_id}`、`user:collects:{user_id}`、`feed:stats:{feed_id}`。
- **RocketMQ**：发送 `interaction.event`（action_type = COLLECT / UNCOLLECT）。
- **MySQL**：不直接写，由 MQ Consumer 异步落库。
