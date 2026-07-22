# Interaction 数据模型设计方法论

> 本文档给出 Interaction 服务 MySQL 表结构与 Redis 数据结构的设计思路。
> 不包含完整 SQL，只描述「为什么这么设计」以及实现时的关键决策点。
> 完整 SQL 脚本最终会落地在 `deploy/sql/interaction.sql`。

---

> **时间戳约定**：内部 RPC 的 `created_at` / `updated_at` 统一使用**毫秒级 Unix 时间戳**（遵循 `docs/agent/proto-writing-guide.md`）。
> Redis ZSet score 在本服务中使用**秒级 Unix 时间戳**即可满足排序精度，代码写入时由毫秒转换。

## 1. MySQL 表设计原则

### 1.1 单表优先，按用户归档后置

- 点赞、收藏各一张表，结构同构但独立，便于后续按业务维度独立扩容。
- 单表可支撑数亿级记录；真正需要分表时，优先按 `user_id` 取模，因为查询模式以「用户维度」为主。
- 归档策略：冷数据按月份归档到 `likes_history_YYYYMM` / `collections_history_YYYYMM`。

### 1.2 ID 生成

- 主键 `id` 使用 `common/idgen` 生成的 Snowflake ID（`BIGINT UNSIGNED`）。
- 业务 ID 在写 DB 前生成，便于提前写入缓存和 MQ。
- 禁止用 MySQL 自增主键作为业务 ID，为未来分库分表铺路。

### 1.3 幂等设计：唯一索引 + 状态字段

- 唯一索引 `(user_id, feed_id)` 保证同一用户对同一帖子只有一条记录。
- 使用 `status` 字段标识当前状态：
  - `1`：已点赞 / 已收藏
  - `2`：已取消
- 保留取消记录的原因：
  - 审计与反作弊需要。
  - 恢复操作时可直接把状态改回 `1`，避免重新 INSERT。
  - 便于后续做「取消率」等运营分析。

### 1.4 索引策略

| 查询场景 | 索引 |
|---------|------|
| 用户对某帖子的互动记录 | `UNIQUE (user_id, feed_id)` |
| 某帖子的所有互动用户 | `(feed_id)` |
| 用户互动列表（我的赞/收藏） | `(user_id, created_at)` |

所有索引都带 `status` 过滤参与 COUNT 时需要注意：若 MySQL 需要精确当前态计数，应 `WHERE status = 1`。

---

## 2. Redis 数据结构设计

### 2.1 帖子点赞用户集合：`like:feed:{feed_id}`

- 类型：Set。
- 作用：快速判断用户是否点赞过该帖。
- TTL：7 天。
- 注意：缓存的是**当前仍点赞的用户 ID**。用户取消点赞时立即 `SREM`。

### 2.2 帖子收藏用户集合：`collect:feed:{feed_id}`

- 类型：Set。
- 作用：快速判断用户是否收藏过该帖。
- TTL：7 天。
- 与点赞集合完全隔离，避免互相影响。

### 2.3 用户点赞过的帖子：`user:likes:{user_id}`

- 类型：ZSet，score = 点赞时间戳（秒级）。
- 作用：支持「我的赞」按时间倒序分页。
- TTL：30 天。
- 取消点赞时 `ZREM` 移除，保证列表只展示当前有效记录。

### 2.4 用户收藏过的帖子：`user:collects:{user_id}`

- 类型：ZSet，score = 收藏时间戳（秒级）。
- 作用：支持「我的收藏」按时间倒序分页。
- TTL：30 天。

### 2.5 帖子互动计数：`feed:stats:{feed_id}`

- 类型：Hash，字段 `{like_count, collect_count}`。
- 作用：供 Feed / Comment / Gateway 快速读取互动数。
- TTL：1 小时。
- 缓存失效时从 MySQL `COUNT` 重建。

---

## 3. 实现时的关键决策点

| 决策 | 推荐方案 | 理由 |
|------|---------|------|
| 取消是物理删除还是软删除 | 软删除（status = 2） | 审计、反作弊、恢复操作都需要 |
| 点赞/收藏是否共用一张表 | 各一张表 | 结构虽然相似，但后续扩容、归档、运营分析可独立 |
| Redis 集合 TTL 7 天是否太短 | 配合缓存重建策略 | 老帖互动行为少，命中低；读取时未命中可回源重建 |
| 用户互动列表是否设置容量上限 | 暂不设置，按时间归档 | 用户侧只查前 N 页，ZSet 自然按时间排序 |
| 计数是否需要单独计数器 | 使用 Hash 字段，不单独 key | 减少 key 数量，批量读取可用 `HMGET` |

---

## 4. 与 data-model.md 的关系

- 本文件是 Interaction 服务视角的方法论。
- `docs/design/data-model.md` 是全局数据模型总览，包含具体 SQL 示例。
- 两者有冲突时，以本文件（服务实现落地）为准，但需同步更新 `data-model.md`。
