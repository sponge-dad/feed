# Interaction 模块 API

> 通用约定见 [README.md](./README.md)。以下 Response 仅描述 `data` 部分。
> 业务码段：14000~14999

## 接口列表

| 方法 | 路径 | 说明 | 需登录 |
|------|------|------|--------|
| POST | `/api/v1/feeds/{feedId}/like` | 点赞帖子 | 是 |
| DELETE | `/api/v1/feeds/{feedId}/like` | 取消点赞 | 是 |
| POST | `/api/v1/feeds/{feedId}/collect` | 收藏帖子 | 是 |
| DELETE | `/api/v1/feeds/{feedId}/collect` | 取消收藏 | 是 |
| GET | `/api/v1/users/me/likes` | 我点赞过的帖子 | 是 |
| GET | `/api/v1/users/me/collects` | 我收藏过的帖子 | 是 |

> 本模块只处理**帖子**的点赞/收藏。评论点赞归 Comment 模块管理。

---

## 设计要点：读写分离 + 异步削峰

点赞/收藏是超高频操作，采用「Redis 先行 + MQ 异步落库」：

```
用户点赞：
  1. Redis SADD like:feed:{feed_id} {user_id}        ← 立即生效
  2. Redis ZADD user:likes:{user_id} {now} {feed_id}
  3. Redis HINCRBY feed:stats:{feed_id} like_count 1
  4. 立即返回成功（毫秒级）
  5. 发送 MQ (interaction.event)
       ├─ Consumer 1 → 异步写 MySQL likes 表（持久化）
       └─ Consumer 2 → 发通知给帖子作者

原则：用户感知 Redis 操作，MySQL 落库异步。避免高频写打爆数据库。
```

计数一致性：平时读 Redis `feed:stats`；每日定时任务从 MySQL COUNT 校准；缓存失效时从 MySQL 重建。

---

## 对其他模块的支撑接口（内部 gRPC，网关聚合用）

Feed 信息流/详情聚合时，需要 Interaction 提供批量接口（避免 N+1）：
- `BatchGetStats([feedIds])` → 各帖子的点赞/评论/收藏计数
- `BatchGetUserState(userId, [feedIds])` → 当前用户对各帖子是否已点赞/收藏

这些是内部 gRPC 接口，不直接对外暴露 REST。

---

## 1. 点赞 / 取消点赞帖子

```
POST   /api/v1/feeds/{feedId}/like     点赞
DELETE /api/v1/feeds/{feedId}/like     取消点赞
```

**Response data**
```json
{
  "success": true,
  "like_count": 1201
}
```

返回最新点赞数，供前端刷新显示。重复点赞/重复取消做幂等处理（不报错，返回当前状态）。

---

## 2. 收藏 / 取消收藏帖子

```
POST   /api/v1/feeds/{feedId}/collect     收藏
DELETE /api/v1/feeds/{feedId}/collect     取消收藏
```

**Response data**
```json
{
  "success": true,
  "collect_count": 341
}
```

---

## 3. 我点赞过的帖子

```
GET /api/v1/users/me/likes?cursor=xxx&page_size=10
```

按点赞时间倒序，返回 FeedCard 列表（结构见 Feed 模块），由网关聚合帖子详情与作者信息。

**Response data**
```json
{
  "list": [
    {
      "id": 500001,
      "feed_type": 2,
      "title": "周末去了趟海边",
      "cover_url": "https://cdn.xxx.com/cover/10001/xxx.jpg",
      "author": { "id": 10001, "nickname": "海绵宝宝", "avatar": "..." },
      "stats": { "like_count": 1200, "comment_count": 89, "collect_count": 340 },
      "interaction": { "is_liked": true, "is_collected": false },
      "created_at": 1720000000000
    }
  ],
  "next_cursor": "eyJ0IjoxNzIwfQ==",
  "has_more": true
}
```

说明：`interaction.is_liked` 在此列表恒为 true（都是我点赞过的），若帖子已被删除则不返回。

---

## 4. 我收藏过的帖子

```
GET /api/v1/users/me/collects?cursor=xxx&page_size=10
```

按收藏时间倒序，返回 FeedCard 列表，结构同上。`interaction.is_collected` 恒为 true。

---

## 业务码

| code | message | 说明 |
|------|---------|------|
| 0 | success | 成功 |
| 14001 | 帖子不存在 | feedId 无效或已删除 |
| 14002 | 操作过于频繁 | 触发限流 |
