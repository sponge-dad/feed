# 关注流（Timeline-Follow）设计方法论

> 本文档描述 Feed 服务中最复杂的「关注流」实现方法论：
> 如何在「推模式」和「拉模式」结合下，生成用户的关注流 Timeline。
> 不包含完整代码，只给出设计原则、算法步骤和边界处理思路。

---

## 1. 问题定义

关注流 = 我关注的人发布的帖子，按时间倒序排列。

直接方案的问题：

- 全量推模式：大V有 1000 万粉丝，发帖时写入 1000 万个 inbox，不可行。
- 全量拉模式：每次读取时去所有关注者的 outbox 拉取，关注 5000 人时延迟不可接受。

**解决方案：推拉结合（Hybrid Push-Pull）。**

---

## 2. 推拉结合策略

### 2.1 普通用户发帖：推模式（Write Fan-out）

- 发帖后，Worker 消费 `feed.created` 事件。
- 从 Relation Service 分批拉取粉丝列表。
- 将 `feed_id` 写入每个粉丝的 `inbox:{fan_id}` ZSet。
- 单批大小 500，使用 Redis Pipeline 批量写入。

### 2.2 大V发帖：拉模式（Read Fan-out）

- 发帖后，Worker 只将该 feed 写入大V自己的 `outbox:{vip_id}`。
- **不**推送到粉丝 inbox。
- 用户读取关注流时，再去自己关注的大V outbox 中拉取最近帖子合并。

### 2.3 大V判定

- 判定标准：粉丝数 >= `Vip.FansThreshold`（生产 10 万，开发测试可配 1 万）。
- 判定来源：`Relation.IsVip(user_id)`。
- 判定时机：
  - 发帖时判定 `is_vip_feed`，用于决定推送策略。
  - 读取时重新判定哪些关注者是大V，用于决定拉取哪些 outbox。

---

## 3. 关注流读取流程

### 3.1 数据源

关注流由两部分合并：

1. **推数据**：`inbox:{user_id}` ZSet（我关注的普通用户发的帖子）。
2. **拉数据**：我关注的每个大V的 `outbox:{vip_id}` ZSet。

### 3.2 算法步骤

```
输入：user_id, cursor, page_size
输出：feed_id 列表 + 下一页 cursor

1. 从我的 inbox 取前 N 条（N 可略大于 page_size，用于合并）。
2. 从 Relation 获取我的关注列表。
3. 对关注列表中的每个用户调用 Relation.IsVip，筛选出大V集合。
4. 对每个大V，从其 outbox 取最近 M 条（M 与 page_size 同量级）。
5. 合并 inbox 和大V outbox 数据，按 score（发帖时间戳）倒序。
6. 从 cursor 位置开始取 page_size 条。
7. 生成下一页 cursor = 最后一条的 (score, feed_id)。
```

### 3.3 Cursor 设计

Cursor 组成：

```
cursor = base64(score_sec + ":" + feed_id)
```

- `score_sec`：发帖时间戳的秒级部分，来自 Redis ZSet score。
- `feed_id`：Snowflake ID，用于处理同一秒内多条帖子的情况。
- 下一页查询时，取 `score_sec < cursor_score` 或 `score_sec == cursor_score 且 feed_id < cursor_feed_id` 的帖子。

> proto 中 `created_at` 用毫秒级，但 Redis ZSet score 用秒级；Cursor 基于秒级 score 生成。

为什么不用 Offset？

- 关注流实时变化，Offset 会导致重复或漏掉。
- Cursor 基于时间 + ID，稳定且可序列化。

---

## 4. 性能优化方法论

### 4.1 限制拉取的大V数量

- 用户关注的大V数量通常远小于总关注数。
- 若用户关注了 100 个大V，每个 outbox 拉 20 条，需要 100 次 Redis 调用。
- 优化：
  - 对活跃大V缓存其最近帖子列表。
  - 引入「我关注的大V」本地缓存，减少 Relation.IsVip 调用。
  - 未来扩展 Relation.BatchIsVip 接口，一次查询多个用户是否大V。

### 4.2 预合并缓存

- 对活跃用户，缓存合并后的关注流前 2 页到 `timeline:{user_id}:follow`，TTL 60 秒。
- 普通用户发帖、关注/取关、大V发帖时，删除该缓存。

### 4.3 降级策略

- 如果某个大V outbox 读取失败，跳过该大V，不阻塞整体返回。
- 如果 inbox 为空且没有大V，直接返回空列表。

---

## 5. 边界场景

| 场景 | 处理 |
|------|------|
| 用户刚关注一个大V | 从关注时刻起，该大V新帖进入拉模式；历史帖子不回填 inbox |
| 用户取关一个大V | 不再拉取其 outbox；已写入 inbox 的历史帖子自然过期 |
| 大V被降级为普通用户 | 后续发帖走推模式；历史 outbox 中的帖子仍可读，直到过期 |
| 普通用户变成大V | 后续发帖走拉模式；历史 inbox 中的帖子自然过期 |
| 帖子被删除 | Worker 消费 feed.deleted，从所有粉丝 inbox 中移除 |

---

## 6. 与 Relation Service 的交互

关注流需要 Relation 提供：

1. **我的关注列表**：`GetFollows(user_id, page, page_size)`。
2. **是否大V**：`IsVip(user_id)`，未来建议扩展为 `BatchIsVip(user_ids)`。

调用方式：

- 读关注流时，先拉取全部关注列表（用户关注上限 5000，可一次拉取）。
- 对每个关注者调用 IsVip，本地缓存结果 1 分钟。

---

## 7. 与 Worker 的关系

- `feed.created` 事件触发 Worker 决定走推还是拉。
- `feed.deleted` 事件触发 Worker 清理粉丝 inbox。
- `relation.created` / `relation.deleted` 事件触发 Worker 处理关注关系变化（如回填历史帖子等，可选）。
