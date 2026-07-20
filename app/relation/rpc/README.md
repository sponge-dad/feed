# Relation RPC 服务

## 服务信息

| 项目 | 说明 |
|---|---|
| 服务名 | relation.rpc |
| 监听端口 | 9002 |
| 协议 | gRPC + Protobuf |
| 服务发现 | etcd（`127.0.0.1:2479`，容器内部 2379，已避开 K8s 自带 etcd 的 2379） |
| 数据库 | MySQL `feed_relation` 库 |
| 缓存 | Redis 7 |

## 目录结构与职责

| 目录/文件 | 职责 |
|---|---|
| `api/proto/relation/relation.proto` | gRPC 接口契约定义 |
| `deploy/sql/relation.sql` | relations 表建表脚本 |
| `app/relation/model/` | goctl 生成的 model 层 + 自定义分页查询 |
| `app/relation/rpc/relation.go` | 服务启动入口，含 Snowflake 初始化 |
| `app/relation/rpc/internal/config/` | 配置结构体（Mysql / CacheRedis / Vip） |
| `app/relation/rpc/internal/svc/` | ServiceContext 依赖注入容器 |
| `app/relation/rpc/internal/logic/` | 6 个接口的业务逻辑实现 |
| `app/relation/rpc/etc/relation.yaml` | 服务运行时配置 |

## 接口列表

| 接口 | 请求 | 说明 |
|---|---|---|
| `Follow` | `follower_id` + `followee_id` | 关注，幂等设计 |
| `Unfollow` | `follower_id` + `followee_id` | 取消关注，未关注也返回成功 |
| `GetFollows` | `user_id` + `page` + `page_size` | 关注列表，Redis ZSet 缓存 |
| `GetFans` | `user_id` + `page` + `page_size` | 粉丝列表，Redis ZSet 缓存 |
| `IsFollow` | `follower_id` + `followee_ids[]` | 批量判断是否关注 |
| `IsVip` | `user_id` | 粉丝数是否达到大V阈值 |

## 数据模型

### relations 表

```sql
CREATE TABLE IF NOT EXISTS `relations` (
    `id`           BIGINT UNSIGNED NOT NULL COMMENT 'Snowflake ID',
    `follower_id`  BIGINT UNSIGNED NOT NULL COMMENT '关注者用户ID',
    `followee_id`  BIGINT UNSIGNED NOT NULL COMMENT '被关注者用户ID',
    `created_at`   BIGINT          NOT NULL COMMENT '关注时间（Unix秒）',
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_follow` (`follower_id`, `followee_id`),
    KEY `idx_follower_id` (`follower_id`, `created_at`),
    KEY `idx_followee_id` (`followee_id`, `created_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='关注关系表';
```

### Redis 缓存设计

| Key | 类型 | 说明 |
|---|---|---|
| `user:follow:{user_id}` | ZSet | 关注列表，score = created_at |
| `user:fans:{user_id}` | ZSet | 粉丝列表，score = created_at |
| `user:fans_count:{user_id}` | String | 粉丝数量缓存 |
| `user:vip_users` | Set | 大V用户集合 |

## 关键实现要点

### 1. 关注/取消关注的幂等设计

- `Follow`：唯一索引 `uk_follow` 保证不重复；先查后插，已存在直接返回成功。
- `Unfollow`：未关注时返回成功，不报错。

### 2. 自己关注自己防御

`follower_id == followee_id` 时直接返回 `RelationSelf` 错误码。

### 3. 列表缓存用 ZSet

关注/粉丝列表天然按时间排序，Redis 的 `ZSet` 同时支持分页（`ZREVRANGE`）和按时间倒序，比纯 List 更合适。

### 4. 大V判定

- 关注成功后，`followee_id` 的粉丝数 +1，超过阈值时加入 `user:vip_users`。
- `IsVip` 优先读 Set，未命中时查 `fans_count` 缓存，再未命中则回源 DB 重建。

### 5. ServiceContext 依赖注入

`ServiceContext` 中挂载了 `RelationModel` / `Redis` / `IdGen`（即 `idgen.Next` 函数引用），logic 层统一通过 `l.svcCtx` 访问。

## 错误码

| 错误码 | 含义 |
|---|---|
| 11001 | 不能关注自己（RelationSelf） |
| 11002 | 已关注该用户（RelationAlreadyFollow） |
| 11003 | 未关注该用户（RelationNotFollow） |
| 11004 | 目标用户不存在（RelationTargetNotFound） |

## 本地运行

```bash
cd app/relation/rpc
go run relation.go -f etc/relation.yaml
```

启动后可用 grpcurl 测试：

```bash
grpcurl -plaintext \
  -proto api/proto/relation/relation.proto \
  -import-path . \
  -d '{"follower_id":1,"followee_id":2}' \
  127.0.0.1:9002 \
  relation.Relation/Follow
```

## 已知设计取舍 / 待办

- `IsFollow` 目前用循环单查，适合粉丝/关注数较少的场景；若需要高频批量判断，未来可改为 `IN` 查询 + 批量缓存。
- `IsVip` 重建粉丝数时只查第一页 1000 条，粉丝数极大的用户未来需要专门优化。
- 关注/取消关注后未接入 RocketMQ 事件，后续 Feed 服务需要监听这些事件来更新"关注流"。
