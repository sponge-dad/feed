# 数据模型设计

> 本文档定义 Feed 流系统所有服务的 MySQL 表结构和 Redis 数据结构。
>
> 通用约定：
> - 主键 `id` 使用 Snowflake 生成的分布式 ID（BIGINT UNSIGNED）
> - 所有表 `ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`
> - 删除统一用软删除（`status` 字段），不物理删除
> - 时间字段用 `DATETIME`，缓存中的时间戳用毫秒级 unix timestamp

---

## 1. User Service

### 1.1 MySQL: `users`

```sql
CREATE TABLE users (
    id           BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    username     VARCHAR(64)     NOT NULL DEFAULT '',
    password     VARCHAR(256)    NOT NULL DEFAULT '',   -- bcrypt hash
    nickname     VARCHAR(64)     NOT NULL DEFAULT '',
    avatar       VARCHAR(512)    NOT NULL DEFAULT '',   -- COS URL
    email        VARCHAR(128)    NOT NULL DEFAULT '',
    phone        VARCHAR(20)     NOT NULL DEFAULT '',
    bio          VARCHAR(512)    NOT NULL DEFAULT '',
    city_code    VARCHAR(16)     NOT NULL DEFAULT '',   -- 城市编码, 如 "440300"=深圳
    city_name    VARCHAR(64)     NOT NULL DEFAULT '',   -- 城市名, 如 "深圳"
    status       TINYINT         NOT NULL DEFAULT 1,    -- 1:正常 2:禁用
    created_at   DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at   DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    PRIMARY KEY (id),
    UNIQUE KEY uk_username (username),
    UNIQUE KEY uk_phone (phone),
    KEY idx_city (city_code),
    KEY idx_created_at (created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
```

设计说明：
- **不分表**：10w 用户单表完全够，百万级再考虑
- **登录方式**：当前只支持用户名 + 密码，手机号为选填
- **城市**：注册时通过 IP 自动定位写入，用户可手动修改

### 1.2 Redis

| Key | 类型 | 说明 | TTL |
|-----|------|------|-----|
| `user:{id}` | Hash | 用户基本信息缓存 | 1小时 |
| `user:token:blacklist` | Set | JWT 黑名单（登出/封号） | 跟 token 过期一致 |
| `user:city:{city_code}` | Set | 同城用户ID集合（供 Feed 同城流用） | 永久 |

---

## 2. Relation Service

### 2.1 MySQL: `relations`（单向表）

```sql
CREATE TABLE relations (
    id            BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    follower_id   BIGINT UNSIGNED NOT NULL,           -- 关注者ID
    following_id  BIGINT UNSIGNED NOT NULL,           -- 被关注者ID
    status        TINYINT         NOT NULL DEFAULT 1,  -- 1:正常 2:已取关（软删除）
    created_at    DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at    DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    PRIMARY KEY (id),
    UNIQUE KEY uk_follower_following (follower_id, following_id),
    KEY idx_following (following_id),
    KEY idx_follower (follower_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
```

设计说明：
- **单向表**：一条记录表示 `follower_id` 关注了 `following_id`
  - 查"我关注了谁"：`WHERE follower_id = ?` 走 `idx_follower`
  - 查"谁关注了我"：`WHERE following_id = ?` 走 `idx_following`
- **软删除**：取关时 `status=2`，保留数据便于回溯分析
- **大V判定**：关注/取关时更新 `follow_count`，`follower_count > 100000` 加入 `vip:list`，不要求实时精确，1小时过期后重新计算

### 2.2 Redis

| Key | 类型 | 说明 | score | TTL |
|-----|------|------|-------|-----|
| `following:{user_id}` | ZSet | 关注列表, member=被关注者ID | 关注时间戳 | 永久 |
| `follower:{user_id}` | ZSet | 粉丝列表, member=粉丝ID | 关注时间戳 | 永久 |
| `follow_count:{user_id}` | Hash | `{following_count, follower_count}` | - | 1小时 |
| `vip:list` | Set | 大V用户ID集合（粉丝数 > 10w） | - | 永久 |

### 2.3 性能与一致性说明

- **查列表不慢**：无论关注/粉丝多少，用户只看前几页，ZSet `ZREVRANGE 0 19` 秒出
- **真正瓶颈在发帖推送**：普通用户发帖需把帖子写入所有粉丝 inbox
  - 解决：分批（每批500）拉粉丝 + Pipeline 批量写 + MQ 异步，不阻塞发帖返回
- **一致性（Cache-Aside）**：Redis 缓存丢失时从 MySQL 重建

```
关注操作流程：
  1. MySQL INSERT (status=1)
  2. Redis ZADD following:{follower_id}
  3. Redis ZADD follower:{following_id}
  4. Redis HINCRBY follow_count:{following_id} follower_count 1
  5. 发送 MQ (relation.created)
  6. 检查 follower_count > 100000 → 加入 vip:list

取关操作流程：
  1. MySQL UPDATE status=2
  2. Redis ZREM following / follower
  3. Redis HINCRBY follow_count -1
  4. 发送 MQ (relation.deleted)
```

---

## 3. Feed Service

### 3.1 MySQL: `feeds`

```sql
CREATE TABLE feeds (
    id            BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    user_id       BIGINT UNSIGNED NOT NULL,               -- 作者ID
    feed_type     TINYINT         NOT NULL DEFAULT 1,      -- 1:图文 2:视频
    title         VARCHAR(256)    NOT NULL DEFAULT '',      -- 标题/主题
    description   TEXT            NOT NULL,                 -- 正文/描述
    media_urls    JSON            DEFAULT NULL,             -- 媒体资源 ["url1","url2"]
    cover_url     VARCHAR(512)    NOT NULL DEFAULT '',      -- 视频封面图
    city_code     VARCHAR(16)     NOT NULL DEFAULT '',      -- 发布时IP城市编码
    city_name     VARCHAR(64)     NOT NULL DEFAULT '',      -- 发布时IP城市名
    ip_location   VARCHAR(64)     NOT NULL DEFAULT '',      -- IP属地, 如"广东"
    status        TINYINT         NOT NULL DEFAULT 1,       -- 1:正常 2:已删除 3:审核中
    is_vip_feed   TINYINT         NOT NULL DEFAULT 0,       -- 0:普通 1:大V发帖
    like_count    INT UNSIGNED    NOT NULL DEFAULT 0,
    comment_count INT UNSIGNED    NOT NULL DEFAULT 0,
    collect_count INT UNSIGNED    NOT NULL DEFAULT 0,
    created_at    DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at    DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    PRIMARY KEY (id),
    KEY idx_user_created (user_id, created_at),
    KEY idx_city_created (city_code, created_at),
    KEY idx_status_created (status, created_at),
    KEY idx_created_at (created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
```

### 3.2 Redis — 三种流的数据结构

| Key | 类型 | 说明 | score | 容量 | TTL |
|-----|------|------|-------|------|-----|
| `inbox:{user_id}` | ZSet | 收件箱（关注流，推模式写入） | 发帖时间戳 | 1000条 | 7天 |
| `outbox:{user_id}` | ZSet | 发件箱（自己发的帖子） | 发帖时间戳 | 大V 2000条 | 7天 |
| `feed:recommend` | ZSet | 推荐流候选池（全局） | random × 时间衰减 | 10万条 | 30天 |
| `feed:city:{city_code}` | ZSet | 同城流候选池（每城市一个） | 发帖时间戳 | 2万条/城市 | 7天 |
| `feed:{feed_id}` | Hash | 帖子详情缓存 | - | - | 30天 |
| `timeline:{user_id}:{tab}` | String | 前2页JSON缓存 | - | - | 60秒 |

### 3.3 三种 Feed 流策略

| 流类型 | 数据来源 | 排序策略 | 分页方式 | 缓存 |
|--------|---------|---------|---------|------|
| **推荐**（默认） | `feed:recommend` 全局池 | random × 时间衰减 | Offset | 前2页 5min |
| **关注** | inbox + 关注的大V的 outbox | 纯时间倒序 | Cursor（防重复） | 前2页 60s |
| **同城** | `feed:city:{city_code}` | 时间倒序 | Offset | 前2页 1min |

### 3.4 推荐流 score 计算

```
score = rand(0, 1) × time_decay_factor
time_decay_factor = 1 / (1 + hours_since_created / 24)

效果：
  - 新帖子：time_decay ≈ 1，score ≈ 随机值，有机会靠前
  - 旧帖子：time_decay → 0，逐渐沉底
  - 随机打散：相同时段的帖子随机排列，避免单调
```

### 3.5 推拉结合策略

```
普通用户发帖（推模式/写扩散）：
  Feed写MySQL → MQ → Worker → 分批写入所有粉丝 inbox ZSet

大V发帖（拉模式/读扩散，粉丝 > 10w）：
  Feed写MySQL → MQ → Worker → 仅写自己 outbox ZSet（不推送粉丝）

关注流拉取：
  inbox数据 + 关注的大V outbox数据 → 合并按时间排序 → 分页

推荐流 / 同城流：
  统一读扩散（全局池/城市池），不推送
  原因：受众是全平台/全城市，无法逐个推送
```

| 参数 | 默认值 | 说明 |
|------|--------|------|
| VIP_FOLLOWER_THRESHOLD | 100,000 | 粉丝数超此阈值判定为大V |
| PUSH_FAN_BATCH_SIZE | 500 | 推模式每批推送粉丝数 |
| INBOX_MAX_SIZE | 1000 | 普通用户收件箱最大容量 |
| OUTBOX_VIP_MAX_SIZE | 2000 | 大V发件箱最大容量 |

---

## 4. Comment Service

### 4.1 MySQL: `comments`（楼中楼）

```sql
CREATE TABLE comments (
    id            BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    feed_id       BIGINT UNSIGNED NOT NULL,              -- 所属帖子
    user_id       BIGINT UNSIGNED NOT NULL,              -- 评论者
    content       VARCHAR(1000)   NOT NULL DEFAULT '',    -- 评论内容
    root_id       BIGINT UNSIGNED NOT NULL DEFAULT 0,     -- 根评论ID（一级评论=0）
    parent_id     BIGINT UNSIGNED NOT NULL DEFAULT 0,     -- 父评论ID（直接回复对象）
    reply_user_id BIGINT UNSIGNED NOT NULL DEFAULT 0,     -- 被回复者ID（@谁）
    like_count    INT UNSIGNED    NOT NULL DEFAULT 0,
    reply_count   INT UNSIGNED    NOT NULL DEFAULT 0,      -- 子回复数（仅根评论维护）
    status        TINYINT         NOT NULL DEFAULT 1,      -- 1:正常 2:已删除
    created_at    DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (id),
    KEY idx_feed_root (feed_id, root_id, created_at),
    KEY idx_root (root_id, created_at),
    KEY idx_user (user_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
```

### 4.2 关键设计：root_id + parent_id 双字段

楼中楼视觉上无限嵌套，**存储上是两层**：

```
展示（视觉无限嵌套）              存储（实际两层平铺）
评论A                            id=1  root_id=0  parent_id=0
  ├─ B回复A                      id=2  root_id=1  parent_id=1  reply_user=A
  ├─ C回复B                      id=3  root_id=1  parent_id=2  reply_user=B
  └─ D回复C                      id=4  root_id=1  parent_id=3  reply_user=C
评论E                            id=5  root_id=0  parent_id=0
  └─ F回复E                      id=6  root_id=5  parent_id=5  reply_user=E
```

| 字段 | 作用 |
|------|------|
| `root_id` | 指向最顶层评论。同楼所有回复 root_id 相同 |
| `parent_id` | 指向直接回复的评论。用于"回复@某人" |
| `reply_user_id` | 被回复者，前端显示"张三 回复 李四：xxx" |

**为什么不用真正的递归树**：查询简单（一条 SQL 查全楼）、分页容易、避免递归查询的性能问题。

### 4.3 查询流程

```sql
-- 1. 查帖子一级评论（分页）
SELECT * FROM comments WHERE feed_id=? AND root_id=0 AND status=1
ORDER BY created_at DESC LIMIT 20;

-- 2. 每条一级评论查前3条子回复（预览）
SELECT * FROM comments WHERE root_id=? AND status=1
ORDER BY created_at ASC LIMIT 3;

-- 3. "查看更多回复"分页查全楼
SELECT * FROM comments WHERE root_id=? AND status=1
ORDER BY created_at ASC LIMIT 20 OFFSET ?;
```

### 4.4 Redis

| Key | 类型 | 说明 | TTL |
|-----|------|------|-----|
| `comment_count:{feed_id}` | String | 帖子评论总数 | 1小时 |
| `comment_hot:{feed_id}` | ZSet | 热门评论（按点赞排序），前N条 | 5分钟 |

---

## 5. Interaction Service

### 5.1 MySQL: `likes` / `collections`

```sql
-- 点赞表
CREATE TABLE likes (
    id          BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    user_id     BIGINT UNSIGNED NOT NULL,
    feed_id     BIGINT UNSIGNED NOT NULL,
    status      TINYINT         NOT NULL DEFAULT 1,  -- 1:已点赞 2:已取消
    created_at  DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at  DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    PRIMARY KEY (id),
    UNIQUE KEY uk_user_feed (user_id, feed_id),
    KEY idx_feed (feed_id),
    KEY idx_user_created (user_id, created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- 收藏表（结构同点赞）
CREATE TABLE collections (
    id          BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    user_id     BIGINT UNSIGNED NOT NULL,
    feed_id     BIGINT UNSIGNED NOT NULL,
    status      TINYINT         NOT NULL DEFAULT 1,
    created_at  DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at  DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    PRIMARY KEY (id),
    UNIQUE KEY uk_user_feed (user_id, feed_id),
    KEY idx_feed (feed_id),
    KEY idx_user_created (user_id, created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
```

### 5.2 Redis — 读写分离

| Key | 类型 | 说明 | TTL |
|-----|------|------|-----|
| `like:feed:{feed_id}` | Set | 点赞了该帖的用户ID集合 | 7天 |
| `collect:feed:{feed_id}` | Set | 收藏了该帖的用户ID集合 | 7天 |
| `user:likes:{user_id}` | ZSet | 用户点赞过的帖子（"我的赞"） | 30天 |
| `user:collects:{user_id}` | ZSet | 用户收藏过的帖子（"我的收藏"） | 30天 |
| `feed:stats:{feed_id}` | Hash | `{like_count, collect_count, comment_count}` | 1小时 |

### 5.3 高频写削峰流程

```
用户点赞：
  1. Redis SADD like:feed:{feed_id} {user_id}       ← 立即生效
  2. Redis ZADD user:likes:{user_id} {now} {feed_id}
  3. Redis HINCRBY feed:stats:{feed_id} like_count 1
  4. 立即返回成功（毫秒级）
  5. 发送 MQ (interaction.event)
       ├─ Consumer 1 → 异步写 MySQL likes 表（持久化）
       └─ Consumer 2 → 发通知给帖子作者

原则：用户感知 Redis 操作，MySQL 落库异步。点赞是超高频操作，
      直接写 MySQL 会打爆数据库，用 Redis 扛 + MQ 异步落库。
```

### 5.4 计数一致性

```
- 平时读 Redis 的 feed:stats（快）
- 定时任务（每天凌晨）从 MySQL COUNT 校准 Redis
- Redis 缓存失效时从 MySQL COUNT 重建
```

---

## 附录：全局 ID 生成

所有表主键使用 **Snowflake** 算法生成：

```
64 bit = 1(符号) + 41(时间戳) + 10(机器ID) + 12(序列号)

- 全局唯一
- 趋势递增（对 MySQL 索引友好）
- 每毫秒每节点可生成 4096 个 ID
- 机器ID = K8s Pod 序号 或 配置分配
```
