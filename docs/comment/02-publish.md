# 发表评论方法论

> 本文档描述 Comment 服务「发表评论 / 回复评论」模块的实现规范与约束。
> 重点：一级评论与回复的字段填充规则、参数校验、写库顺序、缓存更新、事件发送、幂等与防刷。

---

## 1. 接口职责边界

`CreateComment` 只做四件事：

1. 参数校验与鉴权。
2. 计算并填充 `root_id / parent_id / reply_user_id`。
3. 写 MySQL（评论内容）。
4. 更新 Redis 计数/热门，并发送 `comment.event`。

**它不应该做**：
- 不直接调用通知服务（通知服务消费 `comment.event` 后处理）。
- 不直接写 Interaction（点赞数由 Interaction 服务维护并异步同步回 comment，见 `05-stats.md`）。
- 不批量拼装用户昵称头像（列表查询时统一批量填充，见 `04-list.md`）。

---

## 2. 一级评论 vs 回复评论的字段填充

发表请求只需声明 `feed_id` 与（可选）`parent_id`，其余字段由服务端推导：

| 场景 | parent_id | root_id | reply_user_id | 说明 |
|------|-----------|---------|---------------|------|
| 一级评论 | 0 | 0 | 0 | 全新楼层的根 |
| 回复一级评论 | = 该一级评论 id | = 该一级评论 id | = 该一级评论 user_id | 楼内第一层回复 |
| 回复子回复 | = 被回复评论 id | = 被回复评论的 root_id | = 被回复评论的 user_id | 任意深度嵌套，root_id 取被回复者的 root_id |

**推导规则（强制）**：
- `root_id` 不取自身 id，而取「parent 的 root_id，若 parent 是一级评论则取 parent 的 id」。
- `reply_user_id` = parent 的 `user_id`。
- 一级评论 `parent_id = root_id = reply_user_id = 0`。

> 这样无论嵌套多深，存储始终是两层：同楼所有回复共享同一 `root_id`，`parent_id` 记录直接父节点。

---

## 3. 参数校验与鉴权

| 层级 | 位置 | 示例 |
|------|------|------|
| 语法校验 | go-zero 生成的 request validate | `feed_id > 0`，`content` 非空 |
| 业务校验 | logic 层手写 | feed 必须存在、父评论必须存在且归属同一 feed、父评论不能是被删除状态 |
| 内容校验 | logic 层 | 非空（`CommentEmpty=13004`）、超长 1000 字（`CommentTooLong=13005`）、可扩展敏感词/风控 |
| 安全校验 | user_id 来源 | 必须从 RPC metadata / JWT 提取，**禁止从请求体读取**（防替他人发表） |

校验失败返回 `common/errorx` 中 Comment 段错误码：
- 帖子不存在 → `CommentFeedNotFound=13002`
- 父评论不存在 → `CommentParentNotFound=13006`
- 内容为空/超长 → `CommentEmpty=13004` / `CommentTooLong=13005`

**父评论归属校验（防跨帖回复）**：回复时若 `parent_id != 0`，必须校验 parent 的 `feed_id` 与请求 `feed_id` 一致，否则拒绝。

---

## 4. 写库顺序与事务边界

1. 校验通过后，用 `common/idgen` 生成评论 `id`（Snowflake）。
2. 单条 `INSERT` 写入 `comments`（应用层携带 id）。
3. 若写入的是一级评论（`root_id=0`），无需维护 `reply_count`。
4. 若写入的是子回复（`root_id!=0`），需对根评论 `reply_count + 1`：
   - 用 `UPDATE comments SET reply_count = reply_count + 1 WHERE id = root_id AND status = 1`。
   - 该更新与 INSERT 在同事务内，保证回复数与评论数一致。
   - 注意：子回复的 `reply_count` 始终为 0，不维护。

**约束**：写 DB 必须在更新 Redis 之前完成（Cache-Aside：先 DB 后缓存）。

---

## 5. 缓存更新（先 DB 后缓存）

DB 写入成功后，更新 Redis：

1. `INCR comment_count:{feed_id}` —— 帖子评论总数 +1（无论一级还是子回复都 +1）。
2. 若新评论 `like_count` 可能为热门候选，写入/更新 `comment_hot:{feed_id}`（score=当前 like_count，新建时为 0）。
   - 新建评论 like_count=0，是否立即进 hot ZSet 由 `05-stats.md` 口径决定；默认不立即进，待有点赞再进。

**约束**：
- 不先写缓存再写 DB（与 Interaction 区分）。
- 缓存更新失败**不阻塞**主流程，仅记录 error 日志；下次读未命中会自动从 MySQL 重建（`06-cache.md`）。
- `INCR` 用 Pipeline 发送，降低 RT。

---

## 6. 事件发送

DB 与缓存更新后，发送 `comment.event`（`action_type = CREATE`）：
- 事件体至少含：`comment_id`、`feed_id`、`user_id`、`reply_user_id`、`parent_id`、`root_id`、`content_len`、`timestamp`（毫秒）。
- 生产者用 `common/mq`，失败**不阻塞**发表返回（异步通知，可重试/监控兜底）。
- 事件消费方：通知服务（向 `reply_user_id` 发提醒，一级评论可向帖子作者发提醒）。

> 详细事件模型见 `07-mq-event.md`。

---

## 7. 幂等与防刷

| 问题 | 约束 |
|------|------|
| 重复提交 | 评论内容无天然幂等键；由网关/上游做幂等（如请求去重 token），comment 服务不保证同内容去重 |
| 防刷限流 | 高频发表用 `TooManyReq=5`（通用码）限流；可结合 `InteractionTooFrequent` 同类思路做用户级频率限制 |
| 并发写同一楼 | `reply_count + 1` 用 `UPDATE ... + 1` 原子语句，不先读后写，避免计数竞态 |

---

## 8. 异常场景

| 场景 | 处理原则 |
|------|---------|
| DB 写入失败 | 直接返回错误，不更新缓存、不发事件；用户可重试 |
| DB 成功，Redis INCR 失败 | 返回成功，记 error 日志；读时从 MySQL 重建计数 |
| DB 成功，MQ 发送失败 | 返回成功，记 error 日志；通知可延迟/重试，不丢数据（DB 已落） |
| 父评论在并发中被删 | 写入前校验 parent status=1；若已被删，返回 `CommentParentNotFound` |
