# 互动计数与状态查询实现方法论

> 本文档描述 Interaction 服务中「帖子互动计数」与「当前用户互动状态」查询模块的实现方法论。
> 这是被上游（Feed / Gateway）调用最频繁的能力，必须低延迟、支持批量。

---

## 1. GetFeedStats（单条帖子计数）

### 1.1 接口职责边界

`GetFeedStats` 返回指定帖子的：

- `like_count`
- `collect_count`

**它不应该做**：

- 不返回评论数（评论数由 Comment 服务维护）。
- 不返回当前用户是否已点赞/收藏（由 `GetUserInteractionStatus` 负责）。

### 1.2 查询路径

1. 优先读 Redis `feed:stats:{feed_id}`。
2. 命中则直接返回。
3. 未命中则回源 MySQL：
   - `SELECT COUNT(*) FROM likes WHERE feed_id = ? AND status = 1`
   - `SELECT COUNT(*) FROM collections WHERE feed_id = ? AND status = 1`
4. 将结果回写 Redis Hash 并设置 TTL。

### 1.3 计数为 0 也要缓存

- 若帖子没有点赞也没有收藏，回源后回写 `{like_count:0, collect_count:0}`。
- 避免「空结果」被反复回源打穿 MySQL。

---

## 2. BatchGetFeedStats（批量帖子计数）

### 2.1 批量读取禁止循环单查

- 使用 Redis Pipeline 对多个 `feed:stats:{feed_id}` 执行 `HMGET`。
- 未命中部分再批量查 MySQL：
  - `SELECT feed_id, COUNT(*) FROM likes WHERE feed_id IN (...) AND status = 1 GROUP BY feed_id`
  - `SELECT feed_id, COUNT(*) FROM collections WHERE feed_id IN (...) AND status = 1 GROUP BY feed_id`
- 回写缓存时，对每个 `feed_id` 单独 `HMSET` / `HSET`。

### 2.2 缺失 feed_id 的处理

- 如果某个 `feed_id` 在 MySQL 中也查不到记录，返回 `{like_count:0, collect_count:0}` 并缓存空值。
- 调用方传入的 `feed_id` 数量建议上限 100，避免单次请求过大。

---

## 3. GetUserInteractionStatus（单条帖子状态）

### 3.1 接口职责边界

`GetUserInteractionStatus` 返回当前用户对指定帖子的：

- `is_liked`
- `is_collected`

### 3.2 查询路径

1. 优先读 Redis：
   - `SISMEMBER like:feed:{feed_id} {user_id}`
   - `SISMEMBER collect:feed:{feed_id} {user_id}`
2. 若两个 Set 都存在且返回明确结果，直接返回。
3. 任一 Set 未命中，回源 MySQL 并重建对应 Set：
   - `SELECT status FROM likes WHERE user_id = ? AND feed_id = ?`
   - `SELECT status FROM collections WHERE user_id = ? AND feed_id = ?`

### 3.3 冷热数据边界

- 如果用户从未与该帖子互动过，MySQL 无记录，返回 `false`。
- 这种情况下是否需要写入 Redis？
  - 建议不写入，避免为海量「未互动」关系浪费内存。
  - 若产品需要频繁判断「是否已赞」且 Redis 命中率低，可考虑布隆过滤器优化。

---

## 4. BatchGetUserInteractionStatus（批量帖子状态）

### 4.1 批量读取禁止循环单查

- 使用 Redis Pipeline 对多个 `feed_id` 执行 `SISMEMBER`。
- 未命中部分批量查 MySQL `WHERE user_id = ? AND feed_id IN (...)`。
- 根据 MySQL 结果回填 Set（status = 1 时加入，status = 2 时不加入）。

### 4.2 性能目标

- 批量查询 20 个 feed_id 的 P99 < 30ms。
- 使用 Pipeline 将 Redis RTT 从 N 次降到 1 次。

---

## 5. 异常场景方法论

| 场景 | 处理原则 |
|------|---------|
| Redis 命中但 Set 已过期 | 回源 MySQL 重建，不返回错误 |
| MySQL 查询超时 | 返回错误，不返回脏数据；调用方降级展示 |
| 批量查询 feed_id 超过上限 | 截断或返回参数错误，由 proto validate 控制 |
| 用户 ID 未从 metadata 取到 | 返回未登录错误，禁止默认匿名 |

---

## 6. 与下游的协作

- **Redis**：`feed:stats:*`、`like:feed:*`、`collect:feed:*`。
- **MySQL**：`likes`、`collections` 表，仅在缓存未命中时回源。
