# Agent 服务总览

> 定义智能运营与推荐 Agent 的定位、边界、与现有 5 个微服务的关系，并给出 V1/V2/V3 三版场景的可行性结论与后端缺口汇总，作为整个 `agent/` 设计目录的入口。

---

## 1. 概述与定位

### 1.1 是什么

Agent 服务是一个**独立部署的 Go 服务**（`app/agent`，规划 HTTP 端口 8090），用户通过自然语言下达推荐/运营任务，例如：

- 「给我推荐几部最近没看过的悬疑视频，不要太长。」
- 「找出最近七天播放量低于平均值、且完播率下降的视频，给出诊断。」
- 「把低点击率视频的标题全部优化一下。」（触发人工审批）

Agent 不是聊天机器人：它必须**基于工具返回的真实数据**完成分析与决策，涉及写操作时先生成计划、暂停等待人工审批，批准后才执行并验证结果。

产品立项建议以 **FeedOps Agent（智能内容运营 Copilot）** 为主目标：先做可信只读内容盘点，再补齐指标诊断与审批执行。普通用户的对话式推荐只作为 M0 工程验证，不将当前按发布时间排序的公共推荐池描述为个性化推荐。完整需求、优先级和验收标准见 [09-product-requirements.md](./09-product-requirements.md)。

### 1.2 技术选型（已锁定）

| 项 | 决定 | 理由 |
|---|---|---|
| 语言/编排 | Go + [Eino](https://github.com/cloudwego/eino)（Graph 编排 + Interrupt/Resume） | 与仓库单语言栈一致，复用 `zrpc` 客户端 |
| LLM | DeepSeek，经 `eino-ext/components/model/openai`（OpenAI 兼容端点） | 协议通用，可平滑替换 provider |
| HTTP 层 | go-zero `rest`（独立服务，不挂现有 Gateway） | 技术栈统一，独立部署与压测 |
| 下游调用 | go-zero `zrpc` 客户端 → 5 个 gRPC 服务 | 复用现有服务发现（etcd `127.0.0.1:2479`） |
| 状态存储 | 独立 MySQL 库 `feed_agent`（4 张表）+ Redis | 与业务库隔离 |

### 1.3 边界

- Agent **只做编排与决策**，不直接读写业务库（`feed_feed` 等），一切业务数据经 gRPC 工具获取/写入。
- Agent 自身的会话、运行、工具调用、推荐记录落 `feed_agent` 库。
- `common/` 包约束不变：Agent 作为消费方引用各服务的 pb/client，不反向侵入。

## 2. 架构与职责（摘要）

```
用户 ── HTTP ──▶ app/agent (go-zero rest, :8090)
                    │  Eino Graph：意图识别 → 决策/工具选择 → 工具调用
                    │             → 结果汇总 → 方案生成 → [审批中断] → 执行 → 验证
                    ▼  zrpc
   feed(9003) / user(9001) / relation(9002) / interaction(9005) / comment(9004)
                    ▼
        MySQL feed_agent（sessions/runs/tool_calls/recommendations） + Redis
```

详细架构见 [01-architecture.md](./01-architecture.md)。

## 3. 现有服务可复用的工具面

以下 RPC 均已核实存在于 `api/proto/**/*.proto`，可直接封装为 Agent 工具（完整 Schema 见 [02-tools.md](./02-tools.md)）：

| 现有 RPC | 工具用途 |
|---|---|
| `feed.GetRecommendTimeline` / `GetCityTimeline` / `GetUserFeeds` | 候选视频池 |
| `feed.GetFeed` / `BatchGetFeeds` | 视频元数据（标题/封面/作者/时间/状态） |
| `interaction.GetUserLikedFeeds` / `GetUserCollectedFeeds` | 用户互动历史（近似「看过」） |
| `interaction.BatchGetFeedStats` / `BatchGetUserInteractionStatus` | 点赞/收藏计数与状态 |
| `relation.GetFollows` / `IsFollow` | 关注关系（「关注的作者也发了」） |
| `user.GetUser` / `BatchGetUsers` | 作者信息 |
| `comment.GetHotComments` / `BatchGetCommentCount` | 评论热度（可选信号） |
| `feed.CreateFeed` / `DeleteFeed`、`interaction.Like/Collect` 等 | 写操作（V3，须经审批） |

## 4. 三版场景可行性结论

| 版本 | 场景 | 结论 | 依据 |
|---|---|---|---|
| V1-lite | 推荐「我没点赞/收藏过的近期视频」 | ✅ 业务微服务零改动可验证 | 仍需新建 Agent 服务、状态库与运行时；点赞+收藏仅近似「看过」 |
| V1-full | 按「悬疑 / 电影 / 未观看 / 时长短」过滤推荐 | ⚠️ 需后端补列 | `feeds` 表无 `category`/`tags`/`duration_sec`；无观看记录 |
| V2 | 播放量/点击率/完播率诊断 | ❌ 当前不可行 | 全仓库无播放事件、点击事件、完播数据，需新建 stats 服务 |
| V3 | 修改标题/标签/上下架/推荐位（含审批） | ⚠️ 审批可行，执行缺口 | Eino Interrupt/Resume 支持审批；但 Feed 服务无 `UpdateFeed` RPC、无推荐位概念 |

**幻觉红线**：凡后端不存在的指标（播放量、CTR、完播率等），Agent 必须如实声明「当前系统无该指标」，禁止编造。该约束写入方案生成 Prompt 与工具输出校验，见 [05-scenarios.md](./05-scenarios.md) 与 [07-observability.md](./07-observability.md)。

## 5. 后端缺口汇总

| 缺口 | 影响版本 | 工作量 | 详案 |
|---|---|---|---|
| `feeds` 表缺 `category` / `duration_sec` / `tags` | V1-full | 小（加列+回灌） | [06-backend-gaps.md](./06-backend-gaps.md) §2 |
| 无 `UpdateFeed` RPC（仅有 Create/Delete） | V3 执行 | 小 | [06-backend-gaps.md](./06-backend-gaps.md) §3 |
| 无播放记录 / 统计服务（播放量、CTR、完播率） | V2 全部 | 大（独立子项目） | [06-backend-gaps.md](./06-backend-gaps.md) §4 |
| `FeedStatus` 无「下架」态、无推荐位模型 | V3 执行 | 小 | [06-backend-gaps.md](./06-backend-gaps.md) §3 |
| 无运营角色/RBAC、审批人与业务侧权限校验 | 所有运营场景 | 中 | [09-product-requirements.md](./09-product-requirements.md) §8.1 |
| 无内容条件检索/批量运营查询接口 | V1-full / V2 | 中 | [09-product-requirements.md](./09-product-requirements.md) §4.2 |
| 无持久化 run 调度、事件流与重启恢复实现 | 所有版本 | 中 | [09-product-requirements.md](./09-product-requirements.md) §6.1 |
| Feed 事件缺可靠 outbox，缺更新/版本化事件 | V2 / V3 数据一致性 | 中 | [09-product-requirements.md](./09-product-requirements.md) §8.1 |

## 6. 实施顺序建议

1. **阶段一**：搭 Agent 骨架（Graph + 工具 + 四张表 + HTTP API），交付 V1-lite，端到端验证工具调用/状态/留痕。
2. **阶段二**：后端补 `feeds` 三列与 `UpdateFeed`，Agent 侧解锁 V1-full 与 V3（审批 + 标题/标签修改）。
3. **阶段三**：独立排期建 stats 服务，解锁 V2 运营诊断。

## 7. 演进与 TODO

- [ ] 阶段一完成后，将 `docs/design/agent/` 登记进 `AGENTS.md` §2 必读表（若 Agent 服务进入常态开发）。
- [ ] stats 服务立项后，另建 `docs/design/stats/` 设计目录。
- [ ] 多 Agent（Supervisor 模式）暂不设计，单 Agent 验证闭环后再评估。

## 关联文档

- [目录索引](./README.md)
- [架构与编排设计](./01-architecture.md)
- [工具契约](./02-tools.md)
- [场景分版设计](./05-scenarios.md)
- [后端缺口清单](./06-backend-gaps.md)
- [产品需求与建设路线](./09-product-requirements.md)
- [系统架构设计](../architecture.md)
- [服务拆分方案](../service-design.md)
