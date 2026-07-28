# Comment 数据模型方法论

> 本文档定义 Comment 服务的数据存储规范与约束，覆盖 MySQL 表结构、索引选择、
> 楼中楼存储模型、Redis 结构，以及一条强制约束：主键必须使用 Snowflake。
> 完整 DDL 以 `deploy/sql/comment.sql` 为准，本文用表格描述字段与决策。

---

## 1. MySQL：`comments` 表

### 1.1 字段规范

| 字段 | 类型 | 约束 | 说明 |
|------|------|------|------|
| id | BIGINT UNSIGNED | 主键，**应用层 Snowflake 写入，禁止自增** | 评论全局唯一 ID |
| feed_id | BIGINT UNSIGNED | NOT NULL，索引 | 所属帖子 ID |
| user_id | BIGINT UNSIGNED | NOT NULL，索引 | 评论者 ID |
| content | VARCHAR(1000) | NOT NULL DEFAULT '' | 评论内容，超长由 logic 层拦截 |
| root_id | BIGINT UNSIGNED | NOT NULL DEFAULT 0 | 根评论 ID；一级评论=0 |
| parent_id | BIGINT UNSIGNED | NOT NULL DEFAULT 0 | 直接回复的评论 ID；一级评论=0 |
| reply_user_id | BIGINT UNSIGNED | NOT NULL DEFAULT 0 | 被回复者 ID（@谁）；一级评论=0 |
| like_count | INT UNSIGNED | NOT NULL DEFAULT 0 | 点赞数，由 Interaction 服务异步同步 |
| reply_count | INT UNSIGNED | NOT NULL DEFAULT 0 | 子回复数，仅根评论维护 |
| status | TINYINT | NOT NULL DEFAULT 1 | 1:正常 2:已删除（软删除） |
| created_at | DATETIME | NOT NULL DEFAULT CURRENT_TIMESTAMP | 创建时间 |

> 注意：内部 RPC 的 `created_at` 统一用**毫秒级 Unix 时间戳**（int64），此处 MySQL 用 `DATETIME` 仅作持久化，转换在 model 层完成。

### 1.2 强制约束：主键必须 Snowflake

**冲突来源**：`docs/design/data-model.md` 第 4.1 节将 `id` 写为 `AUTO_INCREMENT`，
这与 `AGENTS.md` 4.5（业务实体 ID 用 Snowflake、禁止自增）及 `data-model.md` 通用约定冲突。

**约束**：
1. `comments.id` 由 `common/idgen` 在应用层生成后写入，表结构**不得**带 `AUTO_INCREMENT`。
2. 实现前须修正 `deploy/sql/comment.sql` 与 `docs/design/data-model.md` 第 4.1 节，去掉自增属性。
3. 单机开发固定机器 ID=1；生产多实例须通过环境变量注入唯一机器 ID（`AGENTS.md` 9.3）。

### 1.3 软删除约定

- 与 Feed/Comment 一致，采用 `status` 软删除（=2 表示已删除），**不物理删除**。
- 物理删除会破坏楼中楼引用关系（子回复的 `root_id`/`parent_id` 指向已删记录），故一律软删。
- 所有读查询必须带 `status = 1` 过滤。

---

## 2. 索引设计与选择理由

| 索引 | 字段 | 用途 | 理由 |
|------|------|------|------|
| PRIMARY | id | 按 ID 定位单条 | 主键 |
| KEY idx_feed_root | (feed_id, root_id, created_at) | 查某帖一级评论与某楼全部回复 | 一级评论走 `root_id=0`；子回复走 `root_id=?`；created_at 用于排序 |
| KEY idx_root | (root_id, created_at) | 按根评论查全部子回复 | 独立维度，避免一级查询干扰 |
| KEY idx_user | (user_id) | 「我的评论」类查询（可选） | 用户维度检索 |

**设计原则**：
- 楼中楼查询靠 `root_id` 而非递归 JOIN，因此复合索引 `(feed_id, root_id, created_at)` 是核心。
- 不在 `parent_id` 上建独立索引：分页与预览都按 `root_id` 聚合，不需要逐层 `parent_id` 检索。

---

## 3. 楼中楼存储模型（两层平铺）

视觉上无限嵌套，**存储上只有两层**。这是 Comment 服务最关键的设计决策。

规范示例：

| 视觉（无限嵌套） | 存储（实际两层） |
|------|------|
| 评论 A | id=1, root_id=0, parent_id=0 |
| ├─ B 回复 A | id=2, root_id=1, parent_id=1, reply_user=A |
| ├─ C 回复 B | id=3, root_id=1, parent_id=2, reply_user=B |
| └─ D 回复 C | id=4, root_id=1, parent_id=3, reply_user=C |
| 评论 E | id=5, root_id=0, parent_id=0 |
| └─ F 回复 E | id=6, root_id=5, parent_id=5, reply_user=E |

字段语义约束：

| 字段 | 约束 |
|------|------|
| root_id | 同楼所有回复指向同一最顶层评论 ID；一级评论 root_id=0 |
| parent_id | 指向直接回复的评论 ID；用于"回复 @某人"与前端缩进展示 |
| reply_user_id | 被回复者 ID；前端展示「张三 回复 李四：xxx」 |

**为什么不用真递归树**：查询简单（一条 SQL 查全楼）、分页容易、避免递归 JOIN 性能问题。新增"三级/四级嵌套"只是 `parent_id` 指向更深的节点，存储结构不变。

---

## 4. Redis 数据结构

Redis 只缓存两类数据，不缓存评论内容本身（内容走 MySQL）。

| Key | 类型 | 说明 | TTL | 写入时机 |
|-----|------|------|-----|---------|
| `comment_count:{feed_id}` | String | 帖子评论总数（含一级+子回复的可见数） | 1 小时 | 发表/删除后更新；读未命中从 MySQL 重建 |
| `comment_hot:{feed_id}` | ZSet | 热门评论，member=comment_id，score=like_count | 5 分钟 | 评论点赞数变化时更新；读未命中从 MySQL 按 like_count 重建 |

**约束**：
- Key 命名以本文档为准，新增须先补文档再写代码（`dev-guidelines.md` 6）。
- `comment_count` 是计数值，删除时需正确减（见 `03-delete.md`）。
- `comment_hot` 容量与排序口径见 `05-stats.md`，不在本文展开。

---

## 5. 与 `data-model.md` / `comment.sql` 的对应关系

| 本文定义 | 权威落地文件 | 备注 |
|---------|-------------|------|
| comments 表结构 | `deploy/sql/comment.sql` | 须修正 AUTO_INCREMENT → Snowflake |
| Redis 两个 key | `docs/design/data-model.md` 第 4.4 节 | 已定义，保持一致 |
| 楼中楼双字段 | `docs/design/data-model.md` 第 4.2 节 | 已定义，保持一致 |

**开发前检查清单**：
- [ ] `deploy/sql/comment.sql` 中 `comments.id` 已去掉 `AUTO_INCREMENT`
- [ ] `docs/design/data-model.md` 第 4.1 节已同步修正
- [ ] 索引 `(feed_id, root_id, created_at)`、`(root_id, created_at)`、`(user_id)` 已建立
- [ ] 所有写库路径通过 `common/idgen` 生成 id
