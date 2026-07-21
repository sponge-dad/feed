# 微服务拆分方案

## 整体架构

```
┌─────────────────────────────────────────────────────────┐
│                      API Gateway                        │
│        (go-zero gateway, 鉴权、限流、路由)                │
└────┬────────┬────────┬────────┬────────┬────────────────┘
     │ gRPC   │ gRPC   │ gRPC   │ gRPC   │ gRPC
┌────▼──┐ ┌───▼───┐ ┌──▼──┐ ┌───▼───┐ ┌──▼──────┐
│ User  │ │Relation│ │Feed │ │Comment│ │Interaction│
│Service│ │Service │ │Service│ │Service│ │ Service  │
└───┬───┘ └───┬───┘ └──┬──┘ └───┬───┘ └────┬─────┘
    │         │        │        │           │
    │    ┌────┘    ┌───┘    ┌───┘      ┌────┘
    ▼    ▼         ▼        ▼          ▼
┌──────────┐ ┌──────────┐ ┌──────────────┐
│  MySQL   │ │  Redis   │ │  RocketMQ    │
└──────────┘ └──────────┘ └──────────────┘
```

## 服务清单

| 服务 | 英文名 | 端口 | 一句话职责 |
|------|--------|------|-----------|
| API 网关 | gateway | 8080(HTTP) | 鉴权、限流、路由转发、请求聚合 |
| 用户服务 | user | 9001(gRPC) | 注册登录、用户信息、城市定位 |
| 关系服务 | relation | 9002(gRPC) | 关注取关、粉丝列表、大V判定 |
| Feed 服务 | feed | 9003(gRPC) | 发帖删帖、三种流聚合、内容管理 |
| 评论服务 | comment | 9004(gRPC) | 评论楼中楼 CRUD |
| 互动服务 | interaction | 9005(gRPC) | 点赞、收藏、计数管理 |

---

## 各服务详细职责

### 1. API Gateway

**定位**：系统唯一对外入口，不存储业务数据。

**职责**：
- HTTP 路由转发到内部 gRPC 服务
- JWT Token 鉴权（登录态校验）
- 请求限流（令牌桶）
- 请求参数基础校验
- 跨服务请求聚合（如首页需要同时查 Feed + 用户信息）
- CORS 处理
- 统一错误响应格式

**不负责**：
- 业务逻辑（全部转发到内部服务）
- 数据存储

---

### 2. User Service（用户服务）

**定位**：用户生命周期管理和身份认证。

**职责**：
- 用户注册（用户名 + 密码 + 手机号）
- 用户登录（JWT Token 签发）
- 用户信息查询（单个 + 批量）
- 用户信息修改（昵称、头像、简介）
- 城市定位（注册时 IP 自动定位 + 手动修改）
- Token 验证（供网关调用）
- 用户状态管理（正常/禁用）

**数据归属**：
- MySQL：`users` 表
- Redis：`user:{id}` 用户信息缓存、`token:blacklist` Token 黑名单

**依赖**：
- 无外部服务依赖（基础服务）

---

### 3. Relation Service（关系服务）

**定位**：用户关注关系的管理。

**职责**：
- 关注用户
- 取关用户
- 获取关注列表（分页）
- 获取粉丝列表（分页）
- 判断是否已关注
- 获取关注的大V列表（粉丝数 > 阈值）
- 判断用户是否为大V
- 批量获取粉丝ID列表（供 Feed 推送使用）
- 关注/取关事件发送到 MQ

**数据归属**：
- MySQL：`relations` 表
- Redis：
  - `user:follow:{user_id}` 关注列表 ZSet
  - `user:fans:{user_id}` 粉丝列表 ZSet
  - `user:fans_count:{user_id}` 粉丝数计数 String
  - `user:vip_users` 大V集合 Set

**依赖**：
- User Service（获取用户基本信息）

**配置参数**：
| 参数 | 默认值 | 说明 |
|------|--------|------|
| Vip.FansThreshold | 100,000（生产）/ 10,000（开发测试） | 粉丝数达到此阈值判定为大V。开发/压测环境可配置较小值以便验证 |

---

### 4. Feed Service（Feed 服务）

**定位**：内容发布和 Feed 流聚合的核心服务。

**职责**：
- 发布帖子（视频/图文）
- 删除帖子（软删除）
- 获取帖子详情
- 批量获取帖子
- **推荐流**：随机打散 + 时间衰减，全平台内容
- **关注流**：关注用户的帖子，纯时间倒序
- **同城流**：同城市用户帖子，时间倒序
- 获取用户个人主页帖子列表
- 发帖/删帖事件发送到 MQ

**数据归属**：
- MySQL：`feeds` 表、`feed_media` 表
- Redis：
  - `inbox:{user_id}` ZSet — 收件箱（关注流推送目标）
  - `outbox:{user_id}` ZSet — 发件箱（大V自己发的）
  - `feed:{feed_id}` Hash — 帖子详情缓存
  - `feed:recommend` ZSet — 推荐流候选池
  - `feed:city:{city_code}` ZSet — 同城流候选池
  - `timeline:{user_id}:{tab}` String — Timeline 热点缓存

**依赖**：
- User Service（获取作者信息）
- Relation Service（获取关注列表、大V判定、粉丝列表）
- Interaction Service（获取点赞/收藏数、用户是否已互动）
- RocketMQ（发送发帖事件）

**三种 Feed 流策略**：
| 流类型 | 数据来源 | 排序策略 | 缓存策略 |
|--------|---------|---------|---------|
| 推荐 | 全平台内容池 | 随机打散 + 时间衰减（score = random * time_decay） | 5分钟刷新 |
| 关注 | 关注用户的 outbox | 纯时间倒序 | 实时 + 热点缓存 60s |
| 同城 | 同城内容池 | 时间倒序 | 1分钟刷新 |

---

### 5. Comment Service（评论服务）

**定位**：支持无限嵌套楼中楼的评论系统。

**职责**：
- 发表评论（一级评论 / 回复评论）
- 删除评论（软删除）
- 获取帖子的评论列表（一级评论分页，每页包含前N条子回复）
- 获取评论的回复列表（二级及以下，分页）
- 评论计数管理

**数据归属**：
- MySQL：`comments` 表
- Redis：`comment_count:{feed_id}` String — 帖子评论总数

**评论树结构**：
```
评论楼层1 (parent_id=0, root_id=1)
  ├── 回复1-1 (parent_id=1, root_id=1)
  │    └── 回复1-1-1 (parent_id=1-1的id, root_id=1)
  ├── 回复1-2 (parent_id=1, root_id=1)
评论楼层2 (parent_id=0, root_id=2)
  └── 回复2-1 (parent_id=2, root_id=2)
```

**依赖**：
- User Service（获取评论者信息）
- RocketMQ（发送评论事件，触发通知）

---

### 6. Interaction Service（互动服务）

**定位**：点赞和收藏的轻量 KV 操作服务。

**职责**：
- 点赞帖子 / 取消点赞
- 收藏帖子 / 取消收藏
- 获取用户是否已点赞/收藏某帖子
- 获取用户点赞过的帖子列表
- 获取用户收藏过的帖子列表
- 帖子互动计数管理（点赞数、收藏数）

**数据归属**：
- MySQL：`likes` 表、`collections` 表
- Redis：
  - `like:{feed_id}` Set — 点赞用户集合
  - `collect:{feed_id}` Set — 收藏用户集合
  - `user_likes:{user_id}` ZSet — 用户点赞记录
  - `user_collects:{user_id}` ZSet — 用户收藏记录
  - `feed_stats:{feed_id}` Hash — 帖子互动计数缓存

**依赖**：
- RocketMQ（发送互动事件，触发通知和计数同步）

**设计原则**：
- 写操作先写 Redis，异步写 MySQL（高性能）
- 计数优先从 Redis 读，定时同步到 MySQL
- 点赞/收藏属于高频操作，需要削峰（MQ 异步）

---

## 服务间调用关系

```
                    ┌─────────┐
                    │ Gateway │
                    └────┬────┘
                         │
         ┌───────────────┼───────────────┐
         │               │               │
    ┌────▼────┐    ┌─────▼──────┐   ┌────▼─────┐
    │  Feed   │◄───│ Relation   │   │ Comment  │
    └───┬──┬──┘    └───────────┘   └──────────┘
        │  │
   ┌────▼┐ └─────────┐
   │ User │     ┌────▼─────────┐
   └──────┘     │ Interaction  │
                └──────────────┘
```

调用规则：
1. Gateway → 所有服务（HTTP → gRPC 转发）
2. Feed → User（获取作者信息）、Relation（获取关注列表）、Interaction（获取互动状态）
3. Comment → User（获取评论者信息）
4. User / Relation 是基础服务，不依赖其他业务服务

---

## RocketMQ Topic 归属

| Topic | 生产者 | 消费者 |
|-------|--------|--------|
| `feed.created` | Feed Service | Feed Worker（推送到收件箱） |
| `feed.deleted` | Feed Service | Feed Worker（清理缓存） |
| `relation.created` | Relation Service | Feed Worker（拉历史帖子到粉丝 inbox） |
| `relation.deleted` | Relation Service | Feed Worker（清理 inbox） |
| `interaction.event` | Interaction Service | 通知服务、计数同步 |
| `comment.event` | Comment Service | 通知服务 |
| `user.updated` | User Service | Feed Service（刷新作者缓存） |
| `audit.log` | 所有服务 | 审计服务（写审计日志） |

---

## 服务命名规范

- 仓库名：`feed`
- 服务模块路径：`github.com/sponge-dad/feed`
- 项目结构：monorepo
- gRPC Proto 路径：`api/proto/{service}/{service}.proto`
- 服务入口：`app/{service}/rpc/{service}.go`
- 业务逻辑：`app/{service}/rpc/internal/logic/`
- 数据访问：`app/{service}/model/`
