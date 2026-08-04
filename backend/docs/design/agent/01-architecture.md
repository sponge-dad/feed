# 架构与服务拆分

> 定义 FeedMind Agent 涉及的新增服务、部署形态、关键链路、配置约定，以及对现有服务的改造点与决策记录。

---

## 1. 概述与定位

本项目新增两个服务域，并对四个现有组件做增量改造：

- **Content 服务域**（新增）：视频内容理解与内容检索。
- **Agent 服务域**（新增）：会话、意图识别、Tool 编排、结果生成。
- **改造**：Gateway（request_id、埋点与Agent 路由）、Feed RPC（来源标记、Trace、详情批量接口）、Interaction 服务域（行为指标 + 兴趣画像）、`common/`（事件契约、拦截器）。

## 2. 架构与职责

```text
                       ┌────────────────────────客户端 ────────────────────────┐
                       │  刷流 / 埋点上报 / Agent 会话 / 为什么推荐             │
                       └───────────────────────────┬───────────────────────────┘
                                                   ▼
                                        Gateway :8080 (/api/v1)
                        JWT → user_id ；生成 request_id ；Metadata 透传
   ┌──────────────┬──────────────┬──────────────┬──────────────┬──────────────┬──────────────┐
   ▼              ▼              ▼              ▼              ▼              ▼
User :9001   Relation :9002  Feed :9003   Comment :9004  Interaction :9005  ┌─ Content :9007
                │                          │              └─ Agent   :9006
                                 │                          │                     │
                                 │                          │                     ▼
                                 │                          │              8 个只读 Tool
                                 │                          │              （只经 RPC 取数）
   写路径（异步解耦）             ▼                          ▼
feeds 落库 ─ RocketMQ feed-created ─┬─ Feed Worker（inbox/outbox/推荐池/同城池，进程内）
                                    └─ Content Worker（独立进程：下载→FFmpeg→ASR/OCR→多模态→入库→建索引）

埋点路径
客户端批量上报 ─ Gateway ─ RocketMQ feed-behavior-event ─ Behavior Worker
                                                            ├─ feed_behavior_events（明细）
                                                            ├─ feed_metrics_hourly（小时指标）
                                                            └─ user:interest:{uid}（兴趣画像）
```

## 3. 服务与进程清单

| 组件 | 形态 | 端口 | 归属库 | 职责 |
|------|------|------|--------|------|
| `app/agent/rpc` | 新增 gRPC | 9006 | `feed_agent` | 会话/消息/Run 管理、Eino 编排、Tool 注册与执行 |
| `app/content/rpc` | 新增 gRPC | 9007 | `feed_content` | 内容画像查询、内容检索、重试分析 |
| `app/content/worker` | 新增独立进程 | 无（metrics 9109） | `feed_content` | 消费 `feed-created`/`feed-deleted`，执行分析流水线 |
| `app/interaction/rpc` | 改造 | 9005 | `feed_interaction` | 新增指标查询与兴趣画像查询接口 |
| `app/interaction/worker` | 改造（进程内 worker 扩展） | 无 | `feed_interaction` | 新增消费 `feed-behavior-event`，聚合指标与兴趣 |
| `app/feed/rpc` | 改造 | 9003 | `feed_feed` | 新增来源标记、请求 Trace、详情/创作者列表接口 |
| `app/gateway` | 改造 | 8080 | - | request_id 中间件、埋点接口、Agent 与内容画像路由 |

服务发现沿用现有 etcd `127.0.0.1:2479`，Key 分别为 `agent.rpc`、`content.rpc`。

**为什么 Content Worker 必须独立进程**：FFmpeg 是 CPU/IO 密集型子进程，且需要临时磁盘配额；跑在 Feed 或 Content RPC 进程内会污染在线请求的延迟与内存，也无法独立限流与扩缩容。

## 4. 关键链路

### 4.1 发布 → 内容画像

```text
POST /api/v1/feeds → Feed RPC.CreateFeed
  1. 写 feeds（MySQL）
  2. 发送 feed-created（event_id = uuid）
  3. 立即返回（分析不阻塞发帖）
Content Worker 消费 feed-created
  4. feed_type != 2（非视频）→ 直接 ACK 丢弃
  5. 幂等判定（feed_id + media_hash + model_version）
  6. PENDING → DOWNLOADING → EXTRACTING → ASR_RUNNING → OCR_RUNNING
     → VISION_RUNNING → INDEXING → COMPLETED
  7. 写 feed_content_profiles + 写 ES/向量索引
```

### 4.2 刷流 → 埋点 → 指标

```text
GET /api/v1/feeds/timeline（响应含 request_id + 每条 feed 的 source）
  → 客户端记录 (request_id, feed_id, position)
  → 内容真正可视 → EXPOSE；起播 → PLAY；3s → EFFECTIVE_PLAY；结束 → FINISH/SKIP
POST /api/v1/feeds/behaviors（批量 ≤ 50 条）
  → Gateway 校验 + 服务端补全 author_id →发送 feed-behavior-event
  → Behavior Worker：event_id 幂等 → 明细落库 → Redis 累加 → 定时 flush 小时表→ 更新兴趣ZSet
```

### 4.3 Agent 问答

```text
POST /api/v1/agent/sessions/{sessionId}/messages
  → Agent RPC：创建 run_id（CREATED）
  → 意图识别（UNDERSTANDING，LLM）
  → 权限预检（Go 代码，非模型）
  → Tool 选择与参数校验（TOOL_CALLING）
  → 调用 Feed/Content/Interaction/Relation/User RPC 取数
  → 结构化结论计算（ANALYZING，Go 代码算指标与对比）
  → 语言组织（GENERATING，LLM 只允许引用已给事实）
  → SUCCEEDED / FAILED，落agent_runs + agent_tool_calls
```

## 5. 配置约定

`app/agent/rpc/etc/agent.yaml`（示例，密钥只允许来自环境变量）：

```yaml
Name: agent.rpc
ListenOn: 0.0.0.0:9006
Etcd:
  Hosts:
    - 127.0.0.1:2479
  Key: agent.rpc

FeedRpc:        { Etcd: { Hosts: [127.0.0.1:2479], Key: feed.rpc }, Timeout: 3000 }
ContentRpc:     { Etcd: { Hosts: [127.0.0.1:2479], Key: content.rpc }, Timeout: 5000 }
InteractionRpc: { Etcd: { Hosts: [127.0.0.1:2479], Key: interaction.rpc }, Timeout: 3000 }
RelationRpc:    { Etcd: { Hosts: [127.0.0.1:2479], Key: relation.rpc }, Timeout: 3000 }
UserRpc:        { Etcd: { Hosts: [127.0.0.1:2479], Key: user.rpc }, Timeout: 3000 }

Mysql:
  DataSource: ${AGENT_MYSQL_DSN}      # feed_agent 库
Redis:
  Host: 127.0.0.1:6379
  Type: node

Model:
  Provider: ark                       # eino-ext/components/model/ark
  APIKey: ${ARK_API_KEY}
  ChatModel: ${ARK_CHAT_MODEL}
  TimeoutMs: 20000
  MaxOutputTokens: 1024

AgentLimit:
  MaxToolCalls: 8                     # 单次 Run 最多 Tool 调用
  MaxModelCalls: 4                    # 单次 Run 最多模型调用
  RunTimeoutMs: 60000
  MaxInputRunes: 2000
  HistoryWindow: 20                   # 送入模型的历史消息条数上限
  UserQpm: 10                         # 单用户每分钟 Run 上限

InternalUserIDs: []                   # 内部用户白名单（Trace/明细类Tool）

Prometheus: { Host: 0.0.0.0, Port: 9108, Path: /metrics }
Telemetry:  { Name: agent.rpc, Endpoint: http://127.0.0.1:4318/v1/traces, Sampler: 1.0, Batcher: otlphttp }
```

`app/content/worker/etc/content-worker.yaml` 关键项：

```yaml
Analysis:
  MaxConcurrency: 2                   # 并发分析任务数（FFmpeg 进程数上限）
  MaxRetry: 3
  FFmpegPath: /usr/bin/ffmpeg
  FFmpegTimeoutSec: 120
  MaxVideoBytes: 209715200            # 200MB
  MaxVideoDurationSec: 600
  KeyFrameMax: 20                     # 关键帧上限
  TempDir: /var/tmp/feedmind
  AllowedMediaHosts: ["feed-1250000000-1317318750.cos.ap-guangzhou.myqcloud.com"]
  TranscriptMaxRunes: 4000            # 送模型前截断
```

端口分配（Prometheus）：gateway 9101、user 9102、relation 9103、feed 9104、comment 9105、interaction 9106、content 9107→metrics 9110、agent 9108、content-worker 9109。

## 6. 对现有服务的改造点

| 位置 | 改造 | 详见 |
|------|------|------|
| `app/gateway/internal/middleware/` | 新增 `RequestIDMiddleware`，写入 ctx 与响应头 | [02](./02-request-trace.md) |
| `common/response/response.go` | `requestID(ctx)` 兼容 typed key，避免恒空 | [02](./02-request-trace.md) |
| `common/interceptors/`（新增） | zRPC client/server 拦截器透传 `x-request-id`等 Metadata | [02](./02-request-trace.md) |
| `api/proto/feed/feed.proto` | `FeedBrief`/`FeedInfo` 增 `source`；新增 5 个查询方法 | [11](./11-api.md) |
| `app/feed/rpc/internal/logic/*timeline*.go` | 召回时打来源标记并写 Trace | [02](./02-request-trace.md) |
| `common/event/behavior/`（新增） | `feed-behavior-event` 事件契约 | [03](./03-behavior-event.md) |
| `app/interaction/rpc/internal/worker/worker.go` | 增加行为事件订阅与聚合 | [03](./03-behavior-event.md) |
| `api/proto/interaction/interaction.proto` | 新增指标与兴趣画像查询方法 | [11](./11-api.md) |
| `common/errorx/errorx.go` | 新增 Content（15000~15999）与 Agent（16000~16999）码段 | [11](./11-api.md) |
| 各服务 `etc/*.yaml` | 补`Prometheus` 与 `Telemetry` | [12](./12-observability.md) |

## 7. 决策记录（含与需求文档的差异）

| 编号 | 决策 | 理由 | 与需求文档关系 |
|------|------|------|----------------|
| D1 | 行为指标与兴趣画像放在 **Interaction 服务域**（`feed_interaction` 库+ 进程内 worker） | 复用已有的 Redis 计数与 MQ 消费框架，避免第8 个服务；兴趣画像与互动行为同源 | 与需求 §11「Interaction RPC 增加指标接口」「或新增画像模块」一致 |
| D2 | Content Worker 独立进程，Content RPC 独占 9007 | FFmpeg 资源隔离；RPC 只做查询 | 需求 §9 一致，端口为本文补充 |
| D3 | HTTP 路径统一带 `/api/v1` 前缀（如 `/api/v1/agent/sessions`） | 与现有 `docs/design/api-spec/README.md` §1 约定一致 | 需求 §12 写作 `/api/agent/...`，此处按仓库既有规范收敛 |
| D4 | 行为上报接口路径为 `/api/v1/feeds/behaviors`（复数，批量） | 与 `/api/v1/feeds/...` 资源命名一致 | 需求 §12 为 `/api/feed/behaviors` |
| D5 | 小时指标表写入**绝对值**而非增量 | 即使 flush 重复执行也不会重复累加，双重保证幂等 | 需求 §16「重复消费不重复累加」的实现选择 |
| D6 | 第一版检索使用 Elasticsearch（BM25 + kNN），Redis Stack 作为备选 | 字幕/摘要需要全文检索 + 向量混排，MySQL LIKE 不足 | 需求 §13 允许二者择一 |
| D7 | 内部身份用配置白名单，不引入新角色表 | 第一版仅内部排障使用，避免侵入 User 服务 | 需求 §5 的最小实现 |

## 8. 演进与 TODO

- 画像模块独立为 `profile.rpc`，与Interaction 写路径彻底解耦。
- Content Worker 支持任务队列优先级（新发布优先、重试降级）。
- 检索层引入独立 `search.rpc`，屏蔽 ES/向量库差异。
- Agent 支持流式输出（SSE），当前为「创建 Run + 轮询结果」。

---

## 关联文档

- [总览与定位](./00-overview.md)
- [请求标识与 Feed 链路追踪](./02-request-trace.md)
- [内容分析服务](./04-content-analysis.md)
- [Agent 服务设计](./09-agent-service.md)
- [接口契约](./11-api.md)
- [系统架构](../architecture.md)
- [服务拆分](../service-design.md)
