# Comment 服务总体实现方案

> 本文档从方法论角度描述 Comment 服务（端口 9004）的实现规划。
> 不包含完整代码，只给出能力模块拆分、设计原则、接口规划和落地顺序，
> 供后续按模块逐步实现与 review。

---

## 1. 服务定位

Comment 服务是**支持无限嵌套楼中楼的评论系统**，负责评论的 CRUD 与计数：

- **写**：发表一级评论、回复评论、删除评论（软删除）。
- **读**：帖子一级评论列表（分页，每楼含前 N 条子回复预览）、某根评论下的全部子回复（分页）、帖子评论总数、热门评论。
- **事件**：评论行为发生后向 RocketMQ 发送 `comment.event`，驱动通知与计数同步。

它依赖：

| 下游 | 用途 |
|------|------|
| MySQL | 评论内容的持久化主存储（评论是写后读多、需完整查询，不走 Redis 先行） |
| Redis | 仅缓存「帖子评论总数」与「热门评论」，扛高频读计数 |
| User 服务 | 列表/详情查询时批量获取评论者昵称、头像等基础信息 |
| RocketMQ | 发送 `comment.event` 事件 |

**与 Interaction 的本质区别**：Interaction 是超高频 KV 写（点赞/收藏），采用「Redis 先行 + MQ 异步落库」削峰；Comment 的评论内容必须强一致落库，因此走「先写 DB，再更新/删除缓存」的 Cache-Aside。两者的缓存策略不能混用。

---

## 2. 能力模块拆分

按「数据 → 写 → 读 → 一致性 → 事件 → 测试」的顺序，Comment 服务拆为 8 个模块（不含本总览）：

| 序号 | 模块 | 文档 | 核心问题 |
|------|------|------|---------|
| 1 | 数据模型 | [01-data-model.md](./01-data-model.md) | 楼中楼如何两层平铺存储？主键/索引/Redis 怎么定？ |
| 2 | 发表评论 | [02-publish.md](./02-publish.md) | 一级与回复的字段怎么填充？计数与热门怎么更新？ |
| 3 | 删除评论 | [03-delete.md](./03-delete.md) | 软删除后子树怎么办？根/子计数怎么回填？ |
| 4 | 评论列表 | [04-list.md](./04-list.md) | 一级分页、子回复预览、查看全部回复如何做且避免 N+1？ |
| 5 | 计数与热门 | [05-stats.md](./05-stats.md) | comment_count / comment_hot 如何一致？点赞数从哪同步？ |
| 6 | 缓存一致性 | [06-cache.md](./06-cache.md) | Cache-Aside 下如何重建、防负数、降级？ |
| 7 | MQ 事件 | [07-mq-event.md](./07-mq-event.md) | comment.event 模型、有序、幂等、失败重试？ |
| 8 | 测试策略 | [08-test-strategy.md](./08-test-strategy.md) | 单元/集成/并发/压测分别覆盖什么？ |

---

## 3. gRPC 接口规划方法论

接口设计遵循 `docs/agent/proto-writing-guide.md`：

1. **一接口一职责**：发表、删除、列表、计数各自独立。
2. **批量接口以 `Batch` 开头**：如 `BatchGetCommentCount` 供 Feed 列表批量取评论数。
3. **时间戳统一毫秒级 Unix（int64）**：`created_at` 等字段均用 `int64`。
4. **字段 snake_case，ID/时间戳 int64**：与全项目对齐。
5. **方法顺序**：Create → List/Get → Delete → 统计类。

建议的接口矩阵（在 `api/proto/comment/comment.proto` 中落地）：

| 接口 | 类型 | 说明 |
|------|------|------|
| CreateComment | Unary | 发表一级评论或回复评论 |
| DeleteComment | Unary | 软删除评论（仅作者/管理员） |
| ListComments | Unary | 帖子一级评论分页，每楼含前 N 条子回复预览，附评论总数 |
| ListReplies | Unary | 某根评论下全部子回复分页 |
| GetCommentCount | Unary | 单帖评论总数 |
| BatchGetCommentCount | Unary | 批量帖子评论总数（Feed 流列表用） |
| GetHotComments | Unary | 帖子热门评论（按点赞排序，来自 comment_hot） |

字段约束指针（详细定义见 proto 规范，不在本文贴出）：
- 请求必含 `user_id`（从 metadata 提取，禁止请求体透传）、`feed_id`。
- 回复类请求必含 `parent_id`；一级评论 `parent_id = 0`。
- 列表请求必含分页参数（page / page_size 或 cursor + limit），由各模块文档定义语义。
- 响应中的评论实体（CommentInfo）至少含：comment_id、feed_id、user_id、content、root_id、parent_id、reply_user_id、like_count、reply_count、status、created_at。

---

## 4. 落地顺序建议

```
阶段 1：数据模型 + 发表评论 + 删除评论（最小写闭环）
阶段 2：评论列表 + 计数与热门（读闭环，供 Feed 详情/列表使用）
阶段 3：缓存一致性收尾 + MQ 事件（通知与计数同步）
阶段 4：单元/集成/并发/压测补齐
```

**为什么是这个顺序？**
- 先让「发表 / 删除 / 落库」跑通，形成内容生产闭环。
- 列表与计数是 Feed 详情页、评论数展示的高频依赖，需尽早稳定。
- 缓存一致性与 MQ 事件最复杂，放在内容读写稳定后攻坚。
- 测试贯穿全程，压测最后做（需完整链路）。

---

## 5. 关键非功能要求

| 维度 | 要求 |
|------|------|
| 可用性 | 单条评论 / 单个帖子缓存失效不阻塞其他帖子读写 |
| 性能 | 发表/删除 P99 < 30ms（同步 Redis 仅 2 个 key）；列表首屏 P99 < 50ms |
| 一致性 | MySQL 是评论内容的唯一强一致源；Redis 计数允许秒级不一致；热门评论允许分钟级不一致 |
| 安全性 | 禁止替他人发表/删除；user_id 来自 metadata；评论内容需做长度与敏感校验；所有 DB 操作参数化 |
| 扩展性 | 新增"二级以上嵌套"视觉展示不要求改存储结构（两层平铺已支持无限视觉嵌套） |

---

## 6. 重要规范冲突提示（落地前必处理）

`docs/design/data-model.md` 第 4.1 节 `comments` 表写的是 `id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT`，
但 `AGENTS.md` 4.5 与 `data-model.md` 通用约定均要求：

> 业务实体 ID 使用 `common/idgen`（Snowflake），禁止用数据库自增主键作为业务 ID。

**处理动作**：
1. 实现 comment 服务时，`comments.id` 必须由 `common/idgen` 在应用层生成后写入。
2. 同步修正 `deploy/sql/comment.sql` 与 `docs/design/data-model.md` 第 4.1 节，将 `AUTO_INCREMENT` 改为应用层写入的 Snowflake ID，并去掉 `PRIMARY KEY` 上的自增属性。
3. `01-data-model.md` 已将此作为强制约束写明。

---

## 7. 与现有文档的引用关系

- 整体架构：`docs/design/architecture.md`
- 服务拆分与端口：`docs/design/service-design.md`
- 全局数据模型与 Redis 约定：`docs/design/data-model.md`
- 编码规范：`docs/agent/dev-guidelines.md`
- Proto 规范：`docs/agent/proto-writing-guide.md`
- 错误码：`common/errorx/errorx.go`（Comment 段 `13000~13999`）
