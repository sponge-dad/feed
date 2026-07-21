# Feed 模块 API

> 通用约定见 [README.md](./README.md)。以下 Response 仅描述 `data` 部分。
> 业务码段：12000~12999

## 接口列表

| 方法 | 路径 | 说明 | 需登录 |
|------|------|------|--------|
| POST | `/api/v1/feeds` | 发布帖子 | 是 |
| DELETE | `/api/v1/feeds/{feedId}` | 删除帖子 | 是 |
| GET | `/api/v1/feeds/{feedId}` | 帖子详情 | 是 |
| GET | `/api/v1/feeds/timeline` | 首页信息流（三种流合一） | 是 |
| GET | `/api/v1/users/{userId}/feeds` | 个人主页帖子列表 | 是 |

---

## 数据结构分层

列表和详情使用不同的数据结构，减少列表传输量、保证滑动流畅：

**FeedCard（列表项，轻量）**
- `id`, `feed_type`, `title`
- `cover_url`（视频封面 或 图文首图，统一字段）
- `author`（简要：id/nickname/avatar）
- `stats`（点赞/评论/收藏计数）
- `interaction`（当前用户是否点赞/收藏）
- `created_at`
- **不含** `description`、`media_urls`

**FeedDetail（详情，完整）**
- FeedCard 全部字段 +
- `description`（完整正文）
- `media_urls`（完整视频/图片 URL）
- `ip_location`、`city_name`
- `author.is_following`（是否关注作者）

---

## 网关聚合说明

信息流和详情的组装由**网关**完成（BFF 模式），下游服务保持单一职责：

```
GET /feeds/timeline?type=recommend

网关流程：
  1. Feed.GetTimeline(type, cursor, size) → 返回帖子基础信息 + 作者ID列表
  2. 并行批量调用：
     ├─ User.BatchGetUsers([作者ids])          → 作者昵称/头像
     ├─ Interaction.BatchGetStats([feedIds])   → 点赞/评论/收藏计数
     └─ Interaction.BatchGetUserState(me,[feedIds]) → 当前用户是否点赞/收藏
  3. 组装 FeedCard 列表返回
```

**关键要求**：下游必须提供批量接口，避免 N+1 查询。

---

## 1. 发布帖子

```
POST /api/v1/feeds
```

**Request**
```json
{
  "feed_type": 2,
  "title": "周末去了趟海边",
  "description": "阳光沙滩，超治愈 #旅行 #海边",
  "media_urls": ["https://cdn.xxx.com/video/10001/xxx.mp4"],
  "cover_url": "https://cdn.xxx.com/cover/10001/xxx.jpg"
}
```

| 字段 | 说明 |
|------|------|
| feed_type | 1:图文 2:视频 |
| media_urls | 客户端已直传 COS 拿到的 URL 列表 |
| cover_url | 视频封面；图文帖可为空，服务端取 media_urls 首图 |

**Response data**
```json
{
  "feed": {
    "id": 500001,
    "user_id": 10001,
    "feed_type": 2,
    "title": "周末去了趟海边",
    "description": "阳光沙滩，超治愈 #旅行 #海边",
    "media_urls": ["https://cdn.xxx.com/video/10001/xxx.mp4"],
    "cover_url": "https://cdn.xxx.com/cover/10001/xxx.jpg",
    "city_name": "深圳",
    "ip_location": "广东",
    "created_at": 1720000000000
  }
}
```

说明：后端从请求 IP 解析 `city_code` / `city_name` / `ip_location`。

---

## 2. 删除帖子

```
DELETE /api/v1/feeds/{feedId}
```

**Response data**
```json
{ "success": true }
```

软删除，只能删自己的（网关校验 token user_id == 作者 id，否则返回 403）。

---

## 3. 帖子详情（FeedDetail）

```
GET /api/v1/feeds/{feedId}
```

**Response data**
```json
{
  "id": 500001,
  "feed_type": 2,
  "title": "周末去了趟海边",
  "description": "阳光沙滩，超治愈 #旅行 #海边",
  "media_urls": ["https://cdn.xxx.com/video/10001/xxx.mp4"],
  "cover_url": "https://cdn.xxx.com/cover/10001/xxx.jpg",
  "city_name": "深圳",
  "ip_location": "广东",
  "created_at": 1720000000000,
  "author": {
    "id": 10001,
    "nickname": "海绵宝宝",
    "avatar": "https://cos.../avatar.jpg",
    "is_following": false
  },
  "stats": {
    "like_count": 1200,
    "comment_count": 89,
    "collect_count": 340
  },
  "interaction": {
    "is_liked": true,
    "is_collected": false
  }
}
```

---

## 4. 首页信息流（三种流合一）

```
GET /api/v1/feeds/timeline?type=recommend&cursor=xxx&page_size=10
```

**Query 参数**

| 参数 | 类型 | 说明 |
|------|------|------|
| type | string | `recommend`(推荐/默认) \| `follow`(关注) \| `city`(同城) |
| cursor | string | 游标，第一页不传，后续传上一页的 next_cursor |
| page_size | int | 每页数量，默认10，最大50 |

**三种流的行为**

| type | 数据来源 | 排序 | 城市来源 |
|------|---------|------|---------|
| recommend | 全局推荐池 | 随机 × 时间衰减 | - |
| follow | 关注用户帖子（inbox+大V outbox） | 时间倒序 | - |
| city | 同城池 | 时间倒序 | **网关按当前请求 IP 实时定位** city_code，前端不传 |

**Response data（FeedCard 列表）**
```json
{
  "list": [
    {
      "id": 500001,
      "feed_type": 2,
      "title": "周末去了趟海边",
      "cover_url": "https://cdn.xxx.com/cover/10001/xxx.jpg",
      "author": {
        "id": 10001,
        "nickname": "海绵宝宝",
        "avatar": "https://cos.../avatar.jpg"
      },
      "stats": {
        "like_count": 1200,
        "comment_count": 89,
        "collect_count": 340
      },
      "interaction": {
        "is_liked": false,
        "is_collected": false
      },
      "created_at": 1720000000000
    }
  ],
  "next_cursor": "eyJ0IjoxNzIwfQ==",
  "has_more": true
}
```

说明：
- 列表项为 FeedCard 轻量结构，**不含正文和媒体 URL**，点详情才加载
- 图文帖 `cover_url` 取首图，视频帖取封面，前端统一处理
- 同城流：网关先解析请求 IP → city_code，再传给 Feed 服务

---

## 5. 个人主页帖子列表

```
GET /api/v1/users/{userId}/feeds?cursor=xxx&page_size=10
```

**Response data**：FeedCard 列表，结构同信息流。

按发布时间倒序，返回该用户发布的所有帖子（status=1）。

---

## 业务码

| code | message | 说明 |
|------|---------|------|
| 0 | success | 成功 |
| 12001 | 帖子不存在 | feedId 无效或已删除 |
| 12002 | 无权限操作该帖子 | 删除非本人帖子 |
| 12003 | 帖子内容为空 | 发帖校验 |
| 12004 | 媒体资源为空 | 视频/图文缺少 media_urls |
| 12005 | 不支持的帖子类型 | feed_type 非法 |
| 12006 | IP 定位失败 | 同城流无法解析城市 |
