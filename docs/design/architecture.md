# 分布式 Feed 流系统 - 完整技术方案

## 0. 开发流程总览

从零到一的分布式 Feed 流系统开发遵循以下流程：

```
需求澄清 → 架构设计 → Proto 定义 → 基础设施搭建 → 服务开发 → 集成联调 → 压测优化 → 上线运维
```

| 阶段 | 周期 | 核心产出 | 本方案覆盖 |
|------|------|---------|-----------|
| 需求澄清 | 第0周 | 业务场景、功能边界、性能指标 | ✅ 已确认 |
| 架构设计 | 第0周 | 技术方案、数据模型、接口定义 | ✅ 本文档 |
| Proto 定义 | 第1周 | .proto 文件 + 代码生成 | ✅ 第4节 |
| 基础设施搭建 | 第1周 | Docker Compose、MySQL/Redis/RocketMQ | ✅ 第8节 |
| 服务开发 (MVP) | 第1-2周 | 4个微服务核心链路 | ✅ Phase 1 |
| 异步化+推拉结合 | 第3-4周 | RocketMQ Worker、大V拉模式 | ✅ Phase 2 |
| 生产加固 | 第5-6周 | 限流、缓存、监控、测试 | ✅ Phase 3 |
| 扩展功能 | 第7-8周 | 图片、点赞、评论、推荐 | ✅ Phase 4 |

> **当前状态**: 已完成 User / Relation 服务核心实现，正在进行 Feed / Comment / Interaction 服务与 Gateway 的实现。

---

## 1. 整体架构设计

### 1.1 架构总览

```
                          ┌─────────────────────────────────────┐
                          │           API Gateway               │
                          │        (Gin HTTP REST)              │
                          │         Port: 8080                  │
                          └──────┬──────────┬──────────┬────────┘
                                 │          │          │
                          gRPC   │    gRPC  │    gRPC  │
              ┌──────────────────┤          │          ├──────────────────┐
              ▼                  ▼          ▼          ▼                  ▼
    ┌─────────────────┐ ┌─────────────────┐ ┌─────────────────┐
    │   User Service  │ │ Relation Service│ │   Feed Service  │
    │   (用户服务)     │ │  (关系服务)      │ │   (Feed服务)     │
    │   Port: 9001    │ │  Port: 9002     │ │  Port: 9003     │
    └────────┬────────┘ └────────┬────────┘ └────────┬────────┘
             │                   │                   │
             │         ┌─────────┼─────────┐         │
             │         ▼         ▼         ▼         ▼
             │  ┌──────────────────────────────────────────┐
             │  │              MySQL Cluster               │
             │  │  ┌─────────┐ ┌──────────┐ ┌──────────┐  │
             │  │  │ users   │ │relations │ │  feeds   │  │
             │  │  │  (分库)  │ │  (分表)   │ │  (分库)   │  │
             │  │  └─────────┘ └──────────┘ └──────────┘  │
             │  └──────────────────────────────────────────┘
             │
             │         ┌──────────────────────────────────────────┐
             │         │              Redis Cluster               │
             │         │  ┌───────────┐  ┌───────────┐           │
             │         │  │ Timeline  │  │   Cache   │           │
             │         │  │  Inboxes  │  │ (用户/Feed)│           │
             │         │  │ (ZSet)    │  │ (String)  │           │
             │         │  └───────────┘  └───────────┘           │
             │         └──────────────────────────────────────────┘
             │
             │         ┌──────────────────────────────────────────┐
             │         │             RocketMQ Cluster             │
             │         │  ┌──────────────────────────────────┐    │
             │         │  │  feed.created / feed.deleted     │    │
             │         │  │  relation.created / relation.deleted │ │
             │         │  │  interaction.event / comment.event   │ │
             │         │  └──────────────────────────────────┘    │
             │         └──────────────────────────────────────────┘
             │
             │         ┌──────────────────────────────────────────┐
             │         │      Object Storage (腾讯云 COS)         │
             │         │   图片/视频等静态资源（私有桶+签名URL）     │
             │         └──────────────────────────────────────────┘
```

> 静态资源存储采用**腾讯云 COS**（私有桶 + STS 临时凭证 + 签名 URL），方案详见 [oss/00-overview.md](./oss/00-overview.md)。

### 1.2 核心数据流

#### 发帖流程（推模式 - 普通用户）
```
User A 发帖
  │
  ▼
Feed Service 写入 MySQL (feeds 表)
  │
  ▼
Feed Service 发送 RocketMQ 消息 "feed.created"
  │
  ▼
Feed Worker (消费者) 消费消息
  │
  ├─► 查询 Relation Service 获取 User A 的所有粉丝 ID 列表
  │
  ├─► 批量写入 Redis: 每个粉丝的 inbox ZSet
  │   ZADD inbox:{follower_id} {timestamp} {feed_id}
  │
  └─► 写入 User A 自己的 outbox ZSet
      ZADD outbox:{user_id} {timestamp} {feed_id}
```

#### 发帖流程（拉模式 - 大V）
```
Big V 发帖
  │
  ▼
Feed Service 写入 MySQL (feeds 表)
  │
  ▼
Feed Service 发送 RocketMQ 消息 "feed.created" (标记 is_vip=true)
  │
  ▼
Feed Worker 消费消息
  │
  ├─► 判断粉丝数 > 阈值(10万)，跳过推送到粉丝 inbox
  │
  └─► 仅写入 Big V 自己的 outbox ZSet
      ZADD outbox:{big_v_id} {timestamp} {feed_id}
```

#### 读取 Timeline 流程
```
User B 拉取 Timeline (page=1, size=20)
  │
  ▼
Feed Service
  │
  ├─► 从 Redis 读取 inbox:{user_b_id}
  │   ZREVRANGE inbox:{user_b_id} {offset} {offset+size-1} WITHSCORES
  │
  ├─► 查询 Relation Service 获取 User B 关注的大V列表
  │
  ├─► 对大V列表执行拉模式合并:
  │   对每个大V: ZREVRANGE outbox:{big_v_id} 0 {page*size-1} WITHSCORES
  │
  ├─► 合并两个结果集，按时间戳倒序排列，取前 N 条
  │
  ├─► 批量查询 Feed Service 获取帖子详情 (优先从 Redis 缓存读)
  │
  └─► 返回 Timeline 结果
```

---

## 2. 微服务拆分方案

### 2.1 服务职责矩阵

| 服务 | 职责 | 数据存储 | 端口 |
|------|------|---------|------|
| **API Gateway** | HTTP路由、认证鉴权、限流、请求聚合 | - | 8080 |
| **User Service** | 用户注册/登录、个人信息管理、JWT签发 | MySQL (users DB) + Redis | 9001 |
| **Relation Service** | 关注/取关、粉丝列表、关注列表、大V标记 | MySQL (relations DB) + Redis | 9002 |
| **Feed Service** | 发帖/删帖、Timeline拉取、帖子详情 | MySQL (feeds DB) + Redis | 9003 |
| **Feed Worker** | RocketMQ消费、异步推送、大V判断 | RocketMQ Consumer | - |
| **Counter Service** (可选) | 点赞/评论/转发计数 | Redis + MySQL | 9006 |

### 2.2 User Service 详细设计

**职责**:
- 用户注册（手机号/邮箱）
- 用户登录（JWT Token 签发）
- 用户信息 CRUD
- 用户头像上传

**MySQL 表**: `users`

**Redis 缓存**:
- `user:{user_id}` - Hash，用户基本信息缓存
- `user:token:{token}` - String，JWT 黑名单

**gRPC 接口**（详见 `api/proto/user/user.proto`）：
```protobuf
service User {
  rpc Register(RegisterReq) returns (RegisterResp);
  rpc Login(LoginReq) returns (LoginResp);
  rpc GetUser(GetUserReq) returns (GetUserResp);
  rpc UpdateUser(UpdateUserReq) returns (UpdateUserResp);
  rpc BatchGetUsers(BatchGetUsersReq) returns (BatchGetUsersResp);
}
```

### 2.3 Relation Service 详细设计

**职责**:
- 关注用户
- 取关用户
- 获取关注列表（分页）
- 获取粉丝列表（分页）
- 判断是否关注
- 大V标记维护（粉丝数 > 阈值）

**MySQL 表**: `relations`

**Redis 缓存**:
- `user:follow:{user_id}` - ZSet，按关注时间排序的关注列表
- `user:fans:{user_id}` - ZSet，按关注时间排序的粉丝列表
- `user:fans_count:{user_id}` - String，粉丝数计数
- `user:vip_users` - Set，大V用户ID集合

**gRPC 接口**（详见 `api/proto/relation/relation.proto`）：
```protobuf
service Relation {
  rpc Follow(FollowReq) returns (FollowResp);
  rpc Unfollow(UnfollowReq) returns (UnfollowResp);
  rpc GetFollows(GetFollowsReq) returns (GetFollowsResp);
  rpc GetFans(GetFansReq) returns (GetFansResp);
  rpc IsFollow(IsFollowReq) returns (IsFollowResp);
  rpc IsVip(IsVipReq) returns (IsVipResp);
}
```

> 未来如需 Feed Worker 批量拉取粉丝 ID、获取「我关注的大V列表」等内部支撑接口，再扩展 `BatchGetFans`、`GetFollowingVIPs` 等方法。

### 2.4 Feed Service 详细设计

**职责**:
- 发布帖子（文字 + 图片）
- 删除帖子
- 获取帖子详情
- 拉取用户 Timeline
- 拉取用户个人主页帖子列表
- 批量获取帖子

**MySQL 表**: `feeds`

**Redis 缓存**:
- `inbox:{user_id}` - ZSet，收件箱（推模式写入）
- `outbox:{user_id}` - ZSet，发件箱（用户自己发的帖子）
- `feed:{feed_id}` - Hash，帖子详情缓存
- `feed:timeline:{user_id}` - String，Timeline 热点缓存（前2页）

**gRPC 接口**:
```protobuf
service FeedService {
  rpc CreateFeed(CreateFeedReq) returns (CreateFeedResp);
  rpc DeleteFeed(DeleteFeedReq) returns (DeleteFeedResp);
  rpc GetFeed(GetFeedReq) returns (GetFeedResp);
  rpc BatchGetFeeds(BatchGetFeedsReq) returns (BatchGetFeedsResp);
  rpc GetTimeline(GetTimelineReq) returns (GetTimelineResp);
  rpc GetUserFeeds(GetUserFeedsReq) returns (GetUserFeedsResp);
}
```

---

## 3. 数据模型设计

### 3.1 MySQL 表结构

#### users 表
```sql
CREATE TABLE users (
    id          BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    username    VARCHAR(64)     NOT NULL DEFAULT '',
    password    VARCHAR(256)    NOT NULL DEFAULT '',  -- bcrypt hash
    nickname    VARCHAR(64)     NOT NULL DEFAULT '',
    avatar      VARCHAR(512)    NOT NULL DEFAULT '',
    email       VARCHAR(128)    NOT NULL DEFAULT '',
    phone       VARCHAR(20)     NOT NULL DEFAULT '',
    bio         VARCHAR(512)    NOT NULL DEFAULT '',
    status      TINYINT         NOT NULL DEFAULT 1,   -- 1:正常 2:禁用
    created_at  DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at  DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    PRIMARY KEY (id),
    UNIQUE KEY uk_username (username),
    UNIQUE KEY uk_email (email),
    UNIQUE KEY uk_phone (phone),
    KEY idx_created_at (created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
```

> 各服务完整表结构见 `docs/design/data-model.md`。以下为当前阶段核心表简要示例。

#### relations 表

```sql
CREATE TABLE relations (
    id            BIGINT UNSIGNED NOT NULL,             -- Snowflake ID
    follower_id   BIGINT UNSIGNED NOT NULL,             -- 关注者 ID
    followee_id   BIGINT UNSIGNED NOT NULL,             -- 被关注者 ID
    created_at    BIGINT          NOT NULL,             -- 关注时间，Unix 时间戳（秒）
    PRIMARY KEY (id),
    UNIQUE KEY uk_follow (follower_id, followee_id),
    KEY idx_follower_id (follower_id, created_at),
    KEY idx_followee_id (followee_id, created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
```

#### feeds 表
```sql
CREATE TABLE feeds (
    id          BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    user_id     BIGINT UNSIGNED NOT NULL,
    content     TEXT            NOT NULL,
    images      JSON            DEFAULT NULL,       -- ["url1","url2"]
    feed_type   TINYINT         NOT NULL DEFAULT 1,  -- 1:普通 2:转发 3:回复
    ref_feed_id BIGINT UNSIGNED DEFAULT 0,           -- 转发/回复的原帖ID
    status      TINYINT         NOT NULL DEFAULT 1,  -- 1:正常 2:已删除 3:审核中
    is_vip_feed TINYINT         NOT NULL DEFAULT 0,  -- 0:否 1:大V发帖
    like_count  INT UNSIGNED    NOT NULL DEFAULT 0,
    reply_count INT UNSIGNED    NOT NULL DEFAULT 0,
    share_count INT UNSIGNED    NOT NULL DEFAULT 0,
    created_at  DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at  DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    PRIMARY KEY (id),
    KEY idx_user_id_created (user_id, created_at),
    KEY idx_created_at (created_at),
    KEY idx_status (status)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
-- 可按 user_id 分库/分表，或按时间分区
```

#### feed_images 表
```sql
CREATE TABLE feed_images (
    id          BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    feed_id     BIGINT UNSIGNED NOT NULL,
    url         VARCHAR(512)    NOT NULL DEFAULT '',
    sort_order  INT             NOT NULL DEFAULT 0,
    width       INT             NOT NULL DEFAULT 0,
    height      INT             NOT NULL DEFAULT 0,
    created_at  DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (id),
    KEY idx_feed_id (feed_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
```

### 3.2 Redis 数据结构

| Key Pattern | 类型 | 说明 | TTL | 预估容量 |
|-------------|------|------|-----|---------|
| `inbox:{user_id}` | ZSet | 用户收件箱，score=发帖时间戳，member=feed_id | 7天 | 每位用户最多存1000条 |
| `outbox:{user_id}` | ZSet | 用户发件箱，score=发帖时间戳，member=feed_id | 7天 | 大V最多存2000条 |
| `feed:{feed_id}` | Hash | 帖子详情缓存 | 30天 | 全量 |
| `user:{user_id}` | Hash | 用户基本信息缓存 | 1小时 | 全量 |
| `user:follow:{user_id}` | ZSet | 关注列表，score=关注时间戳 | 永久 | 每位用户最多5000 |
| `user:fans:{user_id}` | ZSet | 粉丝列表，score=关注时间戳 | 永久 | 大V可达百万+ |
| `user:fans_count:{user_id}` | String | 粉丝总数 | 1小时 | 全量 |
| `user:vip_users` | Set | 大V用户ID集合 | 永久 | 全量大V |
| `timeline:{user_id}:{tab}` | String | Timeline前2页JSON缓存 | 60秒 | 活跃用户 |

---

## 4. 核心接口定义

### 4.1 gRPC Proto 文件概要

完整 proto 文件结构：
```
api/proto/
├── user/v1/
│   └── user.proto
├── relation/v1/
│   └── relation.proto
├── feed/v1/
│   └── feed.proto
└── common/v1/
    └── common.proto
```

#### common.proto
```protobuf
syntax = "proto3";
package common.v1;

message Pagination {
  int32 page = 1;       // 页码，从1开始
  int32 page_size = 2;  // 每页大小，默认20，最大50
}

message PageInfo {
  int32 page = 1;
  int32 page_size = 2;
  int32 total = 3;
  int32 total_pages = 4;
  bool has_more = 5;
}
```

#### feed.proto 核心消息
```protobuf
syntax = "proto3";
package feed.v1;
import "common/v1/common.proto";

message FeedInfo {
  int64 id = 1;
  int64 user_id = 2;
  string content = 3;
  repeated string images = 4;
  int32 feed_type = 5;
  int64 ref_feed_id = 6;
  int32 like_count = 7;
  int32 reply_count = 8;
  int32 share_count = 9;
  int64 created_at = 10;  // unix timestamp milliseconds
  UserBrief author = 11;  // 作者简要信息
}

message UserBrief {
  int64 id = 1;
  string username = 2;
  string nickname = 3;
  string avatar = 4;
}

message CreateFeedReq {
  int64 user_id = 1;
  string content = 2;
  repeated string images = 3;
  int32 feed_type = 4;
  int64 ref_feed_id = 5;
}

message GetTimelineReq {
  int64 user_id = 1;
  common.v1.Pagination pagination = 2;
  int64 last_feed_id = 3;   // 游标分页用
  int64 last_timestamp = 4; // 游标分页用
}

message GetTimelineResp {
  repeated FeedInfo feeds = 1;
  common.v1.PageInfo page_info = 2;
}

service FeedService {
  rpc CreateFeed(CreateFeedReq) returns (FeedInfo);
  rpc DeleteFeed(DeleteFeedReq) returns (DeleteFeedResp);
  rpc GetFeed(GetFeedReq) returns (FeedInfo);
  rpc BatchGetFeeds(BatchGetFeedsReq) returns (BatchGetFeedsResp);
  rpc GetTimeline(GetTimelineReq) returns (GetTimelineResp);
  rpc GetUserFeeds(GetUserFeedsReq) returns (GetUserFeedsResp);
}
```

### 4.2 REST API 概要（API Gateway）

```
Base URL: /api/v1

=== 用户模块 ===
POST   /api/v1/users/register           # 用户注册
POST   /api/v1/users/login              # 用户登录
GET    /api/v1/users/:id                # 获取用户信息
PUT    /api/v1/users/:id                # 更新用户信息
POST   /api/v1/users/:id/avatar         # 上传头像

=== 关系模块 ===
POST   /api/v1/relations/follow         # 关注用户
POST   /api/v1/relations/unfollow       # 取关用户
GET    /api/v1/relations/following      # 关注列表（分页）
GET    /api/v1/relations/followers      # 粉丝列表（分页）
GET    /api/v1/relations/is-following   # 是否已关注

=== Feed 模块 ===
POST   /api/v1/feeds                    # 发布帖子
DELETE /api/v1/feeds/:id                # 删除帖子
GET    /api/v1/feeds/:id                # 帖子详情
GET    /api/v1/timeline                 # 拉取 Timeline（首页 Feed 流）
GET    /api/v1/users/:id/feeds          # 用户个人主页帖子列表
```

---

## 5. 推拉结合策略

### 5.1 大V阈值设定

| 参数 | 默认值 | 说明 |
|------|--------|------|
| `VIP_FOLLOWER_THRESHOLD` | 100,000 | 粉丝数超过此阈值判定为大V |
| `PUSH_FAN_BATCH_SIZE` | 500 | 推模式每批推送的粉丝数量 |
| `INBOX_MAX_SIZE` | 1000 | 普通用户收件箱最大容量（超出后裁剪） |
| `OUTBOX_VIP_MAX_SIZE` | 2000 | 大V发件箱最大容量 |

### 5.2 推模式（普通用户）

```
触发条件: 发帖用户粉丝数 <= 100,000
执行流程:
  1. Feed 写入 MySQL
  2. 发送 RocketMQ 消息 (is_vip=false)
  3. Worker 消费后，批量写入粉丝 inbox ZSet
  4. 每个粉丝 inbox 只保留最近 1000 条
  5. 异步通知在线用户（WebSocket 可选）
```

### 5.3 拉模式（大V）

```
触发条件: 发帖用户粉丝数 > 100,000
执行流程:
  1. Feed 写入 MySQL
  2. 发送 RocketMQ 消息 (is_vip=true)
  3. Worker 消费后，仅写入大V自己的 outbox ZSet
  4. 不推送到粉丝 inbox
  5. 粉丝拉取 Timeline 时，合并大V outbox 数据
```

### 5.4 Timeline 拉取时的合并逻辑

```go
// 伪代码
func GetTimeline(userID int64, page, pageSize int) ([]FeedInfo, error) {
    // 1. 从 Redis inbox 获取推模式数据
    inboxFeeds := redis.ZRevRange("inbox:"+userID, offset, offset+pageSize)

    // 2. 获取用户关注的大V列表
    vips := relationClient.GetFollowingVIPs(userID)

    // 3. 拉取每个大V的最新帖子
    var vipFeeds []FeedItem
    for _, vipID := range vips {
        feeds := redis.ZRevRange("outbox:"+vipID, 0, page*pageSize-1)
        vipFeeds = append(vipFeeds, feeds...)
    }

    // 4. 合并排序（按时间戳降序）
    allFeeds := mergeAndSort(inboxFeeds, vipFeeds)

    // 5. 分页截取
    result := allFeeds[offset : offset+pageSize]

    // 6. 批量查询帖子详情（优先从 Redis 缓存读）
    feedDetails := batchGetFeedDetails(result)

    return feedDetails, nil
}
```

### 5.5 冷热数据分离

```
热数据（7天内）:
  - 存储: Redis ZSet (inbox / outbox)
  - 特点: 高频访问，延迟 < 5ms

温数据（7-30天）:
  - 存储: Redis (降级) + MySQL
  - 特点: 中等频率，通过 MySQL 查询

冷数据（30天以上）:
  - 存储: MySQL + 归档表
  - 特点: 低频访问，可接受 100ms+ 延迟
```

---

## 6. 异步消息设计

### 6.1 RocketMQ Topic 定义

| Topic | 队列数 | 说明 |
|-------|--------|------|
| `feed.created` | 16 | 帖子发布事件 |
| `feed.deleted` | 8 | 帖子删除事件 |
| `relation.created` | 8 | 关注事件 |
| `relation.deleted` | 8 | 取关事件 |
| `interaction.event` | 8 | 点赞/收藏事件 |
| `comment.event` | 4 | 评论事件 |
| `user.updated` | 4 | 用户信息变更事件 |

### 6.2 消息格式

#### feed.created
```json
{
  "event_id": "uuid-v4",
  "event_type": "feed.created",
  "feed_id": 123456789,
  "user_id": 987654321,
  "is_vip": false,
  "content": "这是一条帖子内容（摘要，前200字）",
  "timestamp": 1720000000000,
  "metadata": {
    "feed_type": 1,
    "image_count": 2
  }
}
```

#### relation.created
```json
{
  "event_id": "uuid-v4",
  "event_type": "relation.created",
  "follower_id": 12345,
  "followee_id": 67890,
  "is_vip_relation": false,
  "timestamp": 1720000000000
}
```

### 6.3 消费者组设计

| Consumer Group | 订阅 Topic | 职责 |
|----------------|-----------|------|
| `feed-push-worker` | `feed.created`, `feed.deleted` | 推送到粉丝 inbox |
| `relation-sync-worker` | `relation.created`, `relation.deleted` | 同步关注关系缓存 |
| `feed-analytics-worker` | `feed.created` | 统计、推荐（可选） |

### 6.4 幂等性保证

- 每条消息包含唯一 `event_id`
- Worker 处理前检查 `event_id` 是否已处理（Redis `processed:{event_id}` 标记，TTL 24h）
- 若已处理则跳过，保证 At-Least-Once 语义下的幂等

---

## 7. 分页与一致性

### 7.1 分页策略

**方案：混合分页（Offset + Cursor）**

```
第一页 (page=1): 使用 Offset 分页
  - 从 Redis ZSet 直接取
  - ZREVRANGE inbox:{user_id} 0 19

后续页 (page>1): 使用 Cursor 分页
  - 前端传入上一页最后一条的 feed_id 和 timestamp
  - ZREVRANGEBYSCORE inbox:{user_id} {last_timestamp} -inf LIMIT 0 {page_size}
  - 避免翻页过程中新数据插入导致重复/遗漏
```

**请求参数**:
```json
{
  "page": 1,
  "page_size": 20,
  "last_feed_id": 0,
  "last_timestamp": 0
}
```

**响应参数**:
```json
{
  "feeds": [...],
  "page_info": {
    "page": 1,
    "page_size": 20,
    "has_more": true,
    "next_cursor": {
      "last_feed_id": 12345,
      "last_timestamp": 1720000000000
    }
  }
}
```

### 7.2 缓存一致性处理

#### Cache-Aside 模式
```
读流程:
  1. 先查 Redis 缓存
  2. 命中 -> 返回
  3. 未命中 -> 查 MySQL -> 写入 Redis -> 返回

写流程:
  1. 先写 MySQL
  2. 删除/更新 Redis 缓存
  3. 发送 RocketMQ 消息通知其他服务
```

#### Timeline 缓存策略
```
Timeline 前 2 页（40条）:
  - 缓存到 Redis: timeline:{user_id}:{tab}
  - TTL: 60 秒
  - 当用户发帖/新关注时主动失效

Feed 详情:
  - 缓存到 Redis: feed:{feed_id}
  - TTL: 30 天
  - 更新策略: 先更新 MySQL，再删除缓存
```

#### 一致性保证
- **最终一致性**: 大V发帖到粉丝可见，延迟 < 3 秒（RocketMQ消费延迟）
- **读己之写**: 发帖后立即写入自己的 outbox，自己 Timeline 立即可见
- **缓存穿透**: 布隆过滤器过滤不存在的 feed_id
- **缓存雪崩**: TTL 加随机偏移（±10%），避免集中过期

---

## 8. 部署架构

### 8.1 Docker Compose 编排

本地/CVM 开发环境使用项目根目录下的 `deploy/docker-compose.yaml`，一键启动：

```bash
make up      # 启动 MySQL / Redis / etcd / RocketMQ
make down    # 停止并保留数据卷
make down-clean  # 停止并清空数据卷（重新开始）
```

关键约定：
- MySQL root 密码：`root`（开发环境明文，生产走 Secret）。
- Redis 端口：`6379`，已启用密码。
- etcd 宿主机端口：`2479`（避开 K8s 节点默认占用的 `2379`）。
- RocketMQ NameServer：`9876`，Dashboard：`9877`。
- 各 RPC 服务端口：`9001`（User）、`9002`（Relation）、`9003`（Feed）、`9004`（Comment）、`9005`（Interaction）。

> 详细的 compose 配置（含 healthcheck、数据卷、RocketMQ Dashboard 安全版本等）请直接查看 `deploy/docker-compose.yaml`，避免本文档示例与实际文件脱节。

### 8.2 资源配置建议

| 服务 | CPU | 内存 | 实例数 | 说明 |
|------|-----|------|--------|------|
| API Gateway | 1-2核 | 512MB | 2 | 无状态，可水平扩展 |
| User Service | 1核 | 256MB | 2 | 轻量服务 |
| Relation Service | 2核 | 512MB | 2 | 关注/取关高并发 |
| Feed Service | 2-4核 | 1GB | 3 | Timeline 查询压力大 |
| Feed Worker | 2核 | 512MB | 2 | RocketMQ 消费 |
| MySQL | 4核 | 8GB | 1主+1从 | 可后续分库 |
| Redis | 2核 | 4GB | 1主+1从 | 根据数据量扩展 |
| RocketMQ | 2核 | 4GB | 2主+2从 | 生产环境 NameServer 至少 2 节点 |

---

## 9. 项目目录结构

本项目采用 **monorepo + go-zero 标准目录结构**，每个 RPC 服务独立入口，公共能力下沉到 `common/`。

```
feed/
├── api/
│   └── proto/              # 内部 gRPC Proto 契约
│       ├── user/user.proto
│       ├── relation/relation.proto
│       ├── feed/feed.proto
│       ├── comment/comment.proto
│       └── interaction/interaction.proto
│
├── app/
│   ├── user/rpc/           # User 服务入口：user.go，端口 9001
│   ├── relation/rpc/       # Relation 服务入口：relation.go，端口 9002
│   ├── feed/rpc/           # Feed 服务入口：feed.go，端口 9003
│   ├── comment/rpc/        # Comment 服务入口：comment.go，端口 9004
│   ├── interaction/rpc/    # Interaction 服务入口：interaction.go，端口 9005
│   └── gateway/            # HTTP 网关，端口 8080（已运行，仅接入 user/relation 路由）
│       ├── api/            # *.api 接口定义文件
│       └── cmd/api/        # 网关入口 main.go
│
├── common/                 # 跨服务公共代码
│   ├── errorx/             # 统一错误码
│   ├── idgen/              # Snowflake ID
│   ├── jwtx/               # JWT 工具
│   ├── response/           # 统一响应结构
│   └── ...
│
├── deploy/
│   ├── docker-compose.yaml # MySQL/Redis/etcd/RocketMQ 开发环境
│   ├── sql/                # 各服务建表脚本
│   └── k8s/                # K8s 部署配置（待补充）
│
├── docs/
│   ├── agent/              # AI 协作规范
│   ├── design/             # 系统设计文档
│   └── *-test-plan.md      # 各服务测试方案
│
├── scripts/                # 初始化与压测脚本
├── Makefile
├── go.mod
├── go.sum
├── README.md
└── AGENTS.md               # AI 协作总览指南
```

**关键约定**：
- RPC 服务入口为 `app/<svc>/rpc/<svc>.go`，不是 `cmd/<svc>/main.go`。
- Proto 路径为 `api/proto/<svc>/<svc>.proto`，无 `v1` 子目录。
- 业务逻辑写在 `app/<svc>/rpc/internal/logic/`，数据访问写在 `app/<svc>/model/`。

---

## 10. 开发阶段规划

### Phase 1: MVP 基础（第1-2周）

**目标**: 跑通核心链路，单用户发帖+关注+Timeline 拉取

| 任务 | 产出 | 优先级 |
|------|------|--------|
| 项目骨架搭建 | go mod init, 目录结构, Makefile | P0 |
| Proto 定义 | user.proto, relation.proto, feed.proto, common.proto | P0 |
| Proto 代码生成 | buf generate, 生成 Go stub | P0 |
| MySQL 表结构 | users, relations, feeds 建表 SQL | P0 |
| Redis 连接封装 | Redis 客户端初始化, ZSet 操作封装 | P0 |
| User Service (基础) | 注册/登录/获取信息 | P0 |
| Relation Service (基础) | 关注/取关/关注列表 | P0 |
| Feed Service (基础) | 发帖/帖子详情 | P0 |
| Timeline 拉取 (纯推模式) | 发帖推送到粉丝 inbox, 拉取 inbox ZSet | P0 |
| API Gateway (基础) | Gin 路由, 中间件, gRPC 客户端 | P0 |
| Docker Compose | 开发环境一键启动 | P0 |

**验证标准**: 用户A发帖 -> 用户B(关注A)刷新Timeline能看到帖子

### Phase 2: 推拉结合 + RocketMQ 异步（第3-4周）

**目标**: 引入 RocketMQ 异步解耦，实现大V拉模式

| 任务 | 产出 | 优先级 |
|------|------|--------|
| RocketMQ 集成 | Producer/Consumer 封装, Topic 创建 | P0 |
| feed.created 事件 | 发帖后发送 RocketMQ 消息 | P0 |
| Feed Worker | 消费 feed.created, 异步推送 inbox | P0 |
| relation.created 事件 | 关注/取关事件 | P0 |
| Relation Worker | 消费关系事件, 同步缓存 | P1 |
| 大V判定逻辑 | 粉丝数阈值检查, is_vip 标记 | P0 |
| 拉模式合并 | Timeline 拉取时合并大V outbox | P0 |
| ID 生成器 | Snowflake 分布式 ID | P0 |
| 分页优化 | Cursor 分页支持 | P1 |
| 缓存预热 | 热门 Feed 缓存 | P1 |

**验证标准**: 大V发帖不推送到粉丝 inbox, 粉丝拉取时能合并看到

### Phase 3: 生产加固（第5-6周）

**目标**: 性能优化、稳定性、可观测性

| 任务 | 产出 | 优先级 |
|------|------|--------|
| 接口限流 | API Gateway 令牌桶限流 | P0 |
| 缓存穿透防护 | 布隆过滤器 | P0 |
| Timeline 热点缓存 | 前2页缓存, 主动失效 | P0 |
| 消息幂等 | event_id 去重 | P0 |
| 日志系统 | 结构化日志 (zap) | P1 |
| 链路追踪 | OpenTelemetry + Jaeger | P1 |
| 监控告警 | Prometheus + Grafana | P1 |
| 优雅关闭 | signal handling, 消费完再退出 | P1 |
| 单元测试 | service/repository 层测试 | P1 |
| 压力测试 | wrk/k6 压测脚本 | P2 |
| 数据库索引优化 | 慢查询分析, 索引调整 | P2 |

**验证标准**: 单实例支持 1000 QPS Timeline 查询, P99 延迟 < 100ms

### Phase 4: 扩展功能（第7-8周，可选）

| 任务 | 产出 | 优先级 |
|------|------|--------|
| 图片上传 | MinIO 集成, 图片处理 | P2 |
| 点赞功能 | Counter Service, Redis 计数 | P2 |
| 评论功能 | Feed 评论 CRUD | P2 |
| 转发功能 | 转发帖子 | P2 |
| 消息推送 | WebSocket 实时通知 | P3 |
| Feed 推荐 | 基于热度的推荐排序 | P3 |
| 内容审核 | 敏感词过滤 | P3 |
| 数据归档 | 冷数据迁移脚本 | P3 |

---

## 附录 A: 关键技术决策记录

| 决策 | 选项 | 选择 | 理由 |
|------|------|------|------|
| 服务通信 | REST vs gRPC | gRPC | 性能高, 强类型, 适合微服务 |
| 消息队列 | RabbitMQ vs Kafka vs RocketMQ | RocketMQ | 高吞吐, 持久化, 中文社区活跃, 适合 Feed 场景 |
| 收件箱存储 | List vs ZSet | ZSet | 按时间排序, 支持范围查询 |
| ID 生成 | 自增 vs UUID vs Snowflake | Snowflake | 全局唯一, 趋势递增, 高性能 |
| 配置管理 | 环境变量 vs 配置中心 | 环境变量 + YAML | MVP 阶段够用, 后续可接配置中心 |
| 分页方案 | Offset vs Cursor | 混合 | 首页用 Offset, 后续用 Cursor |

## 附录 B: 核心代码示例

### Timeline 合并核心逻辑
```go
// app/feed/rpc/internal/logic/getTimelineLogic.go（示意）

func (l *GetTimelineLogic) GetTimeline(in *feed.GetTimelineReq) (*feed.GetTimelineResp, error) {
    userID := in.UserId
    pageSize := in.PageSize

    // 1. 获取推模式数据（inbox）
    inboxFeeds, err := l.svcCtx.Redis.ZrevrangeCtx(l.ctx, fmt.Sprintf("inbox:%d", userID), 0, int64(pageSize-1))
    if err != nil {
        return nil, err
    }

    // 2. 获取关注列表，并筛选出大V（后续可扩展为 Relation.BatchIsVip 批量接口）
    followsResp, err := l.svcCtx.RelationRpc.GetFollows(l.ctx, &relation.GetFollowsReq{UserId: userID, Page: 1, PageSize: 1000})
    if err != nil {
        return nil, err
    }
    var vipIDs []int64
    for _, followeeID := range followsResp.FolloweeIds {
        vipResp, _ := l.svcCtx.RelationRpc.IsVip(l.ctx, &relation.IsVipReq{UserId: followeeID})
        if vipResp != nil && vipResp.IsVip {
            vipIDs = append(vipIDs, followeeID)
        }
    }

    // 3. 拉取大V发件箱
    var vipFeedItems []FeedItem
    for _, vipID := range vipIDs {
        feeds, err := l.svcCtx.Redis.ZrevrangeCtx(l.ctx, fmt.Sprintf("outbox:%d", vipID), 0, int64(pageSize-1))
        if err != nil {
            continue // 单个大V失败不阻塞整体
        }
        vipFeedItems = append(vipFeedItems, feeds...)
    }

    // 4. 合并排序 + 5. 分页 + 6. 批量获取详情 + 7. 填充作者信息
    allItems := mergeByTimestamp(inboxFeeds, vipFeedItems)
    pageItems := paginate(allItems, in.Page, pageSize)
    feeds := batchGetFeedDetails(pageItems)
    result := fillAuthors(feeds)

    return &feed.GetTimelineResp{Feeds: result}, nil
}
```

---

*本文档为分布式 Feed 流系统完整技术方案，涵盖架构设计、数据模型、接口定义、推拉结合策略、部署方案和开发规划。确认后即可开始编码实施。*
