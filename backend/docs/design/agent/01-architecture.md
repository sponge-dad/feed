# Agent 服务架构设计

> 描述 Agent 独立 HTTP 服务的组件划分、Eino Graph 编排结构、与 5 个下游 gRPC 服务的调用关系，以及工程目录与配置约定。

---

## 1. 概述与定位

Agent 服务是 `app/agent` 下的独立 Go 服务：

- **对外**：go-zero `rest` 提供 HTTP API（规划端口 8090），接口契约见 [08-api.md](./08-api.md)。
- **对内**：Eino Graph 承载「意图识别 → 决策 → 工具调用 → 汇总 → 方案 → 审批 → 执行 → 验证」流程；经 go-zero `zrpc` 客户端调用 feed/user/relation/interaction/comment 五个 gRPC 服务。
- **不做**：不直接访问业务数据库；不承载前端会话渲染；初期不做多 Agent 协作。

### 1.1 为什么 HTTP 层用 go-zero rest 而不是 Hertz

Eino 是纯 Go 库，与 HTTP 框架解耦。选 go-zero `rest` 的理由：与本仓库 Gateway 同栈（JWT 中间件、logx、配置加载可直接复用写法）、部署与运维心智一致。若未来接入 Eino 生态的 ADK/Hertz 示例，可只替换 HTTP 接入层，Graph 与工具层不受影响。

## 2. 架构与职责

### 2.1 组件图

```
                         ┌──────────────────────────────────────────┐
  HTTP(JWT)              │  app/agent                                │
 用户/运营 ────────────▶ │  ┌────────────┐   ┌─────────────────────┐ │
                         │  │ rest 层     │──▶│ Eino Graph          │ │
                         │  │ /sessions   │   │ ① intent 意图识别    │ │
                         │  │ /runs       │   │ ② plan/decide 决策   │ │
                         │  │ /approve    │   │ ③ tools 工具执行     │ │
                         │  └────────────┘   │ ④ aggregate 汇总     │ │
                         │        │          │ ⑤ propose 方案生成   │ │
                         │        ▼          │ ⑥ approval 审批中断★ │ │
                         │  ┌────────────┐   │ ⑦ execute 执行       │ │
                         │  │ svc 上下文  │   │ ⑧ verify 验证        │ │
                         │  │ zrpc 客户端 │   └─────────────────────┘ │
                         │  │ ChatModel   │            │              │
                         │  └────────────┘            │ CheckPoint    │
                         └───────┬───────────────────┬┴──────────────┘
                    zrpc(etcd 2479)                  │
        ┌──────┬─────────┬───────┴──┬──────────┐     ▼
      user   relation   feed   interaction  comment   MySQL feed_agent
     (9001)  (9002)    (9003)    (9005)     (9004)    + Redis(摘要/偏好缓存)
                                                      + DeepSeek(OpenAI 兼容 API)
```

### 2.2 Graph 节点职责

| 节点 | 类型 | 职责 |
|---|---|---|
| `intent` | ChatModel | 从自然语言提取任务类型（recommend / diagnose / operate）与结构化条件（题材、时长、时间窗等），输出 JSON |
| `decide` | ChatModel + ToolsNode 循环 | ReAct 式决策：选择工具、生成参数；受最大迭代次数约束 |
| `tools` | ToolsNode | 执行工具，写 `agent_tool_calls` 留痕；结构化返回，不产生自然语言 |
| `aggregate` | Lambda | 合并去重工具结果，构造「事实上下文」（仅真实数据） |
| `propose` | ChatModel | 基于事实上下文生成结构化方案（推荐列表 / 诊断 JSON / 修改计划） |
| `approval` | 条件分支 + Interrupt | 方案含写操作 → 触发 `Interrupt`，持久化 checkpoint，等待人工审批（见 [04-approval.md](./04-approval.md)） |
| `execute` | ToolsNode（写工具） | 审批通过后按计划逐项调用写 RPC，幂等可重试 |
| `verify` | Lambda + 读工具 | 回读数据校验执行结果，生成执行报告 |

### 2.3 控制流关键规则

- **只读任务短路**：`intent` 判定无写操作时，`propose` 直接 → 结束，不经过 `approval/execute`。
- **迭代护栏**：`decide↔tools` 循环设 `max_iterations`（默认 6）与单 run 工具调用预算（默认 20 次），超限强制进入 `aggregate` 并在结果中声明数据不完整。
- **失败降级**：单个工具失败不终止 run；记录错误后由 `decide` 决定重试（≤2 次）或放弃该数据维度。

## 3. 数据模型

Agent 自身状态存 `feed_agent` 库（`agent_sessions` / `agent_runs` / `agent_tool_calls` / `recommendation_records`），Graph checkpoint 序列化后存入 `agent_runs`。详见 [03-state-session.md](./03-state-session.md)。

## 4. 接口与契约

- 对外 HTTP：见 [08-api.md](./08-api.md)。
- 对内工具 → RPC 映射：见 [02-tools.md](./02-tools.md)。
- LLM：`eino-ext/components/model/openai`，`base_url` 指向 DeepSeek 的 OpenAI 兼容端点；`api_key` **仅**从环境变量 `AGENT_LLM_API_KEY` 读取，配置文件与代码禁止出现明文密钥。

## 5. 错误码

沿用 `common/errorx` 体系，为 Agent 预留独立码段（建议 60000-60999）：

| 码 | 含义 |
|---|---|
| 60001 | 会话不存在 |
| 60002 | run 不存在或状态不允许该操作 |
| 60003 | 审批状态冲突（重复审批/已过期） |
| 60004 | LLM 调用失败（含超时） |
| 60005 | 工具调用预算耗尽 |
| 60006 | 下游服务不可用（降级返回部分结果） |

## 6. 缓存与一致性

- 近期会话摘要、长期偏好读多写少，走 Redis cache-aside（key 约定见 [03-state-session.md](./03-state-session.md)）。
- checkpoint 写 MySQL 为准；恢复执行前重读校验 run 状态，防止重复执行（幂等详见 [04-approval.md](./04-approval.md)）。

## 7. 测试策略

- **单元**：`internal/logic` 下用 mock ChatModel（固定返回 tool_calls JSON）+ 内存工具 stub，验证 Graph 路由、迭代护栏、审批分支。
- **集成**：`app/agent/tests/`，`agent-test.yaml` 指向 `feed_agent_test` 库 + Redis DB1；下游 RPC 用真实服务或 gRPC stub server。
- **回放测试**：以 `agent_tool_calls` 留痕数据回放工具结果，验证 `propose` 的 grounding（输出中的数字必须能在工具结果中找到，见 [07-observability.md](./07-observability.md)）。

## 8. 演进与 TODO

- [ ] 评估 Eino ADK（ReAct Agent 封装）替代手写 `decide↔tools` 循环。
- [ ] SSE 流式输出（首版同步 + 轮询，见 [08-api.md](./08-api.md)）。
- [ ] 长期偏好升级为向量检索（当前为结构化偏好表）。

### 工程目录（规划）

```
app/agent/
├── agent.go                 # 服务入口
├── etc/agent.yaml           # 配置（LLM base_url、下游 etcd、MySQL/Redis）
├── internal/
│   ├── config/
│   ├── handler/             # rest 路由处理
│   ├── logic/               # 会话/run/审批业务逻辑
│   ├── graph/               # Eino Graph 装配、节点实现、checkpoint 存取
│   ├── tools/               # 工具定义与 RPC 封装（见 02-tools.md）
│   ├── model/               # feed_agent 四表 model（goctl 生成 + 扩展）
│   └── svc/                 # ServiceContext：zrpc 客户端、ChatModel、model
└── tests/                   # 集成测试
```

## 关联文档

- [Agent 服务总览](./00-overview.md)
- [工具契约](./02-tools.md)
- [状态与会话设计](./03-state-session.md)
- [审批流程设计](./04-approval.md)
- [对外 API](./08-api.md)
- [系统架构设计](../architecture.md)
