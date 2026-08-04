# 可观测性

> 定义 Prometheus 指标清单、结构化日志字段、OpenTelemetry 接入方式与告警规则。属实施阶段五。

---

## 1. 概述与现状

现状：各服务 `etc/*.yaml` 均**未配置** `Prometheus` 与 `Telemetry`，Prometheus 在 `go.mod` 中仅为间接依赖；日志为 go-zero `logx` 默认输出，未绑定业务标识字段。本篇把它们补成正式能力。

三条主线：

| 主线 | 手段 | 解决的问题 |
|------|------|-----------|
| 指标 | go-zero `Prometheus` 配置 + 自定义metric | 系统健康度、成功率、耗时分布 |
| 日志 | `logx` 字段化 + 统一字段名 | 单请求/单事件排障 |
| 链路 | go-zero `Telemetry`（OTel） | 跨服务/跨 MQ 调用关系 |

## 2. 指标清单

### 2.1 Agent

| 指标 | 类型 | Labels | 说明 |
|------|------|--------|------|
| `agent_run_total` | Counter | `intent`,`status` | Run 总数（含失败/取消） |
| `agent_run_duration_seconds` | Histogram | `intent` | Run 端到端耗时 |
| `agent_tool_call_total` | Counter | `tool_name`,`status` | Tool 调用数（`status`=success/failed/denied/timeout） |
| `agent_tool_call_duration_seconds` | Histogram | `tool_name` | Tool 耗时 |
| `agent_llm_request_total` | Counter | `stage`,`status` | 模型调用数（`stage`=understanding/generating） |
| `agent_llm_tokens_total` | Counter | `type` | token 用量（prompt/completion），成本核算 |
| `agent_llm_guard_total` | Counter | `reason` | 输出校验拦截数（伪造 feed_id / 数字不一致） |
| `agent_limit_exceeded_total` | Counter | `limit_type` | 超限次数（tool/model/timeout/rate） |

### 2.2Content

| 指标 | 类型 | Labels | 说明 |
|------|------|--------|------|
| `content_analysis_total` | Counter | `result` | 分析任务数（completed/failed/skipped/degraded） |
| `content_analysis_duration_seconds` | Histogram | `stage` | 各阶段耗时（download/extract/asr/ocr/vision/index） |
| `content_analysis_failed_total` | Counter | `stage`,`reason` | 失败分布（定位是下载还是模型问题） |
| `content_analysis_inflight` | Gauge | - | 当前并发任务数（应≤ `MaxConcurrency`） |
| `content_search_total` | Counter | `status` | 检索请求数 |
| `content_search_duration_seconds` | Histogram | `backend` | 检索耗时（es/redis） |
| `content_search_empty_total` | Counter | `reason` | 空结果数（no_match/filtered_out） |

### 2.3 行为与 Feed

| 指标 | 类型 | Labels | 说明 |
|------|------|--------|------|
| `feed_behavior_event_total` | Counter | `action_type`,`result` | 事件数（accepted/rejected/duplicated/send_failed） |
| `feed_behavior_consume_lag` | Gauge | `topic` | 消费堆积（事件时间与消费时间差的 P99） |
| `feed_metrics_flush_total` | Counter | `result` | 指标落库批次数 |
| `feed_request_total` | Counter | `tab`,`status` | Timeline 请求数 |
| `feed_returned_count` | Histogram | `tab` | 单次返回条数分布（发现空刷/少刷） |
| `feed_source_count_total` | Counter | `source` | 各来源命中数（观察推拉结合实际分布） |
| `feed_trace_write_failed_total` | Counter | - | Trace 写入失败（降级发生频率） |
| `interest_update_total` | Counter | `result` | 兴趣更新数（updated/skipped_no_profile/deduped） |

### 2.4 通用

沿用 go-zero 内置：`http_server_requests_duration_ms`、`http_server_requests_code_total`、`rpc_server_requests_duration_ms`、`rpc_client_requests_code_total`。补充 `request_id_missing_total{service}`，用于发现漏改的透传点。

配置示例（每个服务 `etc/*.yaml`）：

```yaml
Prometheus:
  Host: 0.0.0.0
  Port: 9104            # 端口分配见 01-architecture.md §5
  Path: /metrics
```

## 3. 日志规范

必须字段（缺一即视为不合规）：

| 字段 | 来源 | 出现位置 |
|------|------|----------|
| `request_id` | Metadata /事件体 | 所有 HTTP、RPC、MQ 消费日志 |
| `trace_id` | OTel（`logx` 自动注入 `traceId`） | 同上 |
| `run_id` | Agent | Agent 及其调用的下游 |
| `event_id` | 事件体 | MQ 消费日志 |
| `user_id` | 业务参数 | 涉及用户的操作 |
| `feed_id` | 业务参数 | 涉及内容的操作 |
| `tool_name` | Agent | Tool 调用日志 |
| `service_name` |配置 `Name` | 所有日志 |

实现方式：服务端拦截器统一 `logx.WithContext(ctx)` + `logx.WithFields(...)`，业务代码直接 `l.Infow("...", logx.Field("feed_id", id))`，不再手工拼字符串。

MQ 消费日志额外要求（现有 `common/mq/consumer.go` 只打印 `topic/msgId/body/err`，需增强）：

```text
必须包含：topic、event_id、feed_id、user_id、action_type、reconsume_times、result、cost_ms
禁止包含：完整字幕、OCR 全文、媒体签名地址、JWT
```

`reconsume_times` 来自 `primitive.MessageExt.ReconsumeTimes`，是判断「是否即将进死信」的关键。

## 4. 链路追踪

```yaml
Telemetry:
  Name: feed.rpc
  Endpoint: http://127.0.0.1:4318/v1/traces
  Sampler: 1.0        # 开发/测试全采，生产 0.1
  Batcher: otlphttp
```

- HTTP → RPC 由 go-zero 自动串联。
- **MQ 断链问题**：RocketMQ 生产/消费不在 OTel 自动埋点范围内，需手动在事件体中携带 `traceparent`（新增字段），消费端用 `otel.GetTextMapPropagator().Extract`恢复上下文，使「发帖 → 内容分析」在同一条链路上。
- Agent 自定义 Span：`agent.run`（父）→ `agent.intent`、`agent.tool.{name}`、`agent.llm.{stage}`，Span 属性带 `run_id`、`intent`、`tool_name`，不带用户输入原文。
- FFmpeg/外部模型调用作为子 Span，记录耗时与结果状态，不记录输入内容。

## 5. 告警规则（建议阈值）

| 告警 | 条件 | 级别 |
|------|------|:---:|
| 内容分析失败率高 | `content_analysis_failed_total` 5min 增量 / 总量 > 20% | P1 |
| 分析积压 | `feed_behavior_consume_lag{topic="feed-created"}` > 300s | P1 |
| 行为消费堆积 | `feed_behavior_consume_lag{topic="feed-behavior-event"}` > 120s 持续 10min | P2 |
| Tool 成功率低 | `agent_tool_call_total{status="success"}` / 总量 < 99%（10min） | P2 |
| 模型不可用 | `agent_llm_request_total{status="failed"}` 5min > 10 | P1 |
| 输出被拦截 | `agent_llm_guard_total` 5min > 5（说明 Prompt 或模型异常） | P2 |
| request_id 缺失 | `request_id_missing_total` 持续增长 | P3 |
| Timeline 空返回 | `feed_returned_count` P50 == 0 持续 5min | P1 |
| 指标落库失败 | `feed_metrics_flush_total{result="failed"}` 5min > 3 | P2 |

## 6. 排障入口

| 场景 | 入口 |
|------|------|
| 用户反馈「刷不出内容」 | 用响应中的 `request_id`查 `GET /api/v1/internal/feed-requests/{requestId}/trace`，看各数据源返回量|
| 创作者反馈「数据不对」 | 用 `feed_id` 查小时指标 + 明细采样，核对 Redis 与 MySQL 差值 |
| 「分析一直没出来」 | 查 `feed_content_profiles.analysis_status` + `error_message` + Worker 日志（`feed_id`） |
| 「Agent 回答不对」 | 用 `run_id` 查 `agent_runs` / `agent_tool_calls`，看意图、Tool 与耗时 |

## 7. 测试策略

| 层次 | 用例 |
|------|------|
| 单元 | 指标注册无重复、label 基数可控（禁止把 `feed_id`/`user_id` 作为 label） |
| 集成 | `/metrics` 可抓取且包含本篇声明的关键指标 |
| 集成 | 一次请求的日志中 `request_id` 在 Gateway/Feed/Interaction 均一致 |
| 集成 | 发帖 → 内容分析在同一 trace 下（MQ 传播生效） |

**label 基数红线**：禁止使用 `feed_id`、`user_id`、`request_id` 等高基数值作为 metric label，只允许出现在日志与 trace 中。

## 8. 演进与 TODO

- Grafana 面板：Agent 概览、内容分析流水线、埋点数据质量、Feed 分发来源分布。
- 数据质量看板：曝光/播放比例异常、无曝光的播放占比、客户端时间偏差分布。
- 成本看板：模型 token、ASR/OCR 调用量与单条视频分析成本。
- 采样策略动态下发（配置中心），避免改配置重启。

---

## 关联文档

- [请求标识与 Feed 链路追踪](./02-request-trace.md)
- [行为事件采集与指标聚合](./03-behavior-event.md)
- [内容分析服务设计](./04-content-analysis.md)
- [Agent 服务设计](./09-agent-service.md)
- [验收标准与测试策略](./14-acceptance-test.md)
