# 数据模型

>汇总本项目新增的 MySQL 表、Redis Key、ES 索引与容量清理策略，是建表脚本与 model 生成的依据。

---

## 1. 库归属

现有库遵循 `feed_<service>` 命名（`feed_user`、`feed_relation`、`feed_feed`、`feed_comment`、`feed_interaction`）。新增：

| 库 | 归属服务 | 新增表 |
|----|----------|--------|
| `feed_content` | Content RPC / Worker | `feed_content_profiles` |
| `feed_interaction`（已有库，新增表） | Interaction RPC / Behavior Worker | `feed_behavior_events`、`feed_metrics_hourly`、`user_interest_profiles` |
| `feed_agent` | Agent RPC | `agent_sessions`、`agent_messages`、`agent_runs`、`agent_tool_calls` |

建表脚本新增：`deploy/sql/content.sql`、`deploy/sql/agent.sql`，并在 `deploy/sql/interaction.sql` 追加三张表。注意 **MySQL 容器只在首次启动执行初始化脚本**，已有环境需手动执行（见 `AGENTS.md` §9）。

## 2. feed_content_profiles

```sql
CREATE DATABASE IF NOT EXISTS `feed_content` DEFAULT CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;
USE `feed_content`;

CREATE TABLE IF NOT EXISTS `feed_content_profiles` (
    `id`             BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    `feed_id`        BIGINT       NOT NULL COMMENT '关联 feeds.id（Snowflake）',
    `author_id`      BIGINT       NOT NULL DEFAULT 0 COMMENT '作者ID，权限校验用',
    `media_hash`     VARCHAR(128) NOT NULL DEFAULT '' COMMENT '媒体指纹，幂等依据',
    `category`       VARCHAR(64)  NOT NULL DEFAULT '' COMMENT '内容类别（白名单）',
    `summary`        TEXT                COMMENT '内容摘要',
    `topics`         JSON                  COMMENT '主题标签 ["露营"]',
    `objects`        JSON                  COMMENT '画面实体',
    `scenes`         JSON                  COMMENT '场景标签',
    `styles`         JSON                  COMMENT '内容形式标签',
    `transcript`     MEDIUMTEXT            COMMENT '语音字幕全文',
    `transcript_segments` JSON             COMMENT '分段字幕 [{start_ms,end_ms,text}]',
    `ocr_text`       TEXT                  COMMENT '关键帧文字（JSON 数组）',
    `language`       VARCHAR(32)  NOT NULL DEFAULT '',
    `media_duration_ms` BIGINT    NOT NULL DEFAULT 0 COMMENT 'ffprobe 探测的真实时长',
    `key_frame_count` INT      NOT NULL DEFAULT 0,
    `analysis_status` VARCHAR(32) NOT NULL COMMENT 'PENDING/DOWNLOADING/.../COMPLETED/FAILED/DISABLED',
    `degraded`       TINYINT      NOT NULL DEFAULT 0 COMMENT '1=部分阶段失败但结果可用',
    `retry_count`    INT          NOT NULL DEFAULT 0,
    `model_version`  VARCHAR(64)  NOT NULL DEFAULT '',
    `error_message`  VARCHAR(1024) NOT NULL DEFAULT '' COMMENT '已脱敏，禁止含签名地址',
    `analyzed_at`    DATETIME(3)           NULL,
    `created_at`     DATETIME(3)  NOT NULL,
    `updated_at`     DATETIME(3)  NOT NULL,
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_feed_id` (`feed_id`),
    KEY `idx_category_status` (`category`, `analysis_status`),
    KEY `idx_status_updated` (`analysis_status`, `updated_at`),
    KEY `idx_author` (`author_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='视频内容画像';
```

设计说明：

- 相对需求文档增加 `author_id`（避免每次权限校验都回查 Feed RPC）、`transcript_segments`（支持「开头 3 秒讲了什么」）、`media_duration_ms`（替代客户端上报时长）、`degraded` / `retry_count`。
- `uk_feed_id` 是幂等主保障；`media_hash + model_version` 用于判断是否需要重跑，不作为唯一键（否则同一 feed 换模型会出现多行）。
- `idx_status_updated` 支撑「捞取卡住的任务」运维查询。

## 3. feed_behavior_events

```sql
USE `feed_interaction`;

CREATE TABLE IF NOT EXISTS `feed_behavior_events` (
    `id`                BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    `event_id`          VARCHAR(64) NOT NULL COMMENT '服务端生成 uuid，幂等键',
    `request_id`        VARCHAR(64) NOT NULL DEFAULT '' COMMENT '来源 Timeline 请求',
    `user_id`           BIGINT      NOT NULL,
    `feed_id`           BIGINT      NOT NULL,
    `author_id`         BIGINT      NOT NULL,
    `action_type`       VARCHAR(32) NOT NULL COMMENT 'EXPOSE/PLAY/EFFECTIVE_PLAY/FINISH/SKIP/SHARE',
    `position`          INT         NOT NULL DEFAULT 0,
    `watch_duration_ms` BIGINT      NOT NULL DEFAULT 0,
    `media_duration_ms` BIGINT      NOT NULL DEFAULT 0,
    `event_time`        DATETIME(3) NOT NULL COMMENT '行为发生时间（客户端，已纠偏）',
    `created_at`        DATETIME(3) NOT NULL,
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_event_id` (`event_id`),
    KEY `idx_feed_event_time` (`feed_id`, `event_time`),
    KEY `idx_user_event_time` (`user_id`, `event_time`),
    KEY `idx_request_id` (`request_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='Feed 行为明细';
```

- `uk_event_id` 是 Redis 幂等之外的**兜底幂等**（Redis 丢 key 时仍不会重复插入）。
- EXPOSE 采样入库（默认 10%），其余全量；保留 30 天。
- 不含 IP / UA / 设备号等隐私字段。

## 4. feed_metrics_hourly

```sql
CREATE TABLE IF NOT EXISTS `feed_metrics_hourly` (
    `id`                   BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    `feed_id`              BIGINT   NOT NULL,
    `author_id`            BIGINT   NOT NULL DEFAULT 0,
    `stat_hour`            DATETIME NOT NULL COMMENT '整小时，如 2026-08-04 13:00:00',
    `expose_count`         BIGINT   NOT NULL DEFAULT 0,
    `play_count`           BIGINT   NOT NULL DEFAULT 0,
    `effective_play_count` BIGINT   NOT NULL DEFAULT 0,
    `finish_count`         BIGINT   NOT NULL DEFAULT 0,
    `skip_count`           BIGINT   NOT NULL DEFAULT 0,
    `watch_duration_ms`    BIGINT   NOT NULL DEFAULT 0,
    `like_count`           BIGINT   NOT NULL DEFAULT 0,
    `collect_count`        BIGINT   NOT NULL DEFAULT 0,
    `comment_count`        BIGINT   NOT NULL DEFAULT 0,
    `share_count`          BIGINT   NOT NULL DEFAULT 0,
    `created_at`           DATETIME(3) NOT NULL,
    `updated_at`           DATETIME(3) NOT NULL,
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_feed_hour` (`feed_id`, `stat_hour`),
    KEY `idx_stat_hour` (`stat_hour`),
    KEY `idx_author_hour` (`author_id`, `stat_hour`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='Feed 小时级指标';
```

- 写入**绝对值**（`ON DUPLICATE KEY UPDATE expose_count = VALUES(expose_count)`），重复 flush 不会重复累加。
- `idx_author_hour` 支撑创作者维度汇总；同类对比查询走 `idx_stat_hour` + 画像表 join（跨库时在应用层两步查询，先按 category 取 feed_id 列表再查指标）。

## 5. user_interest_profiles

```sql
CREATE TABLE IF NOT EXISTS `user_interest_profiles` (
    `id`            BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    `user_id`       BIGINT   NOT NULL,
    `interest_json` JSON     NOT NULL COMMENT '{categories:[],topics:[],total_actions,window_days}',
    `version`       BIGINT   NOT NULL DEFAULT 1 COMMENT '单调递增，识别新旧快照',
    `calculated_at` DATETIME(3) NOT NULL,
    `created_at`    DATETIME(3) NOT NULL,
    `updated_at`    DATETIME(3) NOT NULL,
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_user_id` (`user_id`),
    KEY `idx_calculated_at` (`calculated_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='用户兴趣画像快照';
```

## 6. Agent 自有表

```sql
CREATE DATABASE IF NOT EXISTS `feed_agent` DEFAULT CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;
USE `feed_agent`;

CREATE TABLE IF NOT EXISTS `agent_sessions` (
    `id`         BIGINT UNSIGNED NOT NULL COMMENT 'Snowflake session_id',
    `user_id`    BIGINT      NOT NULL,
    `title`      VARCHAR(128) NOT NULL DEFAULT '',
    `status`     VARCHAR(32)  NOT NULL DEFAULT 'ACTIVE' COMMENT 'ACTIVE/CLOSED',
    `last_active_at` DATETIME(3) NOT NULL,
    `created_at` DATETIME(3) NOT NULL,
    `updated_at` DATETIME(3) NOT NULL,
    PRIMARY KEY (`id`),
    KEY `idx_user_active` (`user_id`, `last_active_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='Agent 会话';

CREATE TABLE IF NOT EXISTS `agent_messages` (
    `id`         BIGINT UNSIGNED NOT NULL COMMENT 'Snowflake message_id',
    `session_id` BIGINT      NOT NULL,
    `run_id`     BIGINT      NOT NULL DEFAULT 0,
    `role`       VARCHAR(16) NOT NULL COMMENT 'user/assistant',
    `content`    TEXT        NOT NULL COMMENT '截断保存，禁止含凭证',
    `created_at` DATETIME(3) NOT NULL,
    PRIMARY KEY (`id`),
    KEY `idx_session_created` (`session_id`, `created_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='Agent 消息';

CREATE TABLE IF NOT EXISTS `agent_runs` (
    `id`          BIGINT UNSIGNED NOT NULL COMMENT 'Snowflake run_id',
    `session_id`  BIGINT      NOT NULL,
    `user_id`     BIGINT      NOT NULL,
    `request_id`  VARCHAR(64) NOT NULL DEFAULT '',
    `intent`      VARCHAR(32) NOT NULL DEFAULT '',
    `status`      VARCHAR(32) NOT NULL COMMENT 'CREATED/UNDERSTANDING/TOOL_CALLING/ANALYZING/GENERATING/SUCCEEDED/FAILED/CANCELLED',
    `tool_call_count`  INT    NOT NULL DEFAULT 0,
    `model_call_count` INT    NOT NULL DEFAULT 0,
    `prompt_tokens`    INT    NOT NULL DEFAULT 0,
    `completion_tokens` INT   NOT NULL DEFAULT 0,
    `error_code`  VARCHAR(64) NOT NULL DEFAULT '',
    `error_message` VARCHAR(512) NOT NULL DEFAULT '',
    `cost_ms`     BIGINT      NOT NULL DEFAULT 0,
    `created_at`  DATETIME(3) NOT NULL,
    `updated_at`  DATETIME(3) NOT NULL,
    PRIMARY KEY (`id`),
    KEY `idx_session_created` (`session_id`, `created_at`),
    KEY `idx_user_created` (`user_id`, `created_at`),
    KEY `idx_status` (`status`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='Agent 执行任务';

CREATE TABLE IF NOT EXISTS `agent_tool_calls` (
    `id`          BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    `run_id`      BIGINT      NOT NULL,
    `seq`         INT         NOT NULL COMMENT '本Run 内序号，从 1 开始',
    `tool_name`   VARCHAR(64) NOT NULL,
    `input_digest`  VARCHAR(1024) NOT NULL DEFAULT '' COMMENT '白名单字段摘要',
    `output_digest` VARCHAR(2048) NOT NULL DEFAULT '' COMMENT '结果摘要，禁止字幕/OCR 全文',
    `status`      VARCHAR(32) NOT NULL COMMENT 'SUCCESS/FAILED/DENIED/TIMEOUT',
    `error_code`  VARCHAR(64) NOT NULL DEFAULT '',
    `cost_ms`     BIGINT      NOT NULL DEFAULT 0,
    `created_at`  DATETIME(3) NOT NULL,
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_run_seq` (`run_id`, `seq`),
    KEY `idx_tool_created` (`tool_name`, `created_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='Agent工具调用留痕';
```

`session_id` / `run_id` / `message_id` 一律用 `common/idgen`（Snowflake），禁止自增主键作为业务 ID（`AGENTS.md` §4.6）。

## 7. Redis Key 约定

| Key | 结构 | 内容 | TTL | 归属 |
|-----|------|------|-----|------|
| `feed:trace:{request_id}` | Hash | `meta` + `f:{feed_id}` → source | 24h（可配，生产建议 30min） | Feed |
| `behavior_event:{event_id}` | String | 消费幂等标记 | 24h | Behavior Worker |
| `behavior:expose:{request_id}:{feed_id}` | String | 曝光去重 | 24h | Behavior Worker |
| `behavior:rate:{user_id}` | String | 埋点上报限流计数 | 60s | Gateway |
| `feed:metrics:h:{feed_id}:{yyyyMMddHH}` | Hash | 小时指标实时累加 | 50h | Behavior Worker |
| `feed:metrics:dirty` | Set | 待 flush 的 `{feed_id}:{hour}` | 无 | Behavior Worker |
| `user:interest:{user_id}` | ZSet | `t:{topic}` / `c:{category}` → score | 90d | Behavior Worker |
| `interest:active:{yyyyMMdd}` | Set | 当日活跃用户 | 7d | Behavior Worker |
| `interest:dedup:{user_id}:{feed_id}:{action}` | String | 同 feed 同行为去重 | 24h | Behavior Worker |
| `content:analysis:lock:{feed_id}` | String | 分析任务互斥锁 | 6min | Content Worker |
| `content:profile:{feed_id}` | String(JSON) | 画像缓存（对外字段） | 1h | Content RPC |
| `agent:run:{run_id}` | Hash | Run 最新状态（低成本轮询） | 1h | Agent RPC |
| `agent:rate:{user_id}` | String | Run 频率限流 | 60s | Agent RPC |
| `agent:session:lock:{session_id}` | String | 单会话串行执行 | 90s | Agent RPC |

命名沿用现有风格（业务前缀 +冒号分隔），各服务在 `internal/keys/` 集中定义，禁止散落硬编码（参考 `app/feed/rpc/internal/keys/keys.go`）。

## 8. ES 索引

索引 `feed_content_v1`（读别名 `feed_content`，写别名 `feed_content_write`），字段与 mapping 见 [05-content-search.md](./05-content-search.md) §3。约定：

- `_id = feed_id`，upsert 语义天然幂等。
- 模型/mapping 升级：建`feed_content_v2` → 全量重建 → 原子切别名 → 删旧索引。
- ES 只作为**检索索引**，不作为数据源；任何字段都能从 MySQL 重建。

## 9. 容量与清理

| 数据 | 增长估算（日活 10 万、人均 100 次曝光） | 策略 |
|------|------------------------------|------|
| `feed_behavior_events` | 曝光 1000 万/日 → 采样 10% + 其他行为约 200 万 → 约 300 万行/日 | 保留 30 天，按`event_time` 定时删除；后续迁 ClickHouse |
| `feed_metrics_hourly` | 活跃 feed 10 万 × 24 → 240 万行/日 | 保留 180 天；30 天后可压缩为日粒度 |
| `feed_content_profiles` | 与视频发布量同阶（约 1 万/日） | 长期保存（含 `transcript`，单行约 5~20KB） |
| `agent_*` | 与 Agent 使用量同阶| 消息/Run 90 天，ToolCall 30 天 |
| `feed:trace:*` | 与 Timeline QPS 同阶 | 靠 TTL + 采样控制，生产必须降采样 |

清理任务统一由定时任务执行，**分批删除**（每批 ≤ 2000 行 + sleep），避免大事务与主从延迟。

## 10. 演进与 TODO

- 行为明细迁移到 ClickHouse，MySQL 只留近7 天热数据。
- `feeds` 表回填 `media_duration_ms`（由 Content Worker 反写），减少跨库查询。
- 画像 embedding 目前只存 ES；如需离线复用，可增加 `feed_content_vectors` 表或对象存储归档。
- 指标表按 `stat_hour` 做分区表，提升清理效率。

---

## 关联文档

- [行为事件采集与指标聚合](./03-behavior-event.md)
- [内容分析服务设计](./04-content-analysis.md)
- [用户兴趣画像](./06-user-interest.md)
- [Agent 服务设计](./09-agent-service.md)
- [接口契约](./11-api.md)
- [全局数据模型](../data-model.md)
