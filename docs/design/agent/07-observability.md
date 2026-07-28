# Agent 可观测性设计

> 定义 Agent 的留痕、追踪、成本统计与幻觉校验机制：让每一次推荐/诊断/执行都能回答「模型看到了什么数据、做了什么调用、花了多少钱、结论是否可溯源」。

---

## 1. 概述与定位

Agent 的故障面比普通服务多一层：除了代码 bug，还有 Prompt 缺陷、工具描述歧义、模型幻觉。可观测性目标：

1. **全留痕**：每次工具调用、每轮 LLM 交互可回放。
2. **可溯源**：输出中的每个事实（feed_id、数字）能定位到具体工具调用。
3. **可算账**：token 成本按 run/session/用户聚合。
4. **可报警**：幻觉率、失败率、预算耗尽率异常时可发现。

## 2. 架构与职责

### 2.1 三级追踪 ID

```
session_id ──▶ run_id ──▶ tool_call seq / llm_call seq
```

- HTTP 层生成 `run_id` 后注入 `context`，logx 全链路携带（`logx.WithContext`）。
- zrpc 调下游时将 `run_id` 放入 gRPC metadata（`x-agent-run-id`），下游服务日志可反向关联。

### 2.2 工具调用留痕

每次工具执行写一行 `agent_tool_calls`（DDL 见 [03-state-session.md](./03-state-session.md)）：`tool_name / input / output / ok / error_msg / cost_ms / seq`。要点：

- 写留痕失败**不阻塞** run（日志记录），但 grounding 校验会降级为「仅 Prompt 约束」。
- `output` 超 64KB 截断存摘要 + 原始长度，防大结果撑爆表。

### 2.3 LLM 调用留痕

Eino callback（`callbacks.Handler`）挂载到 ChatModel：记录每轮的 model、输入消息摘要（哈希 + 截断，**不落全量 Prompt**，避免撑库与泄露）、输出摘要、`prompt_tokens/completion_tokens`、耗时。token 累加进 `agent_runs.token_input/token_output`。

### 2.4 指标（logx + 可选 Prometheus）

| 指标 | 类型 | 说明 |
|---|---|---|
| `agent_run_total{status}` | counter | run 结果分布（succeeded/failed/rejected/expired…） |
| `agent_run_duration_ms` | histogram | run 端到端耗时 |
| `agent_tool_call_total{tool,ok}` | counter | 工具成功率 |
| `agent_tool_cost_ms{tool}` | histogram | 工具耗时 |
| `agent_llm_tokens_total{direction}` | counter | token 消耗 |
| `agent_budget_exhausted_total` | counter | 预算耗尽次数（Prompt/工具描述质量信号） |
| `agent_grounding_violation_total` | counter | 幻觉校验命中次数 |

## 3. 数据模型

复用 `agent_runs` / `agent_tool_calls`，不新增表；LLM 留痕首版写结构化日志（logx），有分析需求再落表。

## 4. 接口与契约（幻觉校验）

`propose` 输出进入终态前经过**代码级 grounding 校验**（Lambda 节点）：

1. **ID 溯源**：结果中出现的所有 `feed_id` 必须 ∈ 本 run 工具结果的 feed_id 并集；越界 → 剔除该条目并计 `grounding_violation`。
2. **数字溯源**：`reason/problems` 文本中的数值（正则抽取）必须能在事实上下文中精确匹配（允许百分比换算误差 ±1%）；不匹配 → 要求模型重生成（≤1 次），仍失败则删除该数字表述。
3. **指标白名单**：V2 前，输出中出现「播放量/点击率/完播率」字样且带具体数值 → 直接拦截（系统无此数据）。
4. **evidence 完整性**：推荐条目缺 `evidence` 字段 → 重生成。

校验结果（违规类型、次数）写入 `agent_runs.result` 的 `_audit` 字段，供离线分析 Prompt 质量。

## 5. 错误码

可观测性组件自身失败不产生对外错误码；只降级并记日志（留痕是尽力而为，业务正确性由状态机保证）。

## 6. 缓存与一致性

指标为进程内计数 + 拉取模式，无一致性要求；留痕表按月归档（`created_at` 分区或定期迁移冷表）。

## 7. 测试策略

- grounding 校验单测：构造含越界 feed_id / 编造数字 / 白名单指标的模型输出，断言拦截行为。
- 留痕完整性集成测试：一次 V1 run 后，`agent_tool_calls` 行数 = 实际工具调用数，seq 连续。
- 成本统计测试：mock ChatModel 返回固定 usage，断言 run 级 token 汇总正确。

## 8. 演进与 TODO

- [ ] 接入 OpenTelemetry：Graph 节点级 span（Eino callback 已具备切入点）。
- [ ] 幻觉校验从「正则抽数字」升级为结构化输出强制（JSON Schema 约束 + 字段级溯源）。
- [ ] 按用户/租户的 token 配额与限流。

## 关联文档

- [Agent 服务总览](./00-overview.md)
- [架构与编排设计](./01-architecture.md)
- [状态与会话设计](./03-state-session.md)
- [场景分版设计](./05-scenarios.md)
