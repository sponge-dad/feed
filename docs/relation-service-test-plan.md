# Relation 服务测试方案（基于异常现象驱动）

> 本方案基于 Relation 服务可能暴露的**业务异常、并发异常、数据不一致、性能与热点、安全与风控**等现象，设计可执行的测试用例矩阵。覆盖范围包括当前 `app/relation/rpc` 已实现的 gRPC 接口，以及未来引入消息队列、分库分表、统计表、风控系统后的扩展场景。

---

## 1. 测试目标与范围

### 1.1 测试目标

| 维度 | 目标 |
|------|------|
| 功能正确性 | Follow/Unfollow/GetFollows/GetFans/IsFollow/IsVip 接口按预期工作 |
| 数据一致性 | MySQL `relations` 表、Redis 关注/粉丝列表、粉丝数、大 V 集合四态一致 |
| 并发安全 | 幂等关注、并发取关、并发读写 Redis 缓存无竞态和脏数据 |
| 接口契约 | gRPC proto 字段、类型、错误码符合 `api/proto/relation/relation.proto` |
| 性能 | 明确写操作（Follow）和读操作（GetFans/IsVip）的 QPS、P99 基线 |
| 安全 | 禁止自关注、越权查询、未登录写、私密账号被绕过 |
| 容错 | MySQL/Redis/下游服务故障时行为可控、错误码透传 |
| 可观测性 | 日志、指标、链路可追踪关注关系变更和缓存失效路径 |
| 风控 | 批量关注/取关、机器人循环、关系遍历、恶意刷量可被识别或限制 |

### 1.2 被测接口与代码

| 接口 | 文件 | 核心逻辑 |
|------|------|----------|
| `Follow` | `app/relation/rpc/internal/logic/followLogic.go` | 参数校验、自关注拦截、幂等检查、DB 插入、Redis 异步更新 |
| `Unfollow` | `app/relation/rpc/internal/logic/unfollowLogic.go` | 参数校验、记录删除、Redis 异步更新 |
| `GetFollows` | `app/relation/rpc/internal/logic/getFollowsLogic.go` | Redis ZSet 优先，未命中回源 MySQL |
| `GetFans` | `app/relation/rpc/internal/logic/getFansLogic.go` | 同 GetFollows，方向相反 |
| `IsFollow` | `app/relation/rpc/internal/logic/isFollowLogic.go` | 循环单次 DB 查询（N+1 风险） |
| `IsVip` | `app/relation/rpc/internal/logic/isVipLogic.go` | Set 命中、fans_count 命中、DB 回源重建 |

### 1.3 已知架构风险（设计测试时重点覆盖）

1. **非原子幂等**：`Follow` 先 `FindOne` 再 `Insert`，高并发下唯一索引可能冲突，当前代码会把冲突当成错误返回。
2. **缓存异步更新**：`updateCacheAfterFollow/Unfollow` 在 goroutine 中执行，存在“DB 已写入但缓存未更新”的窗口。
3. **粉丝数无初始值**：`Incr`/`Decr` 在 key 不存在时从 0 开始，可能导致“先取关后关注”场景下粉丝数为负或错误。
4. **IsVip 重建回源只查 1000 条**：`rebuildFansCount` 调用 `GetFans(PageSize=1000)`，粉丝超过 1000 时统计会少计。
5. **IsFollow N+1**：批量 100 时会发起 100 次 DB 查询，存在性能和连接池风险。
6. **列表无总条数缓存**：`GetFollows/GetFans` 返回的 `Total` 仅是当前页长度，无法校验“粉丝数 = 粉丝列表总数”。
7. **缺少鉴权/风控/块名单/私密账号审批**：当前 Relation 服务未实现，需在 Gateway 或下游服务补充。

---

## 2. 测试环境与数据准备

### 2.1 本地依赖

```bash
cd /data/workspace/feed
make up   # 启动 MySQL / Redis / etcd
```

### 2.2 启动被测服务

```bash
# 启动 Relation RPC（必须已注册 ErrorInterceptor）
go run app/relation/rpc/relation.go -f app/relation/rpc/etc/relation.yaml

# 可选：启动 User RPC / Gateway，用于端到端验证
go run app/user/rpc/user.go -f app/user/rpc/etc/user.yaml
go run app/gateway/cmd/api/gateway.go -f app/gateway/etc/gateway.yaml
```

### 2.3 测试数据隔离

- **单元测试**：使用 `sqlmock` + `miniredis`，不依赖真实存储。
- **集成/并发/性能测试**：使用独立测试库 `feed_relation_test`：

```sql
DROP DATABASE IF EXISTS feed_relation_test;
CREATE DATABASE feed_relation_test DEFAULT CHARACTER SET utf8mb4;
USE feed_relation_test;
-- 执行 deploy/sql/relation.sql 中的 relations 建表语句
```

每次测试前后清理：

```bash
redis-cli --scan --pattern 'user:follow:*' | xargs -r redis-cli del
redis-cli --scan --pattern 'user:fans:*' | xargs -r redis-cli del
redis-cli --scan --pattern 'user:fans_count:*' | xargs -r redis-cli del
redis-cli del user:vip_users
mysql -uroot -p feed_relation_test -e 'TRUNCATE TABLE relations;'
```

---

## 3. 按现象分类的测试用例

### 3.1 关系状态异常

> 现象：重复关注、重复取消关注、同一用户同时存在多条相同关系记录、用户关注自己、已经拉黑的双方仍然可以建立关注关系。

| 用例编号 | 用例名称 | 前置条件 | 操作步骤 | 预期结果 | 验证方法 |
|----------|----------|----------|----------|----------|----------|
| R-001 | 重复关注幂等 | 用户 A 已关注 B | 再次调用 `Follow(A,B)` | 返回 `Success=true`，无新增 DB 记录 | 查 `relations` 表 `COUNT(*)=1`；Redis ZSet 中 member B 仅出现一次 |
| R-002 | 重复取关幂等 | 用户 A 未关注 B | 调用 `Unfollow(A,B)` | 返回 `Success=true`，无 panic/错误 | DB 无记录；Redis 无异常 key |
| R-003 | 并发关注唯一性 | A、B 无关系 | 100 个 goroutine 同时 `Follow(A,B)` | DB 最终只有 1 条记录；Redis 只 1 个 member；无唯一键冲突错误 | 等待后查 DB + Redis；断言无 `1062` 错误码 |
| R-004 | 并发取关唯一性 | A 已关注 B | 100 个 goroutine 同时 `Unfollow(A,B)` | 最终 DB 无记录；Redis 无 member；无重复删除错误 | 等待后查 DB + Redis |
| R-005 | 禁止自关注 | 任意用户 A | 调用 `Follow(A,A)` | 返回 `RelationSelf(11001)`，DB 无记录 | 断言错误码；查 DB |
| R-006 | 非法用户 ID | 无 | `Follow(-1,2)` / `Follow(0,2)` / `Follow(1,-2)` | 返回 `ParamError(2)` | 断言错误码 |
| R-007 | 拉黑后无法关注（预留） | 用户 A 被 B 拉黑 | 调用 `Follow(A,B)` | 返回 `Forbidden(4)` 或 `RelationTargetNotFound(11004)` | 需在 Gateway/用户服务实现黑名单后启用 |
| R-008 | 目标用户不存在 | 用户 B 未注册 | 调用 `Follow(A,B)` | 当前实现允许插入；若后续补充 User 服务校验，应返回 `RelationTargetNotFound(11004)` | 根据设计阶段决定是否启用 |

**并发关注唯一性关键代码检查点**：`followLogic.go:49-68` 必须能把 MySQL 唯一键冲突 `1062` 识别为已存在并返回成功。

---

### 3.2 并发操作下的状态一致性问题

> 现象：并发操作下结果与用户最后一次操作不一致、关注成功但查询仍显示未关注、取关成功后短时间内仍显示已关注、不同设备看到的关系状态不一致。

| 用例编号 | 用例名称 | 操作步骤 | 预期结果 | 验证方法 |
|----------|----------|----------|----------|----------|
| C-001 | 并发 Follow/Unfollow 最终态一致 | 50 个 goroutine 随机执行 `Follow(A,B)` 与 `Unfollow(A,B)`，最终记录最后 100 次操作 | 最终 DB 状态与最后一次操作一致；Redis 与 DB 一致；粉丝数非负 | 1. 用原子操作记录最后操作；2. 等待所有 goroutine + 缓存异步完成；3. 查 DB、Redis、粉丝数 |
| C-002 | 关注后立即查询 | `Follow(A,B)` 成功后立即 `GetFollows(A)` 和 `IsFollow(A,[B])` | 最终应显示已关注；允许短暂不一致，但需在 1s 内收敛 | 循环查询最多 1s，记录首次一致时间 |
| C-003 | 取关后立即查询 | `Unfollow(A,B)` 成功后立即 `GetFollows(A)` 和 `IsFollow(A,[B])` | 最终应显示未关注；1s 内收敛 | 同 C-002 |
| C-004 | 多设备并发交叉操作 | 模拟 2 个客户端交替 Follow/Unfollow 同一对关系 | 最终状态与最后一次操作一致；无“双发后仍然显示未关注” | 记录客户端操作顺序；最后校验 DB 与 Redis |
| C-005 | 读写并发 | 一个 goroutine 持续 Follow/Unfollow，多个 goroutine 持续读取列表 | 读结果可能短暂不一致，但不会出现空列表异常、负数粉丝数、重复 member | 跑 60s 后检查 Redis key 结构与粉丝数 |

---

### 3.3 关注数与粉丝数异常

> 现象：少计、多计、负数、长时间不更新，关系存在但计数没增加，关系删除但计数没减少，粉丝数与粉丝列表数量不一致。

| 用例编号 | 用例名称 | 操作步骤 | 预期结果 | 验证方法 |
|----------|----------|----------|----------|----------|
| N-001 | 粉丝数正确增加 | 10 个不同用户关注 B | `user:fans_count:B` = 10；`IsVip(B)` 根据阈值正确 | 查 Redis `GET user:fans_count:B`；调 `IsVip(B)` |
| N-002 | 粉丝数正确减少 | 上述 10 个用户全部取关 B | `user:fans_count:B` = 0 | 查 Redis |
| N-003 | 负数粉丝数防护 | 直接对不存在的 `user:fans_count:B` 执行 `DECR`（模拟缓存 miss 后取关） | 粉丝数不应变为负数；若出现负数，DB 回源或补偿后应修复 | 构造 Redis key 不存在时调用 `Unfollow(B的某个粉丝)`，然后查 `user:fans_count:B` 是否 ≥ 0 |
| N-004 | 粉丝数与粉丝列表一致 | 100 个用户关注 B；分页拉取全部粉丝列表求和 | 列表总数 == `user:fans_count:B` == DB `COUNT(*)` | 用 `GetFans(B, pageSize=20)` 遍历，与 DB 和 Redis 计数对比 |
| N-005 | 大 V 阈值边界 | 粉丝数 = 阈值 - 1、阈值、阈值 + 1 分别测试 | 阈值及以上 `IsVip=true`，以下 `IsVip=false`；`user:vip_users` 增删正确 | 调 `IsVip` 并查 Redis Set |
| N-006 | 大 V 掉粉后移出集合 | B 粉丝数刚好阈值，再取关一个 | `IsVip(B)` 变 false；`user:vip_users` 移除 B | 查 Redis Set |
| N-007 | 关注数正确性 | 用户 A 关注 50 人，取关 10 人 | `GetFollows(A)` 总长度 = 40；DB 记录数 = 40 | 查 DB + 接口 |

---

### 3.4 消息异步处理异常（若后续引入 MQ）

> 现象：同一关系事件重复处理、部分事件丢失、处理顺序错误、消息堆积、DB 状态已变但统计长时间未同步。

| 用例编号 | 用例名称 | 操作步骤 | 预期结果 | 验证方法 |
|----------|----------|----------|----------|----------|
| M-001 | 重复消费幂等 | 向 MQ 投递 3 条相同的 `Follow(A,B)` 事件 | 最终 DB 只有 1 条记录；粉丝数只增加 1 | 查 DB + 粉丝数 |
| M-002 | 消息乱序最终一致 | 先投递 `Unfollow(A,B)`，再投递 `Follow(A,B)` | 最终状态为已关注 | 按消息 ID 去重或按时间戳覆盖 |
| M-003 | 消息丢失补偿 | 随机丢弃 10% 的 Follow 事件 | 通过定时对账任务，最终 DB 与 Redis 一致 | 统计前对比 DB 记录与 Redis 列表/计数 |
| M-004 | 堆积恢复 | 暂停消费者 5 分钟，累积 10 万事件后恢复 | 消费完毕后 DB/Redis/计数一致；消费延迟可监控 | 恢复后跑对账脚本 |
| M-005 | 统计表延迟同步 | 写入 `relations` 后，统计表更新延迟 | 对账任务发现差异后自动补偿 | 对比 `relations` 与 `relation_stats` 表 |

---

### 3.5 缓存一致性问题

> 现象：缓存状态与 DB 不一致、删除关注后缓存仍命中旧数据、关注成功后缓存仍显示未关注、热点用户缓存失效时请求集中、单个用户关系缓存占用内存过大。

| 用例编号 | 用例名称 | 操作步骤 | 预期结果 | 验证方法 |
|----------|----------|----------|----------|----------|
| K-001 | 关注后缓存命中 | `Follow(A,B)` → `GetFollows(A)` → 再次 `GetFollows(A)` | 第二次读命中 Redis，不再查询 DB | 启用 SQL 日志/慢查询监控，断言第二次无 DB 查询 |
| K-002 | 取关后缓存失效 | `Follow(A,B)` → `Unfollow(A,B)` → `GetFollows(A)` | 列表为空；Redis 中 `user:follow:A` 无 B | 查 Redis ZSet |
| K-003 | 缓存穿透保护 | 对无任何关注用户 X 调用 `GetFollows(X)` | 返回空列表，且不会反复查询 DB | 可设置空值缓存或布隆过滤器；监控 DB 查询次数 |
| K-004 | 缓存雪崩防护 | 大量热点用户 key 同时过期 | 通过互斥锁或随机 TTL，避免 DB 被打挂 | 批量设置同一 TTL 过期，压测 GetFans |
| K-005 | 大 Key 内存控制 | 用户有 100 万粉丝 | Redis ZSet 内存可接受；提供分片或仅缓存 Top N 方案 | 监控 `MEMORY USAGE user:fans:xxx`；压测读取延迟 |
| K-006 | 缓存与 DB 对账 | 随机制造缓存不一致（如手动删除 Redis key） | 对账任务或下次读请求能重建正确缓存 | 执行对账脚本，对比 DB 与 Redis |
| K-007 | 删除后旧数据不返回 | 直接删除 DB 记录，保留 Redis 缓存 | 读接口应通过版本号/过期时间校验，不返回旧数据 | 若当前无版本号，建议新增 TTL 或主动失效 |

---

### 3.6 查询接口异常

> 现象：Feed 中部分用户关注状态缺失、同一页面大量调用 Relation 服务、批量查询结果不完整、部分用户状态返回错误、关注列表或粉丝列表响应缓慢。

| 用例编号 | 用例名称 | 操作步骤 | 预期结果 | 验证方法 |
|----------|----------|----------|----------|----------|
| Q-001 | IsFollow 批量完整性 | 批量查询 100 个 followee_ids，其中 50 已关注 | 返回结果 map 长度 = 100；每个 id 状态正确 | 断言 `len(results) == 100` |
| Q-002 | IsFollow 空列表 | 传入空 `followee_ids` | 返回 `ParamError(2)` 或空 map | 根据接口约定断言 |
| Q-003 | IsFollow 性能（N+1） | 批量 100、500、1000 个 id 压测 | 当前实现 N+1 明显；应给出优化项（IN 查询或 Redis Pipeline） | 监控 DB 查询次数和 P99 |
| Q-004 | Feed 场景批量关注状态 | 模拟 Feed 100 条帖子，批量查询当前用户是否关注所有作者 | 不缺失、不重复、结果正确 | 构造数据后批量调用 |
| Q-005 | 关注列表响应时间 | 用户关注 1000 人，查询第 1/50/100 页 | P99 < 100ms（缓存命中） | 用 ghz/自定义客户端压测 |
| Q-006 | 粉丝列表响应时间 | 大 V 粉丝 100 万，查询第 1/100/1000 页 | 大 V 场景需预热缓存；避免深度分页导致 DB 慢查询 | 压测并监控 Redis/DB 延迟 |

---

### 3.7 分页异常

> 现象：用户重复出现、部分用户被跳过、翻页过程中顺序变化、下一页为空但实际仍有数据、较大页码后查询明显变慢。

| 用例编号 | 用例名称 | 操作步骤 | 预期结果 | 验证方法 |
|----------|----------|----------|----------|----------|
| P-001 | 分页无重复 | 用户关注 50 人，pageSize=10 遍历 5 页 | 5 页结果合并后无重复 id；无遗漏 | 集合去重对比 |
| P-002 | 翻页不遗漏 | 准备 100 条按时间倒序的记录，pageSize=10 遍历 | 每页连续；最终合并 = 100 条 | 遍历断言 |
| P-003 | 翻页过程中新增关注 | 遍历关注列表时，另一线程不断新增/删除关注 | 允许出现重复或遗漏，但需保证无崩溃、无空指针；建议后续使用游标分页 | 观察是否出现异常、总页数是否合理 |
| P-004 | 大页码性能 | 查询第 1000 页（offset 很大） | 若使用 offset 分页，延迟应可接受；否则应引入游标/覆盖索引优化 | 压测 P99 |
| P-005 | 页码边界 | page=0、page=-1、pageSize=0、pageSize=200 | page=0 应归一化为 1；pageSize>100 应截断为 100 | 断言返回长度和页码 |
| P-006 | Total 字段正确 | 用户关注 25 人，pageSize=10 | 当前实现 `Total` 为当前页长度；若业务需要总数，应补充总条数缓存或独立 Count 接口 | 文档化当前行为 |

---

### 3.8 互关与好友关系异常

> 现象：双方已互相关注但未显示好友、单方面取关后仍显示好友、好友数与好友列表数量不一致。

| 用例编号 | 用例名称 | 操作步骤 | 预期结果 | 验证方法 |
|----------|----------|----------|----------|----------|
| F-001 | 互关注判定 | A 关注 B，B 关注 A | 双方互相关注状态为 true | 调 `IsFollow(A,[B])` 和 `IsFollow(B,[A])` |
| F-002 | 单方面取关后非好友 | A 关注 B 且 B 关注 A；A 取关 B | A 视角不再是好友；B 视角仍关注 A 但 A 未关注 B | 分别查询双方 IsFollow |
| F-003 | 好友数与列表一致（预留） | 若存在好友统计表/缓存 | 好友数 = 双向关注列表交集大小 | 构造 100 对互关，对比 count 与 list 交集 |
| F-004 | 好友状态缓存一致性 | 取关后检查双方好友缓存 | 缓存应同步失效，不返回旧的好友状态 | 查 Redis 或重新计算 |

---

### 3.9 数据库层面异常

> 现象：唯一键冲突、锁等待、事务超时、死锁、写入成功但返回失败、请求返回成功但数据没落库、主从读取时短时间查不到刚写入的数据。

| 用例编号 | 用例名称 | 操作步骤 | 预期结果 | 验证方法 |
|----------|----------|----------|----------|----------|
| DB-001 | 唯一键冲突不抛错 | 并发 Follow 导致 `1062 Duplicate entry` | 应识别为已存在，返回 `Success=true` | 断言无错误且 DB 仅 1 条 |
| DB-002 | 锁等待/超时 | 用极小锁等待超时配置压测写操作 | 出现锁等待时返回 `ServerError(1)`，服务不 panic | 监控 MySQL `Lock wait timeout` |
| DB-003 | 写入成功但返回失败 | 模拟 DB 连接在 commit 后断开 | 应通过重试/幂等/查询补偿，最终状态一致 | 使用 chaos 工具或代理注入 |
| DB-004 | 请求成功但数据未落库 | 模拟 DB 在 commit 前失败 | 应返回错误，不能返回成功但数据丢失 | 断言返回错误且 DB 无记录 |
| DB-005 | 主从延迟读不到 | 写入后立即从从库读 | 可能读到旧数据；业务应能容忍或采用读主库/缓存优先策略 | 当前实现缓存优先，本项可复测缓存未命中时 |
| DB-006 | 死锁检测 | 两个事务以相反顺序更新两对关系 | MySQL 自动检测死锁并回滚一个；服务应重试或返回可识别错误 | 构造交叉 Follow 事务 |
| DB-007 | 慢查询监控 | 深度分页/大 offset | 不应出现全表扫描；执行计划使用 `idx_follower_id`/`idx_followee_id` | `EXPLAIN` 检查 |

---

### 3.10 RPC 调用异常

> 现象：超时、重试后重复执行、上游请求已取消但 Relation 仍在处理、部分批量请求成功部分失败、服务实例之间返回结果不一致。

| 用例编号 | 用例名称 | 操作步骤 | 预期结果 | 验证方法 |
|----------|----------|----------|----------|----------|
| RPC-001 | 调用超时 | 客户端设置 50ms 超时，服务端注入 100ms 延迟 | 客户端收到超时错误；服务端仍在处理但不应产生重复数据 | 调用后查 DB 记录数 |
| RPC-002 | 重试幂等 | 客户端超时重试 3 次 `Follow(A,B)` | 最终只有 1 条 DB 记录 | 查 DB |
| RPC-003 | 上游取消后下游不继续 | 客户端发送 `Unfollow` 后立即取消 context | 服务端检测到 ctx cancel 后应停止后续 DB/Redis 操作 | 服务端日志/Metrics 验证 |
| RPC-004 | 多实例一致性 | 同时启动 3 个 Relation 实例，随机路由 | 任意实例返回的状态一致；全局 ID 无冲突 | 通过 etcd 或直连多实例测试 |
| RPC-005 | 错误码透传 | `Follow(A,A)` 经 Gateway 调用 | Gateway 返回 `RelationSelf(11001)` | 验证 HTTP 响应业务码 |
| RPC-006 | 部分失败（批量预留） | 若未来支持批量 Follow/Unfollow | 返回成功/失败明细；成功部分落库，失败部分可重试 | 设计阶段启用 |

---

### 3.11 权限与认证异常

> 现象：用户伪造身份替别人关注、未登录用户调用写接口、普通用户查询到不应公开的粉丝或关注信息、私密账号的关注申请被直接建立为已关注。

| 用例编号 | 用例名称 | 操作步骤 | 预期结果 | 验证方法 |
|----------|----------|----------|----------|----------|
| AUTH-001 | 未登录写操作 | 不带 JWT 调 Gateway 的 Follow 接口 | 返回 `Unauthorized(3)` | Gateway 测试 |
| AUTH-002 | 伪造 follower_id | 用户 A 的 token 请求 `Follow(B,C)` | 应使用 token 中的 user_id 作为 follower_id，忽略参数中的 B | Gateway 参数校验 |
| AUTH-003 | 查询他人私密列表 | 私密用户 P 的关注/粉丝列表 | 未授权用户应返回 `Forbidden(4)` 或空列表 | 用户服务补充私密状态后启用 |
| AUTH-004 | 私密账号需审批 | 私密账号 Q 收到 Follow 请求 | 不应直接写入 `relations` 已关注；应写入 `follow_requests` 待审批 | 设计阶段启用 |
| AUTH-005 | 越权取关 | 用户 A 的 token 请求 `Unfollow(B,C)` | 应拒绝或仅取关 A 自己的关系 | Gateway 测试 |

---

### 3.12 风控与反作弊

> 现象：短时间批量关注、批量取关、关注数异常增长、机器人循环关注和取关、恶意遍历用户关系、单个账号产生大量关系写请求。

| 用例编号 | 用例名称 | 操作步骤 | 预期结果 | 验证方法 |
|----------|----------|----------|----------|----------|
| RC-001 | 短时间内批量关注限流 | 用户 1 秒内关注 1000 人 | 超过阈值后返回 `TooManyReq(5)` | 压测并监控 |
| RC-002 | 机器人循环关注取关 | 同一账号对同一目标反复 Follow/Unfollow | 应被限流或标记异常 | 统计操作频率 |
| RC-003 | 关注数异常增长检测 | 用户 1 小时内粉丝从 0 涨到 10 万 | 触发风控告警或粉丝数审计 | 监控 + 对账 |
| RC-004 | 关系遍历防护 | 高频调用 `GetFans` 遍历全站用户 | 应限制频率或需要更高权限 | 压测 |
| RC-005 | 批量注册马甲刷粉 | 批量注册新用户并关注目标 T | 应识别设备/IP/行为模式并限制 | 风控测试环境 |
| RC-006 | 写请求单账号限流 | 单个用户持续发送写请求 | 达到限流阈值后拒绝，保护 DB | 令牌桶/漏桶测试 |

---

### 3.13 热点与大 V 场景

> 现象：大 V 粉丝列表查询超时、热点用户请求集中在少量数据库分片、某个 Redis key 访问量异常、粉丝数接口正常但粉丝列表接口不可用。

| 用例编号 | 用例名称 | 操作步骤 | 预期结果 | 验证方法 |
|----------|----------|----------|----------|----------|
| H-001 | 大 V 粉丝列表压测 | 用户 V 有 100 万粉丝，并发 1000 读 `GetFans(V)` | 缓存命中时 P99 < 50ms；缓存未命中时通过预热/降级避免 DB 被打挂 | ghz 压测 + Redis 监控 |
| H-002 | 热点用户分片均匀 | 若已分库分表：多个热点用户 ID 应落在不同分片 | 各分片 QPS 差异 < 30% | 监控分片流量 |
| H-003 | Redis 热 Key 监控 | 监控 `user:fans_count:xxx` 和 `user:vip_users` | 热 Key 应触发告警；可考虑本地缓存或分片 | 压测时观察 QPS |
| H-004 | 粉丝数 vs 粉丝列表可用性隔离 | 使 Redis 中粉丝列表 ZSet 异常，但 fans_count 存在 | `GetFans` 可降级读 DB；`IsVip` 仍可用 | 删除 ZSet 后测试 |
| H-005 | 大 V 缓存预热 | 服务启动/大 V 榜单更新时预热 Top 1000 用户缓存 | 预热完成后读请求命中率 > 95% | 统计缓存命中率 |

---

### 3.14 数据修复与运维异常

> 现象：关系表与统计表对不上、数据库与缓存对不上、历史事件重复回放、修复任务重复修改数据、部分分片修复成功而部分失败。

| 用例编号 | 用例名称 | 操作步骤 | 预期结果 | 验证方法 |
|----------|----------|----------|----------|----------|
| OP-001 | 关系表与统计表对账 | 随机制造 100 条不一致 | 对账脚本输出差异清单；修复后一致 | 编写 `scripts/reconcile-relation.sh` |
| OP-002 | DB 与缓存对账修复 | 手动篡改 Redis 缓存 | 修复任务根据 DB 重建缓存；不重复修改 DB | 修复后查 Redis |
| OP-003 | 历史事件重复回放 | 重复消费 MQ 中的 Follow 事件 10 次 | 幂等保证最终状态不变 | 同 M-001 |
| OP-004 | 部分分片修复成功 | 模拟分片 1 修复失败 | 记录失败分片，重试；不标记整体成功 | 修复任务日志 |
| OP-005 | 修复任务可观测 | 运行修复任务 | 可输出每批处理量、差异数、修复耗时、失败明细 | 日志/Metrics 检查 |

---

### 3.15 可观测性不足

> 现象：关系异常无法定位到具体请求、无法确认一次关注是否真正成功、计数错误无法追踪来源、重复消费无法判断发生在哪个事件、接口延迟升高但无法确定具体环节。

| 用例编号 | 用例名称 | 操作步骤 | 预期结果 | 验证方法 |
|----------|----------|----------|----------|----------|
| OBS-001 | 请求链路追踪 | 调用 `Follow` 后查看 trace | trace 包含 DB 查询、DB 写入、Redis 更新、MQ 投递 | 使用 Jaeger/SkyWalking 等 APM |
| OBS-002 | 关注成功可确认 | 调用 `Follow` 后查日志 | 日志包含 request_id、follower_id、followee_id、结果、耗时 | 日志字段检查 |
| OBS-003 | 计数错误溯源 | 制造粉丝数不一致 | 可通过日志/审计表追踪到是哪次 Incr/Decr 或消息消费导致 | 审计日志检查 |
| OBS-004 | 重复消费定位 | 同一事件消费多次 | 日志/Metrics 中可看到 `msg_id` 重复消费次数 | 消息系统消费位点监控 |
| OBS-005 | 延迟分解 | 对接口进行压测 | 可分解出 DB 延迟、Redis 延迟、RPC 序列化延迟、网络延迟 | 分布式 tracing + 服务端 histogram |
| OBS-006 | 告警覆盖 | 模拟 Redis/MQ 故障 | 触发对应告警（关注失败率、缓存命中率、消息堆积、DB 慢查询） | 告警规则测试 |

---

## 4. 测试类型与执行策略

### 4.1 单元测试（Unit Test）

**目标**：不依赖外部服务，验证每个函数/逻辑独立正确。

**必测文件与重点**：

| 文件 | 测试重点 |
|------|----------|
| `followLogic.go` | 参数校验、自关注拦截、幂等关注、唯一键冲突处理、缓存更新 |
| `unfollowLogic.go` | 未关注时返回成功、已关注时删除记录、缓存删除/计数扣减 |
| `getFollowsLogic.go` | 缓存命中/未命中、分页边界、page_size 限制、Total 字段 |
| `getFansLogic.go` | 同 GetFollows，方向相反；大 V 重建回源只查 1000 条的风险 |
| `isFollowLogic.go` | 批量查询、空列表、混合已关注/未关注 |
| `isVipLogic.go` | VIP 阈值边界、Set 缓存命中、fans_count 缓存命中、DB 回源重建 |
| `keys.go` | key 生成一致性、parseIds 边界 |

**推荐工具**：

```go
// 使用 gomock 模拟 RelationsModel
mockCtrl := gomock.NewController(t)
mockRelationModel := mock.NewMockRelationsModel(mockCtrl)

// 使用 miniredis 模拟 Redis
s := miniredis.Run()
defer s.Close()
```

### 4.2 集成测试（Integration Test）

**目标**：真实连接 MySQL + Redis，验证端到端流程和错误码透传。

参考 `relation-service-test-plan.md` 3.2 节示例。

### 4.3 并发测试（Concurrency Test）

**目标**：多 goroutine 同时操作同一条关系数据，验证幂等和缓存一致性。

重点用例：C-001 ~ C-005、R-003、R-004。

### 4.4 缓存一致性测试（Cache Consistency Test）

**目标**：Redis 缓存与 MySQL 数据保持一致，失效/更新路径正确。

重点用例：K-001 ~ K-007。

### 4.5 性能测试（Performance Test）

**目标**：明确 Relation 服务在不同负载下的吞吐和延迟基线。

| 接口 | 类型 | 重点 |
|------|------|------|
| `Follow` | 写密集型 | 唯一索引冲突、DB 写入、Redis ZSet 更新 |
| `Unfollow` | 写密集型 | 删除 + 缓存失效 |
| `GetFollows` / `GetFans` | 读密集型 | 缓存命中时性能、缓存未命中时 DB 回源 |
| `IsFollow` | 批量读 | 批量查询是否 N+1，可优化为 `IN` 查询 |
| `IsVip` | 读密集型 | Set 命中时极快，回源时较慢 |

**推荐工具**：`ghz` / `hey` / 自定义 Go 客户端。

```bash
ghz --proto api/proto/relation/relation.proto \
    --call relation.Relation/Follow \
    -d '{"follower_id":1,"followee_id":{{.Seq}}}' \
    -z 60s -c 100 \
    127.0.0.1:9002
```

### 4.6 容错测试（Fault Tolerance Test）

**目标**：依赖故障时服务不崩溃，行为可控。

| 故障 | 注入方式 | 期望行为 |
|------|----------|----------|
| MySQL 不可用 | 停止 mysql 容器或改错端口 | Follow/IsVip 返回 `ServerError`；不 panic |
| Redis 不可用 | 停止 redis 容器 | 主流程仍依赖 MySQL，应能降级；但 GetFollows 缓存未命中需回源 |
| MySQL 连接池耗尽 | 极小 MaxConns | 请求排队或返回 `ServerError`，不 OOM |
| etcd 不可用 | 停止 etcd | 服务自身应仍能启动（gRPC 直连），但 Gateway 服务发现失败 |

### 4.7 安全测试（Security Test）

重点用例：AUTH-001 ~ AUTH-005、R-005 ~ R-008。

### 4.8 混沌测试（Chaos Test）

使用 Toxiproxy / Chaos Mesh 注入网络延迟、丢包、CPU 抖动、容器重启，验证 DB-001 ~ DB-007、RPC-001 ~ RPC-006。

---

## 5. 测试优先级与排期

| 阶段 | 内容 | 优先级 | 状态 |
|------|------|--------|------|
| P0 | 补齐 Follow 唯一键冲突处理（R-003） | 高 | 待完成 |
| P0 | 补齐 IsVip 重建回源全量粉丝数逻辑（N-004 相关） | 高 | 待完成 |
| P0 | 单元测试覆盖 6 个 logic | 高 | 待完成 |
| P1 | 集成测试 + 并发测试（R/C/K 系列） | 高 | 待完成 |
| P1 | 缓存一致性测试（K 系列） | 高 | 待完成 |
| P1 | 数据库异常与 RPC 异常测试（DB/RPC 系列） | 高 | 待完成 |
| P2 | 性能基准测试（Q/P/H 系列） | 中 | 待完成 |
| P2 | 容错测试 | 中 | 待完成 |
| P3 | 安全测试（含 Gateway REST 鉴权） | 中 | 待完成 |
| P3 | 风控/反作弊测试（RC 系列） | 低 | 待完成 |
| P3 | 可观测性与数据修复测试（OBS/OP 系列） | 低 | 待完成 |

---

## 6. 测试脚本与工具建议

### 6.1 建议新增脚本

| 脚本 | 用途 |
|------|------|
| `scripts/test-relation.sh` | 一键运行单元测试、集成测试、清理环境 |
| `scripts/benchmark-relation.sh` | 准备测试用户、并发 Follow/读/IsFollow 压测、输出 QPS/P99/错误分布 |
| `scripts/reconcile-relation.sh` | 对账：对比 DB `relations`、Redis 列表/计数、VIP Set |
| `scripts/chaos-relation.sh` | 注入 MySQL/Redis/etcd 故障，验证降级 |

### 6.2 推荐测试框架与库

- `stretchr/testify`：断言
- `go-sqlmock` / `miniredis`：单元测试 mock
- `gomock`：model 层 mock
- `ghz`：gRPC 压测
- `Toxiproxy` / `Chaos Mesh`：网络/依赖故障注入
- `Jaeger` / `Prometheus` + `Grafana`：可观测性验证

---

## 7. 关键检查清单

- [ ] `make up` 成功，MySQL/Redis/etcd 运行正常。
- [ ] `relation.yaml` 配置正确，DSN 指向 `feed_relation` 库（或 `feed_relation_test`）。
- [ ] relation RPC 已注册 `serverinterceptors.ErrorInterceptor`。
- [ ] 测试数据库 `feed_relation_test` 已创建并隔离。
- [ ] 每次测试后清理 `relations` 表和 Redis 相关 key。
- [ ] 压测前预热缓存，避免冷启动影响结果。
- [ ] 压测后检查 Redis 内存和 MySQL 慢查询日志，排查异常。
- [ ] 关注 `IsVip` 重建逻辑只查 1000 条粉丝的缺陷，需在测试计划中重点覆盖。
- [ ] 关注 `Follow` 高并发下唯一键冲突未处理为成功的缺陷，优先修复。

---

## 8. 附录：现象 → 根因 → 测试项速查表

| 现象 | 可能根因 | 重点测试项 |
|------|----------|------------|
| 重复关注 | 幂等检查非原子 | R-001, R-003 |
| 同一用户多条相同记录 | 唯一索引缺失或并发绕过 | R-003, DB-001 |
| 用户关注自己 | 缺少校验 | R-005 |
| 拉黑后仍可关注 | 缺少黑名单校验 | R-007, AUTH-004 |
| 并发结果与最后一次操作不一致 | 缓存异步/非原子 | C-001, C-004 |
| 关注成功但查询未关注 | 缓存未刷新 | C-002, K-002 |
| 取关成功仍显示已关注 | 缓存未失效 | C-003, K-002 |
| 不同设备状态不一致 | 缓存/DB 同步延迟 | C-004, K-006 |
| 粉丝数少计/多计/负数 | Incr/Decr 无初始值或异步失败 | N-001 ~ N-007 |
| 粉丝数与粉丝列表不一致 | 无总条数缓存或缓存异步 | N-004, K-006 |
| 消息重复处理 | MQ 消费无幂等 | M-001, OP-003 |
| 消息丢失 | 消费失败未重试 | M-003 |
| 消息乱序 | 消费顺序未保证 | M-002 |
| 缓存与 DB 不一致 | 缓存失效失败 | K-001 ~ K-007 |
| 热点缓存失效雪崩 | 同一 TTL / 大 key | K-004, K-005, H-002 |
| Feed 状态缺失 | IsFollow N+1 或部分失败 | Q-001, Q-004 |
| 分页重复/跳过 | offset 分页 + 数据变动 | P-001 ~ P-004 |
| 大页码变慢 | 深度 offset | P-004 |
| 互关未显示好友 | 缺少双向判断 | F-001 ~ F-004 |
| 唯一键冲突 | 并发插入 | DB-001, R-003 |
| 锁等待/死锁 | 事务顺序/大事务 | DB-002, DB-006 |
| RPC 超时重试重复执行 | 超时 + 未幂等 | RPC-001, RPC-002 |
| 上游取消后仍处理 | 未监听 ctx cancel | RPC-003 |
| 伪造身份关注 | Gateway 未校验 token | AUTH-001, AUTH-002 |
| 批量刷粉 | 缺少限流/风控 | RC-001, RC-003, RC-005 |
| 大 V 列表超时 | 缓存未命中/深度分页 | H-001, H-004 |
| 热 Key 集中 | 未分片/未本地缓存 | H-002, H-003 |
| 关系表与统计表不一致 | 异步更新丢失 | M-005, OP-001 |
| 无法定位异常 | 缺少 trace/request_id | OBS-001, OBS-002 |

---

*版本：2026-07-21*
*维护：每次 Relation 服务增加新功能（黑名单、私密账号、MQ、分片、统计表）时，需同步更新本方案并补充对应测试用例。*
