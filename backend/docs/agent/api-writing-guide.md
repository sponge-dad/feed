# API 文档编写指南（AI 专用）

> 本文档约束 AI 在编写 Feed 项目各服务 REST API 文档时的统一规范。新增或修改接口文档前，必须通读本指南并逐条对齐。已有规范 `../design/api-spec/README.md` 仍需遵守，本指南是对其的 AI 写作执行细则。

---

## 1. 文档定位与文件规范

### 1.1 文件位置

每个对外暴露 REST 接口的模块，在 `../design/api-spec/` 下维护一份独立文档：

| 模块 | 文档路径 |
|------|----------|
| 用户 | `../design/api-spec/user.md` |
| 关系 | `../design/api-spec/relation.md` |
| Feed | `../design/api-spec/feed.md` |
| 评论 | `../design/api-spec/comment.md` |
| 互动 | `../design/api-spec/interaction.md` |

### 1.2 文件名规则

- 小写英文，模块名命名，如 `feed.md`、`interaction.md`。
- 不允许多个模块合并到一个文件，也不允许一个模块拆成多个文件。

### 1.3 文档头部三要素

每个模块文档开头必须包含：

1. 模块标题（如 `# User 模块 API`）
2. 指向通用约定的链接：`> 通用约定见 [README.md](./README.md)。以下 Response 仅描述 `data` 部分。`
3. 业务码段声明：`> 业务码段：10000~10999`

---

## 2. 文档结构模板

必须按以下顺序组织内容，不可跳跃或省略。没有实际接口的章节可写 "无"，但不能删节。

```markdown
# {模块名} 模块 API

> 通用约定见 [README.md](./README.md)。以下 Response 仅描述 `data` 部分。
> 业务码段：{XXXXX~XXXXX}

## 接口列表

| 方法 | 路径 | 说明 | 需登录 |

## 数据/设计说明（可选）

- 若有特殊数据结构（如 FeedCard vs FeedDetail），必须说明
- 若有网关聚合逻辑，必须画出调用链路
- 若有跨模块支撑接口（内部 gRPC），需说明其存在但不展开 REST 定义

## 1. 接口名

### 2. 接口名
...

## 业务码

| code | message | 说明 |
```

### 2.1 接口列表表格

每个接口一行，字段必须包含：方法、路径、说明、是否需登录。

```markdown
| 方法 | 路径 | 说明 | 需登录 |
|------|------|------|--------|
| POST | `/api/v1/users/register` | 注册 | 否 |
| GET | `/api/v1/users/{userId}` | 获取用户信息 | 是 |
```

### 2.2 单接口内部结构

每个接口必须包含以下部分：

1. 接口名（二级标题）
2. 方法 + 路径代码块
3. Request 描述（JSON 示例 + 字段说明表）
4. Response data 描述（JSON 示例 + 字段说明）
5. 特殊说明（如数据来源、权限校验、网关聚合行为）

示例：

```markdown
## 1. 注册

```
POST /api/v1/users/register
```

**Request**
```json
{
  "username": "spongebob",
  "password": "123456"
}
```

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| username | string | 是 | 用户名，4-20 位字母/数字/下划线 |
| password | string | 是 | 密码，6-32 位 |

**Response data**
```json
{
  "user": { ... },
  "token": "eyJhbGc..."
}
```

说明：注册即登录，直接返回 token。
```

---

## 3. URL 设计规范

### 3.1 版本与前缀

- 所有对外 REST 接口必须以 `/api/v1` 开头。
- 未来不兼容升级走 `/api/v2`，当前版本统一为 `/api/v1`。

### 3.2 资源路径

- 资源用名词复数，如 `/users`、`/feeds`。
- 具体资源用路径参数，如 `/users/{userId}`、`/feeds/{feedId}`。
- 当前登录用户统一用 `me`，如 `/users/me`。
- 复杂动作允许用动词，但同一模块内必须保持一致，例如：
  - `/api/v1/relations/follow` 表示关注
  - `/api/v1/feeds/{feedId}/like` 表示点赞

### 3.3 方法语义

| 方法 | 使用场景 | 示例 |
|------|----------|------|
| GET | 查询 | `GET /users/{userId}` |
| POST | 创建 / 复杂操作 | `POST /feeds`、`POST /relations/follow` |
| PUT | 全量替换 | `PUT /users/me`（不推荐，优先 PATCH） |
| PATCH | 部分更新 | `PATCH /users/me` |
| DELETE | 删除 | `DELETE /feeds/{feedId}` |

---

## 4. 请求与响应格式

### 4.1 请求规范

- Query 参数、路径参数、JSON Body 必须分开描述，禁止混写。
- Query 参数必须给出类型、默认值、是否必填、取值范围。
- JSON Body 必须给出字段类型、是否必填、示例值、业务约束。

### 4.2 响应规范

- 只描述 `data` 部分，外层统一结构不在每个接口重复。
- 返回数组时必须包裹 `list` 字段，不能直接返回数组。
- 分页响应格式必须二选一：
  - Offset 分页：`list`, `page`, `page_size`, `total`, `has_more`
  - Cursor 分页：`list`, `page_size`, `next_cursor`, `has_more`

### 4.3 时间格式

- 响应中所有时间字段统一使用 **毫秒级 Unix 时间戳**（int64）。
- 字段名统一为 `created_at`、`updated_at` 等，不使用 `create_time` 等变体。

### 4.4 布尔字段命名

- 状态类布尔字段统一用 `is_xxx` 形式，如 `is_following`、`is_liked`、`is_collected`。

---

## 5. 错误码与业务码

### 5.1 错误码段分配

每个模块只能使用自己分配的业务码段，禁止跨模块占用：

| 码段 | 归属 |
|------|------|
| 0 | 成功 |
| 10000~10999 | User 服务 |
| 11000~11999 | Relation 服务 |
| 12000~12999 | Feed 服务 |
| 13000~13999 | Comment 服务 |
| 14000~14999 | Interaction 服务 |

### 5.2 错误码表要求

每个模块文档末尾必须包含 `## 业务码` 章节，格式：

```markdown
## 业务码

| code | message | 说明 |
|------|---------|------|
| 0 | success | 成功 |
| 10001 | 用户名已存在 | 注册时重复 |
| 10002 | 用户名或密码错误 | 登录失败 |
```

### 5.3 错误码设计原则

- 常见错误优先占码段靠前位置（如 10001、10002）。
- 同一错误场景在同一模块内只用一个码，不允许重复。
- 新增错误码时按顺序递增，避免跳号。
- 错误码 message 必须简洁、面向用户，不写堆栈或调试信息。

---

## 6. 认证与权限

### 6.1 登录态标注

接口列表表格中必须明确标注每个接口是否需要登录。注册、登录等接口标 "否"，其余默认 "是"。

### 6.2 当前用户身份

- 当前登录用户的 ID 从 JWT Token 解析，前端不传递 `user_id` 字段来代表自己。
- 查看他人资源时通过路径参数或 Query 参数传 `user_id`。
- 统一使用 `/users/me` 路径表示当前登录用户资源。

---

## 7. 分页规范

### 7.1 分页方式选择

| 场景 | 分页方式 | 参数 |
|------|----------|------|
| 信息流、时间线、评论 | Cursor | `cursor`, `page_size` |
| 关注列表、粉丝列表、固定总数列表 | Offset | `page`, `page_size` |

### 7.2 分页参数默认值

- `page_size` 默认 20，最大 50。
- 如需特殊默认值（如 Feed 时间线默认 10），必须在接口说明中明确标注。

### 7.3 响应示例

**Offset 分页**：

```json
{
  "list": [],
  "page": 1,
  "page_size": 20,
  "total": 100,
  "has_more": true
}
```

**Cursor 分页**：

```json
{
  "list": [],
  "page_size": 20,
  "next_cursor": "eyJ0IjoxNzIwfQ==",
  "has_more": true
}
```

---

## 8. 数据聚合与 BFF 说明

### 8.1 网关聚合必须声明

如果接口的 Response 数据来自多个下游服务（如用户信息来自 User 服务、关注状态来自 Relation 服务、计数来自 Interaction 服务），必须在文档中说明：

1. 哪些字段由网关聚合而来。
2. 网关调用下游服务的顺序和依赖关系。
3. 为什么这样设计（避免服务间循环依赖、避免 N+1 查询）。

示例：

```markdown
## 数据聚合原则

Relation 服务只存储「ID 关系」，不存用户详情。列表接口返回的用户信息由**网关聚合**：

1. Relation.GetFollowing(...) → 返回纯 ID 列表
2. 并行调用 User.BatchGetUsers(...) 和 Relation.BatchIsFollowing(...)
3. 网关组装完整列表返回

**关键要求**：下游服务必须提供批量接口，避免 N+1 查询。
```

### 8.2 内部 gRPC 接口

内部 gRPC 接口（如 `BatchGetStats`、`BatchGetUserState`）不直接对外暴露 REST，但需要在模块文档中说明其存在，因为网关聚合依赖它们。

---

## 9. 字段命名与数据类型

### 9.1 命名规范

| 类型 | 规范 | 示例 |
|------|------|------|
| JSON 字段 | snake_case | `user_id`, `cover_url`, `is_following` |
| 路径参数 | 驼峰/小写 | `userId`, `feedId` |
| 枚举值 | 英文小写字符串或数字 | `feed_type: 1=图文, 2=视频` |

### 9.2 数据类型

| 类型 | 说明 | 示例 |
|------|------|------|
| int64 | 用户 ID、帖子 ID、评论 ID 等 | `10001` |
| int | 计数、分页页码 | `1200` |
| string | 用户名、昵称、URL | `"spongebob"` |
| bool | 状态标记 | `true` |
| int64 | 时间戳（毫秒） | `1720000000000` |

---

## 10. 编写示例：新增一个接口

假设要在 User 模块新增「获取用户关注数」接口，按以下格式编写：

```markdown
## 7. 获取用户关注数

```
GET /api/v1/users/{userId}/counts
```

**Response data**
```json
{
  "following_count": 120,
  "follower_count": 3500,
  "feed_count": 42
}
```

说明：由网关聚合 Relation 服务与 Feed 服务返回。
```

---

## 11. 检查清单

编写完一个模块 API 文档后，必须逐项检查：

- [ ] 文件路径为 `../design/api-spec/{module}.md`，模块名小写
- [ ] 文档头部包含通用约定链接和业务码段声明
- [ ] 包含接口列表表格，且每个接口标注了是否需登录
- [ ] 每个接口包含方法+路径、Request、Response data、说明
- [ ] 所有 JSON 示例可直接复制使用，无歧义字段
- [ ] 分页接口明确使用 Offset 或 Cursor 模式，且返回结构正确
- [ ] 涉及网关聚合的接口说明了聚合逻辑和下游调用顺序
- [ ] 业务码表在文档末尾，且码段不越界、不重复
- [ ] 所有时间字段为毫秒级 Unix 时间戳
- [ ] 所有布尔字段为 `is_xxx` 形式
- [ ] 所有 JSON 字段使用 snake_case

---

## 12. 与现有文档的关系

- `../design/api-spec/README.md` 是通用约定总纲，必须遵守。
- `../design/api-spec/{module}.md` 是各模块具体接口定义，必须遵守本指南。
- `../design/service-design.md` 和 `../design/data-model.md` 是业务设计依据，接口文档不能与之冲突。
- 当本指南与现有文档冲突时，以本指南为准；若本指南未覆盖，以 `../design/api-spec/README.md` 为准。
