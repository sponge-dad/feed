# Interaction 缓存一致性方法论

> 本文档描述 Interaction 服务在「Redis 先行 + 异步落库」策略下，
> 如何保证计数正确、状态一致、缓存可重建。

---

## 1. 总体策略

Interaction 服务采用**写 Redis 优先**的削峰策略，这与项目通用的「先写 DB 再删缓存」不同：

| 场景 | 策略 | 原因 |
|------|------|------|
| 通用业务 | 先写 DB，再删缓存 | 保证 DB 是强一致数据源 |
| 点赞/收藏 | 先写 Redis，异步写 DB | 超高频写，直接写 DB 会打爆数据库 |

为弥补 Redis 先行带来的不一致窗口，采用以下补偿机制：

- **MQ 异步落库**：Redis 更新成功后立即发事件，Consumer 写 MySQL。
- **缓存过期重建**：Redis key 带 TTL，过期后读请求自动回源 MySQL。
- **定时校准**：每天低峰期全量或抽样校准 `feed:stats:*` 与 MySQL COUNT。

---

## 2. 计数非负保护

### 2.1 为什么计数会变负

- 取消操作时，如果 `SREM` 返回 1 但 `HINCRBY -1` 被重复执行，会导致负数。
- Redis 数据被手动清理或异常丢失后，状态与计数可能不一致。

### 2.2 保护策略

1. **操作级保护**：取消时先 `HGET` 当前计数，为 0 则不再减。
2. **校准级保护**：定时任务扫描所有 `feed:stats:*`，若发现负数则重置为 0 并标记审计。
3. **MySQL 回源保护**：缓存未命中时从 MySQL `COUNT` 重建，不会使用错误计数。

---

## 3. 缓存重建策略

### 3.1 feed:stats 重建

- 触发条件：key 不存在或已过期。
- 重建方式：
  ```text
  SELECT feed_id, COUNT(*) FROM likes WHERE feed_id = ? AND status = 1
  SELECT feed_id, COUNT(*) FROM collections WHERE feed_id = ? AND status = 1
  HMSET feed:stats:{feed_id} like_count x collect_count y
  EXPIRE feed:stats:{feed_id} 3600
  ```

### 3.2 like:feed / collect:feed 重建

- 触发条件：调用 `GetUserInteractionStatus` 或 `BatchGetUserInteractionStatus` 时发现 key 不存在。
- 重建方式：
  ```text
  SELECT user_id FROM likes WHERE feed_id = ? AND status = 1
  SADD like:feed:{feed_id} user_id1 user_id2 ...
  EXPIRE like:feed:{feed_id} 604800
  ```
- 注意：只对存在查询需求的帖子重建，避免全量预热。

### 3.3 user:likes / user:collects 重建

- 触发条件：调用列表接口时发现 key 不存在。
- 重建方式：
  ```text
  SELECT feed_id, created_at FROM likes WHERE user_id = ? AND status = 1 ORDER BY created_at DESC LIMIT N
  ZADD user:likes:{user_id} score1 feed_id1 score2 feed_id2 ...
  EXPIRE user:likes:{user_id} 2592000
  ```

---

## 4. 并发安全

### 4.1 单用户单帖子并发点赞/取消点赞

- Redis `SADD` / `SREM` 是原子操作，天然幂等。
- `HINCRBY` 也是原子操作。
- 但「判断 SADD 返回值 → 决定是否 HINCRBY」不是原子复合操作。
- 在单请求内按顺序执行即可，因为同一用户的同一请求不会并发到自己；不同用户之间的并发互不影响。

### 4.2 缓存重建与写操作并发

- 缓存重建可能读取到较旧的 MySQL 数据，而同时有新的点赞写 Redis。
- 由于写操作只增不删（Set/ZSet/Hash 都是增量更新），重建不会覆盖新写入的数据：
  - `SADD` 不会删除已存在成员。
  - `HINCRBY` 在重建后若继续有写，会叠加到正确值上。
- 唯一风险是重建时漏掉了刚发生的写，但该写已持久化到 MySQL，下次重建会补齐。

---

## 5. 降级与容灾

| 场景 | 处理 |
|------|------|
| Redis 完全不可用 | 写接口返回错误；读接口可降级为只读 MySQL（需限流，避免打爆 DB） |
| MQ 堆积 | 不影响用户感知，但 MySQL 与 Redis 不一致窗口拉长，需监控堆积量 |
| MySQL 延迟 | 缓存命中时不受影响；缓存未命中时回源可能读到旧数据，业务可接受 |

---

## 6. 与项目通用规范的衔接

- `docs/agent/dev-guidelines.md` 允许超高频写场景采用 Redis 先行 + MQ 异步落库。
- 普通查询接口（计数、状态、列表）仍遵循 Cache-Aside 模式：先读 Redis，未命中读 DB，再回写缓存。
