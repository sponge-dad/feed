# Comment 服务开发手册

> 本目录存放 Comment 服务（端口 9004）的**分模块开发手册**。
> 每份手册描述一个能力模块的「规范、约束与具体技术方案」，不包含实现代码，
> 供开发时按模块落地与 review。
>
> 阅读顺序：先读 `00-overview.md` 建立总体认知，再按需进入各模块。

## 文档清单

| 文件 | 内容 | 核心问题 |
|------|------|---------|
| [00-overview.md](./00-overview.md) | 服务总体方案：定位、模块拆分、gRPC 接口规划、落地顺序、非功能要求 | comment 服务整体怎么切、接口怎么定 |
| [01-data-model.md](./01-data-model.md) | 数据模型：comments 表字段/索引、楼中楼双字段模型、Redis 结构、Snowflake 约束 | 评论如何存储、主键与索引怎么定 |
| [02-publish.md](./02-publish.md) | 发表评论：一级/回复字段填充、校验、写库、缓存更新、事件、幂等防刷 | 怎么发一条评论且不脏数据 |
| [03-delete.md](./03-delete.md) | 删除评论：软删除、权限、子树处理、计数回填 | 删评论时子树和计数怎么办 |
| [04-list.md](./04-list.md) | 评论列表：一级分页、子回复预览、查看全部回复、用户填充 | 楼中楼怎么查、怎么分页 |
| [05-stats.md](./05-stats.md) | 计数与热门：comment_count、comment_hot、点赞数联动 | 总数与热门如何一致、点赞数从哪来 |
| [06-cache.md](./06-cache.md) | 缓存一致性：Cache-Aside、重建、并发安全、降级 | Redis 与 MySQL 怎么保持一致 |
| [07-mq-event.md](./07-mq-event.md) | MQ 事件：comment.event 模型、生产者/消费者、幂等有序 | 事件怎么发、谁消费、如何不丢 |
| [08-test-strategy.md](./08-test-strategy.md) | 测试策略：单元/集成/并发/压测分层与关键用例 | 各模块测什么、边界在哪 |
| [dataflow.md](./dataflow.md) | 全部 logic 数据流：入口/校验/主流程/数据源/失败降级/副作用/ASCII 图 | 每个 logic 的数据从哪来到哪去 |

## 与现有文档的引用关系

- 整体架构：`../design/architecture.md`
- 服务拆分与端口：`../design/service-design.md`
- 全局数据模型：`../design/data-model.md`
- 编码规范：`../agent/dev-guidelines.md`
- Proto 规范：`../agent/proto-writing-guide.md`
- 错误码：`../common/errorx/errorx.go`（Comment 段 `13000~13999`）

## 关键约束速记（开发前必读）

1. **主键必须 Snowflake**：`comments.id` 由 `common/idgen` 应用层写入，**禁止**使用 MySQL 自增。设计文档 `data-model.md` 第 4.1 节的 `AUTO_INCREMENT` 为待修正项，落地前须同步修正 `deploy/sql/comment.sql` 与 `data-model.md`。
2. **写路径先 DB 后缓存**：comment 内容写 MySQL，再更新/删除 Redis 缓存（Cache-Aside）。这与 Interaction 的「Redis 先行 + MQ 异步落库」**不同**，不要照搬。
3. **用户身份来源**：`user_id` 一律从 RPC metadata / JWT 提取，禁止从请求体读取。
4. **错误码统一**：业务错误走 `common/errorx`，禁止裸 `errors.New`。
