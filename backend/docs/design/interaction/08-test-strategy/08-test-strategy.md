# Interaction 服务测试策略

> 本文档描述 Interaction 服务各层测试的覆盖范围、测试环境和关键用例。

---

## 1. 测试分层

| 类型 | 位置 | 说明 |
|---|---|---|
| 单元测试 | `app/interaction/rpc/internal/logic/*_test.go` | 用 `miniredis` + model stub，不依赖真实存储 |
| 集成测试 | `app/interaction/rpc/tests/*_test.go` | 启动真实服务 + MySQL/Redis，验证端到端 |
| 并发/缓存测试 | `app/interaction/rpc/tests/*_test.go` | 多 goroutine 点赞/取消、缓存一致性 |
| 压力测试 | `scripts/benchmark-interaction.sh` | 使用 `ghz` 压测 LikeFeed / GetFeedStats |

---

## 2. 测试环境隔离

- 集成测试使用独立配置文件：`etc/interaction-test.yaml`
- MySQL 使用独立测试库：`feed_interaction_test`
- Redis 使用独立 DB（如 `select 5`）或不同 key 前缀
- MQ 使用独立 consumer group 或测试 topic，避免污染线上/开发数据

---

## 3. 单元测试重点

### 3.1 LikeFeed / UnlikeFeed

| 用例 | 预期 |
|------|------|
| 首次点赞 | Redis Set 新增成员，计数 +1，发送 MQ |
| 重复点赞 | 幂等返回成功，计数不变 |
| 取消已点赞 | Set 移除成员，计数 -1 |
| 重复取消 | 幂等返回成功，计数不变 |
| 计数已为 0 时取消 | 不执行 HINCRBY -1，计数保持 0 |

### 3.2 GetFeedStats / BatchGetFeedStats

| 用例 | 预期 |
|------|------|
| 缓存命中 | 直接返回 Redis 值 |
| 缓存未命中 | 回源 MySQL，回写缓存 |
| 计数为 0 | 缓存空值，避免反复回源 |
| 批量查询部分命中 | 命中部分直接返回，未命中部分批量回源 |

### 3.3 GetUserInteractionStatus

| 用例 | 预期 |
|------|------|
| 已点赞 | 返回 is_liked = true |
| 未点赞且 MySQL 无记录 | 返回 false，不写入 Redis |
| 已取消但 Redis Set 未移除 | 以 Redis 为准？否，需设计为取消时即时移除，测试验证 |

---

## 4. 集成测试重点

### 4.1 端到端主链路

```text
1. 调用 LikeFeed
2. 调用 GetUserInteractionStatus → 期望 is_liked = true
3. 调用 GetFeedStats → 期望 like_count = 1
4. 调用 UnlikeFeed
5. 调用 GetFeedStats → 期望 like_count = 0
6. 等待 MQ 消费后查 MySQL → 期望 status = 2
```

### 4.2 列表分页

```text
1. 用户点赞 5 条帖子
2. 调用 GetUserLikedFeeds 每页 2 条
3. 验证页数、游标、顺序
4. 取消其中 1 条点赞
5. 再次调用列表，验证该 feed_id 已消失
```

---

## 5. 并发测试重点

### 5.1 同一用户并发点赞/取消

- 启动 N 个 goroutine，对同一 `user_id + feed_id` 交替调用 LikeFeed / UnlikeFeed。
- 最终状态只能是「点赞」或「未点赞」之一。
- `feed:stats:like_count` 只能是 0 或 1，不能为负或大于 1。

### 5.2 多用户并发点赞同一帖子

- 启动 N 个 goroutine，每个代表不同用户点赞同一帖子。
- 最终 `like_count` 应等于成功点赞的用户数。
- MySQL 落库后 `COUNT` 应与 Redis 一致（允许 MQ 消费完成后的短暂窗口）。

### 5.3 缓存重建与写并发

- 删除 `feed:stats:{feed_id}` 同时发起大量点赞。
- 验证计数最终收敛到正确值。

---

## 6. 压测重点

### 6.1 压测目标

| 接口 | 目标 QPS | P99 RT |
|------|---------|--------|
| LikeFeed | > 5k | < 20ms |
| GetFeedStats | > 10k | < 10ms |
| BatchGetUserInteractionStatus(20) | > 5k | < 30ms |

### 6.2 压测脚本

- 使用 `ghz` 对 gRPC 接口进行压测。
- 脚本位置：`scripts/benchmark-interaction.sh`
- 压测数据准备：预先写入一批 feed_id 和用户，避免实时注册产生噪音。

---

## 7. 提交前检查

- `go test -race ./app/interaction/rpc/...` 通过
- `go build ./...` 通过
- `gofmt -w .` 已执行
- 新增逻辑配套单元测试，修复 bug 配套回归测试
