# Relation 模块 API

> 通用约定见 [README.md](./README.md)。以下 Response 仅描述 `data` 部分。
> 业务码段：11000~11999

## 接口列表

| 方法 | 路径 | 说明 | 需登录 |
|------|------|------|--------|
| POST | `/api/v1/relations/follow` | 关注用户 | 是 |
| DELETE | `/api/v1/relations/follow` | 取关用户 | 是 |
| GET | `/api/v1/relations/following` | 关注列表（我关注了谁） | 是 |
| GET | `/api/v1/relations/followers` | 粉丝列表（谁关注了我） | 是 |
| GET | `/api/v1/relations/is-following` | 是否关注某人 | 是 |

---

## 数据聚合原则

Relation 服务只存储「ID 关系」，不存用户详情。列表接口返回的用户信息（昵称/头像/bio）和 `is_following` 状态由**网关聚合**：

```
GET /api/v1/relations/following?user_id=10001

网关内部流程：
  1. Relation.GetFollowing(10001, page, size) → 返回纯ID列表 [10002,10003,...]
  2. 并行发起两个批量调用：
     ├─ User.BatchGetUsers([...])          → 昵称/头像/bio
     └─ Relation.BatchIsFollowing(me,[...]) → {id:bool} 互关状态
  3. 网关组装成完整列表返回
```

**关键要求**：下游服务必须提供批量接口，避免 N+1 查询（20个ID循环调40次RPC）。

**架构原则**：下游服务保持单一数据源，跨服务数据组装交给网关（BFF 思想），避免服务间循环依赖和调用链膨胀。

---

## 1. 关注用户

```
POST /api/v1/relations/follow
```

**Request**
```json
{
  "following_id": 10002
}
```

`follower_id` 从 Token 解析，无需前端传。

**Response data**
```json
{
  "success": true,
  "follower_count": 3501
}
```

返回对方最新粉丝数，供前端刷新显示。

---

## 2. 取关用户

```
DELETE /api/v1/relations/follow
```

**Request**
```json
{
  "following_id": 10002
}
```

**Response data**
```json
{
  "success": true,
  "follower_count": 3500
}
```

---

## 3. 关注列表

```
GET /api/v1/relations/following?user_id=10001&page=1&page_size=20
```

**Query 参数**
| 参数 | 类型 | 说明 |
|------|------|------|
| user_id | int64 | 查看谁的关注列表 |
| page | int | 页码，默认1 |
| page_size | int | 每页大小，默认20，最大50 |

**Response data**
```json
{
  "list": [
    {
      "id": 10002,
      "nickname": "章鱼哥",
      "avatar": "https://cos.../a.jpg",
      "bio": "我爱单簧管",
      "is_following": true
    }
  ],
  "page": 1,
  "page_size": 20,
  "total": 120,
  "has_more": true
}
```

`is_following` 表示当前登录用户是否也关注了此人（用于「互相关注」标记）。

---

## 4. 粉丝列表

```
GET /api/v1/relations/followers?user_id=10001&page=1&page_size=20
```

**Response data**
```json
{
  "list": [
    {
      "id": 10005,
      "nickname": "派大星",
      "avatar": "https://cos.../b.jpg",
      "bio": "",
      "is_following": false
    }
  ],
  "page": 1,
  "page_size": 20,
  "total": 3500,
  "has_more": true
}
```

`is_following` 表示当前登录用户是否回关了这个粉丝（用于「回关」按钮）。

---

## 5. 是否关注某人

```
GET /api/v1/relations/is-following?target_id=10002
```

**Response data**
```json
{
  "is_following": true
}
```

用于进入他人主页时判断显示「关注」还是「已关注」按钮。

---

## 业务码

| code | message | 说明 |
|------|---------|------|
| 0 | success | 成功 |
| 11001 | 不能关注自己 | following_id == follower_id |
| 11002 | 已经关注该用户 | 重复关注 |
| 11003 | 未关注该用户 | 取关时并未关注 |
| 11004 | 目标用户不存在 | following_id 无效 |
