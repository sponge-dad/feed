# Interaction 服务总体实现方案

> 本文档从方法论角度描述 Interaction 服务（端口 9005）的实现规划。
> 它不包含完整代码，只给出能力模块拆分、设计原则、接口规划和落地顺序，
> 供后续按模块逐步实现与 review。

---

## 1. 服务定位

Interaction 服务是**点赞、收藏、互动计数与互动状态**的轻量 KV 操作服务：

- **写**：点赞 / 取消点赞、收藏 / 取消收藏。
- **读**：帖子点赞数 / 收藏数、当前用户是否已点赞 / 收藏、用户点赞过的帖子列表、用户收藏过的帖子列表。
- **事件**：互动行为发生后向 RocketMQ 发送事件，驱动异步落库与消息通知。

它依赖：

| 下游 | 用途 |
|------|------|
| Redis | 用户互动关系、帖子互动计数的主存储（扛高频写） |
| MySQL | 互动记录的持久化与审计数据源 |
| RocketMQ | 发送 `interaction.event` 事件 |

**不依赖其他业务服务**：Interaction 只保存 `user_id ↔ feed_id` 的关系，不调用 User / Feed / Comment 服务，保持自身轻量。

---

## 2. 能力模块拆分

按「数据 → 写 → 读 → 一致性 → 事件 → 测试」的顺序，Interaction 服务拆为 8 个模块：

| 序号 | 模块 | 文档 | 核心问题 |
|------|------|------|---------|
| 1 | 数据模型 | [01-data-model/01-data-model.md](./01-data-model/01-data-model.md) | 点赞/收藏表如何支持幂等、审计与快速重建缓存？ |
| 2 | 点赞与取消点赞 | [02-like/02-like.md](./02-like/02-like.md) | 高频写如何保证「用户感知 Redis、MySQL 异步落库」？ |
| 3 | 收藏与取消收藏 | [03-collect/03-collect.md](./03-collect/03-collect.md) | 与点赞同构但独立存储，如何复用模式、隔离数据？ |
| 4 | 互动计数与状态查询 | [04-stats/04-stats.md](./04-stats/04-stats.md) | 帖子维度的计数和当前用户状态如何批量、低延迟返回？ |
| 5 | 用户互动列表 | [05-user-list/05-user-list.md](./05-user-list/05-user-list.md) | 「我的赞」「我的收藏」如何按时间倒序分页？ |
| 6 | 缓存一致性 | [06-cache/06-cache.md](./06-cache/06-cache.md) | Redis 先行 + 异步落库模式下，如何防止计数负数与脏读？ |
| 7 | MQ 事件与异步落库 | [07-mq-event/07-mq-event.md](./07-mq-event/07-mq-event.md) | 如何保证事件有序、幂等、失败可重试？ |
| 8 | 测试策略 | [08-test-strategy/08-test-strategy.md](./08-test-strategy/08-test-strategy.md) | 单元/集成/并发/压测分别覆盖什么？ |

---

## 3. gRPC 接口规划方法论

接口设计遵循以下原则：

1. **一接口一职责**：Like、Unlike、Collect、Uncollect、GetStats、BatchGetStats、GetStatus、BatchGetStatus、ListUserLikes、ListUserCollects 各自独立。
2. **动作类接口返回最小信息**：点赞成功后只返回成功 / 重复点赞幂等成功，不返回最新计数（由调用方主动查询）。
3. **批量接口只返回必要字段**：`BatchGetFeedStats` 返回每个 `feed_id` 的 `like_count` / `collect_count`；`BatchGetUserStatus` 返回每个 `feed_id` 的 `is_liked` / `is_collected`。
4. **时间戳统一使用毫秒级 Unix 时间戳**：与项目其他服务对齐，`created_at` 字段均用 `int64`。
5. **分页优先 Cursor**：用户互动列表（我的赞 / 我的收藏）按时间倒序，游标由 `score + member` 组成，避免新增/取消导致偏移错位。

建议的接口矩阵（在 `api/proto/interaction/interaction.proto` 中落地）：

| 接口 | 类型 | 说明 |
|------|------|------|
| LikeFeed | Unary | 点赞帖子 |
| UnlikeFeed | Unary | 取消点赞 |
| CollectFeed | Unary | 收藏帖子 |
| UncollectFeed | Unary | 取消收藏 |
| GetFeedStats | Unary | 单条帖子互动计数 |
| BatchGetFeedStats | Unary | 批量帖子互动计数 |
| GetUserInteractionStatus | Unary | 当前用户对单条帖子的点赞/收藏状态 |
| BatchGetUserInteractionStatus | Unary | 批量查询当前用户对多条帖子的状态 |
| GetUserLikedFeeds | Unary | 用户点赞过的帖子列表，Cursor 分页 |
| GetUserCollectedFeeds | Unary | 用户收藏过的帖子列表，Cursor 分页 |

---

## 4. 落地顺序建议

```
Week 1: 数据模型 + 点赞/取消点赞 + 收藏/取消收藏
Week 2: 互动计数与状态查询 + 用户互动列表
Week 3: 缓存一致性收尾 + MQ 事件 + 异步落库
Week 4: 单元/集成/并发/压测补齐
```

**为什么是这个顺序？**

- 先让「点赞、收藏、查计数」能跑通，形成最小可用闭环。
- 计数与状态查询是 Feed/Comment 上游最常调用的能力，需尽早稳定。
- 用户互动列表相对独立，放在第二阶段集中实现。
- 缓存一致性、异步落库、消息有序性最复杂，放在第三阶段攻坚。
- 测试贯穿全程，压测放在最后，等有完整链路后再做。

---

## 5. 关键非功能要求

| 维度 | 要求 |
|------|------|
| 可用性 | 单条 Redis key 失效或单个 MQ 消息延迟不阻塞其他帖子的互动读写 |
| 性能 | 点赞/收藏 P99 < 20ms（纯 Redis 路径）；批量查状态 P99 < 30ms |
| 一致性 | Redis 是用户可见状态的唯一数据源；MySQL 是持久化与审计数据源，允许秒级不一致 |
| 扩展性 | 点赞、收藏、计数、列表独立实现，未来新增「转发」「踩」等互动类型不侵入现有代码 |
| 安全 | 禁止替他人点赞/收藏；用户 ID 从 RPC metadata / JWT 中提取；所有 DB 操作参数化 |

---

## 6. 与现有文档的引用关系

- 整体架构：`docs/design/architecture.md`
- 服务拆分与端口：`docs/design/service-design.md`
- 全局数据模型与 Redis 约定：`docs/design/data-model.md`
- 编码规范：`docs/agent/dev-guidelines.md`
- Proto 规范：`docs/agent/proto-writing-guide.md`
- 错误码：`common/errorx/errorx.go`
