# 缓存一致性方法论

> 本文档定义 Comment 服务 Redis 缓存（comment_count / comment_hot）的一致性规范。
> 核心策略：**Cache-Aside（先写 DB，再更新/删除缓存）**，与 Interaction 的「Redis 先行 + MQ 落库」不同。

---

## 1. 总体策略

Comment 服务缓存只护两类高频读数据（详见 `01-data-model.md`）：

- `comment_count:{feed_id}`（String，TTL 1h）
- `comment_hot:{feed_id}`（ZSet，TTL 5min）

**采用 Cache-Aside，而非 Redis 先行**：

| 维度 | Interaction | Comment（本文） |
|------|-------------|----------------|
| 写路径 | Redis 先行，MQ 异步落库（削峰） | 先写 MySQL，再更新/删除 Redis |
| 原因 | 点赞超高频 KV，内容即计数 | 评论内容需强一致落库，缓存仅护计数 |
| 一致性 | 最终一致 | 缓存围绕强一致 DB 重建 |

**写后缓存动作**：
- 发表/删除成功后：`comment_count` 用 `INCR`/`DECR` 即时更新（或 `DEL` 触发下次读重建，二选一；见第 4 节）。
- `comment_hot` 在 like_count 变化时 `ZADD`/`ZREM`。

**约束**：缓存更新失败不阻塞写主流程，记录 error 日志即可（读未命中会重建）。

---

## 2. comment_count 重建

- 读未命中（`GET` 返回 nil）→ 查 MySQL 重建 → `SET` 回写（带 TTL）。
- **重建口径与 `05-stats.md` 第 1 节一致**：必须 `LEFT JOIN` 排除「根已删（方案 A）」楼内仍 `status=1` 的子回复，否则重建值会含折叠楼、与写时减量口径冲突导致计数漂移。
- 重建期间并发读：允许短暂多次重建（幂等，结果一致），无需加锁。
- 重建失败（MySQL 异常）：返回错误，不写缓存。

---

## 3. comment_hot 重建

- 读未命中 → MySQL `SELECT ... WHERE root_id=0 AND status=1 ORDER BY like_count DESC LIMIT K` → `DEL` 旧 ZSet 后 `ZADD` 批量写入（带 TTL）。
- 重建用「先删后批量 ZADD」保证整体一致，避免增量累积脏数据。

---

## 4. comment_count 的更新 vs 删除选择

两种写后策略：

| 策略 | 做法 | 优点 | 缺点 |
|------|------|------|------|
| INCR/DECR | 写后直接原子增减 | 读命中率最高，RT 最低 | 需保证计数非负、防越界 |
| DEL 触发重建 | 写后 `DEL`，下次读重建 | 实现简单，无负数风险 | 写后首读有重建开销 |

**决策（强制）**：
- `comment_count` 采用 **INCR/DECR**，并加非负保护（见第 5 节），保证读路径绝大多数命中。
- 仅在重建失败或 Redis 大规模失效时退化为重建。
- **补充决策（防脏负数）**：INCR/DECR 仅在 `comment_count:{feed_id}` **已存在**时执行；若 key 不存在（尚未被读/写过），跳过增量操作，直接依赖下次读时从 MySQL 重建（重建值天然正确、非负）。该策略与第 5 节的非负保护互为兜底：前者从根上避免"对尚无计数的帖子 DECR 出负值"，后者在 key 已存在时兜底竞态下可能出现的瞬时负数。

---

## 5. 并发安全

| 风险 | 约束 |
|------|------|
| 计数变负 | `DECR` 后用 `GET` 判断；若 < 0 则 `SET 0`。或统一在应用层基于事务内 MySQL COUNT 结果 `SET` 绝对值（更稳，但写后多一次读）。首版用非负保护 + 重建兜底 |
| reply_count 竞态 | `UPDATE ... reply_count = reply_count ± 1 WHERE id=? AND status=1`，DB 原子自增自减，不先读后写 |
| 并发删同一根评论 | `UPDATE ... SET status=2 WHERE id=? AND status=1` 受影响行数做幂等，避免重复减 count |
| 热 key 漂移 | comment_count 单帖 QPS 高时，Redis 单分片可接受；无需特殊分片 |

---

## 6. 降级与容灾

| 场景 | 处理 |
|------|------|
| Redis 不可用 | 评论读写主流程不依赖缓存：发表/删除照常写 MySQL；读取评论内容不受影响；comment_count 临时从 MySQL COUNT 返回（性能降级但不出错） |
| 缓存脏数据 | 提供手动 `DEL comment_count:{feed_id}` / `comment_hot:{feed_id}` 的运维/调试入口；复用 TTL 自动收敛 |
| 重建放大 | 重建并发由 Redis 单点串行化（Redis 命令原子），多次重建结果一致，无放大副作用 |

**约束**：任何缓存兜底层都不得返回「评论内容」的缓存（评论内容不缓存），内容读一律 MySQL（`01-data-model.md` 已规定）。
