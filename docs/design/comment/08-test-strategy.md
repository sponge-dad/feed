# Comment 测试策略方法论

> 本文档定义 Comment 服务的测试分层与关键用例，供各模块实现时配套测试。
> 遵循 `AGENTS.md` 第 5 节测试规范：单元 / 集成 / 并发 / 压测分层，测试先行。

---

## 1. 测试分层与位置

| 类型 | 位置 | 依赖 | 目标 |
|------|------|------|------|
| 单元测试 | `app/comment/rpc/internal/logic/*_test.go` | miniredis + model stub | 单 logic 分支正确 |
| 集成测试 | `app/comment/rpc/tests/*_test.go` | 真实 MySQL + Redis + User stub | 端到端读写闭环 |
| 并发/缓存测试 | `app/comment/rpc/tests/*_test.go` | 真实存储 | 计数一致、缓存收敛 |
| 压力测试 | `scripts/benchmark-comment.sh` | 真实服务 | P99/吞吐基线 |

---

## 2. 各模块测试映射

| 模块 | 重点测试点 |
|------|-----------|
| 数据模型 | Snowflake id 唯一；索引查询命中；软删不影响正常读 |
| 发表评论 | root/parent/reply_user_id 推导正确；父评论归属校验；跨帖回复拒绝；reply_count+1 |
| 删除评论 | 软删；权限拒绝；根评论删除后整楼折叠、count 减量含子回复；幂等 |
| 评论列表 | 一级分页；N+1 已避免（批量 User）；预览前 N 条；查看全部回复 cursor 正确；已删不可见 |
| 计数与热门 | comment_count 与 MySQL COUNT 一致；like_count 同步后 hot 更新；Feed 计数同步 |
| 缓存一致性 | 写后 INCR；DEL 后重建一致；负数保护；Redis 不可用降级读 MySQL |
| MQ 事件 | CREATE/DELETE 事件字段正确；发送失败不阻断；幂等消费；乱序安全 |
| 测试策略 | 全链路用例覆盖上表 |

---

## 3. 关键用例清单（必覆盖）

**楼中楼正确性**
- 一级评论 → 回复 → 再回复更深层，验证所有子回复 `root_id` 相同、`parent_id` 指向直接父。
- 列表预览仅取每楼前 N 条，且为时间正序。
- 查看全部回复 cursor 翻页无重复无遗漏。

**删除与子树**
- 删子回复：仅该条不可见，兄弟可见，根 reply_count-1，comment_count-1。
- 删根评论：整楼折叠（方案 A），comment_count 减（1+可见子回复数），hot 移除该楼。
- 重复删除：幂等，计数不二次减少。
- 非作者删除：返回 `CommentNoPermission`。

**计数一致性**
- 并发发表同一楼 100 次，comment_count（含缓存 INCR 与读时重建）与 MySQL 最终一致。
- 删根评论后：缓存减量与 MySQL 重建值一致（均**排除根已删楼**内的子回复，见 `03-delete.md` / `05-stats.md` 口径）。测试对账用的 `SELECT COUNT` 必须采用同一 `LEFT JOIN` 口径，否则会对不上。
- 并发删除/发表混合，计数无负数（非负保护 + "增量仅当 key 存在"双兜底，见 `06-cache.md` 第 4/5 节）。
- 缓存 DEL 后重建值与 MySQL 一致。

**跨服务**
- User 服务返回后，列表用户信息正确填充、不 N+1（断言 User 批量调用次数）。
- User 不可用时列表降级成功。
- interaction.event 同步后 `like_count` 与 `comment_hot` 更新正确。

**边界**
- 空帖列表返回 total=0。
- 超长评论（>1000）被拒 `CommentTooLong`。
- 跨帖回复被拒。
- 父评论已删时回复被拒 `CommentParentNotFound`。

---

## 4. 测试环境隔离

遵循 `AGENTS.md` 5.2：

- 独立配置：`comment-test.yaml`。
- MySQL：`feed_comment_test` 独立库（执行 `deploy/sql/comment.sql` 建表）。
- Redis：独立 DB（如 `select 2`）或 key 前缀 `test:` 隔离。
- User 服务：用 stub/mock 返回固定用户，避免依赖真实服务。
- MQ：测试用独立 Topic 或关闭发送断言（校验事件构造正确即可）。

---

## 5. 压测指标

| 场景 | 工具 | 目标 |
|------|------|------|
| 发表评论 | ghz / hey | P99 < 30ms |
| 列表首屏 | ghz / hey | P99 < 50ms |
| BatchGetCommentCount | ghz / hey | 高吞吐，P99 < 20ms |

压测脚本：`scripts/benchmark-comment.sh`，复用 `scripts/benchmark-relation.sh` 风格。

---

## 6. 提交前检查

- `go build ./...` 通过
- `go test ./app/comment/rpc/...` 通过（含 `-race`）
- `gofmt -w .` 已执行
- 新逻辑均有对应单元测试；修复类改动有回归测试
