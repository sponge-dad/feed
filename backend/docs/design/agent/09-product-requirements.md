# FeedOps Agent 项目需求文档

> 基于 Feed 项目当前真实实现，定义智能内容运营 Agent 的产品定位、功能边界、依赖能力、后端改造清单、分期路线与验收标准，作为产品评审和研发排期的统一输入。

---

## 1. 概述与定位

### 1.1 项目推荐

推荐建设 **FeedOps Agent（智能内容运营 Copilot）**，而不是直接建设“全自动推荐 Agent”。

FeedOps Agent 面向内容运营人员，以自然语言完成：

1. 查询和筛选平台内容；
2. 基于真实指标发现异常内容并解释原因；
3. 生成标题、标签、上下架等操作建议；
4. 经人工审批后调用业务服务执行；
5. 回读业务数据验证结果，并沉淀审计记录。

普通用户侧的“对话式找内容”可以作为 M0 工程验证场景，但不应作为首个正式产品目标。当前系统缺少观看历史、内容分类和个性化排序信号，直接承诺“懂用户的推荐 Agent”会把模型语言能力误当成推荐能力。

仓库已有的 [FeedMind Agent 需求文档](../../agent需求文档.md) 面向普通用户与内容创作者，重点是内容理解、自然语言检索和作品分析；本文不替代该方案，而是补充运营 Copilot、人工审批、业务写入安全以及后端产品化路线。两者可以共享内容分析、行为指标、Agent 运行时和受控工具层。

### 1.2 当前实现审计

下表以代码为准，不采用尚未落地的设计描述。

| 领域 | 已实现能力 | 对 Agent 的限制 | 代码依据 |
|---|---|---|---|
| 用户与鉴权 | 注册、登录、JWT、用户资料 | JWT 只有 `user_id`/`username`，没有运营角色、权限和封禁态校验链路 | `common/jwtx/jwtx.go`、`api/proto/user/user.proto` |
| 内容 | 发布、软删除、详情、批量详情、个人主页列表 | 无内容更新、上下架、运营审核、批量查询和条件搜索 | `api/proto/feed/feed.proto` |
| 推荐流 | 全局推荐池分页 | 当前 score 仅为发布时间，`user_id` 未参与排序，不是个性化推荐 | `app/feed/rpc/internal/worker/worker.go`、`getrecommendtimelinelogic.go` |
| 内容元数据 | 标题、正文、媒体、城市、互动镜像计数 | 无分类、标签、视频时长、字幕、质量分，无法精确按题材或时长筛选 | `deploy/sql/feed.sql` |
| 互动 | 点赞、收藏、计数、用户互动列表 | 无曝光、点击、播放、停留、完播、分享、负反馈 | `api/proto/interaction/interaction.proto` |
| 评论 | 评论/回复、热评、评论计数 | 评论点赞路由当前返回“功能暂未开放”，热评 `like_count` 不能作为稳定信号；另无情感、举报、审核和风险标签 | `api/proto/comment/comment.proto`、`app/gateway/internal/logic/comment/likeCommentLogic.go` |
| 关系 | 关注、粉丝、是否关注、大 V 判断 | 可作为作者偏好信号，但缺少批量画像和人群分群 | `api/proto/relation/relation.proto` |
| Gateway | 已接入 user/relation/feed/comment/interaction HTTP 路由 | 面向 C 端请求，没有运营后台接口和 Agent API | `app/gateway/api/gateway.api` |
| 事件 | Feed、互动、评论已定义 MQ 事件 | 事件不覆盖曝光/播放/更新；Feed 发消息失败只有日志，未见可靠 outbox 落地 | `common/event/`、`createfeedlogic.go` |
| Agent | 已有设计文档 | 尚无 `app/agent`、Agent 数据库、运行时、工具、任务队列和评测 | `docs/design/agent/` |

结论：

- 当前业务服务足以支撑一个**只读、能力受限、明确免责声明**的 Agent 技术验证。
- 当前后端不足以支撑可信的运营诊断、精准推荐或批量运营执行。
- Agent 项目必须和业务数据能力一起建设；只接入大模型不会自动补齐缺失的数据与权限。

### 1.3 项目目标

| 编号 | 目标 | 衡量方式 |
|---|---|---|
| G1 | 运营人员能用自然语言完成内容查询与诊断 | 核心任务完成率、人工节省时长 |
| G2 | 所有事实、ID 和指标都能追溯到业务工具结果 | grounding 通过率 |
| G3 | 所有业务写操作都经过审批、鉴权、幂等执行和回读验证 | 未审批写入数必须为 0 |
| G4 | Agent 失败不会破坏 Feed 主链路 | Agent 与主业务独立部署、可降级 |
| G5 | 形成“诊断—行动—效果评估”的运营闭环 | 计划执行率、执行后指标变化 |

### 1.4 非目标

首三个里程碑不做：

- 不允许 Agent 直接连接或执行任意业务 SQL；
- 不允许模型绕过审批自主批量修改、删除或下架内容；
- 不用 LLM 直接替代在线推荐排序服务；
- 不做开放式网页搜索、任意插件或任意 URL 抓取；
- 不做多 Agent 协作、数字人或语音交互；
- 不对不存在的播放量、CTR、完播率等指标做推测。

### 1.5 用户角色

| 角色 | 主要诉求 | 权限 |
|---|---|---|
| 普通用户 | 对话式查找内容、解释推荐原因 | 仅访问本人可见数据；只能代本人点赞/收藏 |
| 内容运营 | 查询全站内容、诊断、生成运营计划 | 只读全站内容与聚合指标；可提交变更计划 |
| 审批人 | 审核高风险运营计划 | 批准、部分批准、拒绝、要求修改 |
| 管理员 | 配置角色、权限、限额和策略 | 不通过自然语言直接执行高风险业务操作 |

M0 可只开放普通用户与受控测试运营账号；正式开放运营能力前必须完成服务端 RBAC。

## 2. 架构与职责

### 2.1 系统边界

```text
运营工作台 / 用户端
        │ HTTP + JWT
        ▼
app/agent
  ├── 会话与 Run API
  ├── 意图识别与结构化计划
  ├── 只读工具编排
  ├── 审批中断与恢复
  ├── 写工具执行与回读验证
  ├── Grounding / 安全策略
  └── 运行、成本、审计、评测
        │ gRPC（禁止直连业务库）
        ├── User / Authz
        ├── Feed / Content Query
        ├── Interaction / Feedback
        ├── Comment
        ├── Relation
        └── Stats
```

职责边界：

- Agent 负责理解任务、选择受控工具、组织事实、生成建议、驱动审批和验证结果。
- 业务微服务负责数据真实性、权限校验、状态机、幂等和最终写入。
- LLM 不负责权限判定、数值计算、状态迁移和数据库一致性。
- 过滤、聚合、同比/环比、阈值判断必须由确定性代码完成。

### 2.2 核心业务场景

#### 场景 A：内容盘点（M0）

示例：“列出最近发布且互动较高的 20 条视频，汇总热门评论。”

流程：

1. 从现有推荐池或个人内容列表获取候选；
2. 批量补充内容、作者、点赞、收藏、评论数；
3. 由代码计算候选池内的互动排序；
4. Agent 输出列表、证据来源和能力限制。

限制：只能称为“候选池内互动较高”，不能称为“全站播放最好”。

#### 场景 B：条件化内容查询（M1）

示例：“找出最近 7 天发布的悬疑短视频，时长小于 3 分钟。”

依赖 `SearchFeeds`、内容分类、标签和视频时长。筛选在 Feed 服务或 Agent 确定性节点完成，LLM 只负责把自然语言条件转换为受控查询条件。

#### 场景 C：运营诊断（M1）

示例：“找出最近 7 天曝光高但点击率低的视频，并分析问题。”

依赖 Stats 服务。指标值、均值、分位数和环比由代码计算；LLM 只基于结果和评论上下文生成解释与建议。

#### 场景 D：审批后执行（M2）

示例：“为这些低点击内容生成新标题，批准后替换。”

流程：

1. 读取当前值和版本；
2. 生成逐条 `before/after/reason` 计划；
3. 进入 `awaiting_approval`；
4. 审批人整单或部分批准；
5. 写工具携带 `request_id`、`plan_item_id` 和 `expected_version` 执行；
6. 回读比对，输出成功、失败和冲突明细。

#### 场景 E：效果复盘（M3）

示例：“评估上周标题优化计划是否提升 CTR。”

Agent 将执行计划与变更前后固定窗口指标关联，输出效果差异。严禁用两个未对齐的人群、流量入口或时间窗口直接得出因果结论。

### 2.3 功能需求

| 编号 | 优先级 | 需求 | 验收要点 |
|---|---|---|---|
| FR-001 | P0 | 会话与 Run 管理 | 创建、查询、取消、归档；用户只能访问本人资源 |
| FR-002 | P0 | 持久化异步执行 | HTTP 返回后任务可继续；进程重启后可恢复或明确失败 |
| FR-003 | P0 | 只读工具编排 | 工具白名单、参数 Schema、超时、重试、调用预算 |
| FR-004 | P0 | 事实溯源 | 输出的内容 ID、作者、数值和指标均能关联工具调用 |
| FR-005 | P0 | 能力降级 | 缺数据时返回 `unsupported` 或免责声明，不编造 |
| FR-006 | P0 | 身份与角色 | 服务端识别 user/operator/approver/admin；模型不能声明角色 |
| FR-007 | P1 | 内容条件检索 | 按时间、作者、类型、状态、城市、分类、标签、时长游标查询 |
| FR-008 | P1 | 指标诊断 | 曝光、播放、CTR、完播、观看时长及趋势/基线比较 |
| FR-009 | P1 | 结构化运营计划 | 每个条目包含目标、当前值、建议值、理由、风险和预期影响 |
| FR-010 | P1 | 人工审批 | 整单/部分批准、拒绝、超时；审批不可绕过 |
| FR-011 | P1 | 幂等执行与验证 | 重试不重复生效；版本冲突不覆盖人工新修改 |
| FR-012 | P1 | 运行可观测性 | 节点、工具、模型、token、耗时、错误和审批全留痕 |
| FR-013 | P1 | 推荐反馈 | 记录点击、采纳、负反馈，为后续偏好与评测提供数据 |
| FR-014 | P2 | 策略实验 | Agent 只能提交实验草案；需审批、灰度、指标护栏和回滚 |
| FR-015 | P2 | 离线评测平台 | 固定数据集回放，比较 Prompt/模型/工具版本的效果和成本 |

### 2.4 安全需求

- 防提示注入：业务内容和评论属于“不可信数据”，不得把其中指令提升为系统指令。
- 工具最小权限：普通用户工具不得接收任意 `user_id`；运营工具必须二次鉴权。
- 写操作双保险：Agent 审批状态校验 + 下游业务服务权限校验，任一失败都不得写入。
- 高风险动作：批量删除、批量下架、策略发布必须由审批人批准；单 run 默认最多 20 条。
- 数据保护：Prompt、工具日志和会话记录脱敏；密钥只来自环境变量或配置中心。
- SSRF/RCE：不提供任意 URL 请求、Shell、SQL 或代码执行工具。
- 输出安全：模型文本在前端展示前转义，防止内容和评论中的 XSS。

## 3. 数据模型

### 3.1 Agent 域

现有设计中的四张表继续保留：

- `agent_sessions`：会话；
- `agent_runs`：一次任务及状态；
- `agent_tool_calls`：工具调用留痕；
- `recommendation_records`：推荐与反馈。

正式运营闭环建议新增：

| 表/实体 | 优先级 | 用途 |
|---|---|---|
| `agent_run_events` | P0 | 保存 run 进度事件，支持 SSE 断线续传、排障和恢复 |
| `agent_approval_plans` | P1 | 审批单主记录、发起人、审批人、状态、过期时间 |
| `agent_approval_items` | P1 | 独立保存每个 before/after、批准结果、执行结果和幂等键 |
| `agent_eval_cases` / `agent_eval_results` | P2 | 固定任务、期望约束、模型/Prompt/工具版本与评测结果 |

仅将审批计划放在 checkpoint JSON 中不利于独立审计、查询和部分批准，M2 前应拆为结构化表。

### 3.2 业务域新增数据

| 领域 | 新增数据 | 目的 |
|---|---|---|
| User/Authz | 角色、权限、账号角色关系 | 识别运营、审批人和管理员 |
| Feed | 分类、标签、时长、内容语言、审核状态、版本号 | 内容检索、条件推荐、乐观锁更新 |
| Stats | 曝光、点击/播放、观看时长、完播、分享及日级聚合 | 运营诊断与效果复盘 |
| Interaction | 不感兴趣、举报、分享 | 负反馈、风险和推荐质量 |
| Operation Audit | 操作者、来源、request_id、前后值、原因 | 业务侧不可抵赖审计 |
| Experiment | 策略版本、实验、分流、护栏、回滚版本 | M3 推荐策略闭环 |

Stats 明细必须定义事件去重键、客户端时间、服务端接收时间、流量入口和会话标识，避免重复上报与口径混乱。

## 4. 接口与契约

### 4.1 Agent 对外接口

沿用 [08-api.md](./08-api.md) 的 session/run/approve/reject/feedback 接口，并补充：

| 方法 | 路径 | 优先级 | 用途 |
|---|---|---|---|
| GET | `/api/v1/agent/runs/:run_id/events` | P0 | SSE/长轮询获取进度，支持 `Last-Event-ID` |
| GET | `/api/v1/agent/runs/:run_id/audit` | P1 | 查看事实来源、工具调用、审批与执行明细 |
| POST | `/api/v1/agent/runs/:run_id/revise` | P1 | 审批人要求修改计划，不直接编辑模型 checkpoint |
| GET | `/api/v1/agent/capabilities` | P0 | 返回当前已启用场景、工具和数据限制 |

`POST /runs` 必须支持客户端 `request_id`，相同用户和 `request_id` 重试时返回同一 run。

### 4.2 下游业务接口

| 服务 | 建议新增 RPC | 优先级 | 说明 |
|---|---|---|---|
| User/Authz | `CheckPermission`、`GetUserRoles` | P0 | 每次运营读/写工具均服务端鉴权 |
| Feed | `SearchFeeds` | P0 | 受控条件、稳定 cursor、最大页大小；替代 Agent 拉全量后自行筛选 |
| Feed | `UpdateFeed` | P1 | field mask、`expected_version`、`request_id`、操作者与原因 |
| Feed | `BatchGetFeedSnapshots` | P1 | 返回内容当前版本与完整 before 快照 |
| Feed | `BatchOperateFeeds` | P1 | 受限批量上/下架；逐条结果，不要求跨条目大事务 |
| Stats | `ReportImpression`、`ReportPlay` | P1 | 高频事件上报，幂等接收 |
| Stats | `BatchGetFeedMetrics` | P1 | 按窗口、入口、作者/分类范围查询聚合指标 |
| Stats | `GetUserWatchHistory` | P1 | 真实观看历史，替代点赞/收藏近似 |
| Interaction | `DislikeFeed`、`ReportFeed`、`ShareFeed` | P1 | 负反馈与分享信号 |
| Audit | `ListOperationAudits` | P1 | Agent 验证、运营复盘和安全审计 |
| Experiment | `CreateDraftExperiment`、`PublishExperiment`、`RollbackExperiment` | P2 | 发布和回滚必须审批 |

### 4.3 `SearchFeeds` 最小查询条件

```text
author_ids[] / feed_types[] / statuses[] / city_codes[]
categories[] / tags[] / duration_sec_min / duration_sec_max
created_at_start / created_at_end
keyword（标题与正文的受控全文检索）
cursor / page_size / order_by
```

接口必须：

- 只返回调用者有权查看的状态；
- 默认只返回正常内容；
- 使用稳定 cursor，禁止 Agent 通过无上限 offset 扫描全表；
- 单页不超过 100，批量 ID 不超过 100；
- 返回查询实际采用的过滤条件，便于 Agent 解释。

### 4.4 工具契约

工具分为三类：

1. **Read**：可在授权范围内直接调用；
2. **Plan-only**：只生成计划，不产生业务写入；
3. **Write**：必须携带已批准的 `plan_item_id`、`request_id` 和权限上下文。

工具输出统一包含：

```json
{
  "ok": true,
  "data": {},
  "source": "stats.BatchGetFeedMetrics",
  "source_version": "v1",
  "fetched_at": 1785283200000,
  "trace_id": "..."
}
```

## 5. 错误码

沿用 60000 段并补充：

| 错误码 | 含义 | 对用户处理 |
|---|---|---|
| 60001 | 会话不存在 | 返回资源不存在 |
| 60002 | run 不存在或无权访问 | 不暴露资源是否真实存在 |
| 60003 | 审批状态冲突 | 刷新审批单状态 |
| 60004 | 模型调用失败 | 可重试，不执行写操作 |
| 60005 | 工具/Token 预算耗尽 | 返回部分结果和限制说明 |
| 60006 | 下游服务不可用 | 只读任务可返回部分结果；写任务停止 |
| 60007 | checkpoint 不兼容 | run 失败，要求重新发起 |
| 60008 | 能力未启用或数据不存在 | 正常展示“不支持”，不伪造结果 |
| 60009 | Grounding 校验失败 | 重生成一次，仍失败则删除不可信结论 |
| 60010 | 写入版本冲突 | 不覆盖新值，重新生成计划 |
| 60011 | run 幂等键冲突 | 返回原 run |
| 60012 | 操作超出数量/风险上限 | 拆单或升级审批 |

“当前没有播放数据”属于能力状态，优先用结构化 `unsupported` 结果表达，不应伪装成系统异常。

## 6. 缓存与一致性

### 6.1 Run 可靠执行

- `agent_runs.status` 是任务状态唯一事实源；
- HTTP handler 不在请求 goroutine 内完成长耗时 Graph；
- 使用独立 worker 消费持久化任务，至少保证 at-least-once；
- 每个 Graph 节点提交 checkpoint 和状态后再确认任务；
- 服务重启扫描 `running/executing` 超时任务并恢复或标记失败；
- 取消请求写入持久状态，节点边界检查取消标记。

可以用 RocketMQ 承载 run 调度，但 MySQL 中必须保留可恢复的任务记录，不能只依赖内存 goroutine。

### 6.2 写操作一致性

- Agent 层用 `plan_item_id` 防重复调度；
- 业务服务用 `request_id` 唯一键防重复写入；
- Feed 更新用 `expected_version` 乐观锁防止覆盖审批后的人工修改；
- 更新后发送 `feed.updated` 事件，并可靠清理详情、列表和推荐缓存；
- 回读验证失败不自动回滚未知状态，标记为 `needs_attention` 交由人工处理。

### 6.3 数据新鲜度

每个工具输出必须带 `fetched_at`。审批前若计划超过配置时长（默认 30 分钟），需重新读取 before 和 version；指标报告需明确统计窗口和数据延迟。

## 7. 测试策略

### 7.1 测试分层

| 类型 | 核心内容 |
|---|---|
| 单元测试 | 意图 Schema、工具参数、权限、状态机、数值计算、grounding、风险分级 |
| 工具契约测试 | proto 兼容、批量拆分、超时、错误映射、source 字段 |
| Graph 回放测试 | 固定工具结果，不调用真实模型或用固定模型输出 |
| 集成测试 | MySQL/Redis/RocketMQ + gRPC stub，覆盖重启恢复、审批与幂等 |
| 安全测试 | 提示注入、越权 user_id、绕过审批、任意 URL/SQL/工具调用 |
| 评测 | 任务完成率、事实一致性、建议可用性、成本与延迟 |
| 压测 | 并发 run、SSE 连接、工具扇出、下游降级 |

### 7.2 M0 验收标准

- 只读任务中，结果里的 `feed_id` 100% 来源于工具结果；
- 对播放量、CTR、完播率等未实现指标，编造具体数值的比例为 0；
- 用户无法读取他人的 session/run；
- 单 run 工具调用次数和 token 不超过配置上限；
- Agent 进程重启后，已受理 run 可恢复或进入明确终态，不永久停留在 `running`；
- 下游部分失败时返回已获得的事实和缺失项，不把缺失值当作 0；
- 每个结果可查看使用过的工具、时间和数据来源。

### 7.3 M2 验收标准

- 未经审批的业务写入数量为 0；
- 重复 approve、消息重投和 worker 重启不会重复生效；
- 版本冲突不会覆盖他人新修改；
- 每个写条目均有 before、after、审批人、执行结果和回读结果；
- 越权运营账号无法调用写 RPC，即使绕过 Agent 直接调用也会被下游拒绝。

### 7.4 建议 SLO

| 指标 | 目标 |
|---|---|
| Agent API 可用性 | ≥ 99.9%，不计 LLM provider 故障 |
| M0 只读 run 成功率 | ≥ 95% |
| 工具调用成功率 | ≥ 99%（下游健康时） |
| Grounding 通过率 | ≥ 99.5% |
| 未审批/越权写入 | 0 |
| 只读 run P95 | ≤ 20 秒 |
| 审批后开始执行 P95 | ≤ 5 秒 |

## 8. 演进与 TODO

### 8.1 后端新增功能总清单

| 优先级 | 功能 | 所属模块 | 解锁能力 |
|---|---|---|---|
| P0 | Agent 独立服务、配置、四张基础表、持久化 worker | Agent | 所有场景基础 |
| P0 | 角色/权限/RBAC 与服务端权限校验 | User/Authz、Gateway、Agent | 运营能力安全开放 |
| P0 | `SearchFeeds` 条件查询与稳定 cursor | Feed | 内容盘点、受控检索 |
| P0 | Run 幂等、取消、恢复、进度事件 | Agent | 可生产运行 |
| P0 | 工具白名单、Grounding、预算、审计 | Agent | 防幻觉与成本控制 |
| P0 | MQ 可靠事件/outbox、事件版本与幂等消费 | Feed/Common | 防止 Agent 读到长期不一致数据 |
| P1 | category/tags/duration/language/version 元数据 | Feed | 精准筛选与乐观锁 |
| P1 | 曝光/播放/停留/完播事件与 Stats 服务 | Stats、Gateway/客户端 | 运营诊断与真实观看历史 |
| P1 | 不感兴趣/举报/分享 | Interaction | 负反馈与内容风险 |
| P1 | 评论点赞、计数同步与热门评论更新链路 | Comment/Interaction | 可靠的评论热度和定性分析 |
| P1 | `UpdateFeed`、上下架、批量操作 | Feed | 审批执行闭环 |
| P1 | 业务操作审计 | Feed/Audit | 追责、回滚依据 |
| P1 | 审批计划结构化存储 | Agent | 独立审计、部分批准 |
| P1 | 内容审核与风险标签 | Feed/Moderation | 安全运营场景 |
| P1 | 指标口径、数据延迟和质量监控 | Stats | 避免错误诊断 |
| P2 | 推荐策略配置、实验、灰度和回滚 | Recommend/Experiment | 策略优化闭环 |
| P2 | Prompt/模型/工具版本与离线评测 | Agent | 可持续迭代 |
| P2 | OpenTelemetry 与成本配额 | Agent/Common | 生产治理 |

### 8.2 分阶段路线

| 里程碑 | 建议周期 | 范围 | 退出条件 |
|---|---|---|---|
| M0：可信只读 Agent | 2～3 周 | Agent 骨架、现有 RPC 工具、持久化 run、grounding、内容盘点 | 完成 §7.2；明确展示数据限制 |
| M1：数据与诊断 | 4～6 周 | RBAC、SearchFeeds、内容元数据、Stats、负反馈 | 可完成条件查询和真实指标诊断 |
| M2：审批执行闭环 | 3～4 周 | Update/上下架、审批表、幂等、版本冲突、业务审计 | 完成 §7.3 |
| M3：效果与策略实验 | 4～6 周 | 前后效果复盘、实验草案、灰度、护栏、回滚 | Agent 只能在审批后发布可回滚实验 |

周期为单个小组的粗略估算；Stats 事件采集涉及客户端或 Gateway 上报，需单独确认协作范围。

### 8.3 立项建议

1. 先批准 M0，只验证 Agent 工程闭环和可信度，不以“提升推荐效果”为验收目标。
2. M0 与 M1 的数据契约并行设计，优先确定 RBAC、`SearchFeeds` 和 Stats 指标口径。
3. M1 数据稳定后再进入 M2，避免 Agent 对错误或不完整指标执行批量修改。
4. M3 前先建立离线评测基线和人工运营对照组，没有基线不开放策略发布。

### 8.4 待产品确认

- 首批运营人员及审批人范围；
- 高风险动作清单和审批层级；
- 指标窗口、数据延迟容忍度和 CTR/完播率唯一口径；
- M0 是否对普通用户开放，还是仅内部运营试用；
- LLM provider、单 run 成本上限和数据出境要求；
- 内容审核由现有 Feed 服务扩展，还是独立 Moderation 服务承载。

## 关联文档

- [Agent 服务总览](./00-overview.md)
- [Agent 架构设计](./01-architecture.md)
- [Agent 工具契约](./02-tools.md)
- [状态与会话设计](./03-state-session.md)
- [人工审批流程](./04-approval.md)
- [场景分版设计](./05-scenarios.md)
- [后端扩展缺口](./06-backend-gaps.md)
- [可观测性设计](./07-observability.md)
- [Agent HTTP API](./08-api.md)
- [FeedMind Agent 需求文档](../../agent需求文档.md)
- [服务拆分方案](../service-design.md)
- [数据模型](../data-model.md)
