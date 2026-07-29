# MQ 事件方法论（comment.event）

> 本文档定义 Comment 服务的 RocketMQ 事件规范与约束。
> 重点：comment.event 模型、生产者用法、消费者边界、幂等、有序、失败重试。
> 与 `docs/design/service-design.md` / `data-model.md` 中 comment 相关事件保持一致。

---

## 1. comment.event 事件模型

事件由 Comment 服务在评论行为发生后发出，供通知等下游消费。

| 字段 | 类型 | 说明 |
|------|------|------|
| event_id | string | 全局唯一事件 ID（Snowflake 或 UUID），用于消费幂等 |
| action_type | string | CREATE / DELETE |
| comment_id | int64 | 评论 ID（Snowflake） |
| feed_id | int64 | 所属帖子 ID |
| user_id | int64 | 评论作者 ID |
| reply_user_id | int64 | 被回复者 ID（CREATE 时有效，一级评论为 0） |
| parent_id | int64 | 直接父评论 ID |
| root_id | int64 | 根评论 ID（一级为 0） |
| content_len | int32 | 评论内容长度（不发原文，保护隐私/降带宽） |
| timestamp | int64 | 毫秒级事件时间 |

**约束**：
- 不发评论原文 body 到 MQ（仅发 `content_len`），降低带宽与隐私风险；需原文时消费方回查 Comment 服务。
- `action_type` 用枚举式字符串，扩展行为（未来如「置顶」）向后兼容。

---

## 2. 生产者用法

- 用 `common/mq` 的 `NewProducer` + `SendSync`（实现为异步发送，单条小消息低延迟）。
- **失败不阻塞主流程**：发表/删除已落 MySQL 成功，事件发送失败只记 error 日志，重试/监控兜底，不因通知失败而回滚评论。
- Topic：实际名为 **`comment-event`**（连字符）。原因：RocketMQ 的 topic 命名仅允许 `^[%|a-zA-Z0-9_-]+$`，不允许 `.`，故代码常量 `TopicCommentEvent = "comment-event"`，与 data-model.md 文档约定名做了一致性调整。该 topic 作为评论服务广播的统一出口，CREATE/DELETE 共用，靠 `action_type` 区分。

---

## 3. 消费者边界

| 消费方 | 动作 |
|------|------|
| Feed 服务（**已实现**） | 消费 CREATE / DELETE，异步维护 `feeds.comment_count` 镜像列：收到事件后调用 Comment 服务 `BatchGetCommentCount` 拉取**权威值覆盖**镜像列（而非自行推算增量）。原因：方案 A 下「删根评论减 (1+N)」的口径 Feed 无从推算，直接增量会与权威值漂移；覆盖法天然幂等、口径一致。幂等以 `event_id` 去重（Redis SETNX，TTL 24h），重放仅重复覆盖相同值 |
| 通知服务（规划） | 收到 CREATE，且 `reply_user_id != 0` → 给被回复者发提醒；一级评论可向帖子作者发提醒；DELETE 可撤回对应未读通知（可选） |

**约束**：
- Comment 服务**不**直接调通知服务，全部经 MQ 解耦。
- 事件消费方不得反向写 Comment 库（计数/内容由 Comment 自管），仅做通知等副作用。

---

## 4. 削峰定位（与 Interaction 区分）

- Comment **不**把评论内容落库放 MQ（内容已在 `CreateComment` 同步写 MySQL，强一致）。
- MQ 在 Comment 中的角色是「行为广播」：驱动通知、驱动 Feed 计数异步同步、驱动统计。
- 因此 Comment 的 MQ 是**通知型**，不是**落库型**，与 Interaction 的「Redis 先行 + MQ 异步落库」削峰模型本质不同，不要照搬。

---

## 5. 幂等、有序、重试

| 维度 | 约束 |
|------|------|
| 幂等 | 消费方以 `event_id` 去重（Redis SETNX 或消费记录表），重复事件只生效一次 |
| 有序 | 同 `feed_id` 的事件尽量顺序消费（RocketMQ 同 key 顺序）；CREATE 与 DELETE 若乱序，DELETE 幂等成功、CREATE 已落库不冲突 |
| 重试 | 消费失败按 MQ 重试策略重投；业务侧保证可重入（如通知重复发应被去重） |
| 丢失兜底 | 事件丢失不影响评论数据本身（DB 已落）；仅通知可能缺失，可接受最终一致 |

---

## 6. 与已有事件体系的关系

- `comment.event` 与 `data-model.md` 第 5 节描述的评论事件一致：用户评论帖子后通知作者等。
- Comment 规划中是 **interaction.event 的消费者**（`05-stats.md` 第 3 节）：收到评论点赞变更时更新 `comments.like_count` 与 `comment_hot`。**该消费链路当前未实现**——`common/event/interaction` 事件契约尚未定义（见 `05-stats.md` 第 3 节实现状态），待契约明确后 Comment 新增消费 worker。
- 双向事件流（规划）：Comment 发 `comment-event`（给通知）；Comment 收 `interaction-event`（更新点赞数）。两条流职责清晰、互不回写对方库。当前版本 Comment 仅生产 `comment-event`，不消费 `interaction-event`。
- **`comment-event` 消费现状（已实现）**：Feed Worker 已订阅 `comment-event`，收到事件后以 `event_id` 去重，并拉取 Comment 服务 `BatchGetCommentCount` 权威值覆盖 `feeds.comment_count` 镜像列（见第 3 节消费者边界表）。这使 Gateway 聚合层的降级路径（Comment 服务不可用时读镜像列）也能拿到真实评论数，与推荐路径「Gateway 直接读 Comment 计数」并存、互不冲突。通知型消费（通知服务）仍为规划项。
