# 点赞与取消点赞实现方法论

> 本文档描述 Interaction 服务中「点赞 / 取消点赞」模块的实现方法论。
> 重点在于高频写削峰、Redis 先行、MySQL 异步落库和幂等控制。

---

## 1. LikeFeed（点赞）

### 1.1 接口职责边界

`LikeFeed` 只做三件事：

1. 参数与权限校验。
2. 更新 Redis（用户可见状态 + 计数）。
3. 发送 `interaction.event` 事件到 RocketMQ。

**它不应该做**：

- 不直接写 MySQL（由 MQ Consumer 异步完成）。
- 不发通知（通知服务消费 MQ 后处理）。
- 不更新 Feed 服务自己的计数宽表（Feed 读取 Interaction 的计数即可）。

### 1.2 参数校验与鉴权

| 层级 | 位置 | 示例 |
|------|------|------|
| 语法校验 | go-zero 自动生成的 request validate | `feed_id > 0` |
| 业务校验 | logic 层手写 | 当前用户不能点赞自己？产品决定 |
| 安全校验 | 用户 ID 来源 | 必须从 RPC metadata / JWT 中取 `user_id`，禁止从请求体读取 |

### 1.3 幂等：已点赞再次点赞返回成功

- 使用 `SADD like:feed:{feed_id} {user_id}` 的原子返回值判断：
  - 返回 `1`：新增点赞，计数 +1。
  - 返回 `0`：已经存在，幂等返回成功，计数不变。
- 不要用先 `SISMEMBER` 再 `SADD` 的两步操作，避免竞态条件。

### 1.4 Redis 更新顺序

```text
1. SADD    like:feed:{feed_id}          {user_id}
2. ZADD    user:likes:{user_id}         {now_sec} {feed_id}
3. HINCRBY feed:stats:{feed_id}         like_count 1   // 仅当 SADD 返回 1 时
```

- 三条命令使用 Pipeline 批量发送，降低 RT。
- `HINCRBY` 必须受 `SADD` 返回值保护，防止重复点赞导致计数虚高。

### 1.5 事件发送策略

- 点赞成功后发送 `interaction.event`。
- 事件体至少包含：
  - `user_id`
  - `feed_id`
  - `action_type = LIKE`
  - `timestamp`（毫秒级 Unix）
- MQ 发送失败怎么办？
  - 方案 A：同步发送，失败则整个点赞失败（简单，但增加 RT）。
  - 方案 B：异步发送，失败记录日志/本地队列重试（推荐，不影响用户感知 RT）。
- 本项目推荐方案 B：先写 Redis 再发 MQ，MQ 失败不阻塞点赞返回。

---

## 2. UnlikeFeed（取消点赞）

### 2.1 幂等：未点赞再次取消返回成功

- 使用 `SREM like:feed:{feed_id} {user_id}` 的原子返回值判断：
  - 返回 `1`：确实取消，计数 -1。
  - 返回 `0`：本来就没点赞，幂等返回成功，计数不变。

### 2.2 Redis 更新顺序

```text
1. SREM    like:feed:{feed_id}          {user_id}
2. ZREM    user:likes:{user_id}         {feed_id}
3. HINCRBY feed:stats:{feed_id}         like_count -1  // 仅当 SREM 返回 1 时
```

- 同样用 Pipeline。
- `HINCRBY -1` 必须受 `SREM` 返回值保护，防止计数变负。

### 2.3 计数非负保护

- 即使受 SREM 保护，极端情况下（Redis 脏数据、手动运维）仍可能出现 `like_count < 0`。
- 方案：在 `HINCRBY -1` 之前先 `HGET` 当前值；若已为 0 则不再减。
- 该检查会带来一次额外 RT，可只对取消操作做，点赞不需要。

---

## 3. 异常场景方法论

| 场景 | 处理原则 |
|------|---------|
| Redis SADD/SREM 成功，MQ 发送失败 | 返回成功，MQ 失败记录 error log，由重试/监控兜底 |
| Redis 更新失败 | 直接返回错误，不继续发 MQ；用户可重试 |
| 高并发重复点赞 | Redis Set 天然幂等，计数不会虚高 |
| 取消点赞时计数已为 0 | 不执行 HINCRBY -1，避免负数 |

---

## 4. 与下游的协作

- **Redis**：`like:feed:{feed_id}`、`user:likes:{user_id}`、`feed:stats:{feed_id}`。
- **RocketMQ**：发送 `interaction.event`（action_type = LIKE / UNLIKE）。
- **MySQL**：不直接写，由 MQ Consumer 异步落库。
