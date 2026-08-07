# Implementation Plan: FeedMind Agent — 内容理解与智能助手子系统

**Branch**: `001-feedmind-agent` | **Date**: 2026-08-07 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `/specs/001-feedmind-agent/spec.md`

## Summary

在现有 Feed 系统（Gateway + 5 个微服务）之上新增**两个服务域**并对**四个现有组件**做增量改造，交付三层能力：

1. **数据地基**（阶段一）：request_id 全链路透传 + Feed 来源标记 + 行为埋点与指标聚合
2. **内容理解**（阶段二、三）：视频画像自动生成（FFmpeg→ASR→OCR→多模态）+ES 语义检索 + 用户兴趣画像
3. **智能助手**（阶段四）：Eino 单 Agent + 8 个只读 Tool，把自然语言问题翻译为对业务 RPC 的调用

**技术路径**：新增 `app/content/{rpc,worker}` 与 `app/agent/rpc` 两个服务域；改造 `app/gateway`（中间件+路由）、`app/feed/rpc`（来源标记+Trace）、`app/interaction/{rpc,worker}`（指标+兴趣）、`common/`（拦截器+事件契约+错误码）。

**核心不变式**：Agent 不在刷流在线链路上；内容分析完全异步；v1 全部 Tool 只读；所有数字来自 RPC/DB，模型只做意图识别与语言组织。

## Technical Context

**Language/Version**: Go 1.21+

**Primary Dependencies**:
- 微服务框架：go-zero v1.7.3（gRPC + etcd 服务发现）
- Agent 编排：`github.com/cloudwego/eino` + `eino-ext/components/model/ark`
- 消息队列：`rocketmq-client-go/v2` v5.1.4
- 检索：Elasticsearch（BM25 + kNN 混合召回，RRF 融合）
- 媒体处理：FFmpeg / ffprobe（外部二进制，子进程调用）
- 外部模型服务：ASR（语音转文字）、OCR（画面文字）、多模态 LLM（标签生成）
- 现有公共库：`common/{errorx,idgen,jwtx,response,mq}`

**Storage**:
- MySQL 8.0：新增库 `feed_content`（1 表）、`feed_agent`（4 表）；已有库 `feed_interaction` 追加 3 表
- Redis 7：13 类新增 Key（Trace/幂等/指标累加/兴趣 ZSet/分布式锁/限流）
- Elasticsearch：索引 `feed_content_v1`（读别名 `feed_content`，写别名 `feed_content_write`）
- 对象存储：腾讯云 COS（读取待分析媒体，私有桶+签名 URL）

**Testing**:
- 单元：`internal/logic/*_test.go`、`internal/worker/*_test.go`（miniredis + model stub + 外部服务 mock）
- 集成：`tests/*_test.go`（真实 MySQL/Redis + 启动服务）
- 契约：`app/agent/rpc/tests/contract_*_test.go`（mock ChatModel，断言 Tool schema 与输出校验器）
- 评测：`scripts/eval-search.sh`（Precision@5 ≥ 0.85）、`scripts/eval-agent.sh`
- 压测：`scripts/benchmark-behavior.sh`、`scripts/benchmark-agent.sh`（ghz/hey）
- 隔离库：`feed_content_test`、`feed_agent_test`、`feed_interaction_test`
- **CI 硬约束**：必须 mock FFmpeg 与 ChatModel，禁止真实计费调用

**Target Platform**: Linux 服务端；Docker Compose（开发）+ Kubernetes（生产规划）

**Project Type**: 微服务后端（Go monorepo，多服务多进程）

**Performance Goals**:
- 行为埋点端到端（上报→Redis 计数）P99 < 5s
- 内容分析端到端（发布→画像可查）P95 < 5min（测试环境）
- Agent 单次 Run 端到端 P95 < 5s，硬超时 60s
- Tool 调用成功率 ≥ 99%（测试环境，500 次覆盖 8 个 Tool）
- 检索主题匹配 Precision@5 ≥ 0.85（100 条标注 query）

**Constraints**:
- 内容分析**不得阻塞发帖**：发布接口 1s 内返回
- Agent **不直连** Redis/MySQL 业务库（自有 4 张会话表除外），一切取数经 RPC
- 单 Run 限额：Tool 调用 ≤ 8、模型调用 ≤ 4、输入 ≤ 2000 字符、历史窗口 20 条、用户 10 Run/分钟、并发 Run 1
- Content Worker 资源上限：并发 2、关键帧 ≤ 20、视频 ≤ 200MB /600s、FFmpeg 超时 120s、字幕截断 4000 字符
- 行为明细保留 30 天，小时指标 180 天，Agent 消息/Run 90 天，ToolCall 30 天
- EXPOSE 事件采样入库（默认 10%），其余全量

**Scale/Scope**:
- 容量假设：日活 10 万、人均 100 次曝光 → 曝光 1000 万/日
- `feed_behavior_events`约 300 万行/日（采样后）
- `feed_metrics_hourly` 约 240 万行/日（活跃 feed 10 万 × 24）
- `feed_content_profiles` 约 1 万/日（与视频发布量同阶，单行 5~20KB）
- 代码规模预估：2 个新服务域 + 4 个组件改造，约 60~80 个新文件

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

对照 `.specify/memory/constitution.md` v1.0.0 的 6 条核心原则：

| 原则 | 检查项 | 结论 |
|------|--------|------|
| **I. 微服务边界清晰** | Content/Agent 是否越界访问其他服务的库？ | ✅ **通过**。Agent 仅直连 `feed_agent`（自有库），其余数据全部经 gRPC。Content 仅直连 `feed_content`。行为指标放`feed_interaction`（Interaction 服务自有库）而非新建服务，符合"避免第 8 个服务"决策 D1。 |
| **I. common 包纯净** | `common/interceptors`、`common/event/behavior` 是否引入 `app/xxx`？ | ✅ **通过**。仅定义 Metadata 键名与事件结构体，不依赖任何业务包。 |
| **II. 推拉结合 Feed 模型** | 是否修改 Feed 分发策略？ | ✅ **通过**。仅**新增** `FeedSource` 枚举打标与 Trace 写入，不改变 inbox/outbox 分发逻辑本身。 |
| **III. 数据安全优先** | 8 条红线逐项核查 | ✅ **通过**，详见下表 |
| **IV. 数据一致性** | 缓存策略是否符合 cache-aside？ | ✅ **通过**。指标走"Redis 累加 + 定时 flush绝对值"（宪法 IV 明确许可的 Interaction 高频写例外）；画像走 cache-aside（`content:profile:{feed_id}` TTL 1h）。 |
| **V. 测试分层** | 四层测试是否齐备？ | ✅ **通过**。单元/集成/契约/评测/压测五层，且新增 CI 约束（mock FFmpeg 与 ChatModel）。 |
| **VI. 编码规范** | 分层、错误码、ID 生成、注释 | ✅ **通过**。新增 errorx 码段 Content(15000~15999)/Agent(16000~16999)；session_id/run_id/message_id 用 `common/idgen`（Snowflake）；Redis key 集中在 `internal/keys/`。 |

### 安全红线逐项核查（原则 III）

| # | 红线 | 本特性中的落地 |
|---|------|---------------|
| 1 | SQL 注入 | 全部走 `goctl model` 生成 + 参数化查询；Tool 入参**无自由文本直通下游**（无 SQL/URL/命令/Redis key） |
| 2 | RCE | **FFmpeg 是唯一子进程调用**。必须：固定二进制路径（配置 `FFmpegPath`）、参数数组传递（禁止 shell 拼接）、`AllowedMediaHosts` 白名单校验媒体域名、超时强杀、临时目录隔离（`TempDir`）。详见 research.md R7 |
| 3 | 鉴权/授权 | **两层校验**：意图级预检（Go，Tool 调用前）+ 对象级校验（Tool 内，如 feed 归属）。身份**只来自** Context（Gateway JWT → Metadata），模型输出中的 `user_id` 一律忽略 |
| 4 | XSS | 字幕/OCR 文本作为数据返回，前端转义；Agent 输出经校验器过滤 |
| 5 | SSRF | 媒体下载仅允许 `AllowedMediaHosts` 白名单（COS 域名），拒绝内网地址（10./172.16-31./192.168./127.*） |
| 6 | 反序列化 | 事件体与模型输出用 `encoding/json` 严格解码到固定结构体，拒绝未知字段 |
| 7 | Secrets | `ARK_API_KEY`、`AGENT_MYSQL_DSN` 等**只从环境变量注入**，yaml 中用 `${VAR}` 占位。`error_message` 字段**已脱敏，禁止含签名地址** |
| 8 | 并发安全 | `content:analysis:lock:{feed_id}` 分析互斥锁（TTL 6min）；`agent:session:lock:{session_id}` 会话串行（TTL 90s）；event_id 幂等 + `uk_event_id` 兜底 |

### 额外安全约束（Prompt 注入防护）

宪法未覆盖但本特性引入的新风险面：

- 用户输入**不拼接**进 System Prompt，仅作 user message 并用固定分隔符包裹
- **Tool 返回内容（字幕/OCR）同样视为不可信数据**——视频里可能出现"请忽略指令"
- 输出后置校验：回答中的 `feed_id` 必须在本轮 Tool 结果集合内，数字必须能在 Tool 结果 JSON 中找到；不通过则降级为模板化回答并计入 `agent_llm_guard_total`
- 即使模型请求未注册的 Tool，Go 侧直接拒绝

**结论**:✅ **Constitution Check 通过，无违规项，Complexity Tracking 表为空。**

## Project Structure

### Documentation (this feature)

```text
specs/001-feedmind-agent/
├── spec.md              # 功能规格（已完成）
├── plan.md              # 本文件
├── research.md          # Phase 0：技术决策与调研
├── data-model.md        # Phase 1：数据模型（MySQL/Redis/ES）
├── quickstart.md        # Phase 1：环境准备与验证步骤
├── contracts/           # Phase 1：接口契约
│   ├── content.proto.md      # Content RPC gRPC 契约
│   ├── agent.proto.md        # Agent RPC gRPC 契约
│   ├── feed-delta.proto.md   # Feed RPC 增量方法与FeedSource
│   ├── interaction-delta.proto.md  # Interaction RPC 增量方法
│   ├── http-api.md# Gateway HTTP 端点
│   ├── events.md             # RocketMQ 事件契约
│   └── agent-tools.md        #8 个 Tool 的 JSON Schema
├── checklists/
│   └── requirements.md  # 规格质量清单（已完成）
└── tasks.md             # Phase 2 输出（由 /speckit.tasks 生成，本命令不创建）
```

### Source Code (repository root = `backend/`)

```text
api/proto/
├── content/content.proto           # 新增：Content RPC 契约
├── agent/agent.proto               # 新增：Agent RPC 契约
├── feed/feed.proto                # 改造：FeedSource 枚举 + 5 个查询方法
└── interaction/interaction.proto    # 改造：指标与兴趣画像查询方法

app/
├── content/                # 新增服务域
│   ├── rpc/                        # :9007（metrics 9110）
│   │   ├── content.go
│   │   ├── etc/content.yaml
│   │   ├── internal/
│   │   │   ├── config/
│   │   │   ├── logic/              # GetContentProfile / SearchContent / RetryAnalysis
│   │   │   ├── server/
│   │   │   ├── svc/
│   │   │   ├── keys/               # Redis key 集中定义
│   │   │   ├── model/              # feed_content_profiles
│   │   │   └── search/             # ES 客户端与三路召回 + RRF
│   │   └── tests/
│   └── worker/                     # 新增独立进程（metrics 9109）
│       ├── worker.go
│       ├── etc/content-worker.yaml
│       └── internal/
│           ├── config/
│           ├── pipeline/           # 状态机：PENDING→...→COMPLETED
│           ├── media/              # FFmpeg/ffprobe 封装（可注入 executor 便于 mock）
│           ├── asr/                # ASR 接入层
│           ├── ocr/                # OCR 接入层
│           ├── vision/             # 多模态标签生成
│           └── consumer/           # feed-created / feed-deleted
│
├── agent/                          # 新增服务域
│   └── rpc/                        # :9006（metrics 9108）
│       ├── agent.go
│       ├── etc/agent.yaml
│       ├── internal/
│       │   ├── config/             # 含 AgentLimit / InternalUserIDs
│       │   ├── logic/              # CreateSession / SendMessage / GetRun / CancelRun / List*
│       │   ├── server/
│       │   ├── svc/
│       │   ├── keys/
│       │   ├── model/              # agent_sessions/messages/runs/tool_calls
│       │   ├── orchestrator/       # Eino Graph 编排 + Run 状态机
│       │   ├── tools/              # 8 个只读 Tool（含权限预检）
│       │   ├── intent/             # 意图分类 + Go 侧校验
│       │   ├── guard/              # 输出校验器（feed_id/数字一致性）
│       │   └── prompt/             # System Prompt 与注入防护
│       └── tests/
│           └── contract_*_test.go  # Tool schema 契约测试
│
├── feed/rpc/                       # 改造
│   └── internal/
│       ├── logic/                  # timeline 召回打来源标记 + 写 Trace
│       └── keys/                   # 追加 feed:trace:{request_id}
│
├── interaction/                    # 改造
│   ├── rpc/internal/logic/         # GetCreatorMetrics / GetPeerMetrics / GetUserInterestProfile
│   └── rpc/internal/worker/        # 追加 feed-behavior-event 消费与聚合
│
└── gateway/                        # 改造
    └── internal/
        ├── middleware/             # 新增 RequestIDMiddleware
        └── handler/                #埋点上报 + Agent 路由 + 内容画像路由

common/
├── interceptors/                   # 新增：gRPC client/server Metadata 透传
│   ├── keys.go# Metadata 键名统一定义
│   ├── requestid.go
│   └── ...
├── event/behavior/                 # 新增：feed-behavior-event 事件契约
├── errorx/errorx.go                # 改造：Content(15xxx) + Agent(16xxx) 码段
└── response/response.go            # 改造：requestID(ctx) 兼容 typed key

deploy/sql/
├── content.sql                     # 新增：feed_content 库 + 1 表
├── agent.sql                       # 新增：feed_agent 库 + 4 表
└── interaction.sql                 # 追加：3 张表

scripts/
├── eval-search.sh                  # 新增：检索评测
├── eval-agent.sh                   # 新增：Agent 评测
├── benchmark-behavior.sh           # 新增：埋点压测
└── benchmark-agent.sh              # 新增：Agent 压测
```

**Structure Decision**: 沿用现有 go-zero monorepo 布局（`app/<svc>/rpc/internal/{config,logic,server,svc}`）。两点偏离及理由：

1. **`app/content/worker` 为独立进程**（非 `rpc/internal/worker` 进程内 worker）：FFmpeg 是 CPU/IO 密集型子进程且需临时磁盘配额，跑在 RPC 进程内会污染在线请求延迟与内存，且无法独立限流扩缩容。对比 `app/interaction/rpc/internal/worker` 保持进程内（轻量计数聚合，复用现有框架）。
2. **Agent 新增 5 个非标准子包**（`orchestrator/tools/intent/guard/prompt`）：Eino 编排、Tool 注册表、意图分类、输出校验、Prompt 组装各自是独立关注点，塞进 `logic/` 会形成巨型文件。`logic/` 仍只放 gRPC 方法入口，符合宪法 VI「logic 层只放业务逻辑」。

## Implementation Phases

严格按依赖顺序推进，**每阶段必须可独立运行、可演示、可回滚**（明确避免"先做聊天界面再补数据"的反面做法）。

```text
阶段一（request_id + 埋点）
   ├──▶ 阶段二（Content 画像）────┐
   │                            ├──▶ 阶段三（检索 + 兴趣）──▶ 阶段四（Agent）──▶ 阶段五（可观测）
   └────────────────────────────┘
```

| 阶段 | 目标 | 核心交付 | 验收| 演示 |
|------|------|---------|------|------|
| **一** | 让"一次请求"和"一次观看"可追踪、可统计 | RequestIDMiddleware；gRPC 透传拦截器；FeedSource 枚举与打标；inbox 回源重建；`feed:trace:*`；`feed-behavior-event` 契约与上报接口；Behavior Worker（幂等+明细+小时指标） | A8、A9、E2E-4/7/9 | 刷流 → 用 request_id 查 Trace → 看到各数据源返回量 |
| **二** | 视频发布后自动产出画像 | `app/content/rpc`(9007) + `app/content/worker`；FFmpeg 抽帧/音频；ASR/OCR/多模态接入；`feed_content_profiles`；幂等与重试；`feed-deleted` 下线 | A1、A2、A3、E2E-1/2/3 | 发一条标题模糊的视频 → 自动生成「西安周边露营攻略」类标签 |
| **三** | 内容可语义检索，兴趣可量化 | ES 索引 + 三路召回 + RRF 融合；`SearchContent`；兴趣权重与时间衰减；`user:interest:{uid}` + `user_interest_profiles`；`GetUserInterestProfile` | A4、A11、E2E-8 | 用「西安周边新手露营」命中阶段二生成的画像 |
| **四** | 自然语言入口打通三类场景 | `app/agent/rpc`(9006)；Eino 单 Agent + 8 只读 Tool；Run 状态机与限额；输出校验器；推荐原因规则引擎；创作者漏斗诊断；Gateway Agent 路由 | A5、A6、A7、A10、A12、E2E-5/6/10 | 问「找露营视频」「为什么推荐这条」「分析我这条为什么播放差」 |
| **五** | 系统可度量、问题可定位 | 各服务 Prometheus/Telemetry 配置；自定义指标；日志字段统一；MQ trace 传播；告警规则；内部诊断接口 | 12-observability §7 全部用例 | 用 request_id 串起 Gateway→Feed RPC→Redis→聚合层→下游 RPC |

**阶段间依赖说明**：阶段三的兴趣画像同时依赖阶段一的行为事件与阶段二的内容标签；阶段四的创作者分析依赖阶段一的指标与阶段二的画像；阶段四的推荐解释依赖阶段一的来源标记与阶段三的兴趣画像。

## Key Risks & Mitigations

| 风险 | 影响 | 对策 |
|------|------|------|
| 外部模型/ASR/OCR 成本超预算 | 无法长期运行 | 关键帧 ≤ 20、字幕截断 4000 字、并发上限 2、只分析视频类（`feed_type==2`）、失败不无限重试（MaxRetry 3） |
| 拦截器接入点漏改导致 request_id 断链 | 排障能力失效 | `request_id_missing_total` 指标兜底告警；下游缺失时记录 `request_id=missing` 但不中断请求 |
| 模型幻觉（编造 feed_id/数字） | 回答不可信 | Go 计算 + 输出校验器（feed_id必在 Tool 结果集内、数字必可溯源）+ 模板降级 |
| Prompt 注入（含Tool 结果内注入） | 越权/数据泄漏 | 用户输入不进 System Prompt；Tool 结果同样视为不可信；权限双层校验；未注册 Tool 直接拒绝 |
| FFmpeg 子进程 RCE / SSRF | 服务器失陷 | 固定二进制路径 + 参数数组（禁 shell）+ `AllowedMediaHosts` 白名单 + 超时强杀 + 临时目录隔离 |
| Trace 数据量导致 Redis 内存压力 | 影响在线服务 | 采样 + 短 TTL（生产建议 30min）+ 只存必要字段 |
| 指标 label 基数爆炸 | Prometheus 崩溃 | 禁止高基数 label（feed_id/user_id 不得作 label）——已列为红线 |
| 多服务改造铺开过大 | 交付风险 | 严格按五阶段推进，每阶段可独立演示与回滚 |
| 客户端埋点不可控 | 数据质量差 | 服务端重判阈值 + 5 条数据质量校验规则（播放≤曝光、完播≤播放等）+ 时间偏差 P99<5min 丢弃 |

## Complexity Tracking

> 无需填写——Constitution Check 全部通过，无违规项需要论证。

以下为**已在设计阶段主动简化**的决策记录（来自 `docs/design/agent/01-architecture.md` §7）：

| 编号 | 决策 | 简化收益 |
|------|------|---------|
| D1 | 行为指标与兴趣画像放 Interaction 服务域，不新建 profile 服务 | 避免第 8 个微服务，复用现有 Redis 计数与 MQ 消费框架 |
| D7 | 内部身份用配置白名单（`InternalUserIDs`），不引入角色表 | 避免侵入 User 服务，第一版仅内部排障使用 |
| — | v1 无写 Tool | 消除审批流、回滚、副作用隔离等一整类复杂度 |
| — | Agent「创建 Run + 轮询」而非流式 SSE | 避免长连接与背压处理，`agent:run:{run_id}` 缓存支撑低成本轮询 |
