# Feed 服务设计文档目录

> 本目录存放 Feed 微服务（端口 9003）的设计方案。
> 文档按模块拆分，每个文件聚焦一个能力域，以方法论为主，不包含完整代码。

---

## 文档索引

| 文件 | 主题 | 一句话说明 |
|------|------|-----------|
| [00-overview.md](./00-overview.md) | 总览与接口规划 | Feed 服务定位、模块拆分、接口规划、落地顺序 |
| [01-data-model.md](./01-data-model.md) | 数据模型 | MySQL 表设计、Redis 数据结构、索引与容量策略 |
| [02-post-publish.md](./02-post-publish.md) | 帖子发布与管理 | 发帖、删帖、查帖子的流程与校验方法论 |
| [03-timeline-follow.md](./03-timeline-follow.md) | 关注流 | 推拉结合的关注流合并、Cursor 分页、大V拉模式 |
| [04-timeline-recommend.md](./04-timeline-recommend.md) | 推荐流 | 全局候选池、随机打散、时间衰减 score |
| [05-timeline-city.md](./05-timeline-city.md) | 同城流 | 按城市聚合、时间倒序、城市编码规范 |
| [06-cache-strategy.md](./06-cache-strategy.md) | 缓存策略 | Cache-Aside、缓存分层、一致性保障、击穿穿透雪崩防护 |
| [07-mq-event.md](./07-mq-event.md) | MQ 事件与 Worker | feed.created / feed.deleted 事件、Worker 推/拉处理 |
| [08-test-strategy.md](./08-test-strategy.md) | 测试策略 | 单元/集成/并发/压测分层与典型用例 |

---

## 阅读顺序

```
00-overview → 01-data-model → 02-post-publish
                          ↓
            03-timeline-follow → 04-timeline-recommend → 05-timeline-city
                          ↓
                  06-cache-strategy → 07-mq-event
                          ↓
                      08-test-strategy
```

---

## 与项目其他文档的关系

- 整体架构：`../architecture.md`
- 服务拆分与端口约定：`../service-design.md`
- 全局数据模型：`../data-model.md`
- 编码规范：`../../agent/dev-guidelines.md`
- Proto 规范：`../../agent/proto-writing-guide.md`
- 错误码：`../../../common/errorx/errorx.go`

---

## 实现状态

| 模块 | 状态 | 备注 |
|------|------|------|
| Proto 定义 | 已实现 | `api/proto/feed/feed.proto` 已定义 8 个 RPC（Create/Delete/Get/BatchGet/Recommend/Follow/City/UserFeeds） |
| 数据模型 | 已实现 | 建表脚本见 `deploy/sql/feed.sql` |
| 帖子发布 | 已实现 | CreateFeed / DeleteFeed |
| 关注流 | 已实现 | GetFollowTimeline（推拉结合，收件箱/发件箱见 `feed_follow.go`） |
| 推荐流 | 已实现 | GetRecommendTimeline |
| 同城流 | 已实现 | GetCityTimeline |
| Worker | 已实现 | 关注关系变更驱动的收件箱/缓存维护 |
| 测试 | 未开始 | 参考 08-test-strategy.md |
