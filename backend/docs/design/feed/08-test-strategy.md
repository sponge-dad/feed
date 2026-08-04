# Feed 服务测试策略方法论

> 本文档描述 Feed 服务各模块的测试策略，包括单元测试、集成测试、并发测试和压测。
> 遵循项目统一测试规范，但针对 Feed 的多流、异步、缓存特点做细化。

---

## 1. 测试分层

| 类型 | 位置 | 工具 | 覆盖目标 |
|------|------|------|---------|
| 单元测试 | `app/feed/rpc/internal/logic/*_test.go` | miniredis + model stub | 纯业务逻辑，不依赖真实存储 |
| 集成测试 | `app/feed/rpc/tests/*_test.go` | 真实 MySQL/Redis/Relation Stub | 端到端调用，验证 DB + Redis + MQ 协作 |
| 并发测试 | `app/feed/rpc/tests/*_test.go` | goroutine + sync.WaitGroup | 幂等、竞态、缓存一致性 |
| 压测 | `scripts/benchmark-feed.sh` | ghz | 各接口基准 RPS/延迟 |

---

## 2. 单元测试策略

### 2.1 需要单测的 logic

- `createFeedLogic`
- `deleteFeedLogic`
- `getFeedLogic`
- `batchGetFeedsLogic`
- `getRecommendTimelineLogic`
- `getFollowTimelineLogic`
- `getCityTimelineLogic`
- `getUserFeedsLogic`

### 2.2 单测方法

- Redis 用 `miniredis` 模拟。
- MySQL model 用 go-sqlmock 或手写 stub。
- RPC 下游（User/Relation/Interaction）用 mock client。
- 断言重点：
  - 参数校验分支（空值、越界、类型错误）。
  - 正常路径返回的字段。
  - 异常路径返回的错误码（使用 `common/errorx`）。

### 2.3 典型单测用例

**CreateFeed**

| 用例 | 输入 | 期望 |
|------|------|------|
| 图文正常发布 | feed_type=1, media_urls=["url1"] | 返回 FeedInfo，DB 有记录 |
| 视频缺少封面 | feed_type=2, cover_url="" | 返回参数错误码 |
| 图文无媒体 | feed_type=1, media_urls=[] | 返回参数错误码 |

**GetFollowTimeline**

| 用例 | 输入 | 期望 |
|------|------|------|
| 无关注 | user_id=1 | 返回空列表 |
| 有普通关注者 | inbox 有数据 | 按时间倒序返回 |
| 有大V关注者 | inbox + outbox 合并 | 合并后按时间倒序返回 |

---

## 3. 集成测试策略

### 3.1 测试环境

- MySQL 库：`feed_feed_test`
- Redis DB：独立 key 前缀或独立 DB index
- 配置文件：`app/feed/rpc/etc/feed-test.yaml`
- RocketMQ：测试环境可mock，或启动本地 nameserver/broker

### 3.2 TestMain 启动

- 启动 Feed RPC 服务。
- 启动 miniredis 或连接 Docker Redis。
- 初始化测试库表结构。
- 结束后清理数据。

### 3.3 集成测试用例

| 编号 | 场景 | 验证点 |
|------|------|--------|
| I-001 | 发帖后查详情 | DB 有记录、Redis 有缓存、返回字段正确 |
| I-002 | 删帖后查详情 | status=2、缓存删除、详情接口返回已删除错误 |
| I-003 | 推荐流读取 | 推荐池有数据、分页正确 |
| I-004 | 同城流读取 | 按城市过滤、分页正确 |
| I-005 | 关注流读取（普通用户） | 发帖后粉丝 inbox 有数据（Worker 消费后） |
| I-006 | 关注流读取（大V） | 大V发帖不推 inbox，粉丝读取时从 outbox 拉取 |
| I-007 | 批量获取帖子 | 部分命中缓存、部分回源 |

---

## 4. 并发测试策略

### 4.1 关注点

- **并发发帖**：同一用户高并发发帖，ID 不重复，记录不丢。
- **并发删帖**：同一帖子被并发删除，幂等返回成功。
- **并发推送**：Worker 并发消费同一 feed.created，inbox 数据不重复。
- **缓存一致性**：删帖后缓存立即失效或短时间收敛。

### 4.2 典型并发用例

| 编号 | 场景 | 验证点 |
|------|------|--------|
| C-001 | 100  goroutine 同时发帖 | DB 记录数 = 100，feed_id 唯一 |
| C-002 | 删帖后立即读详情 | 最终返回已删除，缓存不返回旧数据 |
| C-003 | 并发关注后发帖 | 粉丝 inbox 最终包含该帖子 |
| C-004 | Worker 并发消费同一 feed.created | inbox 中帖子不重复 |

---

## 5. 压测策略

### 5.1 压测接口

| 接口 | 关注点 |
|------|--------|
| PublishFeed | 写吞吐、MySQL 压力、MQ 发送延迟 |
| GetFeed | 缓存命中率、单条读延迟 |
| BatchGetFeeds | Pipeline 批量读效率 |
| GetRecommendTimeline | 推荐流分页性能 |
| GetFollowTimeline | 合并 inbox + outbox 性能 |
| GetCityTimeline | 同城流分页性能 |

### 5.2 压测数据准备

- 预置 1000 个用户、10 万条帖子。
- 大V 用户 1 个，粉丝 1 万。
- 普通用户发帖测试推模式。
- 大V发帖测试拉模式。

### 5.3 压测工具

- ghz：gRPC 压测。
- 压测脚本：`scripts/benchmark-feed.sh`。

---

## 6. 测试环境隔离

- 所有测试数据独立库、独立 Redis key 前缀。
- 测试结束后清理：

```bash
mysql -h127.0.0.1 -uroot -proot -e "DROP DATABASE IF EXISTS feed_feed_test; CREATE DATABASE feed_feed_test;"
docker exec feed-redis redis-cli -a mUd0ZLc312DPJ4Acaf4PnIoF --no-auth-warning EVAL \
  "local keys = redis.call('keys', ARGV[1]); for i=1,#keys do redis.call('del', keys[i]); end; return #keys" \
  0 'feed:*' 'inbox:*' 'outbox:*' 'timeline:*'
```

---

## 7. 与项目测试规范的对应

- 单元/集成/并发测试位置：`docs/agent/dev-guidelines.md` 第 5 节。
- 压测脚本示例：`scripts/benchmark-relation.sh`。
- 错误码使用：`common/errorx/errorx.go`。
