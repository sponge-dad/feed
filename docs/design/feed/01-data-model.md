# Feed 数据模型设计方法论

> 本文档给出 Feed 服务 MySQL 表结构与 Redis 数据结构的设计思路。
> 不包含建表语句的完整代码，只描述「为什么这么设计」以及实现时需要注意的决策点。
> 完整 SQL 脚本最终会落地在 `deploy/sql/feed.sql`。

---

## 1. MySQL 表设计原则

### 1.1 单表优先，分表后置

- 10 亿级帖子之前，`feeds` 单表 + 合理索引即可支撑。
- 真正需要分表时，按 `user_id` 分表会让「个人主页查询」简单，但会让「推荐流/同城流」变成全分表扫描。
- **推荐按时间归档**：热数据在 `feeds`，冷数据按月归档到 `feeds_history_YYYYMM`。

### 1.2 ID 生成

- 主键 `id` 使用 `common/idgen` 生成的 Snowflake ID（BIGINT UNSIGNED）。
- 禁止用 MySQL 自增主键作为业务 ID：
  - 自增 ID 不利于未来分库分表。
  - Snowflake ID 趋势递增，对 B+ 树索引友好。
  - 业务 ID 需要在写 DB 前生成，便于提前写入缓存和 MQ。

### 1.3 软删除 vs 物理删除

- Feed 采用**软删除**（`status = 2`）。
- 原因：
  - 帖子被删除后，推荐池、inbox、outbox 中可能仍残留引用，需要靠 `status` 过滤。
  - 审计、举报、内容合规回溯都需要保留原始记录。
- 软删除的代价是索引需要带 `status` 过滤，避免已删除数据污染查询。

### 1.4 媒体字段设计

- 图文/视频统一用 `feed_type` 字段区分（1=图文，2=视频）。
- 媒体 URL 用 JSON 数组存储：`media_urls = ["url1", "url2"]`。
- 封面图单独字段 `cover_url`，视频场景必填，图文场景可为空。
- 不在这里存储图片宽高、视频时长等元数据，避免表过宽；需要时通过对象存储 URL 或扩展表解决。

### 1.5 城市字段

- `city_code` 使用标准行政区划编码，如 `440300`（深圳）。
- `city_name` 冗余存储，减少查询时反查字典。
- `ip_location` 存储省/直辖市级别，用于前端展示「来自广东」。
- 同城流查询时按 `city_code + created_at` 索引。

### 1.6 计数字段

- `like_count`、`comment_count`、`collect_count` 在 `feeds` 表中冗余维护。
- 这是「写宽表」策略：
  - 优点：查帖子列表时无需 join Interaction/Comment 服务。
  - 缺点：计数更新需要异步同步，存在短暂不一致。
- 计数来源以 Interaction/Comment 服务的 Redis/MySQL 为准，Feed 表的计数只用于列表展示和排序辅助。

### 1.7 索引策略

| 查询场景 | 索引 |
|---------|------|
| 个人主页 | `(user_id, created_at)` |
| 同城流 | `(city_code, created_at)` |
| 推荐流后台刷新 | `(status, created_at)` |
| 单条详情 | `PRIMARY KEY (id)` |

所有索引都需要带 `status = 1` 过滤，利用最左前缀原则。

---

## 2. Redis 数据结构设计

### 2.1 帖子详情缓存：`feed:{feed_id}`

- 类型：Hash。
- 作用：加速单条/批量帖子详情查询。
- TTL：30 天。
- 注意：缓存的是**帖子基础字段**，不包含作者信息和实时互动计数；聚合由上层完成。

### 2.2 发件箱：`outbox:{user_id}`

- 类型：ZSet，score = 发帖时间戳（秒级）。
- 作用：存储用户自己发的帖子 ID 列表。
- 容量：普通用户不限；大V保留最近 2000 条。
- TTL：7 天。
- 为什么是 ZSet 而不是 List：
  - 需要按时间范围查询（如拉取最近 7 天）。
  - 需要支持 Cursor 分页（score + member）。

### 2.3 收件箱：`inbox:{user_id}`

- 类型：ZSet，score = 发帖时间戳（秒级）。
- 作用：存储关注用户发的帖子 ID。
- 容量：1000 条。
- TTL：7 天。
- 设计要点：
  - 普通用户发帖后，Worker 异步将帖子 ID 写入所有粉丝的 inbox。
  - 大V发帖**不写入粉丝 inbox**，关注流读取时再去大V outbox 拉取（读扩散）。
  - 超过容量时按 ZSet 分数删除最旧的。

### 2.4 推荐池：`feed:recommend`

- 类型：ZSet，score = `rand(0,1) × time_decay_factor`。
- 作用：全局推荐流候选池。
- 容量：10 万条。
- TTL：30 天。
- score 设计见 [04-timeline-recommend.md](./04-timeline-recommend.md)。

### 2.5 同城池：`feed:city:{city_code}`

- 类型：ZSet，score = 发帖时间戳（秒级）。
- 作用：按城市聚合的候选池。
- 容量：2 万条/城市。
- TTL：7 天。

### 2.6 Timeline 热点缓存：`timeline:{user_id}:{tab}`

- 类型：String，存储前 2 页 JSON。
- 作用：对活跃用户的热点 Timeline 做短期缓存，降低重复计算。
- TTL：60 秒。
- tab 取值：`recommend`、`follow`、`city`。

---

## 3. 实现时的关键决策点

| 决策 | 推荐方案 | 理由 |
|------|---------|------|
| feeds 是否分表 | 不分，先按时间归档 | 推荐/同城流需要全局扫描，分表反直觉 |
| media 存 JSON 还是单独表 | JSON | 1-N 媒体在 Feed 场景数量少，JSON 足够 |
| 计数实时性 | 异步同步 | 实时读 Interaction Redis，Feed 表只兜底 |
| inbox 容量 | 1000 条 | 超过后旧内容自然淘汰，符合 Feed 流特性 |
| 大V outbox 容量 | 2000 条 | 大V发帖频率高，保留更多用于读扩散 |

---

## 4. 与 data-model.md 的关系

- 本文件是 Feed 服务视角的方法论。
- `docs/design/data-model.md` 是全局数据模型总览，包含具体 SQL 示例。
- 两者有冲突时，以本文件（服务实现落地）为准，但需同步更新 `data-model.md`。
