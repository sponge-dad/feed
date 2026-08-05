# GetFollowTimeline 关注流 N+1 大V判定

> 用户关注数较多、且前排关注对象中普通用户（非大V）占比高时，`GetFollowTimeline` 对关注列表逐一发起 `IsVip` 远程调用，产生 N 次 gRPC + N 次 Redis 往返，关注流接口延迟随非大V数量线性增长；最坏情况（关注满 5000 人且全为普通用户）仅 IsVip 调用就耗费约 10 秒。

---

## 1. 基本信息

- 编号：BUG-20260805-001
- 日期：2026-08-05
- 报告人：开发者（QTA bugbot 静态扫描 + 人工复核）
- 状态：已修复（A 类，代码缺陷；修复随本次提交落地）
- 影响范围：关注流接口 `GetFollowTimeline`（inbox 命中主路径）；关注数越大、非大V占比越高受影响越严重。inbox 为空走 `rebuildInbox` 分支不受影响。
- 类别：A 代码缺陷（性能 / N+1 RPC）

## 2. 现象与复现

- 现象：关注流接口 P99 延迟随用户关注数非线性上升；关注数千人且多为普通用户时，接口明显卡顿甚至超时。粗算：每次 `IsVip`（内部 `SISMEMBER` + 一次 `GET fans_count`）≈ 1~5ms，5000 次 ≈ 5~10s。
- 触发条件（三者缺一不可）：
  1. 用户关注数多：`GetFollows` 以 `PageSize:5000` 拉全量关注者。
  2. 前排关注对象中**非大V占比高**：`IsVip` 返回 `false` 时执行 `continue` 且不影响 `bigVCount`，循环不会提前退出，每一个非大V都是一次无谓 RPC。
  3. inbox 命中主路径：走 `rebuildInbox` 分支不调用 `IsVip`，不会复现。
- 环境：任意环境（本地 / 生产）均可能，取决于数据分布。
- 复现步骤：
  1. 构造用户 A 关注 ≥ 1000 人，且其中绝大多数为普通用户（大V粉丝数未达阈值）。
  2. 确保 A 的 inbox 非空（如已有历史点赞/关注产生的 inbox 条目）。
  3. 调用 `GetFollowTimeline(user_id=A)`。
  4. 观察：接口耗时 ≈ 非大V关注数 × 单次 IsVip 延迟；密切关注 `RelationRpc.IsVip` 调用次数。

## 3. 排查过程（时间线）

- **假设 1**：关注流延迟来自 outbox 拉取放大。
  - 验证：读 `getfollowtimelinelogic.go` 主路径，`rebuildInbox` 用 `errgroup` 并行拉所有关注者 outbox，仅对大V做"拉模式"取最新 N 条，outbox 拉取次数有 `followMaxBigV`(200) 上限，非瓶颈。
- **假设 2（命中）**：大V识别是逐个 RPC。
  - 实证：`getfollowtimelinelogic.go` 主路径 `for _, fid := range follows.FolloweeIds { ... IsVip(fid) ... }` 对每位关注者逐一调用 `RelationRpc.IsVip`；非大V `continue` 不退循环，导致关注列表中每个普通用户都产生一次跨服务 RPC。这就是经典 N+1。
  - 对比：`IsVip` 本身实现（`isVipLogic.go`）优先读 `user:vip_users` 集合、回退 `fans_count`、再回源 MySQL，判定口径正确，问题在**调用方式**而非判定逻辑。

## 4. 根因

- **直接原因**：在关注流读路径上，对关注列表做了 `for` 循环逐个 `IsVip` 远程调用（N+1 RPC），而非一次性批量判定。
- **根本原因**：大V判定能力只提供了单条 `IsVip` 接口，缺少批量入口；调用方在热路径上退化为循环调用。
- **证据**：
  - `backend/app/feed/rpc/internal/logic/getfollowtimelinelogic.go` 主路径逐条 `IsVip`。
  - `backend/app/relation/rpc/internal/logic/isVipLogic.go` 单条判定，Redis/DB 均支持批量，接口层面缺失批量封装。
  - 调用不收敛：`!vip.IsVip` 分支 `continue` 但不增加 `bigVCount`，循环对前排普通用户全部执行。

## 5. 处置方案（A 类：代码修复）

新增 `Relation.BatchIsVip` 批量大V判定接口，将 N 次 RPC 收敛为 1 次，内部三层逐级批量下探（与单条 `IsVip` 口径完全一致）：

1. **proto / client**：`api/proto/relation/relation.proto` 新增 `BatchIsVipReq{repeated int64 user_ids}` / `BatchIsVipResp{map<int64,bool> results}`，`relationclient` 同步 `BatchIsVip` 方法（入参去重、上限 5000，校验正整数）。
2. **批量判定逻辑** `batchIsVipLogic.go`：
   - 一层 `PipelinedCtx` 一次 pipeline 批量 `SISMEMBER user:vip_users`；
   - 二层 `MgetCtx` 一次批量读 `user:fans_count:{id}`，按 `Vip.FansThreshold` 判定，命中的大V补写回 `user:vip_users`；
   - 三层 `RelationModel.CountByFolloweeIds` 一次 SQL `group by` 回源统计粉丝数，回填 `fans_count` 与 `user:vip_users`；
   - 任一层 Redis/DB 异常均不阻断，下探到下一层，最终保证结果准确。
3. **model** `relationsmodel.go` 新增 `CountByFolloweeIds`（占位符参数绑定，避免 SQL 注入），一次 `WHERE followee_id IN (...)` 统计，替换 N 次 `CountByFolloweeId`。
4. **调用方改造** `getfollowtimelinelogic.go`：原先的 `for` 循环逐条 `IsVip` 替换为一次 `RelationRpc.BatchIsVip(follows.FolloweeIds)`，再遍历 `vipResp.Results[fid]` 收大V。

- 改动文件：
  - `api/proto/relation/relation.proto`
  - `app/relation/rpc/relationclient/relation.go`
  - `app/relation/rpc/internal/server/relationserver.go`（注册转发）
  - `app/relation/rpc/internal/logic/batchIsVipLogic.go`（新增）
  - `app/relation/model/relationsmodel.go`（新增 `CountByFolloweeIds`）
  - `app/feed/rpc/internal/logic/getfollowtimelinelogic.go`（调用方改造）
  - 测试桩：`app/feed/rpc/internal/logic/feed_logic_test.go`、`app/relation/rpc/internal/logic/followLogic_test.go`

> 说明：本次按需求**只做读路径收敛（消除逆操作/新缓存结构）**，未引入"关注时预存 per-user 大V集合"的写时优化（即 `user:{followerID}:follow_vips` 方案）。如后续关注数极大（数千人）且单批 `BatchIsVip` 的 Redis/DB 往返仍成瓶颈，可在此基础叠加该二级缓存（写时入集合、读时 `SMEMBERS` + 懒刷新兜底大V身份变更），见下方"后续优化"。

## 6. 验证与收敛

- **验证方法**：
  - 单测：运行 `go test ./app/feed/rpc/internal/logic/... ./app/relation/rpc/internal/logic/...`，覆盖 BatchIsVip 三层判定与 GetFollowTimeline 用批量结果收大V。
  - 性能核对：在构造的"关注 5000 人且全为普通用户"用例下，关注流接口 `IsVip` 远程调用次数由 ~5000 降为 1；端到端耗时由秒级降到常量级。
  - 一致性核对：制造 `user:vip_users` 缺失场景，验证 BatchIsVip 能经 `fans_count` / MySQL 回源给出正确结果。
- **回归关注点**：
  - 单条 `IsVip` 语义不变，现有依赖方不受影响。
  - `BatchIsVip` 入参校验（去重 / 上限 / 正整数）避免超大列表打爆 Redis/DB。
  - `rebuildInbox` 分支本就不调用 IsVip，改动无回归。

## 7. 后续优化（可选，未做）

- 写时预存大V集合：关注成功 / 取关时维护 `user:{followerID}:follow_vips`（Set）；读时 `SMEMBERS` 一次取回大V，并用 `SMISMEMBER user:vip_users` 懒校验兜底"关注后对象才升为大V"的遗漏。需处理大V身份变更的一致性问题，权衡存储成本（每用户约 大V数 × 8B）。

## 关联文档

- [Bug 总结 SOP](../agent/bug-summary-sop.md)
- 相关代码：`app/feed/rpc/internal/logic/getfollowtimelinelogic.go`、`app/relation/rpc/internal/logic/isVipLogic.go`、`app/relation/rpc/internal/logic/batchIsVipLogic.go`、`app/relation/model/relationsmodel.go`、`app/relation/rpc/relationclient/relation.go`、`api/proto/relation/relation.proto`
