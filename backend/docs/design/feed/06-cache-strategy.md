# Feed 缓存策略方法论

> 本文档描述 Feed 服务各类缓存的维护策略、一致性保障和异常处理。
> 遵循项目统一的 Cache-Aside 原则，但针对 Feed 的多流场景做细化。
> 不包含完整代码。

---

## 1. 缓存分层

| 层级 | 代表 | 作用 |
|------|------|------|
| L1：本地缓存（可选） | go-zero 的本地缓存 | 热点帖子详情，QPS 极高时减少对 Redis 压力 |
| L2：Redis | `feed:{id}`、`inbox` 等 | 主要缓存层 |
| L3：MySQL | `feeds` 表 | 唯一数据源 |

V1 建议只使用 L2 + L3，L1 在压测发现 Redis 瓶颈后再引入。

---

## 2. Cache-Aside 在 Feed 中的落地

### 2.1 读流程

```
1. 读 Redis 缓存。
2. 命中则返回。
3. 未命中则读 MySQL。
4. 写回 Redis（设置 TTL）。
5. 返回结果。
```

### 2.2 写流程

```
1. 写 MySQL。
2. 删除相关 Redis 缓存（不是更新，是删除）。
3. 异步更新/重建需要的 Redis 结构（如推荐池、同城池）。
```

### 2.3 为什么写操作是删缓存而不是更新缓存

- 避免并发写导致缓存脏数据。
- 删除后下一次读请求自然触发重建，重建结果一定来自最新的 MySQL。

---

## 3. 各类缓存的维护细则

### 3.1 帖子详情 `feed:{feed_id}`

- **读取**：优先读 Redis Hash。
- **写入**：PublishFeed 后写入缓存；GetFeed 未命中时从 DB 回写。
- **删除**：DeleteFeed 时删除。
- **TTL**：30 天。
- **注意**：缓存字段与 DB 字段一一对应，不要缓存动态计数（点赞数等）。

### 3.2 发件箱 `outbox:{user_id}`

- **读取**：GetUserFeeds、关注流拉取大V outbox 时使用。
- **写入**：PublishFeed 后 ZADD。
- **删除**：DeleteFeed 时 ZREM。
- **容量**：保留最近 2000 条（大V）。
- **重建**：缓存丢失时，从 MySQL 按 `user_id` 查询最近 N 条重建。

### 3.3 收件箱 `inbox:{user_id}`

- **读取**：关注流使用。
- **写入**：Worker 在 `feed.created` 时批量写入。
- **删除**：Worker 在 `feed.deleted` 时批量删除；Relation 取关时可选择删除该用户相关 feed_id。
- **容量**：1000 条，超出时删除最旧。
- **重建**：缓存丢失时不建议全量重建（粉丝数可能很大），可标记为未初始化，由后续新帖子逐步填充。

### 3.4 推荐池 `feed:recommend`

- **读取**：推荐流使用。
- **写入**：定时任务重建 + 新帖实时 ZADD。
- **删除**：帖子删除时 ZREM；定时任务重建时自然淘汰旧内容。
- **容量**：10 万条。

### 3.5 同城池 `feed:city:{city_code}`

- **读取**：同城流使用。
- **写入**：Worker 在 `feed.created` 时 ZADD。
- **删除**：帖子删除时 ZREM。
- **容量**：2 万条/城市。

### 3.6 Timeline 热点缓存 `timeline:{user_id}:{tab}`

- **读取**：各 Timeline 接口先查该缓存。
- **写入**：首次计算后写入。
- **删除**：发帖、删帖、关注/取关时删除。
- **TTL**：推荐流 5 分钟、关注流 60 秒、同城流 1 分钟。

---

## 4. 缓存一致性保障

### 4.1 允许短时间不一致

- Feed 流是「最终一致」场景，用户可接受 1~60 秒延迟。
- 通过 TTL 控制缓存过期时间，过期后自动对齐 DB。

### 4.2 关键操作的缓存清理

| 操作 | 需要清理/删除的缓存 |
|------|-------------------|
| 发帖 | `outbox:{user_id}` ZADD、`feed:recommend` 刷新、`feed:city:{city_code}` ZADD、`timeline:{user_id}:*` 删除 |
| 删帖 | `feed:{feed_id}` 删除、`outbox:{user_id}` ZREM、`feed:recommend` ZREM、`feed:city:{city_code}` ZREM、`timeline:*:follow` 删除 |
| 关注 | `timeline:{user_id}:follow` 删除 |
| 取关 | `timeline:{user_id}:follow` 删除，并清理 inbox 中该用户的 feed_id（可选） |

### 4.3 缓存更新失败处理

- 缓存更新/删除失败，**不阻塞主流程**。
- 记录 error log，由监控告警。
- 下次读请求会触发缓存重建，自动修复。

---

## 5. 热点与穿透防护

### 5.1 缓存击穿

- 大V帖子详情被高并发访问时，若缓存失效，多个请求同时打到 MySQL。
- 解决：
  - 使用互斥锁（Redis SETNX），只允许一个请求回源。
  - 或设置热点 key 永不过期，由更新操作主动失效。

### 5.2 缓存穿透

- 查询不存在的 feed_id，每次都不命中，打到 MySQL。
- 解决：
  - 对不存在的 key 设置短时空值缓存（如 60 秒）。
  - 业务层校验 feed_id 范围（Snowflake ID 有时间戳位）。

### 5.3 缓存雪崩

- 大量 key 同时过期，导致 MySQL 压力突增。
- 解决：
  - TTL 加随机偏移，如 30 天 + rand(0, 3600) 秒。
  - 定时任务预热热点 key。

---

## 6. 监控指标

| 指标 | 意义 |
|------|------|
| feed 详情缓存命中率 | 评估 `feed:{id}` 效果 |
| timeline 缓存命中率 | 评估 `timeline:{user_id}:{tab}` 效果 |
| 缓存回源 QPS | 判断是否需要加本地缓存或预热 |
| 缓存更新失败率 | 发现 Redis 异常或网络问题 |
