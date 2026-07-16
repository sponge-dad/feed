# REST API 设计规范（总纲）

> 本文档定义所有对外 REST API 的通用约定。各模块具体接口见 `docs/api/` 下对应文件。

## 设计风格

采用**务实的 RESTful**：
- 标准增删改查用标准 REST（资源名词 + HTTP 方法）
- 复杂业务操作（关注、点赞、收藏）允许带动词，但保持团队内一致
- 优先"团队看得懂、好维护"，不做纯 REST 强迫症

## 通用约定

### 1. URL 前缀与版本

```
/api/v1/...
```

所有接口带 `/api/v1` 版本前缀，未来不兼容升级走 `/api/v2`。

### 2. HTTP 方法语义

| 方法 | 语义 | 幂等 |
|------|------|------|
| GET | 查询 | ✅ |
| POST | 创建 / 复杂操作 | ❌ |
| PUT | 全量替换 | ✅ |
| PATCH | 部分更新 | ❌ |
| DELETE | 删除 | ✅ |

### 3. 当前登录用户用 `me`

- 看别人：`GET /users/{userId}`
- 看/改自己：`GET /users/me`、`PATCH /users/me`
- `me` 由网关从 JWT Token 解析出用户 ID，前端无需先知道自己的 id

### 4. 统一响应结构

所有接口返回统一包裹：

```json
{
  "code": 0,
  "message": "success",
  "data": { },
  "request_id": "a1b2c3d4"
}
```

| 字段 | 说明 |
|------|------|
| `code` | 业务码，0=成功，非0=业务错误 |
| `message` | 提示信息，成功为 "success"，失败为错误描述 |
| `data` | 实际数据，失败时可为 null |
| `request_id` | 链路追踪ID，排查问题用 |

> 下文各接口文档中的 "Response" 均只描述 `data` 部分。

### 5. HTTP 状态码 vs 业务码

**HTTP 状态码**表达请求本身的结果：

| 码 | 含义 |
|----|------|
| 200 | 请求成功（业务成功与否看 code） |
| 400 | 参数错误 |
| 401 | 未认证（未登录 / token失效） |
| 403 | 无权限 |
| 404 | 资源不存在 |
| 429 | 请求过于频繁（限流） |
| 500 | 服务器内部错误 |

**业务码**表达业务逻辑结果（详见各模块），分段规划：

| 段 | 归属 |
|----|------|
| 0 | 成功 |
| 10000~10999 | User 服务 |
| 11000~11999 | Relation 服务 |
| 12000~12999 | Feed 服务 |
| 13000~13999 | Comment 服务 |
| 14000~14999 | Interaction 服务 |

### 6. 分页规范

| 场景 | 方式 | 参数 |
|------|------|------|
| 信息流（Feed/评论） | **Cursor 分页** | `cursor`、`page_size` |
| 普通列表（关注/粉丝） | **Offset 分页** | `page`、`page_size` |

**Offset 分页响应**：
```json
{
  "list": [ ],
  "page": 1,
  "page_size": 20,
  "total": 100,
  "has_more": true
}
```

**Cursor 分页响应**：
```json
{
  "list": [ ],
  "page_size": 20,
  "next_cursor": "eyJ0IjoxNzIwfQ==",
  "has_more": true
}
```

`page_size` 默认 20，最大 50。

### 7. 认证

- 除注册/登录外，所有接口需在 Header 携带 Token：
  ```
  Authorization: Bearer eyJhbGc...
  ```
- 网关统一校验 Token，解析出 user_id 透传给下游服务

### 8. 时间格式

- 响应中的时间统一用**毫秒级 unix 时间戳**（int64），由前端本地化展示

## 文件上传约定

采用**客户端直传 COS**（视频大文件必须如此，不占后端带宽）：

```
1. 客户端调 POST /api/v1/upload/token 获取 COS 临时凭证
2. 客户端用凭证直接上传文件到 COS
3. 上传成功后拿到文件 URL
4. 客户端调对应业务接口（如 PATCH /users/me 或 POST /feeds）把 URL 存入
```

上传与业务写入是分离的两步。
