# Feed 服务设计方案总览

> 本文档从方法论角度描述 Feed 服务（端口 9003）的实现规划。
> 它不包含完整代码，而是给出每个能力模块的拆分方式、设计原则和落地顺序，
> 供后续按模块逐步实现与review。

---

## 1. 服务定位

Feed 服务是「内容发布 + 三种 Feed 流聚合」的核心服务：

- **写**：发帖、删帖、内容管理。
- **读**：推荐流、关注流、同城流、帖子详情、个人主页列表。
- **事件**：发帖/删帖后向 RocketMQ 发送事件，驱动异步推送与缓存清理。

它依赖：

| 下游 | 用途 |
|------|------|
| User Service（9001） | 获取作者基础信息、城市信息 |
| Relation Service（9002） | 获取关注列表、粉丝列表、大V判定 |
| Interaction Service（9005） | 获取点赞/收藏数、当前用户是否已互动 |
| RocketMQ | 发送 feed.created / feed.deleted 事件 |

---

## 2. 能力模块拆分

按「数据 → 写 → 读 → 事件 → 测试」的顺序，Feed 服务拆为 8 个模块：

| 序号 | 模块 | 文档 | 核心问题 |
|------|------|------|---------|
| 1 | 数据模型 | [01-data-model.md](./01-data-model.md) | 帖子表如何支持图文/视频、软删除、城市索引？ |
| 2 | 帖子发布 | [02-post-publish.md](./02-post-publish.md) | 如何校验参数、生成 ID、写入 DB、触发事件？ |
| 3 | 关注流 | [03-timeline-follow.md](./03-timeline-follow.md) | 推拉结合下如何合并 inbox + 大V outbox？ |
| 4 | 推荐流 | [04-timeline-recommend.md](./04-timeline-recommend.md) | 全局池如何随机打散 + 时间衰减？ |
| 5 | 同城流 | [05-timeline-city.md](./05-timeline-city.md) | 按城市聚合与分页的最佳实践？ |
| 6 | 缓存策略 | [06-cache-strategy.md](./06-cache-strategy.md) | Cache-Aside 在 Feed 场景如何落地？ |
| 7 | MQ 事件与 Worker | [07-mq-event.md](./07-mq-event.md) | 发帖后如何异步推送到粉丝 inbox？ |
| 8 | 测试策略 | [08-test-strategy.md](./08-test-strategy.md) | 单元/集成/并发/压测分别覆盖什么？ |

---

## 3. gRPC 接口规划方法论

接口设计遵循以下原则：

1. **一接口一职责**：Publish、Delete、GetFeed、BatchGetFeeds、GetTimeline 各自独立。
2. **读接口按流类型拆分**：不要一个接口通过 `type` 参数区分推荐/关注/同城，未来扩展困难。
3. **批量接口只返回必要字段**：BatchGetFeeds 返回帖子基础信息，作者/互动计数由调用方二次聚合。
4. **时间戳统一使用秒级 Unix 时间戳**：与 Relation 服务对齐，ZSet score 和 proto 字段均用 `int64 created_at`。
5. **分页优先 Cursor，必要时 Offset**：
   - 关注流用 Cursor（score + id 组成游标），避免新增/删除导致偏移错位。
   - 推荐/同城可用 Offset，因为候选池相对稳定。

建议的接口矩阵（在 `api/proto/feed/feed.proto` 中落地）：

| 接口 | 类型 | 说明 |
|------|------|------|
| PublishFeed | Unary | 发帖 |
| DeleteFeed | Unary | 删帖（软删除） |
| GetFeed | Unary | 帖子详情 |
| BatchGetFeeds | Unary | 批量帖子详情 |
| GetRecommendTimeline | Unary | 推荐流 |
| GetFollowTimeline | Unary | 关注流 |
| GetCityTimeline | Unary | 同城流 |
| GetUserFeeds | Unary | 个人主页帖子列表 |

---

## 4. 落地顺序建议

```
Week 1: 数据模型 + 帖子发布 + 帖子查询
Week 2: 推荐流 + 同城流
Week 3: 关注流（推拉结合）
Week 4: MQ 事件 + Feed Worker + 缓存一致性收尾
Week 5: 单元/集成/并发/压测补齐
```

**为什么是这个顺序？**

- 先让「发帖子、查帖子」能跑通，形成最小可用闭环。
- 推荐/同城流只依赖全局/城市池，实现相对独立。
- 关注流最复杂（推拉结合、大V判定、合并排序），放在后面集中攻坚。
- 测试贯穿全程，但压测放在最后，等有完整链路后再做。

---

## 5. 关键非功能要求

| 维度 | 要求 |
|------|------|
| 可用性 | 单个大V/单条帖子缓存失效不阻塞整页返回 |
| 性能 | 关注流 P99 < 100ms（缓存命中时） |
| 一致性 | DB 是内容唯一数据源；Redis 是视图缓存，允许秒级不一致 |
| 扩展性 | 三种流独立实现，未来新增「话题流」「热搜流」不侵入现有代码 |
| 安全 | 删帖只能删自己的；曝光敏感内容需网关层风控 |

---

## 6. 与现有文档的引用关系

- 整体架构：`docs/design/architecture.md`
- 服务拆分与端口：`docs/design/service-design.md`
- 全局数据模型与 Redis 约定：`docs/design/data-model.md`
- 编码规范：`docs/agent/dev-guidelines.md`
- Proto 规范：`docs/agent/proto-writing-guide.md`
- 错误码：`common/errorx/errorx.go`
