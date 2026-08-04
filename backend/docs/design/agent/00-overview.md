# FeedMind Agent 总览与定位

> 说明「短视频内容理解与 Feed 智能助手」的项目定位、现状差距、第一版范围、角色权限与标识体系，是本目录其余文档的入口。

---

## 1. 概述与定位

FeedMind Agent 在现有 Feed 系统（Gateway + User/Relation/Feed/Comment/Interaction）之上补齐三块能力：

1. **内容理解**：视频发布后异步抽帧、转写、识别，生成结构化「内容画像」。
2. **行为数据**：补齐曝光、播放、有效播放、完播、快速划走等埋点，聚合为内容指标与用户兴趣画像。
3. **智能助手**：以单 Agent + 受控 Tool 的方式，把自然语言问题翻译成对业务 RPC 的调用，再把结构化结果组织成回答。

边界必须明确：

| 项 | 结论 |
|------|------|
| 在线刷流 | 仍由 Feed RPC 完成，Agent **不在** Feed 在线链路上 |
| 视频理解 | 由 Content Worker **离线异步**完成，Agent 只读已生成的画像 |
| 数据可信性 | 所有数字来自 RPC/DB，模型**只做意图识别、Tool 选择与语言组织** |
| 写能力 | 第一版 Agent **全部 Tool 只读**，不改标题/标签/权重/兴趣/推荐池 |
| 身份来源 | 只信任 Gateway JWT 注入 Context 的身份，不信任模型或请求体里的 `user_id` |

## 2. 现状与差距

现有能力（已实现，见 `docs/design/feed/`、`docs/design/interaction/`）：

- 推拉结合 Timeline：`inbox:{userID}` / `outbox:{userID}` / `feed:recommend` / `feed:city:{cityCode}`（`app/feed/rpc/internal/keys/keys.go`）。
- 异步解耦：`feed-created`、`feed-deleted`、`comment-event`、`interaction-event` 四个 Topic（`common/event/`）。
- 点赞/收藏 Redis 先行 + MQ 落库，评论计数镜像列增量更新（`app/feed/rpc/internal/worker/worker.go`）。

差距清单（本项目要补的洞）：

| 编号 | 差距 | 现状证据 | 影响 |
|------|------|----------|------|
| G1 | `request_id` 未真正生成 | `common/response/response.go` 只从 `ctx.Value("request_id")` 取值，全链路无人写入，故恒为空 | 无法按请求排障，推荐解释无锚点 |
| G2 | Feed 返回不带来源 | `FeedBrief` 无 source 字段，三个 timeline logic 未打标 | 无法回答「为什么推荐」 |
| G3 | 无 Feed 请求 Trace | 无任何请求级召回/合并记录 | 无法诊断 Timeline 结果 |
| G4 | 无曝光/播放类埋点 | 仅有 like/collect/comment 事件 | 创作者分析、兴趣画像缺原料 |
| G5 | 无内容理解结果 | `feeds` 表仅 title/description/media_urls/cover_url | 无法语义检索，无法解释内容主题 |
| G6 | 无兴趣画像 | 无相关表与 Redis 结构 | 推荐解释只能到「同城/关注」层|
| G7 | 无语义检索能力 | 仅 MySQL 索引，无 ES / 向量检索 | 字幕、摘要不可检索 |
| G8 | 可观测性缺失 | 各 `etc/*.yaml` 无 `Prometheus`、无 `Telemetry`；Prometheus仅为间接依赖 | 无指标、无分布式链路 |

## 3. 建设目标（用户故事）

| 角色 | 故事 | 依赖能力 | 对应文档 |
|------|------|----------|----------|
| 普通用户 | 「找一些西安周边适合新手的露营视频」 | 内容画像 + 混合检索 | [05](./05-content-search.md) |
| 普通用户 | 「为什么给我展示这条视频」 | Feed来源 + 请求 Trace + 兴趣画像 | [07](./07-recommend-reason.md) |
| 普通用户 | 「我最近对什么内容感兴趣」 | 兴趣画像摘要 | [06](./06-user-interest.md) |
| 创作者 | 「分析我这条视频为什么播放效果不好」 | 行为指标 + 同类对比 | [08](./08-creator-metrics.md) |
| 创作者 | 「系统认为我的视频属于什么类别」 | 内容画像查询 | [04](./04-content-analysis.md) |
| 内部人员 | 「用 request_id 查一次 Feed 请求走了哪些数据源」 | 请求标识链路 + Trace | [02](./02-request-trace.md) |

## 4. 第一版范围

**做**：

| 序号 | 交付 | 说明 |
|------|------|------|
| S1 | 请求标识链路 | Gateway 生成 `request_id`，gRPC Metadata 透传，日志与响应回写 |
| S2 | Feed 来源标记 + 请求 Trace | 五种来源枚举，请求级召回/合并记录 |
| S3 | 行为埋点闭环 | `feed-behavior-event` Topic、批量上报接口、幂等消费、小时级指标 |
| S4 | 内容分析流水线 | FFmpeg + ASR + OCR + 多模态摘要 → `feed_content_profiles` |
| S5 | 混合语义检索 | 关键词 + 向量 + 标签召回，后端过滤与排序 |
| S6 | 用户兴趣画像 | 规则权重 + 时间衰减，Redis 实时 + MySQL 快照 |
| S7 | 创作者作品分析 | 指标计算 + 同类匿名对比 + 漏斗诊断 |
| S8 | 单 Agent + 8 个只读 Tool | Eino 编排，Run / ToolCall 全量留痕 |
| S9 | 可观测性 | Prometheus 指标、OTel 链路、结构化日志字段 |

**不做**（明确排除，避免范围蔓延）：

- Agent 写操作、Agent 直连 Redis/MySQL。
- 多 Agent、以及超出注册 Tool 范围的自主动作。
- 深度学习召回/精排模型，或替换现有推拉结合 Timeline。
- 内容画像人工修改与审核工作流（第一版只收集创作者反馈）。
- 自建 Python 推理服务（ASR/OCR/多模态一律走外部服务接口）。
- 秒级实时看板（第一版指标粒度为小时聚合 + Redis 实时累加）。

## 5. 角色与权限

| 能力 | 普通用户 | 创作者（本人内容） | 内部用户 |
|------|:---:|:---:|:---:|
| 语义检索公开 Feed | ✅ | ✅ | ✅ |
| 查看自己的推荐原因 | ✅ | ✅ | ✅ |
| 查看自己的兴趣摘要 | ✅ | ✅ | ✅ |
| 查看他人兴趣画像 | ❌ | ❌ | ❌ |
| 查看作品内容画像全文（字幕/OCR） | ❌ | ✅ | ✅ |
| 查看作品行为指标 | ❌ | ✅ | ✅ |
| 查看同类平均指标 | ❌ | ✅（匿名聚合） | ✅ |
| 查看他人作品明细指标 | ❌ | ❌ | ✅ |
| 查看内部排序分数 / 行为明细 | ❌ | ❌ | ✅ |
| 查看 Feed 请求 Trace | ❌ | ❌ | ✅ |

- 身份来源：Gateway JWT → Context（`app/gateway/internal/middleware/auth.go` 的 `UserIDFromContext`）→ gRPC Metadata → 下游 `ctx`。
- 内部身份：第一版用 Agent/Content 配置中的 `InternalUserIDs` 白名单判定，后续接入 `users.role`。详见 [13-security.md](./13-security.md)。

## 6. 标识体系

| 标识 | 生成者 | 生命周期 | 用途 | 客户端可见 |
|------|--------|----------|------|:---:|
| `request_id` | Gateway | 一次 HTTP 业务请求 | 业务排障、Feed Trace 锚点、推荐原因锚点 | ✅（响应头 + 响应体） |
| `trace_id` | go-zero Telemetry（OTel） | 一次分布式调用链 | APM 链路串联 |❌（仅日志/采集端） |
| `run_id` | Agent RPC | 一次 Agent 执行 | 执行留痕、结果轮询 | ✅ |
| `event_id` | 事件生产者（uuid v4） | 一条 MQ 事件 | 消费幂等去重 | ❌ |

四者互不替代：`request_id` 是业务口径，`trace_id` 是链路口径，`run_id` 是任务口径，`event_id` 是消息口径。约定与实现见 [02-request-trace.md](./02-request-trace.md)。

## 7. 文档地图

```
00-overview（本文）
   ├── 01-architecture      服务拆分、部署形态、改造点、决策记录
   ├── 02-request-trace     request_id 链路 + Feed 来源 + 请求 Trace   ← 阶段一
   ├── 03-behavior-event    埋点契约、幂等消费、指标聚合               ← 阶段一
   ├── 04-content-analysis  FFmpeg/ASR/OCR/多模态、状态机、幂等        ← 阶段二
   ├── 05-content-search    索引、混合召回、业务过滤、评测             ← 阶段三
   ├── 06-user-interest     权重、时间衰减、Redis/MySQL 双写           ← 阶段三
   ├── 07-recommend-reason  来源枚举 → reason_codes → 文案            ← 阶段四
   ├── 08-creator-metrics   指标口径、同类对比、漏斗诊断               ← 阶段四
   ├── 09-agent-service     Eino 编排、Run 状态机、Tool 契约、限额     ← 阶段四
   ├── 10-data-model        MySQL / Redis / ES 结构
   ├── 11-api               HTTP + RPC 契约、错误码
   ├── 12-observability     指标、日志、链路                          ← 阶段五
   ├── 13-security          权限、注入、SSRF/RCE、脱敏
   ├── 14-acceptance-test   测试分层与验收标准映射
   └── 15-roadmap           五阶段实施与演示脚本
```

## 8. 演进与 TODO

- 内容画像纠错工作流（创作者反馈 → 人工审核 → 覆盖标签）。
- 兴趣画像由规则权重演进为离线模型特征。
- 指标从小时聚合演进到分析型存储（ClickHouse / Doris）。
- Agent 从单 Agent 演进为「路由 Agent + 领域 Agent」，并引入受控写操作与审批。

---

## 关联文档

- [FeedMind Agent 目录索引](./README.md)
- [源需求文档](../../agent需求文档.md)
- [系统架构](../architecture.md)
- [服务拆分](../service-design.md)
- [全局数据模型](../data-model.md)
- [Feed 服务设计](../feed/README.md)
- [Interaction 服务设计](../interaction/README.md)
