# Interaction 服务设计文档

> 本目录存放 Interaction 服务（端口 9005）的分模块设计方案。
> 先从 `00-overview.md` 开始阅读，了解总体实现方案，再按需阅读各模块文档。

## 文档清单

| 文件 | 内容 |
|------|------|
| [00-overview.md](./00-overview.md) | Interaction 服务总体实现方案：定位、模块拆分、接口规划、落地顺序、非功能要求 |
| [01-data-model/01-data-model.md](./01-data-model/01-data-model.md) | MySQL 表结构、Redis 数据结构、索引与关键设计决策 |
| [02-like/02-like.md](./02-like/02-like.md) | 点赞与取消点赞：幂等、Redis 先行、MQ 异步落库 |
| [03-collect/03-collect.md](./03-collect/03-collect.md) | 收藏与取消收藏：与点赞同构但隔离的实现模式 |
| [04-stats/04-stats.md](./04-stats/04-stats.md) | 帖子互动计数与用户互动状态查询：单条与批量 |
| [05-user-list/05-user-list.md](./05-user-list/05-user-list.md) | 「我的赞」「我的收藏」列表：Cursor 分页与缓存重建 |
| [06-cache/06-cache.md](./06-cache/06-cache.md) | Redis 先行策略下的缓存一致性、计数非负保护与并发安全 |
| [07-mq-event/07-mq-event.md](./07-mq-event/07-mq-event.md) | `interaction.event` 事件模型、有序消费、异步落库与幂等 |
| [08-test-strategy/08-test-strategy.md](./08-test-strategy/08-test-strategy.md) | 单元/集成/并发/压测分层策略与关键用例 |
| [dataflow.md](./dataflow.md) | 全部 logic 数据流 | 每个 logic 的入口/校验/主流程/数据源/失败降级/副作用/ASCII 图 |

## 关联文档

- 整体架构：`../architecture.md`
- 服务拆分与端口：`../service-design.md`
- 全局数据模型：`../data-model.md`
- 编码规范：`../../agent/dev-guidelines.md`
- Proto 规范：`../../agent/proto-writing-guide.md`
