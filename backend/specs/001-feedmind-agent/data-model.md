# Phase 1 — 数据模型

**Feature**: FeedMind Agent | **Date**: 2026-08-07 | **Plan**: [plan.md](./plan.md)

本文件是**建表脚本与 `goctl model` 生成的依据**。所有 DDL 以 `docs/design/agent/10-data-model.md` 为准，此处补充实体关系、状态机与实施注意事项。

---

## 1. 库归属与建表脚本

现有库遵循 `feed_<service>` 命名。本特性新增：

| 库 | 归属服务 | 新增表 | 建表脚本 |
|----|---------|--------|---------|
| `feed_content` | Content RPC / Worker | `feed_content_profiles` | `deploy/sql/content.sql`（新建） |
| `feed_interaction`（已有） | Interaction RPC / Behavior Worker | `feed_behavior_events`、`feed_metrics_hourly`、`user_interest_profiles` | `deploy/sql/interaction.sql`（追加） |
| `feed_agent` | Agent RPC | `agent_sessions`、`agent_messages`、`agent_runs`、`agent_tool_calls` | `deploy/sql/agent.sql`（新建） |

> ⚠️ **实施注意**：MySQL 容器**只在首次启动执行初始化脚本**（`AGENTS.md` §9 已知陷阱 #2）。已有环境必须**手动执行**新增脚本，否则表不存在。

**ID 生成约束**：`session_id` / `run_id` / `message_id` 一律用 `common/idgen`（Snowflake），**禁止自增主键作为业务 ID**（宪法 VI）。`feed_content_profiles`、`feed_behavior_events`、`feed_metrics_hourly`、`agent_tool_calls` 的 `id` 是纯物理自增主键，不对外暴露。

---

## 2. 实体关系

```text
                ┌──────────────┐
                    │    feeds     │ (已有，feed_feed 库)
                    └──────┬───────┘
                           │ feed_id (跨库逻辑外键，无FK 约束)
        ┌──────────────────┼──────────────────┬─────────────────────┐
        ▼                  ▼                  ▼                     ▼
┌───────────────────┐ ┌──────────────────┐ ┌──────────────────┐   │
│feed_content_      │ │feed_behavior_    │ │feed_metrics_     │   │
│profiles           │ │events            │ │hourly            │   │
│(feed_content)│ │(feed_interaction)│ │(feed_interaction)│   │
│                   │ ││ │                  │   │
│uk: feed_id        │ │uk: event_id      │ │uk:(feed_id,      │   │
│1feed = 1 画像     │ │明细，采样10%曝光│ │    stat_hour)    │   │
└───────────────────┘ └────────┬─────────┘ │写绝对值           │   │
        │                      │           └──────────────────┘   │
        │ ES 索引               │ 聚合                │
        ▼                      ▼                                  │
┌───────────────────┐ ┌──────────────────────┐                    │
│feed_content_v1    │ │user_interest_profiles│                    │
│(Elasticsearch)    │ │(feed_interaction)    │                    │
│_id = feed_id      │ │uk: user_id           │                    │
│只作索引不作数据源│ │JSON 快照 + version    │                    │
└───────────────────┘ └──────────────────────┘                    │
                                                │
   ┌───────────────────────── feed_agent 库 ─────────────────────┐ │
   │                                                             │ │
   │ agent_sessions ──1:N──▶ agent_messages│ │
   │       │                │ run_id                      │ │
   │       └───────1:N──────▶ agent_runs ──1:N──▶ agent_tool_calls│ │
   │                uk:(run_id, seq) │ │
   │全部 Snowflake ID；引用的 feed_id 来自 Tool 结果 ◀──────────┼─┘
   └─────────────────────────────────────────────────────────────┘
```

**跨库引用原则**：`feed_id` / `user_id` / `author_id` 是**跨库逻辑外键，不建 FK 约束**（沿用现有做法）。跨库查询在应用层两步走——例如"同类对比"先按 `category` 从 `feed_content_profiles` 取 `feed_id` 列表，再查 `feed_metrics_hourly`。

---

## 3. feed_content_profiles（内容画像）

**归属**: `feed_content` 库 | **DDL**: `docs/design/agent/10-data-model.md` §2

### 3.1 字段分组

| 组| 字段 | 说明 |
|----|------|------|
| **标识** | `feed_id`(uk)、`author_id`、`media_hash` | `author_id` 冗余存储，避免每次权限校验回查 Feed RPC |
| **结构化标签** | `category`、`topics`(JSON)、`objects`(JSON)、`scenes`(JSON)、`styles`(JSON) | `category` 必须来自**白名单类目** |
| **文本内容** | `summary`、`transcript`(MEDIUMTEXT)、`transcript_segments`(JSON)、`ocr_text` | `transcript_segments` 格式 `[{start_ms,end_ms,text}]`，支撑"开头 3 秒讲了什么" |
| **媒体元信息** | `language`、`media_duration_ms`、`key_frame_count` | `media_duration_ms` 由 **ffprobe 探测**，替代客户端上报（不可信） |
| **流程状态** | `analysis_status`、`degraded`、`retry_count`、`model_version`、`error_message` | 见 §3.2 状态机 |
| **时间** | `analyzed_at`、`created_at`、`updated_at` | 全部 `DATETIME(3)` 毫秒精度 |

### 3.2 分析状态机

```text
PENDING ──▶ DOWNLOADING ──▶ EXTRACTING ──▶ ASR_RUNNING ──▶ OCR_RUNNING
                                                │
                                              ┌──────────────────┘
                                              ▼
                                       VISION_RUNNING ──▶ INDEXING ──▶ COMPLETED
                
   任意阶段失败 ──▶ FAILED（整单失败，无可用结果）
   部分阶段失败但有结果 ──▶ COMPLETED + degraded=1
   非视频类型(feed_type != 2) ──▶ DISABLED（直接 ACK 丢弃）
```

**`analysis_status` × `degraded` 组合语义**（正交建模，详见 research.md R11）：

| status | degraded | 含义 | Agent 行为 |
|--------|:--------:|------|-----------|
| `COMPLETED` | 0 | 全部成功 | 正常引用 |
| `COMPLETED` | 1 | 部分阶段失败但结果可用 | 引用可用字段，**必须告知信息不完整** |
| `FAILED` | - | 整单失败 | 返回 `PROFILE_NOT_READY`(15002) |
| `PENDING` / `*_RUNNING` | - | 处理中 | 返回 `PROFILE_NOT_READY`(15002) |
| `DISABLED` | - | 非视频，不分析 | 返回"该内容类型不支持分析" |

### 3.3 索引设计

| 索引 | 用途 |
|------|------|
| `uk_feed_id` | **幂等主保障**——一 feed 一画像 |
| `idx_category_status` | 按类目筛选已完成画像（同类对比、检索预筛） |
| `idx_status_updated` | **运维查询**：捞取卡住的任务（`status IN (*_RUNNING) AND updated_at < now-6min`） |
| `idx_author` | 作者维度查询 |

> **注意**：`media_hash + model_version` 用于**判断是否需要重跑**，**不建唯一键**——否则同一 feed 换模型版本会产生多行（详见 research.md R10）。

### 3.4 安全约束

- `error_message` **必须脱敏，禁止含签名地址**（COS 签名 URL 含临时凭证）
- `transcript` / `ocr_text` 是**用户生成内容**，Agent 引用时视为不可信数据（Prompt 注入通道，见 research.md R8）

---

## 4. feed_behavior_events（行为明细）

**归属**: `feed_interaction` 库 | **DDL**: `10-data-model.md` §3

### 4.1 关键字段

| 字段 | 说明 |
|------|------|
| `event_id`(uk) | **服务端生成 uuid**，幂等键 |
| `request_id` | 来源 Timeline 请求，串联"推荐了什么"与"用户看了什么" |
| `action_type` | `EXPOSE` / `PLAY` / `EFFECTIVE_PLAY` / `FINISH` / `SKIP` / `SHARE` |
| `position` | 在 Timeline 中的位置，用于分析首屏效果 |
| `watch_duration_ms` | 观看时长（客户端上报） |
| `media_duration_ms` | 媒体总时长（**服务端从画像表补全**，不信客户端） |
| `event_time` | 行为发生时间（客户端，**已纠偏**） |

### 4.2 采样与保留策略

| 项 | 策略 | 理由 |
|----|------|------|
| EXPOSE | **采样 10% 入库** | 日活 10万 × 100曝光 = 1000 万/日，全量不可承受 |
| 其他行为 | 全量入库 | 量级小 2 个数量级 |
| **Redis 小时指标** | **全量累加，不采样** | 指标必须精确，创作者看到的数字不能跳变 |
| 保留期 | 30 天，按 `event_time` 分批删除 | 每批 ≤ 2000 行 + sleep，避免大事务与主从延迟 |

详见 research.md R12。

### 4.3 隐私约束

**不含 IP / UA / 设备号等隐私字段**——只记录`user_id` + 行为 + 时间。

### 4.4 三层幂等

| 层 | 机制 | 作用 |
|---|------|------|
| 1 | Redis `behavior_event:{event_id}`（TTL 24h） | 消费幂等（快路径） |
| 2 | `uk_event_id` 唯一键 | **兜底**——Redis 丢 key 时仍不重复插入 |
| 3 | 小时表写绝对值 | flush 幂等（见 §5） |

**曝光去重**：`behavior:expose:{request_id}:{feed_id}`（TTL 24h）——同一请求同一 feed 只算一次曝光。

---

## 5. feed_metrics_hourly（小时指标）

**归属**: `feed_interaction` 库 | **DDL**: `10-data-model.md` §4

### 5.1 指标字段

| 类别 | 字段 |
|------|------|
| **行为原子指标** | `expose_count`、`play_count`、`effective_play_count`、`finish_count`、`skip_count`、`watch_duration_ms` |
| **互动指标** | `like_count`、`collect_count`、`comment_count`、`share_count` |
| **维度** | `feed_id`、`author_id`、`stat_hour`（整小时，如 `2026-08-04 13:00:00`） |

**派生率在应用层计算**（不落库）：完播率 = `finish_count / play_count`、有效播放率 = `effective_play_count / play_count`、快划率 = `skip_count / play_count`。

### 5.2 写入语义（核心决策）

```sql
INSERT INTO feed_metrics_hourly (...) VALUES (...)
ON DUPLICATE KEY UPDATE
  expose_count = VALUES(expose_count),   -- ✅ 绝对值覆盖
  play_count   = VALUES(play_count),
  ...
-- ❌ 禁止：expose_count = expose_count + VALUES(expose_count)
```

**理由**：真实值来源是 Redis Hash `feed:metrics:h:{feed_id}:{yyyyMMddHH}`，MySQL 只是快照落盘。写绝对值使**重复 flush 不会重复累加**（详见 research.md R4）。

### 5.3 索引

| 索引 | 用途 |
|------|------|
| `uk_feed_hour (feed_id, stat_hour)` | 幂等键+ 单 feed 时序查询 |
| `idx_stat_hour` | 同类对比（配合画像表两步查询） |
| `idx_author_hour` | 创作者维度汇总 |

### 5.4 容量与清理

- 活跃 feed 10 万 × 24 小时 = **240 万行/日**
- 保留 180 天；30 天后可压缩为日粒度
- 演进：按 `stat_hour` 做分区表

---

## 6. user_interest_profiles（兴趣画像快照）

**归属**: `feed_interaction` 库 | **DDL**: `10-data-model.md` §5

| 字段 | 说明 |
|------|------|
| `user_id`(uk) | 一用户一快照 |
| `interest_json` | `{categories:[], topics:[], total_actions, window_days}` |
| `version` | **单调递增**，识别新旧快照（防并发写回退） |
| `calculated_at` | 计算时间，Agent 回答时可标注"数据截至" |

**双写模型**：
- **Redis ZSet `user:interest:{user_id}`**（TTL 90d）：实时权重累加，成员格式 `t:{topic}` / `c:{category}`
- **MySQL 快照**：定时从ZSet 计算并落盘，供 Redis 失效时兜底

**时间衰减**：权重随时间衰减，具体公式见 `docs/design/agent/06-user-interest.md`。

---

## 7. Agent 自有表（feed_agent 库）

**DDL**: `10-data-model.md` §6

### 7.1 agent_sessions

| 字段 | 说明 |
|------|------|
| `id` | **Snowflake** session_id |
| `user_id` | 归属用户，**`SendMessage`/`GetSessionMessages` 必须校验 `session.user_id == ctx.user_id`** |
| `title` | 由**首条消息截断**生成 |
| `status` | `ACTIVE` / `CLOSED` |
| `last_active_at` | 配合 `idx_user_active` 支撑"最近会话"列表 |

### 7.2 agent_messages

| 字段 | 说明 |
|------|------|
| `id` | Snowflake message_id |
| `session_id`、`run_id` | `run_id=0` 表示用户消息（尚未产生 Run） |
| `role` | `user` / `assistant` |
| `content` | **原文保存用户消息**（用于体验优化），但**禁止含 JWT / 媒体签名地址**；超长截断 |

> **Tool 原始输出不入 messages**——仅入本轮上下文，避免历史膨胀与敏感数据落库。

### 7.3 agent_runs（Run 状态机）

```text
CREATED ──▶ UNDERSTANDING ──▶ TOOL_CALLING ⇄ (多轮) ──▶ ANALYZING ──▶ GENERATING ──▶ SUCCEEDED
   │              │                  │                      │              │
   │              └──────────────────┴──────────────────────┴──────────────┴──▶ FAILED
   └──▶ CANCELLED (用户主动取消)

特例：UNDERSTANDING ──▶ GENERATING（无需取数的问题，如闲聊拒答）
```

| 状态 | 含义 | 允许后继 |
|------|------|---------|
| `CREATED` | Run 已创建 | `UNDERSTANDING`、`FAILED`、`CANCELLED` |
| `UNDERSTANDING` | 模型识别意图 | `TOOL_CALLING`、`GENERATING`、`FAILED` |
| `TOOL_CALLING` | 执行 Tool 取数 | `ANALYZING`、`TOOL_CALLING`、`FAILED` |
| `ANALYZING` | **Go 代码**计算指标/对比/过滤 | `GENERATING`、`FAILED` |
| `GENERATING` | 模型组织语言 | `SUCCEEDED`、`FAILED` |
| `SUCCEEDED` / `FAILED` / `CANCELLED` | 终态 | — |

**可观测字段**：`intent`、`tool_call_count`、`model_call_count`、`prompt_tokens`、`completion_tokens`、`error_code`、`error_message`、`cost_ms`。

**状态同步**：流转即时写 MySQL，并在 Redis `agent:run:{run_id}`（Hash，TTL 1h）缓存最新状态，供 `GetRun` **低成本轮询**。

### 7.4 agent_tool_calls（留痕）

| 字段 | 脱敏要求 |
|------|---------|
| `input_digest` | **只存白名单字段摘要** |
| `output_digest` | **只存摘要**（条数、feed_id 列表、关键指标），**禁止字幕/OCR 全文** |
| `status` | `SUCCESS` / `FAILED` / `DENIED` / `TIMEOUT` |
| `uk_run_seq (run_id, seq)` | seq 从 1 开始，保证同Run 内顺序唯一 |

### 7.5 保留期

| 表 | 保留 |
|----|------|
| `agent_messages`、`agent_runs` | 90 天 |
| `agent_tool_calls` | 30 天 |

---

## 8. Redis Key 清单

**约定**：各服务在 `internal/keys/` **集中定义**，禁止散落硬编码（参考 `app/feed/rpc/internal/keys/keys.go`）。

| Key | 结构 |内容 | TTL | 归属 |
|-----|------|------|-----|------|
| `feed:trace:{request_id}` | Hash | `meta` + `f:{feed_id}`→source | 24h（**生产建议 30min**） | Feed |
| `behavior_event:{event_id}` | String | 消费幂等标记 | 24h | Behavior Worker |
| `behavior:expose:{request_id}:{feed_id}` | String | 曝光去重 | 24h | Behavior Worker |
| `behavior:rate:{user_id}` | String | 埋点上报限流计数 | 60s | Gateway |
| `feed:metrics:h:{feed_id}:{yyyyMMddHH}` | Hash | 小时指标**实时累加** | 50h | Behavior Worker |
| `feed:metrics:dirty` | Set | 待 flush 的 `{feed_id}:{hour}` |无 | Behavior Worker |
| `user:interest:{user_id}` | ZSet | `t:{topic}`/`c:{category}`→score | 90d | Behavior Worker |
| `interest:active:{yyyyMMdd}` | Set | 当日活跃用户 | 7d | Behavior Worker |
| `interest:dedup:{user_id}:{feed_id}:{action}` | String | 同 feed 同行为去重 | 24h | Behavior Worker |
| `content:analysis:lock:{feed_id}` | String | 分析任务**互斥锁** | 6min | Content Worker |
| `content:profile:{feed_id}` | String(JSON) | 画像缓存（对外字段） | 1h | Content RPC |
| `agent:run:{run_id}` | Hash | Run 最新状态（低成本轮询） | 1h | Agent RPC |
| `agent:rate:{user_id}` | String | Run 频率限流 | 60s | Agent RPC |
| `agent:session:lock:{session_id}` | String | **单会话串行执行** | 90s | Agent RPC |

**锁 TTL 设计说明**：`content:analysis:lock` 6min 略大于 `FFmpegTimeoutSec(120s)` + ASR/OCR/多模态预期总耗时，避免锁提前释放导致重复分析。

---

## 9. Elasticsearch 索引

**索引**: `feed_content_v1` | **读别名**: `feed_content` | **写别名**: `feed_content_write`

| 约定 | 说明 |
|------|------|
| `_id = feed_id` | **upsert 语义天然幂等** |
| 混合召回 | BM25（字幕/摘要全文）+ kNN（向量语义）+ 标签精确匹配，**RRF 融合** |
| 升级流程 | 建 `v2` → 全量重建 → **原子切别名** → 删旧索引 |
| **定位** | ES **只作检索索引，不作数据源**——任何字段都能从 MySQL 重建 |

字段与 mapping 详见 `docs/design/agent/05-content-search.md` §3。

**结果校验（安全要求）**：检索结果必须经 `BatchGetFeeds` 校验**真实存在且状态正常**——ES 可能残留已删除 feed（A4 验收标准）。

---

## 10. 容量与清理策略

| 数据 | 日增（日活 10 万、人均 100 曝光） | 策略 |
|------|--------------------------------|------|
| `feed_behavior_events` | 曝光采样 10% + 其他约 200 万 ≈ **300 万行/日** | 保留 30 天；演进迁 ClickHouse |
| `feed_metrics_hourly` | 活跃 feed 10 万 × 24 ≈ **240 万行/日** | 保留 180 天；30 天后压缩为日粒度 |
| `feed_content_profiles` | 与视频发布量同阶 ≈ **1 万/日**（单行 5~20KB） | 长期保存 |
| `agent_*` | 与 Agent 使用量同阶 | 消息/Run 90 天，ToolCall 30 天 |
| `feed:trace:*` | 与 Timeline QPS 同阶 | **靠 TTL + 采样控制，生产必须降采样** |

**清理实施要求**：统一由定时任务执行，**分批删除**（每批 ≤ 2000 行 + sleep），避免大事务与主从延迟。

---

## 11. 实施检查清单

- [ ] `deploy/sql/content.sql` 新建（`feed_content` 库 + 1 表）
- [ ] `deploy/sql/agent.sql` 新建（`feed_agent` 库 + 4 表）
- [ ] `deploy/sql/interaction.sql` 追加 3 张表
- [ ] **已有环境手动执行**上述脚本（MySQL 初始化脚本只跑一次）
- [ ] 各服务 `internal/keys/keys.go` 定义 Redis key（13 个）
- [ ] `goctl model` 生成 8 张新表的 model 代码
- [ ] 复杂查询在 `customXXXModel` 中扩展（宪法 VI）
- [ ] ES 索引 `feed_content_v1` + 双别名创建脚本
- [ ] 测试库 `feed_content_test`、`feed_agent_test`、`feed_interaction_test`
- [ ] 清理定时任务（分批删除，4 类数据）
- [ ] 数据质量校验规则（5 条，见 `14-acceptance-test.md` §5）
