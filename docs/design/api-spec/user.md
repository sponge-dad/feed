# User 模块 API

> 通用约定见 [README.md](./README.md)。以下 Response 仅描述 `data` 部分。
> 业务码段：10000~10999

## 接口列表

| 方法 | 路径 | 说明 | 需登录 |
|------|------|------|--------|
| POST | `/api/v1/users/register` | 注册 | 否 |
| POST | `/api/v1/users/login` | 登录 | 否 |
| GET | `/api/v1/users/{userId}` | 获取用户信息（他人主页） | 是 |
| GET | `/api/v1/users/me` | 获取当前登录用户信息 | 是 |
| PATCH | `/api/v1/users/me` | 更新当前用户信息 | 是 |
| POST | `/api/v1/upload/token` | 获取 COS 临时上传凭证 | 是 |

---

## 1. 注册

```
POST /api/v1/users/register
```

**Request**
```json
{
  "username": "spongebob",
  "password": "123456",
  "nickname": "海绵宝宝"
}
```

**Response data**
```json
{
  "user": {
    "id": 10001,
    "username": "spongebob",
    "nickname": "海绵宝宝",
    "avatar": "",
    "city_name": "深圳",
    "bio": ""
  },
  "token": "eyJhbGc..."
}
```

说明：
- 后端根据请求 IP 自动定位 `city_name` / `city_code`
- 注册即登录，直接返回 `token`

---

## 2. 登录

```
POST /api/v1/users/login
```

**Request**
```json
{
  "username": "spongebob",
  "password": "123456"
}
```

**Response data**
```json
{
  "user": {
    "id": 10001,
    "username": "spongebob",
    "nickname": "海绵宝宝",
    "avatar": "https://cos.../avatar.jpg",
    "city_name": "深圳",
    "bio": "我住在比奇堡"
  },
  "token": "eyJhbGc..."
}
```

---

## 3. 获取用户信息

```
GET /api/v1/users/{userId}
```

**Response data**
```json
{
  "id": 10001,
  "username": "spongebob",
  "nickname": "海绵宝宝",
  "avatar": "https://cos.../avatar.jpg",
  "bio": "我住在比奇堡",
  "city_name": "深圳",
  "following_count": 120,
  "follower_count": 3500,
  "feed_count": 42,
  "is_following": false
}
```

说明：
- `following_count` / `follower_count` 由网关聚合 Relation 服务返回
- `feed_count` 由网关聚合 Feed 服务返回
- `is_following` 表示当前登录用户是否关注了目标用户；看自己主页时前端忽略该字段

---

## 4. 获取当前登录用户信息

```
GET /api/v1/users/me
```

**Response data**：同「获取用户信息」，`is_following` 无意义。

---

## 5. 更新当前用户信息

```
PATCH /api/v1/users/me
```

**Request**（只传要修改的字段）
```json
{
  "nickname": "新昵称",
  "avatar": "https://cos.../new.jpg",
  "bio": "新简介",
  "city_code": "440300"
}
```

**Response data**
```json
{
  "user": {
    "id": 10001,
    "username": "spongebob",
    "nickname": "新昵称",
    "avatar": "https://cos.../new.jpg",
    "bio": "新简介",
    "city_name": "深圳"
  }
}
```

说明：
- 头像上传流程：先调 `/upload/token` 拿凭证 → 客户端直传 COS 拿到 URL → 调本接口把 `avatar` URL 存入
- 上传与更新是分离的两步

---

## 6. 获取 COS 临时上传凭证

```
POST /api/v1/upload/token
```

**Request**
```json
{
  "file_type": "image",
  "file_ext": "jpg"
}
```

`file_type` 可选值：`image` / `video`

**Response data**
```json
{
  "upload_url": "https://feed-xxx.cos.ap-guangzhou.myqcloud.com",
  "credentials": {
    "tmp_secret_id": "...",
    "tmp_secret_key": "...",
    "session_token": "...",
    "expired_time": 1720003600
  },
  "file_key": "image/10001/20250101/uuid.jpg",
  "file_url": "https://cdn.xxx.com/image/10001/20250101/uuid.jpg"
}
```

说明：
- `file_key` 是 COS 存储路径，客户端上传时用
- `file_url` 是上传成功后可访问的 CDN 地址，客户端存这个到业务接口
- `credentials` 为 STS 临时密钥，有效期短（如1小时）

---

## 业务码

| code | message | 说明 |
|------|---------|------|
| 0 | success | 成功 |
| 10001 | 用户名已存在 | 注册时重复 |
| 10002 | 用户名或密码错误 | 登录失败 |
| 10003 | 用户不存在 | 查询的用户不存在 |
| 10004 | 密码格式不符合要求 | 注册校验 |
| 10005 | 用户已被禁用 | status=2 |
| 10006 | 获取上传凭证失败 | COS STS 异常 |
