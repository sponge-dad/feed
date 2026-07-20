# Comment 模块 API

> 通用约定见 [README.md](./README.md)。以下 Response 仅描述 `data` 部分。
> 业务码段：13000~13999

## 接口列表

| 方法 | 路径 | 说明 | 需登录 |
|------|------|------|--------|
| POST | `/api/v1/feeds/{feedId}/comments` | 发表评论/回复 | 是 |
| GET | `/api/v1/feeds/{feedId}/comments` | 一级评论列表（带子回复预览） | 是 |
| GET | `/api/v1/comments/{rootId}/replies` | 子回复列表（查看全部回复） | 是 |
| DELETE | `/api/v1/comments/{commentId}` | 删除评论 | 是 |
| POST | `/api/v1/comments/{commentId}/like` | 点赞评论 | 是 |
| DELETE | `/api/v1/comments/{commentId}/like` | 取消点赞评论 | 是 |

---

## 楼中楼结构说明

界面表现（视觉无限嵌套）：
```
帖子详情页
├─ 评论A（一级评论）              👍120
│   ├─ B 回复 A                   👍5
│   ├─ C 回复 B                   👍2
│   └─ [查看全部 8 条回复]
├─ 评论E（一级评论）
└─ [加载更多评论]
```

存储实为两层（详见 `../data-model.md`）：
- `root_id`：指向最顶层评论，同楼回复 root_id 相同（一级评论 root_id=0）
- `parent_id`：直接回复的评论 id，用于展示"回复@某人"
- `reply_user_id`：被回复者，前端显示"张三 回复 李四"

**评论点赞归本模块管理**（与帖子点赞分离），点赞逻辑简单，不放 Interaction 服务。

---

## 排序策略：热门 + 最新分段

热度排序与 Cursor 分页天生不兼容（点赞数随时变，cursor 无法定位），故分两段：

```
第一页（cursor 为空）：
  ├─ hot_comments: 单独查热门评论前3条（按点赞排序）
  └─ list:         最新评论第一页（时间倒序，Cursor）

后续页（cursor 有值）：
  ├─ hot_comments: 空数组（不再重复返回）
  └─ list:         最新评论（时间倒序，Cursor 翻页）
```

前端把 `hot_comments` 渲染在最上方（带🔥标记），`list` 接在后面。

| 配置 | 值 |
|------|-----|
| 热门评论条数 N | 3 |
| 子回复预览条数 | 2 |
| 分页方式 | Cursor |

---

## 1. 发表评论 / 回复

```
POST /api/v1/feeds/{feedId}/comments
```

**Request（发一级评论）**
```json
{
  "content": "拍得真好看！",
  "root_id": 0,
  "parent_id": 0,
  "reply_user_id": 0
}
```

**Request（回复某条评论）**
```json
{
  "content": "谢谢夸奖~",
  "root_id": 800001,
  "parent_id": 800005,
  "reply_user_id": 10008
}
```

**Response data**
```json
{
  "comment": {
    "id": 800010,
    "feed_id": 500001,
    "content": "谢谢夸奖~",
    "root_id": 800001,
    "parent_id": 800005,
    "author": { "id": 10001, "nickname": "海绵宝宝", "avatar": "..." },
    "reply_user": { "id": 10008, "nickname": "派大星" },
    "like_count": 0,
    "created_at": 1720000000000
  }
}
```

`reply_user` 在一级评论时为 null。

---

## 2. 一级评论列表

```
GET /api/v1/feeds/{feedId}/comments?cursor=xxx&page_size=20
```

**Response data（第一页，cursor 为空）**
```json
{
  "hot_comments": [
    {
      "id": 800001,
      "content": "拍得真好看！",
      "author": { "id": 10008, "nickname": "派大星", "avatar": "..." },
      "like_count": 120,
      "is_liked": false,
      "reply_count": 8,
      "created_at": 1720000000000,
      "sub_replies": [
        {
          "id": 800005,
          "content": "同感！",
          "author": { "id": 10009, "nickname": "珊迪", "avatar": "..." },
          "reply_user": { "id": 10008, "nickname": "派大星" },
          "like_count": 5,
          "is_liked": false,
          "created_at": 1720000001000
        }
      ]
    }
  ],
  "list": [
    {
      "id": 800020,
      "content": "最新的一条评论",
      "author": { "id": 10010, "nickname": "蟹老板", "avatar": "..." },
      "like_count": 0,
      "is_liked": false,
      "reply_count": 0,
      "created_at": 1720000100000,
      "sub_replies": []
    }
  ],
  "next_cursor": "eyJ0IjoxNzIwfQ==",
  "has_more": true
}
```

**Response data（后续页，cursor 有值）**：`hot_comments` 返回空数组，其余同上。

说明：
- `hot_comments`：热门评论前3条，仅第一页返回
- `list`：最新评论，时间倒序
- 每条一级评论带 `sub_replies`（前2条子回复预览）和 `reply_count`（该楼总回复数）

---

## 3. 子回复列表

```
GET /api/v1/comments/{rootId}/replies?cursor=xxx&page_size=20
```

点击"查看全部N条回复"时调用，返回该楼所有子回复（时间正序，先发的在上）。

**Response data**
```json
{
  "list": [
    {
      "id": 800005,
      "content": "同感！",
      "author": { "id": 10009, "nickname": "珊迪", "avatar": "..." },
      "reply_user": { "id": 10008, "nickname": "派大星" },
      "like_count": 5,
      "is_liked": false,
      "created_at": 1720000001000
    }
  ],
  "next_cursor": "...",
  "has_more": true
}
```

---

## 4. 删除评论

```
DELETE /api/v1/comments/{commentId}
```

**Response data**
```json
{ "success": true }
```

软删除，只能删自己的评论。删除一级评论时，其下子回复一并标记删除。

---

## 5. 评论点赞 / 取消点赞

```
POST   /api/v1/comments/{commentId}/like     点赞
DELETE /api/v1/comments/{commentId}/like     取消点赞
```

**Response data**
```json
{
  "success": true,
  "like_count": 121
}
```

---

## 业务码

| code | message | 说明 |
|------|---------|------|
| 0 | success | 成功 |
| 13001 | 评论不存在 | commentId 无效或已删除 |
| 13002 | 帖子不存在 | feedId 无效 |
| 13003 | 无权限删除该评论 | 删除非本人评论 |
| 13004 | 评论内容为空 | content 为空 |
| 13005 | 评论内容超长 | 超过1000字 |
| 13006 | 父评论不存在 | 回复的目标评论无效 |
