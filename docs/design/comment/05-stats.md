# 评论计数与热门方法论

> 本文档描述 Comment 服务「计数与热门」模块的规范与约束。
> 重点：comment_count 与 comment_hot 的口径、缓存来源、以及评论点赞数（like_count）的跨服务同步。

---

## 1. comment_count（帖子评论总数）

**定义**：某帖子下所有 `status=1` 的评论数量（一级 + 子回复，均计入）。

**读取链路**：
1. 优先读 Redis `comment_count:{feed_id}`（String）。
2. 未命中 → 从 MySQL 重建并写回（TTL 1h，见 `06-cache.md`）。
3. 返回 int64。

**重建口径（强制）**：计数 = 自身 `status=1` 且「一级评论（`root_id=0`）**或**其根评论 `status=1`」的评论数。即重建 SQL 需 `LEFT JOIN` 同表根评论，排除「根已删（方案 A）」楼内仍 `status=1` 的子回复。详见 `03-delete.md` 方案 A 计数说明——若不排除，全量重建会把折叠楼的子回复算回，与写时减量口径冲突，造成计数漂移。`BatchGetCommentCount` 批量统计必须采用同一口径（一条 `GROUP BY` 复用该 JOIN 条件）。

**更新时机**（见 `02-publish` / `03-delete`）：
- 发表：`INCR +1`（无论一级/子回复）。
- 删除：`DECR` 对应减量（根评论含整楼可见子回复数）。

**与 Feed 服务的关系**：
- `feeds` 表有 `comment_count` 字段，但与 Comment 服务是跨服务，禁止 Comment 直接写 Feed 库。
- 规范：Feed 帖子详情展示的评论数，由 Feed 服务**读取 Comment 服务的计数**（`GetCommentCount` / `BatchGetCommentCount`），或 Feed 消费 `comment-event` 异步同步自身 `feeds.comment_count`。
- 二选一，推荐 **Feed 直接读 Comment 计数接口 + 缓存**，避免双写不一致。
- **当前实现**：Gateway 聚合层采用推荐路径，直接调用 Comment 服务 `BatchGetCommentCount`（失败降级读 `feeds.comment_count` 镜像列）；同时 Feed Worker 已实现消费 `comment-event`，拉取 Comment 权威计数覆盖 `feeds.comment_count` 镜像列（见 `07-mq-event.md` 第 3 节），使降级路径也能拿到真实评论数。两条路径并存、互不冲突。

---

## 2. comment_hot（热门评论）

**定义**：某帖子下按 `like_count` 降序的热门评论集合。

**存储**：Redis ZSet `comment_hot:{feed_id}`，member=comment_id，score=like_count，TTL 5 分钟。

**计算口径（强制）**：
- score = 该评论当前 `like_count`（来自 comments 表，由 Interaction 同步，见第 3 节）。
- 取 Top-K（如 K=3）作为热门。
- 仅一级评论可进热门？还是全部评论？规范：**仅一级评论（root_id=0）**进热门，符合"热门楼层"产品语义。

**更新/重建时机**：
- 评论 `like_count` 变化时（Interaction 同步回来）更新 ZSet 成员 score，`ZADD`。
- 评论被删或整楼折叠：`ZREM`。
- 读未命中：从 MySQL `SELECT ... WHERE feed_id=? AND root_id=0 AND status=1 ORDER BY like_count DESC LIMIT K` 重建并写回。

**容量**：ZSet 仅存 Top-K 量级数据，无需全量，控制内存。

---

## 3. like_count 的来源与同步（跨服务）

**关键约束**：评论点赞由 **Interaction 服务** 负责，Comment 服务的 `comments.like_count` 不参与点赞计数逻辑，只做存储与展示。

同步链路（规范，待 Interaction 服务接口确认）：

1. 用户对评论点赞 → Interaction 服务处理，记录 `like_count` 增量（针对 comment 类型目标）。
2. Interaction 发送 `interaction.event`（`target_type=comment`，`target_id=comment_id`，`like_count`）。
3. Comment 服务消费该事件，执行 `UPDATE comments SET like_count = ? WHERE id = comment_id` 并刷新 `comment_hot` ZSet。

**一致性窗口**：因异步同步，`comments.like_count` 与 Interaction 实时值允许秒级延迟；热门评论刷新允许分钟级延迟。列表/详情展示以 Comment 库内 `like_count` 为准，不实时回查 Interaction。

**当前实现状态（诚实记录）**：`common/event/` 下尚未定义 Interaction 事件契约，因此 Comment 侧**暂未实现** `interaction.event` 消费 worker；`comments` 表已预留 `like_count` 字段与 `UpdateLikeCount` 存储能力，待 `common/event/interaction` 契约明确后接入。在此之前，`comment_hot` 不启用基于实时点赞的准实时重建，仅依赖存量 `like_count`；第 2 节热门口径仍成立，但"点赞即时上热门"的能力需等同步链路落地。

**待确认（与 Interaction 服务对齐）**：
- Interaction 服务需支持 `target_type=comment` 的点赞计数。
- `interaction.event` 是否携带 `target_type` 与目标业务 ID，供 Comment 区分评论与帖子点赞。
- 事件契约落地位置：`common/event/interaction`（目前不存在），明确后由 Comment 新增消费 worker。

---

## 4. 一致性小结

| 数据 | 强一致源 | 缓存 | 允许延迟 |
|------|---------|------|---------|
| 评论内容 | MySQL | 无 | 即时（写即读） |
| comment_count | MySQL COUNT | Redis（TTL 1h） | 秒级（删后重建前） |
| like_count | MySQL（Interaction 同步） | 无 | 秒级（异步同步） |
| comment_hot | MySQL（like_count 推导） | Redis ZSet（TTL 5min） | 分钟级 |

---

## 5. 与 Interaction 的边界（重申）

- Interaction 管"点赞动作"与"点赞数"的发源。
- Comment 管"评论内容"与"评论数（comment_count）"。
- 评论的点赞数 `like_count` 是 Interaction 的事实、Comment 的镜像；任何"重置/回退点赞"由 Interaction 发起并通知 Comment 重算，Comment 不主动改 `like_count`。
