# Feature Specification: FeedMind Agent — 内容理解与智能助手子系统

**Feature Branch**: `001-feedmind-agent`

**Created**: 2026-08-07

**Status**: Draft

**Input**: 基于 `docs/design/agent/` 目录下 16 份设计文档（00-overview ~ 15-roadmap + README），为 Feed 系统新增三层增量能力：内容理解、行为数据、AI 智能助手。

## User Scenarios & Testing

### User Story 1 — 全链路请求追踪与 Feed 来源标记 (Priority: P1)

作为**开发者/运维人员**，当用户反馈某个 Feed 推荐不合理时，我需要能从请求日志中还原完整的调用链路（哪个请求、哪个用户、经历了哪些服务），并知道每条 Feed 是"为什么"推荐的（关注收件箱/大V拉取/推荐池/同城池），以便快速定位问题。

**Why this priority**: 这是所有后续功能（行为采集、Agent 问答、推荐解释）的数据地基。没有 request_id，无法串联用户行为与请求上下文；没有来源标记，Agent 无法回答"为什么推荐"。

**Independent Test**: 发送任意 API 请求，检查响应头和响应体均包含有效的 `request_id`（32 位十六进制）；通过 gRPC Metadata 向下游传播；Feed 时间线返回的每条 Feed 携带非零 `feed_source` 字段。

**Acceptance Scenarios**:

1. **Given** 客户端发起 GET /api/v1/feeds/timeline 请求（不带 X-Request-Id），**When** Gateway 处理请求，**Then** 自动生成 32 位十六进制 request_id，同时出现在响应头 `X-Request-Id` 和响应体 `request_id` 字段中。
2. **Given** 客户端发起请求并携带格式合法的 X-Request-Id（字母数字/下划线/短横线，≤64 字符），**When** Gateway 接收请求，**Then** 复用客户端提供的 request_id 并在全链路透传。
3. **Given** 客户端发起请求并携带格式非法的 X-Request-Id（含特殊字符或超长），**When** Gateway 接收请求，**Then** 丢弃非法值并重新生成 request_id。
4. **Given** 下游 RPC 服务收到不带 request_id 的 Metadata，**When** 服务处理请求，**Then** 日志记录 `request_id=missing` 并触发告警，但不中断请求处理。
5. **Given** 用户获取关注流时间线，**When** Feed RPC 返回 Feed 列表，**Then** 每条 Feed 的 `feed_source` 字段正确标注为：普通关注用户帖子返回 `FEED_SOURCE_FOLLOW_INBOX`(1)，大V（粉丝>10万）帖子返回 `FEED_SOURCE_VIP_OUTBOX`(2)。
6. **Given** 用户获取推荐流，**When** Feed RPC 返回 Feed 列表，**Then** 每条 Feed 标注为 `FEED_SOURCE_RECOMMEND_POOL`(5)。
7. **Given** 用户获取同城流，**When** Feed RPC 返回 Feed 列表，**Then** 每条 Feed 标注为 `FEED_SOURCE_CITY_POOL`(4)。
8. **Given** 收件箱缺失后回源重建的场景，**When** Feed RPC 返回重建的 Feed，**Then** 标注为 `FEED_SOURCE_INBOX_REBUILD`(3)。

---

### User Story 2 — Feed 行为埋点与指标聚合 (Priority: P1)

作为**产品运营人员**，当用户浏览 Feed 流时，我需要系统自动记录用户的观看行为（曝光、播放、有效播放、完播、快速划走），并实时聚合为每条 Feed 的观看指标（曝光数、播放数、完播率等），以便了解内容质量和用户偏好。

**Why this priority**: 行为数据是创作者分析（P3）和用户兴趣画像（P3）的基础原料。没有埋点数据，Agent 无法回答"我的帖子表现如何"、"用户喜欢什么内容"。

**Independent Test**: 模拟客户端上报一条播放行为事件，通过 RocketMQ 消费后在 Redis 中查询到该 Feed 的播放次数已累加；小时级别维度数据写入 MySQL 的 `feed_behavior_hourly` 表。

**Acceptance Scenarios**:

1. **Given** 用户观看了一条 Feed 超过 1 秒，**When** 客户端上报 `action=play` 行为到 POST /api/v1/feeds/behaviors，**Then** 系统通过 Gateway 校验 JWT 身份后写入 RocketMQ Topic `feed-behavior-event`。
2. **Given** RocketMQ 中积压多条行为事件，**When** Behavior Worker 消费事件，**Then** 检测 `event_id` 幂等性（已处理则跳过），写入 `feed_behavior_detail` 明细表，同时更新 Redis 中对应 Feed 的行为指标计数器。
3. **Given** 同一行为事件被 MQ 重复投递，**When** Behavior Worker 消费到重复 `event_id`，**Then** 识别重复并跳过，确保指标不会重复计数。
4. **Given** Behavior Worker 定时 flush 任务触发（每小时），**When** 执行 flush，**Then** 将 Redis 累加的曝光数/播放数/有效播放数/完播数/快划数 绝对值写入 `feed_behavior_hourly` 表（保证双重幂等：event_id + 写绝对值）。
5. **Given** 用户对同一条 Feed 产生多种行为（先曝光、再播放、再完播），**When** 客户端分别上报各行为事件，**Then** 各行为指标独立累加，互不覆盖。

---

### User Story 3 — 视频内容自动分析 (Priority: P2)

作为**内容运营人员**，当创作者发布一条视频 Feed 后，我需要系统自动分析视频内容，提取关键帧、语音转文字（ASR）、画面文字识别（OCR），并生成结构化标签和内容摘要，以便后续支持语义检索和内容推荐解释。

**Why this priority**: 内容画像是语义检索（P3）和推荐解释的基础。没有内容理解，Agent 只能靠标题/标签回答，无法提供深度内容洞察。

**Independent Test**: 发布一条含语音+文字的视频 Feed，等待 Content Worker 处理后，通过 Content RPC 的查询接口获取到该 Feed 的关键帧列表、完整转录文本、OCR 识别的画面文字和生成的结构化标签，并在 Elasticsearch 中可检索到该 Feed。

**Acceptance Scenarios**:

1. **Given** 用户发布一条视频 Feed，**When** Feed RPC 发送 `feed-created` 消息到 RocketMQ，**Then** Content Worker 消费消息后启动分析流水线（FFmpeg 抽帧 → ASR 语音转文字 → OCR 画面文字识别 → 多模态标签生成），所有结果写入 `feed_content` 数据库并在 Elasticsearch 建索引。
2. **Given** 分析完成后，**When** 调用 Content RPC 查询 `GetContentProfile(feed_id)`，**Then** 返回结构化内容画像：关键帧图片 URL 列表、完整转录文本、OCR 文本、AI 生成的摘要标签、分析状态和完成时间。
3. **Given** 发布一条纯图片 Feed（无视频），**When** Content Worker 处理，**Then** 跳过 FFmpeg 抽帧和 ASR 步骤，只执行 OCR 和多模态标签生成。
4. **Given** 视频分析过程中某一步骤失败（如 ASR 服务不可用），**When** Content Worker 重试 3 次后仍失败，**Then** 标记该 Feed 分析状态为 `PARTIAL`，已成功的步骤结果保留，失败步骤记录错误信息，支持后续手动触发重试。
5. **Given** 用户删除了已分析的 Feed，**When** Feed RPC 发送 `feed-deleted` 消息，**Then** Content Worker 标记该内容画像为已删除并从 Elasticsearch 中移除索引。

---

### User Story 4 — AI 智能助手自然语言问答 (Priority: P3)

作为**普通用户**，我希望能用自然语言向系统提问（如"最近同城在流行什么"、"为什么给我推荐这条视频"、"我的帖子表现怎么样"），系统基于实际数据分析后给我答案，而不是给出泛泛的通用回复。

**Why this priority**: 这是 FeedMind Agent 的核心价值所在——把底层的结构化数据转化为用户可理解的自然语言洞察。依赖 P1 和 P2 的数据积累后才能有意义地工作。

**Independent Test**: 在已发布若干 Feed 且有行为数据的环境中，向 Agent 发送"最近同城最火的 3 条帖子是什么？"，Agent 返回基于实际数据的回答，包含具体的帖子标题/播放数。通过 Agent RPC 的 Run 接口追踪完整的意图识别→ Tool 调用→数据取回→回答生成的链路。

**Acceptance Scenarios**:

1. **Given** 用户通过 Gateway 发送消息到 Agent 会话接口，**When** Agent 收到"为什么给我推荐了 XXX"（描述一条具体的帖子），**Then** Agent 识别意图为推荐解释 → 调用 `GetFeedDetail` Tool 确认帖子存在 → 调用 `GetFeedSource` Tool 获取来源 → 如果来源是推荐池/同城池，组织语言解释推荐逻辑（如"它和你最近看过的 YYY 内容相似"）。
2. **Given** 用户提问"最近同城最火的 3 条帖子"，**When** Agent 处理请求，**Then** 识别意图为趋势查询 → 调用 `GetCityTrending` Tool 获取同城热门 Feed → 聚合返回 Feed 标题/作者/播放数，用自然语言呈现。
3. **Given** 用户提问"我的帖子最近一周表现如何"，**When** Agent 处理，**Then** 从 JWT 提取当前用户 ID → 调用 `GetCreatorMetrics` Tool 获取该用户的帖子列表及播放/互动指标 → 返回汇总分析。
4. **Given** 用户提问越权问题"张三是谁"（一个普通用户名），**When** Agent 处理，**Then** 权限预检（Go 代码）拒绝该 Tool 调用（该 Tool 仅对内容运营白名单用户开放），Agent 回复友好的权限拒绝提示。
5. **Given** Agent 在一次 Run 中调用了多个 Tool，**When** Run 完成后，**Then** `agent_runs` 表记录完整 Run 信息（意图、耗时、成功/失败），`agent_tool_calls` 表记录每次 Tool 调用的参数/返回/耗时。

---

### User Story 5 — 创作者内容数据分析 (Priority: P3)

作为**内容创作者**，我希望能在个人中心看到自己发布的每条帖子的表现数据（播放次数、完播率、互动数），以便了解哪些内容受欢迎并调整创作方向。

**Why this priority**: 创作者分析直接提升创作者留存和内容质量。依赖 P1 的行为数据积累才有意义。

**Independent Test**: 以创作者身份调用 Interaction RPC 的 `GetCreatorMetrics` 接口，返回该创作者所有帖子的最新指标数据（不限时间范围，raw counts），数据与 `feed_behavior_hourly` 聚合结果一致。

**Acceptance Scenarios**:

1. **Given** 创作者发布了多条 Feed，且有行为数据积累，**When** 调用 `GetCreatorMetrics(user_id)`，**Then** 返回该用户每条 Feed 的总播放数、总有效播放数、总完播数、总曝光数、完播率（完播数÷总播放数），按发布时间倒序排列。
2. **Given** 某 Feed 尚未产生任何行为数据，**When** 查询该 Feed 的指标，**Then** 各指标返回 0 而非 null 或错误。
3. **Given** 查询一个不存在的 user_id，**When** 调用 `GetCreatorMetrics`，**Then** 返回「用户不存在」错误。
4. **Given** 行为事件正在处理中（MQ 积压），**When** 查询创作者指标，**Then** 返回当前已聚合的最新数据，可能存在秒级延迟（最终一致性）。

---

### Edge Cases

- **request_id 断链**：如果 gRPC 中间链路某服务未透传 Metadata，下游日志出现 `request_id=missing` 且触发告警。系统不能因此中断请求。
- **行为事件重复投递**：RocketMQ 可能多次投递同一消息。Worker 通过 `event_id` 做幂等去重，已处理的 event_id 跳过。
- **视频分析超时**：FFmpeg/ASR/OCR 调用可能因第三方服务不可用而超时。Content Worker 单次超时后重试最多 3 次，全部失败标记 PARTIAL 状态，支持手动重试。
- **无行为数据时的统计查询**：查询某 Feed 的创作者指标时，如果该 Feed 没有任何行为数据记录（包括小时表未刷新），所有指标返回 0，不返回错误。
- **Agent 多轮对话中的身份切换**：用户已完成多轮 Agent 对话，若 token 过期，后续消息被 401 拦截，前端自动跳转登录，不影响已有会话数据。
- **ES 索引延迟**：内容分析完成后，ES 索引可能存在秒级延迟。语义检索接口应返回"处理中/不可检索"状态，而非静默失败。
- **Agent 副作用隔离**：第一版所有 Tool 只读——即使模型幻觉尝试调用修改类 Tool（如改写帖子标签），Go 层拒绝执行。只读约束是硬编码的，不依赖模型 self-discipline。

## Requirements

### Functional Requirements

**请求追踪与来源标记**:
- **FR-001**: Gateway MUST 实现 RequestIDMiddleware，自动为每个请求生成 32 位十六进制 request_id（uuid v4 去横线），同时出现在响应头 `X-Request-Id` 和响应体 `request_id`。
- **FR-002**: Gateway MUST 支持客户端携带 `X-Request-Id`，但必须校验格式（`^[A-Za-z0-9_-]+$`，长度 ≤ 64），非法则丢弃并重新生成。
- **FR-003**: gRPC 通信 MUST 通过 Metadata `x-request-id` 透传 request_id，新增 `common/interceptors/` 中 `UnaryClientRequestID`（客户端注入）和 `UnaryServerRequestID`（服务端提取）拦截器。
- **FR-004**: 所有 RocketMQ 事件体（`feed-created`、`feed-deleted`、`feed-behavior-event` 等）MUST 包含 `request_id` 字段。
- **FR-005**: FeedBrief Proto 消息 MUST 新增 `feed_source` 枚举字段（FOLLOW_INBOX / VIP_OUTBOX / RECOMMEND_POOL / CITY_POOL / INBOX_REBUILD），Feed RPC 在返回时间线时必须填写正确的来源值。
- **FR-006**: Metadata 键名 MUST 统一在 `common/interceptors/keys.go` 中定义，全项目引用，禁止各服务各自硬编码。

**行为埋点与指标**:
- **FR-007**: Gateway MUST 新增 POST `/api/v1/feeds/behaviors` 端点，接收**批量**（1~50 条，超限返回 14004）行为事件，每条含 `feed_id`、`action_type`（EXPOSE/PLAY/EFFECTIVE_PLAY/FINISH/SKIP/SHARE）、`watch_duration_ms`、`position`、`request_id`。`event_id` **由服务端生成 uuid v4**，客户端传入的 `client_event_id` 仅用于日志排查、**不作为幂等键**（防伪造吞事件）。单用户限流 300 条/分钟。
- **FR-008**: Behavior Worker（归属 `interaction/worker`）MUST 消费 RocketMQ Topic `feed-behavior-event`，对每个事件做 event_id 幂等去重后写入 `feed_behavior_detail` 明细表。
- **FR-009**: Behavior Worker MUST 更新 Redis 中对应 Feed 的实时行为计数器（曝光/播放/有效播放/完播/快划），并定时（每小时）将绝对值写入 `feed_behavior_hourly` 表。
- **FR-010**: 行为上报接口 MUST 校验 JWT 身份识别上报用户，匿名请求拒绝。
- **FR-011**: 行为指标计数器更新 MUST NOT 阻塞主请求链路——Gateway 接收后立即返回 200，由 MQ 异步处理。

**内容分析**:
- **FR-012**: Content Worker（独立进程）MUST 消费 RocketMQ Topic `feed-created`，对视频 Feed 执行：FFmpeg 抽帧（每 3 秒取一帧）→ ASR 语音转文字 → OCR 画面文字识别 → 多模态 LLM 标签生成。
- **FR-013**: Content RPC（新增服务，端口 9007）MUST 提供 5 个方法：`GetContentProfile`（**分级返回**——字幕/OCR 全文仅作者本人或内部可见）、`BatchGetContentProfile`（≤50，仅公开字段）、`SearchContent`、`RetryContentAnalysis`（内部用户）、`SubmitProfileFeedback`（作者纠错，**只记录不改画像**）。
- **FR-014**: Content Worker MUST 在消费 `feed-deleted` 消息时标记内容画像为已删除、从 Elasticsearch 移除索引。
- **FR-015**: 内容分析流水线 MUST 支持单步失败重试（最多 3 次），全部失败后标记 PARTIAL 状态，已成功的步骤结果保留。
- **FR-016**: 分析完成的内容 MUST 在 Elasticsearch 中建索引（文本字段 + 标签字段），支持后续 BM25 + kNN 混合检索。
- **FR-017**: Content RPC MUST 提供 `SearchContent` 接口，支持关键词全文检索和语义向量检索，返回匹配的 Feed 列表。

**AI 智能助手**:
- **FR-018**: Agent RPC（新增服务，端口 9006）MUST 提供完整的会话管理：`CreateSession`、`SendMessage`、`ListMessages`、`ListSessions`。
- **FR-019**: Agent RPC 的 Run 流程 MUST 按顺序执行：意图识别（LLM）→ 权限预检（Go 代码，基于 JWT 身份和 Tool 白名单）→ Tool 选择与参数校验 → RPC 调用取数据 → 结构化计算（Go 代码）→ 语言组织（LLM）。
- **FR-020**: Agent 第一版所有注册的 Tool MUST 为只读操作（查询类），禁止任何写操作（修改帖子内容/标签/权重/推荐池）。
- **FR-021**: Agent MUST 使用 Eino 框架进行 Tool 编排和流程控制。
- **FR-022**: 每次 Run MUST 在 `agent_runs` 表记录完整信息（session_id、意图、耗时、状态），每次 Tool 调用 MUST 在 `agent_tool_calls` 表记录参数/结果/耗时。
- **FR-023**: 权限预检 MUST 基于配置白名单——内容运营类 Tool 仅对白名单用户可用，普通用户只能调用个人数据查询 Tool。

**创作者分析**:
- **FR-024**: Interaction RPC MUST 新增 5 个方法：`GetFeedMetrics`（单 feed 原子指标+派生率，含 `window`）、`BatchGetFeedMetrics`（≤100，仅原子指标）、`GetCreatorMetrics`（含 `viewer_id` 归属校验）、`GetPeerAverageMetrics`（**匿名聚合，禁止返回 feed_id/author_id**）、`GetUserInterestProfile`（`user_id` 必须等于 `viewer_id`）。
- **FR-025**: 派生率（完播率/有效播放率/快划率）MUST 在应用层计算，不落库。

**可观测性**:
- **FR-026**: 所有新增服务 MUST 暴露 Prometheus metrics 端点（content:9110, agent:9108, content-worker:9109）。
- **FR-027**: 所有新增服务 MUST 接入 go-zero 内置的 OpenTelemetry 分布式追踪。

### Key Entities

- **Session（会话）**: 用户与 Agent 的一次多轮对话上下文。属性：session_id（Snowflake）、user_id、status（active/closed）、title（Agent 自动生成摘要）、created_at、messages 数量。
- **AgentMessage（消息）**: 会话中的单条对话。属性：message_id（Snowflake）、session_id、role（user/assistant）、content、tool_calls 引用列表、created_at。
- **AgentRun（执行记录）**: Agent 处理一条消息的完整执行记录。属性：run_id（Snowflake）、session_id、message_id、intent（识别的用户意图）、status（success/partial/failure）、latency_ms、tool_calls 数量、created_at。
- **AgentToolCall（Tool 调用记录）**: Run 中单次 Tool 调用的细节。属性：call_id、run_id、tool_name、input_params（JSON）、output_result（JSON）、status（success/error）、latency_ms。
- **ContentProfile（内容画像）**: Feed 内容分析的结构化结果。属性：profile_id、feed_id、status（pending/processing/partial/complete/deleted）、keyframe_urls[]、transcript、ocr_text、tags[]、error_info、analysis_version、created_at、updated_at。
- **FeedBehaviorDetail（行为明细）**: 单条用户行为记录。属性：detail_id、event_id（幂等键）、feed_id、user_id、action（expose/play/complete/skip）、play_duration_ms、client_timestamp、created_at。
- **FeedBehaviorHourly（小时指标）**: 按小时聚合的 Feed 行为绝对值。属性：id、feed_id、hour_bucket、expose_count、play_count、effective_play_count、complete_count、skip_count。
- **UserInterestProfile（兴趣画像）**: 用户兴趣标签权重。属性：user_id、tags[]（{tag_name, weight, source（行为/搜索）, last_updated}）。

## Success Criteria

### Measurable Outcomes

- **SC-001**: 任意 API 请求的 request_id 在 Gateway→RPC→MQ 全链路可追踪，查日志时能通过单个 request_id 还原完整调用链。
- **SC-002**: Feed 时间线返回的每条 Feed 的 `feed_source` 字段 100% 非零，覆盖所有推荐来源。
- **SC-003**: 行为埋点从客户端上报到 Redis 计数器更新的端到端延迟在 5 秒以内（P99）。
- **SC-004**: 行为事件重复投递时，指标不会重复计数（event_id 幂等去重生效）。
- **SC-005**: 视频内容分析从发布到画像可查询的端到端时间在 5 分钟以内（P95，取决于视频时长和第三方服务）。（备注：分析包含 FFmpeg 抽帧、ASR 转录、OCR 识别、多模态标签生成。视频时长会影响 FFmpeg 耗时，第三方服务会影响 ASR/OCR 耗时。）
- **SC-006**: Agent 回答自然语言问题的端到端耗时在 5 秒内（P95，含 LLM + RPC 调用）。
- **SC-007**: Agent 回答直接数据查询类问题的准确率达到 100%（数字与数据库一致，不含开放式观点问题）。
- **SC-008**: 创作者能在个人中心看到自己每篇帖子的最新表现数据，数据延迟不超过 1 小时（小时级聚合刷新）。
- **SC-009**: 所有新增服务在 Pod 重启后 30 秒内恢复正常工作（健康检查通过）。
- **SC-010**: Content Worker 的视频分析不阻塞 Feed 发布流程——发布接口在 1 秒内返回，分析异步进行。

## Assumptions

- 现有 User / Relation / Feed / Comment / Interaction 服务已完成且稳定运行，Agent 模块作为增量功能接入。
- 基础设施（MySQL 8.0、Redis 7、RocketMQ 5.1.4、etcd 3.5）已通过 Docker Compose 就绪，新增的 Elasticsearch 实例需额外部署。
- FFmpeg 二进制在 Content Worker 所在容器/宿主机上可用，ASR 和 OCR 服务作为外部 API 调用（或本地模型部署）。
- 多模态 LLM（用于标签生成和 Agent 意图识别）通过 API 调用外部模型服务，API Key 在环境变量中配置。
- Agent 第一版面向单个用户的自助查询场景，不处理"对比两个创作者的帖子表现"等跨用户查询。
- 用户兴趣画像的更新基于行为事件（观看历史），不包括主动搜索行为——搜索暂不在 v1 范围内。
- Content Worker 作为独立进程部署（不嵌入 Content RPC），以隔离 FFmpeg 资源消耗。
- 所有数据库操作（含新表）遵循现有 `goctl model` 生成 + custom model 扩展的模式；数据库建表脚本放入 `deploy/sql/agent/`。
- Elasticsearch 使用 BM25 + kNN 混合检索；若环境受限可用 Redis Stack 的向量检索能力作为备选方案。
- Agent Tool 白名单在 `app/agent/rpc/etc/agent.yaml` 中配置，支持热更新或重启生效。
- 本规范涉及的 request_id 透传和 Feed source 标记修改属于现有 gateway 和 feed 服务的改造范围，但不改变其核心业务逻辑。
