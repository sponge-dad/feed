# 用户互动列表实现方法论

> 本文档描述 Interaction 服务中「我的赞」和「我的收藏」列表的实现方法论。
> 核心是按时间倒序分页，并保证列表只展示当前仍有效的互动记录。

---

## 1. GetUserLikedFeeds（用户点赞过的帖子）

### 1.1 接口职责边界

`GetUserLikedFeeds` 返回指定用户点赞过的帖子 ID 列表：

- 按点赞时间倒序。
- 支持 Cursor 分页。
- 只返回当前仍有效的点赞（取消点赞后不再出现）。

**它不应该做**：

- 不返回帖子详情（由调用方用 feed_id 列表去 Feed 服务批量查询）。
- 不做用户鉴权以外的业务过滤。

### 1.2 分页方式：Cursor

- 游标由 `last_score + last_feed_id` 组成。
- 下一页查询：
  ```text
  ZREVRANGEBYSCORE user:likes:{user_id} (last_score -inf
  或
  ZREVRANGE user:likes:{user_id} start stop
  ```
- Cursor 比 Offset 更适合：
  - 用户取消点赞后，Offset 会导致错位。
  - 新增点赞在头部，Cursor 不会漏页。

### 1.3 查询路径

1. 优先读 Redis `user:likes:{user_id}`。
2. 命中则按 Cursor 取出一页 `feed_id`。
3. 未命中则回源 MySQL：
   - `SELECT feed_id, created_at FROM likes WHERE user_id = ? AND status = 1 ORDER BY created_at DESC LIMIT ?`
4. 将结果回写 Redis ZSet 并设置 TTL。

---

## 2. GetUserCollectedFeeds（用户收藏过的帖子）

### 2.1 与点赞列表同构

- 使用 Redis key `user:collects:{user_id}`。
- 使用 MySQL 表 `collections`。
- 分页、回源、缓存策略与点赞列表完全一致。

### 2.2 列表去重与有效性

- 取消收藏时立即 `ZREM user:collects:{user_id} {feed_id}`，保证列表实时不含无效记录。
- 如果 Redis ZSet 未命中，回源 MySQL 时带 `status = 1` 过滤。

---

## 3. Cursor 设计细节

### 3.1 游标组成

- 建议游标编码为 `base64(score:feed_id)`，例如 `MTcxNTUzMjgwMDoxMjM0NTY=`。
- 第一页传空游标。
- 服务端解析游标后，用 `(score` 开区间避免重复。

### 3.2 边界情况

| 情况 | 处理 |
|------|------|
| 游标过期后用户又取消了一条 | Cursor 基于 score，不会漏；可能返回已在上一页出现过的 score 相同记录，需客户端配合 `feed_id` 去重 |
| 同一秒点赞多个帖子 | score 相同，使用 `feed_id` 作为第二排序键 |
| 列表为空 | 返回空列表 + 空 next_cursor |

---

## 4. 与下游的协作

- **Redis**：`user:likes:{user_id}`、`user:collects:{user_id}`。
- **MySQL**：`likes`、`collections` 表，缓存未命中时回源。
- **Feed Service**：调用方拿到 `feed_id` 列表后，批量查询帖子详情。
