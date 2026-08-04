# agent 设计目录

> 本目录存放 **Agent 服务（智能运营与推荐 Agent）** 的分模块设计文档。Agent 是一个独立的 Go + Eino 服务，通过自然语言接收运营/推荐任务，编排调用现有 5 个 gRPC 微服务完成「理解任务 → 选择工具 → 获取数据 → 分析结果 → 制定计划 → 请求审批 → 执行操作 → 验证结果」的完整闭环。

---

## 文档索引

| 文件 | 内容 |
|------|------|
| [00-overview.md](./00-overview.md) | Agent 定位、与 5 个微服务的关系、三版场景可行性结论与后端缺口汇总 |
| [01-architecture.md](./01-architecture.md) | 独立 HTTP 服务架构、Eino Graph 编排设计、目录结构、技术选型理由 |
| [02-tools.md](./02-tools.md) | Agent 工具清单：每个工具的 Schema、到现有 RPC 的映射、grounding 约束 |
| [03-state-session.md](./03-state-session.md) | 状态三层设计：Graph State、会话摘要、长期偏好；`feed_agent` 库四张表 DDL |
| [04-approval.md](./04-approval.md) | 人工审批流程：Eino Interrupt/Resume、计划持久化、恢复执行与结果验证 |
| [05-scenarios.md](./05-scenarios.md) | V1 个性化推荐 / V2 运营诊断 / V3 执行与审批 三版场景逐步设计 |
| [06-backend-gaps.md](./06-backend-gaps.md) | 后端扩展清单：`feeds` 表扩列、`UpdateFeed` RPC、轻量 stats 服务（SQL/proto 草案） |
| [07-observability.md](./07-observability.md) | 可观测性：工具调用留痕、run 追踪、token 成本统计、幻觉校验 |
| [08-api.md](./08-api.md) | Agent 对外 HTTP REST 接口、DeepSeek（OpenAI 兼容）配置、鉴权设计 |
| [09-product-requirements.md](./09-product-requirements.md) | 补充 FeedMind 的运营 Copilot 需求、后端产品化能力、分期路线与验收标准 |

## 阅读顺序

1. 先读 `00-overview.md`，了解 Agent 定位与「哪些场景现在能做、哪些要先补后端」。
2. 产品立项和研发排期读 `09-product-requirements.md`，确认需求、范围、依赖与验收标准。
3. 再读 `01-architecture.md` 与 `02-tools.md`，理解编排结构与工具面。
4. 之后读 `03-state-session.md` 与 `04-approval.md`，掌握状态与人机协作机制。
5. 按需查阅 `05-scenarios.md`（分版本实施）、`06-backend-gaps.md`（首批技术改造草案）。
6. 实现 HTTP 层与运维时参考 `08-api.md` 与 `07-observability.md`。

## 关联文档

- [design 目录索引](../README.md)
- [系统架构设计](../architecture.md)
- [服务拆分方案](../service-design.md)
- [数据模型](../data-model.md)
- [文档编写规范](../../agent/doc-writing-guide.md)
