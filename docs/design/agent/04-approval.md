# Agent 人工审批流程设计

> 定义写操作的人机协作机制：Agent 生成修改计划 → Eino Interrupt 暂停并持久化 → 用户审批 → Resume 恢复执行 → 回读验证。任何写操作**必须**经过本流程，无例外。

---

## 1. 概述与定位

触发条件：`propose` 节点产出的方案包含任一写工具调用（[02-tools.md](./02-tools.md) §4.2）。只读任务不进入本流程。

设计目标：

1. **不可绕过**：写工具在无审批上下文时直接拒绝（错误码 60003）。
2. **可中断可恢复**：审批等待期间服务可重启，checkpoint 从 `agent_runs` 恢复。
3. **精确到条目**：用户可整单批准/拒绝，也可剔除部分条目后批准（部分批准）。
4. **幂等执行**：恢复执行不重复生效，单条目失败可重试。

## 2. 架构与职责

### 2.1 状态机（`agent_runs.status`）

```
running ──(方案含写操作)──▶ awaiting_approval ──approve──▶ executing ──▶ succeeded / failed
   │                              │        └─reject──▶ rejected
   └──(纯只读)──▶ succeeded        └─(超时 24h)──▶ expired
```

- `awaiting_approval → approved/rejected` 仅允许**发起该 run 的用户本人**（或 operator 角色的会话属主）操作，服务端校验 `agent_runs.user_id == JWT.user_id`，防越权审批。
- 状态迁移采用 `UPDATE ... WHERE status='awaiting_approval'` 条件更新，受影响行数为 0 即返回 60003（重复审批/已过期）。

### 2.2 时序

```
用户            rest 层               Graph                    下游 RPC
 │ 指令          │                     │                         │
 │──────────────▶│── run.start ───────▶│── 读工具 × N ───────────▶│
 │                │                     │  propose: 生成计划       │
 │                │◀── Interrupt ───────│  (checkpoint 落库,       │
 │◀─ 202 + plan ──│   status=awaiting   │   status 变更同事务)     │
 │  审批 UI 展示   │                     │                         │
 │─ approve ─────▶│── 校验+加锁 ────────▶│── Resume ──────────────▶│
 │                │  agent:run:lock     │  execute: 写工具逐条      │
 │                │                     │  verify: 回读校验 ───────▶│
 │◀─ 执行报告 ─────│◀────────────────────│                         │
```

### 2.3 审批计划（ApprovalPlan）结构

`Interrupt` 时随 run 一并持久化、并在 API 返回给前端：

```json
{
  "plan_id": "1948391xxxx",
  "run_id": "1948390xxxx",
  "summary": "计划修改 8 个视频标题",
  "expires_at": 1753776000000,
  "items": [
    {
      "item_id": "p1",
      "tool": "update_video_meta",
      "target": {"feed_id": 1024},
      "before": {"title": "旧标题"},
      "after":  {"title": "新标题"},
      "reason": "点击率低于同类均值，标题缺少关键信息"
    }
  ]
}
```

规则：

- **必须**含 `before`（来自读工具的真实当前值）与 `after`（拟改值），供审批者对比；`before` 取不到则该条目标记为不可执行。
- 条目数上限默认 20；超过则要求 Agent 拆分为多个 run（防「一句话改全站」）。

### 2.4 恢复与执行

1. `approve` 请求校验属主与状态 → 抢 `agent:run:lock:{run_id}`（Redis SETNX，60s）防并发恢复。
2. 反序列化 checkpoint → Eino `Resume`，携带用户决定（整单/部分条目）。
3. `execute` 节点逐条调用写工具：每条执行前后各写一条 `agent_tool_calls` 留痕；单条失败记录后继续（不回滚已成功条目，最终报告标注 partial）。
4. **幂等**：写工具携带 `plan_item_id`；`execute` 先查该 item 是否已有成功留痕，有则跳过——服务重启后重放安全。
5. `verify` 节点用读工具回读目标数据，比对 `after` 值，生成执行报告写入 `agent_runs.result`。

## 3. 数据模型

复用 `agent_runs`（status/checkpoint/result）与 `agent_tool_calls`（含 `plan_item_id` 于 input JSON 内），不新增表。DDL 见 [03-state-session.md](./03-state-session.md)。

## 4. 接口与契约

对外 API：`GET /runs/:id`（含 plan）、`POST /runs/:id/approve`、`POST /runs/:id/reject`，定义见 [08-api.md](./08-api.md)。

## 5. 错误码

| 码 | 场景 |
|---|---|
| 60002 | run 不存在 / 非本人 |
| 60003 | 状态冲突：重复审批、已过期、无审批上下文调用写工具 |
| 60007 | checkpoint 反序列化失败（版本不兼容），run 置 failed 并要求重新发起 |

## 6. 缓存与一致性

- checkpoint 与 status 变更在**同一事务**内落库，避免「状态已变但现场丢失」。
- 恢复锁仅防并发入口；真正的幂等靠 `plan_item_id` 留痕判断（锁过期也安全）。

## 7. 测试策略

- 单元：状态机非法迁移全拒绝；部分批准条目过滤正确。
- 集成：`awaiting_approval` 期间 kill 服务 → 重启 → approve → 恢复执行成功。
- 并发：同一 run 并发 approve 只生效一次；执行中重复 approve 返回 60003。
- 幂等：execute 中途 kill → 重放 → 已成功条目不重复执行。

## 8. 演进与 TODO

- [ ] 审批人与发起人分离（运营主管审批模式）：需引入角色/审批人字段，当前版本为发起人自审。
- [ ] 计划条目支持人工编辑 `after` 值后再批准（audit-edit 模式）。

## 关联文档

- [Agent 服务总览](./00-overview.md)
- [架构与编排设计](./01-architecture.md)
- [工具契约](./02-tools.md)
- [状态与会话设计](./03-state-session.md)
- [对外 API](./08-api.md)
