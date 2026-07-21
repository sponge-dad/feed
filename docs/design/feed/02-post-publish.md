# 帖子发布与内容管理方法论

> 本文档描述 Feed 服务中「发帖、删帖、查帖子」模块的实现方法论。
> 重点在于参数校验、写入流程、事件触发和幂等控制，不包含完整代码。

---

## 1. PublishFeed（发帖）

### 1.1 接口职责边界

PublishFeed 只做三件事：

1. 参数校验与风控预审。
2. 写入 MySQL `feeds` 表。
3. 发送 `feed.created` 事件到 RocketMQ。

**它不应该做**：

- 不直接写入粉丝的 inbox（那是 Worker 的事）。
- 不调用 User Service 更新用户计数（避免同步链路过长）。
- 不处理封面图/视频转码（由对象存储 + 异步任务处理）。

### 1.2 参数校验方法论

校验分层：

| 层级 | 位置 | 示例 |
|------|------|------|
| 语法校验 | go-zero 自动生成的 request validate | 字段非空、类型、长度 |
| 业务校验 | logic 层手写 | 图文至少 1 张图、视频必须带 cover_url、城市编码合法 |
| 安全校验 | Gateway 层 | 敏感词、图片鉴黄、频率限制 |

Feed Service 内部主要做业务校验：

- `feed_type = 1`（图文）时，`media_urls` 不能为空。
- `feed_type = 2`（视频）时，`media_urls` 有且仅有 1 个视频 URL，`cover_url` 必填。
- `title`、`description` 长度上限按产品需求定。
- 不能替其他用户发帖，`user_id` 从 RPC metadata / JWT 中提取。

### 1.3 ID 与创建时间生成

- `id` 由 `common/idgen` 生成，调用一次即可。
- `created_at` 使用生成 ID 时的秒级 Unix 时间戳，确保：
  - MySQL 写入时间与 Redis ZSet score 一致。
  - 后续 Cursor 分页稳定。
- 不要在 MySQL 中用 `DEFAULT CURRENT_TIMESTAMP`，否则可能和 Redis score 有几秒偏差。

### 1.4 写入 MySQL 策略

- 直接 INSERT，无需先 SELECT 检查。
- 软删除不影响：重新发布的新帖子是新记录。
- 记录 `status = 1`（正常），`is_vip_feed` 根据作者是否大V决定（调用 Relation.IsVip）。

### 1.5 事件发送策略

- 发帖成功后发送 `feed.created` 事件。
- 事件体至少包含：
  - `feed_id`
  - `user_id`
  - `is_vip_feed`
  - `city_code`
  - `created_at`
- MQ 发送失败怎么办？
  - 方案 A：同步发送，失败则整个发帖失败（简单，但增加发帖延迟）。
  - 方案 B：异步发送，失败记录日志/本地队列重试（推荐，不影响发帖 RT）。
- 本项目推荐方案 B：先写 DB 再发 MQ，MQ 失败不阻塞发帖返回。

### 1.6 发布后的缓存处理

- 发帖成功后立即删除（或更新）相关缓存：
  - 作者本人的 `outbox:{user_id}` —— 可以直接 ZADD，也可以先删除缓存让下次查询重建。
  - 推荐池 `feed:recommend` —— 由后台定时任务刷新，或者实时 ZADD 一条。
  - 同城池 `feed:city:{city_code}` —— 实时 ZADD。
  - 作者本人的 `timeline:{user_id}:*` —— 删除，让下次读取重建。
- 推荐原则：**先写 DB，再删/更新缓存**。

---

## 2. DeleteFeed（删帖）

### 2.1 软删除流程

1. 校验当前用户是帖子作者（`user_id` 从 metadata 取）。
2. 更新 `feeds` 表 `status = 2`。
3. 删除/更新相关 Redis 缓存：
   - `feed:{feed_id}` 删除。
   - `outbox:{user_id}` 中移除该 feed_id。
   - `feed:recommend`、`feed:city:{city_code}` 中移除。
   - 所有可能包含该帖子的 `timeline:{user_id}:*` 删除（可用 key 扫描或设置短期过期）。
4. 发送 `feed.deleted` 事件，通知 Worker 清理粉丝的 inbox。

### 2.2 权限校验

- Feed Service 内部必须校验 `user_id == feed.user_id`。
- 管理员删除、违规下架等场景通过 Gateway 转发到专门的审核服务处理，不直接调用 Feed.DeleteFeed。

### 2.3 幂等

- 对同一条已删除帖子再次调用 DeleteFeed，返回成功（幂等）。
- 对不存在的帖子调用 DeleteFeed，返回成功或「帖子不存在」均可，但需统一。

---

## 3. GetFeed / BatchGetFeeds（查询帖子）

### 3.1 单条详情

- 优先读 `feed:{feed_id}` 缓存。
- 缓存未命中查 MySQL，回写缓存。
- 返回字段：帖子基础字段 + 是否已删除（status）。
- 不在这里返回作者信息和互动计数，调用方自行聚合。

### 3.2 批量详情

- 批量查询优先用 Redis Pipeline `HMGET` 多个 `feed:{feed_id}`。
- 未命中部分再批量查 MySQL `WHERE id IN (...)`。
- 回写缓存时，对每条命中的记录单独 SETEX，不要一次性覆盖整个集合。

### 3.3 已删除帖子处理

- 缓存中如果 status = 2，直接返回「帖子已删除」错误码。
- MySQL 查询到的 status = 2 也要过滤或明确返回错误。

---

## 4. 异常场景方法论

| 场景 | 处理原则 |
|------|---------|
| DB 写入成功，MQ 发送失败 | 发帖返回成功，MQ 失败记录 error log，由监控/重试任务兜底 |
| 缓存更新失败 | 不阻塞主流程，下次读请求自动回源重建 |
| 高并发重复发帖 | 前端幂等 token + 业务层 5 秒内相同内容去重 |
| 作者大V状态变更 | 发帖时刻的 is_vip_feed 以当时 Relation.IsVip 结果为准，不追溯 |

---

## 5. 与下游的协作

- **Relation.IsVip**：发帖时调用，决定 `is_vip_feed`。
- **RocketMQ**：发送 `feed.created` / `feed.deleted`。
- **Redis**：缓存帖子详情、发件箱、推荐池、同城池。
- **对象存储**：媒体 URL 由 Gateway/上传服务预签名后传入，Feed Service 只存 URL。
