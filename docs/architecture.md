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
| 基础设施搭建 | 第1周 | Docker Compose、MySQL/Redis/Kafka | ✅ 第8节 |
| 服务开发 (MVP) | 第1-2周 | 4个微服务核心链路 | ✅ Phase 1 |
| 异步化+推拉结合 | 第3-4周 | Kafka Worker、大V拉模式 | ✅ Phase 2 |
| 生产加固 | 第5-6周 | 限流、缓存、监控、测试 | ✅ Phase 3 |
| 扩展功能 | 第7-8周 | 图片、点赞、评论、推荐 | ✅ Phase 4 |

> **当前状态**: 正处于「架构设计」阶段，本文档即为完整技术方案。确认后进入「Proto 定义 + 基础设施搭建」阶段。

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
    │   Port: 50051   │ │  Port: 50052    │ │  Port: 50053    │
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
             │         │              Kafka Cluster               │
             │         │  ┌──────────────────────────────────┐    │
             │         │  │  feed.created / feed.deleted     │    │
             │         │  │  relation.created / relation.deleted │ │
             │         │  └──────────────────────────────────┘    │
             │         └──────────────────────────────────────────┘
             │
             │         ┌──────────────────────────────────────────┐
             │         │          Object Storage (MinIO)          │
             │         │        图片/视频等静态资源存储              │
             │         └──────────────────────────────────────────┘
```

### 1.2 核心数据流

#### 发帖流程（推模式 - 普通用户）
```
User A 发帖
  │
  ▼
Feed Service 写入 MySQL (feeds 表)
  │
  ▼
Feed Service 发送 Kafka 消息 "feed.created"
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
Feed Service 发送 Kafka 消息 "feed.created" (标记 is_vip=true)
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
| **User Service** | 用户注册/登录、个人信息管理、JWT签发 | MySQL (users DB) + Redis | 50051 |
| **Relation Service** | 关注/取关、粉丝列表、关注列表、大V标记 | MySQL (relations DB) + Redis | 50052 |
| **Feed Service** | 发帖/删帖、Timeline拉取、帖子详情 | MySQL (feeds DB) + Redis | 50053 |
| **Feed Worker** | Kafka消费、异步推送、大V判断 | Kafka Consumer | - |
| **Counter Service** (可选) | 点赞/评论/转发计数 | Redis + MySQL | 50054 |

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

**gRPC 接口**:
```protobuf
service UserService {
  rpc CreateUser(CreateUserReq) returns (CreateUserResp);
  rpc GetUser(GetUserReq) returns (GetUserResp);
  rpc GetUserByUsername(GetUserByUsernameReq) returns (GetUserResp);
  rpc UpdateUser(UpdateUserReq) returns (UpdateUserResp);
  rpc BatchGetUsers(BatchGetUsersReq) returns (BatchGetUsersResp);
  rpc VerifyToken(VerifyTokenReq) returns (VerifyTokenResp);
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
- `following:{user_id}` - ZSet，按关注时间排序的关注列表
- `follower:{user_id}` - ZSet，按关注时间排序的粉丝列表
- `follow_count:{user_id}` - Hash，{following_count, follower_count}

**gRPC 接口**:
```protobuf
service RelationService {
  rpc Follow(FollowReq) returns (FollowResp);
  rpc Unfollow(UnfollowReq) returns (UnfollowResp);
  rpc GetFollowing(GetFollowingReq) returns (GetFollowingResp);
  rpc GetFollowers(GetFollowersReq) returns (GetFollowersResp);
  rpc IsFollowing(IsFollowingReq) returns (IsFollowingResp);
  rpc GetFollowingVIPs(GetFollowingVIPsReq) returns (GetFollowingVIPsResp);  // 获取关注的大V列表
  rpc IsVIP(IsVIPReq) returns (IsVIPResp);  // 判断用户是否为大V
  rpc GetFollowerIDs(GetFollowerIDsReq) returns (GetFollowerIDsResp);  // 批量获取粉丝ID（用于推送）
}
```

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

#### relations 表（按 follower_id 分表）
```sql
CREATE TABLE relations (
    id              BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    follower_id     BIGINT UNSIGNED NOT NULL,       -- 关注者 ID
    following_id    BIGINT UNSIGNED NOT NULL,       -- 被关注者 ID
    status          TINYINT         NOT NULL DEFAULT 1,  -- 1:正常 2:已取关
    is_vip_relation TINYINT         NOT NULL DEFAULT 0,  -- 0:否 1:是（following_id 是大V）
    created_at      DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at      DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    PRIMARY KEY (id),
    UNIQUE KEY uk_follower_following (follower_id, following_id),
    KEY idx_following_id (following_id),
    KEY idx_follower_id (follower_id),
    KEY idx_created_at (created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
-- 按 follower_id 分 64 张表: relations_0 ~ relations_63
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
| `following:{user_id}` | ZSet | 关注列表，score=关注时间戳 | 永久 | 每位用户最多5000 |
| `follower:{user_id}` | ZSet | 粉丝列表，score=关注时间戳 | 永久 | 大V可达百万+ |
| `follow_count:{user_id}` | Hash | {following_count, follower_count} | 1小时 | 全量 |
| `vip:list` | Set | 大V用户ID集合 | 永久 | 全量大V |
| `timeline_cache:{user_id}` | String | Timeline前2页JSON缓存 | 60秒 | 活跃用户 |

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
  2. 发送 Kafka 消息 (is_vip=false)
  3. Worker 消费后，批量写入粉丝 inbox ZSet
  4. 每个粉丝 inbox 只保留最近 1000 条
  5. 异步通知在线用户（WebSocket 可选）
```

### 5.3 拉模式（大V）

```
触发条件: 发帖用户粉丝数 > 100,000
执行流程:
  1. Feed 写入 MySQL
  2. 发送 Kafka 消息 (is_vip=true)
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

### 6.1 Kafka Topic 定义

| Topic | 分区数 | 副本数 | 说明 |
|-------|--------|--------|------|
| `feed.created` | 16 | 2 | 帖子发布事件 |
| `feed.deleted` | 8 | 2 | 帖子删除事件 |
| `relation.created` | 8 | 2 | 关注事件 |
| `relation.deleted` | 8 | 2 | 取关事件 |
| `user.updated` | 4 | 2 | 用户信息变更事件 |

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
  "following_id": 67890,
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
  3. 发送 Kafka 消息通知其他服务
```

#### Timeline 缓存策略
```
Timeline 前 2 页（40条）:
  - 缓存到 Redis: timeline_cache:{user_id}
  - TTL: 60 秒
  - 当用户发帖/新关注时主动失效

Feed 详情:
  - 缓存到 Redis: feed:{feed_id}
  - TTL: 30 天
  - 更新策略: 先更新 MySQL，再删除缓存
```

#### 一致性保证
- **最终一致性**: 大V发帖到粉丝可见，延迟 < 3 秒（Kafka消费延迟）
- **读己之写**: 发帖后立即写入自己的 outbox，自己 Timeline 立即可见
- **缓存穿透**: 布隆过滤器过滤不存在的 feed_id
- **缓存雪崩**: TTL 加随机偏移（±10%），避免集中过期

---

## 8. 部署架构

### 8.1 Docker Compose 编排

```yaml
version: '3.8'

services:
  # ============ 基础设施 ============

  mysql:
    image: mysql:8.0
    container_name: feed-mysql
    environment:
      MYSQL_ROOT_PASSWORD: root123
      MYSQL_DATABASE: feed
    ports:
      - "3306:3306"
    volumes:
      - mysql-data:/var/lib/mysql
      - ./deploy/mysql/init.sql:/docker-entrypoint-initdb.d/init.sql
    command: --default-authentication-plugin=mysql_native_password
    networks:
      - feed-network

  redis:
    image: redis:7-alpine
    container_name: feed-redis
    ports:
      - "6379:6379"
    volumes:
      - redis-data:/data
      - ./deploy/redis/redis.conf:/usr/local/etc/redis/redis.conf
    command: redis-server /usr/local/etc/redis/redis.conf
    networks:
      - feed-network

  zookeeper:
    image: confluentinc/cp-zookeeper:7.5.0
    container_name: feed-zookeeper
    environment:
      ZOOKEEPER_CLIENT_PORT: 2181
      ZOOKEEPER_TICK_TIME: 2000
    networks:
      - feed-network

  kafka:
    image: confluentinc/cp-kafka:7.5.0
    container_name: feed-kafka
    depends_on:
      - zookeeper
    ports:
      - "9092:9092"
    environment:
      KAFKA_BROKER_ID: 1
      KAFKA_ZOOKEEPER_CONNECT: zookeeper:2181
      KAFKA_ADVERTISED_LISTENERS: PLAINTEXT://kafka:9092
      KAFKA_OFFSETS_TOPIC_REPLICATION_FACTOR: 1
    networks:
      - feed-network

  minio:
    image: minio/minio:latest
    container_name: feed-minio
    ports:
      - "9000:9000"
      - "9001:9001"
    environment:
      MINIO_ROOT_USER: minioadmin
      MINIO_ROOT_PASSWORD: minioadmin
    command: server /data --console-address ":9001"
    volumes:
      - minio-data:/data
    networks:
      - feed-network

  # ============ 业务服务 ============

  api-gateway:
    build:
      context: .
      dockerfile: build/Dockerfile.gateway
    container_name: feed-api-gateway
    ports:
      - "8080:8080"
    environment:
      - USER_SERVICE_ADDR=user-service:50051
      - RELATION_SERVICE_ADDR=relation-service:50051
      - FEED_SERVICE_ADDR=feed-service:50051
      - REDIS_ADDR=redis:6379
    depends_on:
      - user-service
      - relation-service
      - feed-service
    networks:
      - feed-network

  user-service:
    build:
      context: .
      dockerfile: build/Dockerfile.user
    container_name: feed-user-service
    environment:
      - MYSQL_DSN=root:root123@tcp(mysql:3306)/feed?charset=utf8mb4&parseTime=True
      - REDIS_ADDR=redis:6379
      - GRPC_PORT=50051
    depends_on:
      - mysql
      - redis
    networks:
      - feed-network

  relation-service:
    build:
      context: .
      dockerfile: build/Dockerfile.relation
    container_name: feed-relation-service
    environment:
      - MYSQL_DSN=root:root123@tcp(mysql:3306)/feed?charset=utf8mb4&parseTime=True
      - REDIS_ADDR=redis:6379
      - GRPC_PORT=50051
    depends_on:
      - mysql
      - redis
    networks:
      - feed-network

  feed-service:
    build:
      context: .
      dockerfile: build/Dockerfile.feed
    container_name: feed-feed-service
    environment:
      - MYSQL_DSN=root:root123@tcp(mysql:3306)/feed?charset=utf8mb4&parseTime=True
      - REDIS_ADDR=redis:6379
      - KAFKA_BROKERS=kafka:9092
      - GRPC_PORT=50051
      - RELATION_SERVICE_ADDR=relation-service:50051
      - USER_SERVICE_ADDR=user-service:50051
    depends_on:
      - mysql
      - redis
      - kafka
    networks:
      - feed-network

  feed-worker:
    build:
      context: .
      dockerfile: build/Dockerfile.worker
    container_name: feed-worker
    environment:
      - REDIS_ADDR=redis:6379
      - KAFKA_BROKERS=kafka:9092
      - RELATION_SERVICE_ADDR=relation-service:50051
    depends_on:
      - redis
      - kafka
    networks:
      - feed-network

networks:
  feed-network:
    driver: bridge

volumes:
  mysql-data:
  redis-data:
  minio-data:
```

### 8.2 资源配置建议

| 服务 | CPU | 内存 | 实例数 | 说明 |
|------|-----|------|--------|------|
| API Gateway | 1-2核 | 512MB | 2 | 无状态，可水平扩展 |
| User Service | 1核 | 256MB | 2 | 轻量服务 |
| Relation Service | 2核 | 512MB | 2 | 关注/取关高并发 |
| Feed Service | 2-4核 | 1GB | 3 | Timeline 查询压力大 |
| Feed Worker | 2核 | 512MB | 2 | Kafka 消费 |
| MySQL | 4核 | 8GB | 1主+1从 | 可后续分库 |
| Redis | 2核 | 4GB | 1主+1从 | 根据数据量扩展 |
| Kafka | 2核 | 4GB | 3节点 | 生产环境需3节点 |

---

## 9. 项目目录结构

```
feed/
├── api/
│   └── proto/
│       ├── common/v1/
│       │   └── common.proto              # 公共消息定义
│       ├── user/v1/
│       │   └── user.proto                # 用户服务接口
│       ├── relation/v1/
│       │   └── relation.proto            # 关系服务接口
│       └── feed/v1/
│           └── feed.proto                # Feed 服务接口
│
├── cmd/
│   ├── gateway/
│   │   └── main.go                       # API Gateway 入口
│   ├── user/
│   │   └── main.go                       # User Service 入口
│   ├── relation/
│   │   └── main.go                       # Relation Service 入口
│   ├── feed/
│   │   └── main.go                       # Feed Service 入口
│   └── worker/
│       └── main.go                       # Feed Worker 入口
│
├── internal/
│   ├── gateway/
│   │   ├── handler/                      # HTTP 请求处理器
│   │   │   ├── user_handler.go
│   │   │   ├── relation_handler.go
│   │   │   └── feed_handler.go
│   │   ├── middleware/                   # 中间件
│   │   │   ├── auth.go
│   │   │   ├── ratelimit.go
│   │   │   └── recovery.go
│   │   └── router/
│   │       └── router.go                 # 路由注册
│   │
│   ├── user/
│   │   ├── server/
│   │   │   └── grpc.go                   # gRPC Server 实现
│   │   ├── repository/
│   │   │   └── user_repo.go              # 数据访问层
│   │   ├── service/
│   │   │   └── user_service.go           # 业务逻辑层
│   │   └── model/
│   │       └── user.go                   # 数据模型
│   │
│   ├── relation/
│   │   ├── server/
│   │   │   └── grpc.go
│   │   ├── repository/
│   │   │   └── relation_repo.go
│   │   ├── service/
│   │   │   └── relation_service.go
│   │   └── model/
│   │       └── relation.go
│   │
│   ├── feed/
│   │   ├── server/
│   │   │   └── grpc.go
│   │   ├── repository/
│   │   │   └── feed_repo.go
│   │   ├── service/
│   │   │   └── feed_service.go
│   │   ├── timeline/
│   │   │   └── timeline.go               # Timeline 合并逻辑
│   │   └── model/
│   │       └── feed.go
│   │
│   └── worker/
│       ├── consumer/
│       │   ├── feed_consumer.go           # feed.created 消费者
│       │   └── relation_consumer.go       # relation.created 消费者
│       └── pusher/
│           └── inbox_pusher.go            # 收件箱推送逻辑
│
├── pkg/
│   ├── config/
│   │   └── config.go                      # 统一配置管理
│   ├── database/
│   │   ├── mysql.go                       # MySQL 连接管理
│   │   └── redis.go                       # Redis 连接管理
│   ├── kafka/
│   │   ├── producer.go                    # Kafka Producer
│   │   └── consumer.go                    # Kafka Consumer
│   ├── grpc/
│   │   └── client.go                      # gRPC 客户端连接池
│   ├── middleware/
│   │   └── interceptor.go                 # gRPC 拦截器
│   ├── errors/
│   │   └── errors.go                      # 统一错误码
│   └── utils/
│       ├── idgen.go                       # ID 生成器 (Snowflake)
│       ├── jwt.go                         # JWT 工具
│       └── validator.go                   # 参数校验
│
├── deploy/
│   ├── docker-compose.yaml                # 开发环境编排
│   ├── mysql/
│   │   └── init.sql                       # 初始化 SQL
│   ├── redis/
│   │   └── redis.conf                     # Redis 配置
│   └── kafka/
│       └── create-topics.sh               # Topic 创建脚本
│
├── scripts/
│   ├── gen-proto.sh                       # Proto 代码生成脚本
│   └── migrate.sh                         # 数据库迁移脚本
│
├── configs/
│   ├── config.yaml                        # 默认配置
│   ├── config.dev.yaml                    # 开发环境配置
│   └── config.prod.yaml                   # 生产环境配置
│
├── Makefile                               # 构建、测试、部署命令
├── go.mod
├── go.sum
└── README.md
```

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

### Phase 2: 推拉结合 + Kafka 异步（第3-4周）

**目标**: 引入 Kafka 异步解耦，实现大V拉模式

| 任务 | 产出 | 优先级 |
|------|------|--------|
| Kafka 集成 | Producer/Consumer 封装, Topic 创建 | P0 |
| feed.created 事件 | 发帖后发送 Kafka 消息 | P0 |
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
| 消息队列 | RabbitMQ vs Kafka | Kafka | 高吞吐, 持久化, 适合 Feed 场景 |
| 收件箱存储 | List vs ZSet | ZSet | 按时间排序, 支持范围查询 |
| ID 生成 | 自增 vs UUID vs Snowflake | Snowflake | 全局唯一, 趋势递增, 高性能 |
| 配置管理 | 环境变量 vs 配置中心 | 环境变量 + YAML | MVP 阶段够用, 后续可接配置中心 |
| 分页方案 | Offset vs Cursor | 混合 | 首页用 Offset, 后续用 Cursor |

## 附录 B: 核心代码示例

### Timeline 合并核心逻辑
```go
// internal/feed/timeline/timeline.go

func (s *TimelineService) GetTimeline(ctx context.Context, userID int64, page, pageSize int32) ([]model.FeedWithAuthor, *PageInfo, error) {
    // 1. 获取推模式数据（inbox）
    inboxFeeds, err := s.redis.ZRevRange(ctx, fmt.Sprintf("inbox:%d", userID), 0, int64(page*pageSize-1))
    if err != nil {
        return nil, nil, err
    }

    // 2. 获取关注的大V列表
    vips, err := s.relationClient.GetFollowingVIPs(ctx, userID)
    if err != nil {
        return nil, nil, err
    }

    // 3. 拉取大V发件箱
    var vipFeedItems []model.FeedItem
    for _, vipID := range vips {
        feeds, err := s.redis.ZRevRange(ctx, fmt.Sprintf("outbox:%d", vipID), 0, int64(page*pageSize-1))
        if err != nil {
            continue // 单个大V失败不阻塞整体
        }
        vipFeedItems = append(vipFeedItems, feeds...)
    }

    // 4. 合并排序
    allItems := s.mergeByTimestamp(inboxFeeds, vipFeedItems)

    // 5. 分页
    offset := (page - 1) * pageSize
    if int(offset) >= len(allItems) {
        return nil, &PageInfo{HasMore: false}, nil
    }
    end := offset + pageSize
    if int(end) > len(allItems) {
        end = int32(len(allItems))
    }
    pageItems := allItems[offset:end]

    // 6. 批量获取详情
    feedIDs := extractIDs(pageItems)
    feeds, err := s.feedRepo.BatchGetByIDs(ctx, feedIDs)
    if err != nil {
        return nil, nil, err
    }

    // 7. 填充作者信息
    userIDs := extractUserIDs(feeds)
    users, _ := s.userClient.BatchGetUsers(ctx, userIDs)
    result := s.fillAuthors(feeds, users)

    return result, &PageInfo{
        Page:     page,
        PageSize: pageSize,
        HasMore:  int(end) < len(allItems),
    }, nil
}
```

---

*本文档为分布式 Feed 流系统完整技术方案，涵盖架构设计、数据模型、接口定义、推拉结合策略、部署方案和开发规划。确认后即可开始编码实施。*
