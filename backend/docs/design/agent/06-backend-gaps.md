# 后端扩展缺口清单

> 列出支撑 Agent V1-full / V2 / V3 所需的后端改造项：`feeds` 表扩列、`UpdateFeed` RPC、轻量 stats 服务，附 SQL 与 proto 草案及优先级。本文档是后端排期的输入，Agent 侧文档只引用不重复。

---

## 1. 概述与定位

以下缺口均已核对现有 `api/proto/**/*.proto` 与 `deploy/sql/*.sql` 确认不存在。改造分三批，与 [00-overview.md](./00-overview.md) §6 的实施阶段对应：

本文聚焦最初提出的内容元数据、`UpdateFeed` 与 Stats 三项技术草案。完整产品化还需要 RBAC、内容条件检索、持久化 run、可靠事件、操作审计、负反馈与实验能力，统一见 [09-product-requirements.md](./09-product-requirements.md) §8.1。

| 批次 | 内容 | 解锁能力 | 工作量 |
|---|---|---|---|
| 一 | `feeds` 加 `category`/`duration_sec`/`tags` | V1-full 条件推荐 | 小 |
| 二 | `UpdateFeed` RPC + `FeedStatus` 扩「下架」 | V3 执行 | 小 |
| 三 | stats 服务（播放事件 + 聚合指标） | V2 诊断、真实观看历史 | 大 |
| 三+ | 推荐位数据模型 | 推荐位调整场景 | 中 |

## 2. 批次一：`feeds` 表扩列（SQL 草案）

```sql
-- deploy/sql/feed-alter-v2.sql（草案，需手动执行，MySQL 自动建表只跑一次）
USE `feed_feed`;

ALTER TABLE `feeds`
    ADD COLUMN `category`     VARCHAR(32)  NOT NULL DEFAULT '' COMMENT '内容分类，如"悬疑"，字典见 category_dict' AFTER `feed_type`,
    ADD COLUMN `duration_sec` INT UNSIGNED NOT NULL DEFAULT 0  COMMENT '视频时长秒，图文为 0' AFTER `category`,
    ADD COLUMN `tags`         JSON         NULL COMMENT '标签数组 ["国产","短剧"]' AFTER `duration_sec`,
    ADD KEY `idx_category_created` (`category`, `created_at`);
```

配套改动：

- `feed.proto` 的 `FeedInfo`/`FeedBrief`/`CreateFeedReq` 增加对应字段（proto3 追加字段向后兼容，编号顺延）。
- 存量数据回灌：`duration_sec` 可离线解析媒体文件；`category/tags` 可先由运营批量标注或 LLM 离线打标（打标结果需抽样人审后落库）。
- Agent 侧：解锁 [02-tools.md](./02-tools.md) 中 `get_video_detail` 的过滤字段，移除「无分类/时长」的 disclaimer。

## 3. 批次二：`UpdateFeed` RPC（proto 草案）

现状：`feed.proto` 仅有 `CreateFeed`/`DeleteFeed`（软删），无任何更新入口；`FeedStatus` 只有 NORMAL/DELETED/AUDITING，无「下架」态。

```protobuf
// feed.proto 追加（草案）

// FeedStatus 追加：
//   FEED_STATUS_OFFLINE = 4;  // 已下架（运营操作，可恢复，区别于软删除）

message UpdateFeedReq {
    int64  feed_id      = 1;  // 目标帖子
    int64  operator_id  = 2;  // 操作者，服务端校验权限（作者本人或运营角色）
    // 以下字段：proto3 无法区分"未设置"与零值，采用 field_mask 显式声明要更新的字段
    repeated string update_mask = 3; // 允许值：title/description/cover_url/category/tags/status
    string title        = 4;
    string description  = 5;
    string cover_url    = 6;
    string category     = 7;
    string tags_json    = 8;  // JSON 数组字符串
    int32  status       = 9;  // 仅允许 1(上架)/4(下架)，删除仍走 DeleteFeed
}
message UpdateFeedResp {
    FeedInfo feed = 1; // 更新后的完整帖子
}

// service Feed 追加：
//   rpc UpdateFeed(UpdateFeedReq) returns (UpdateFeedResp);
```

实现要点（Feed 服务侧）：

- 权限：`operator_id` 为作者本人，或经运营角色白名单校验；越权返回权限错误码。
- 缓存：先写 DB，再删帖子缓存（仓库通用约定）；`status` 变更需同步剔除各 Timeline 缓存中的该帖。
- 审计：更新前后值写服务日志（Agent 侧另有 `agent_tool_calls` 留痕，双保险）。

## 4. 批次三：轻量 stats 服务（新服务草案）

独立服务 `app/stats/rpc`（规划端口 9006，独立库 `feed_stats`），职责：接收播放/曝光事件，聚合产出指标。

### 4.1 数据模型（草案）

```sql
CREATE DATABASE IF NOT EXISTS `feed_stats` DEFAULT CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;
USE `feed_stats`;

-- 播放事件明细（高频写：Redis 先行 + RocketMQ 异步落库，同 Interaction 削峰策略）
CREATE TABLE IF NOT EXISTS `play_records` (
    `id`             BIGINT UNSIGNED NOT NULL COMMENT 'Snowflake ID',
    `user_id`        BIGINT UNSIGNED NOT NULL,
    `feed_id`        BIGINT UNSIGNED NOT NULL,
    `watch_duration` INT UNSIGNED    NOT NULL DEFAULT 0 COMMENT '本次观看秒数',
    `completed`      TINYINT         NOT NULL DEFAULT 0 COMMENT '是否完播',
    `source`         VARCHAR(16)     NOT NULL DEFAULT '' COMMENT '入口：recommend/follow/city/search',
    `created_at`     DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (`id`),
    KEY `idx_user_created` (`user_id`, `created_at`),
    KEY `idx_feed_created` (`feed_id`, `created_at`)
) ENGINE=InnoDB COMMENT='播放事件明细表';

-- 曝光事件（计算 CTR 用），结构同上省略：impressions(user_id, feed_id, source, created_at)

-- 天级聚合（定时任务产出，Agent 查询走这张表，不扫明细）
CREATE TABLE IF NOT EXISTS `feed_stats_daily` (
    `feed_id`         BIGINT UNSIGNED NOT NULL,
    `stat_date`       DATE            NOT NULL,
    `impression_cnt`  INT UNSIGNED    NOT NULL DEFAULT 0,
    `play_cnt`        INT UNSIGNED    NOT NULL DEFAULT 0,
    `complete_cnt`    INT UNSIGNED    NOT NULL DEFAULT 0,
    `total_watch_sec` BIGINT UNSIGNED NOT NULL DEFAULT 0,
    PRIMARY KEY (`feed_id`, `stat_date`),
    KEY `idx_date` (`stat_date`)
) ENGINE=InnoDB COMMENT='帖子天级统计聚合表';
```

### 4.2 gRPC 接口（草案）

```protobuf
service Stats {
    rpc ReportPlay(ReportPlayReq) returns (ReportPlayResp);            // 客户端/网关上报播放
    rpc ReportImpression(ReportImpressionReq) returns (ReportImpressionResp);
    rpc GetFeedMetrics(GetFeedMetricsReq) returns (GetFeedMetricsResp);        // 单帖窗口指标
    rpc BatchGetFeedMetrics(BatchGetFeedMetricsReq) returns (BatchGetFeedMetricsResp);
    rpc GetUserWatchHistory(GetUserWatchHistoryReq) returns (GetUserWatchHistoryResp); // 真实观看历史
}
// GetFeedMetricsResp 核心字段：play_count, impression_count, ctr,
// completion_rate, avg_watch_duration —— 均由聚合表计算，窗口参数 window_days。
```

### 4.3 与 Agent 的衔接

- 上线后注册工具 `get_play_metrics`、`get_user_watch_history`（[02-tools.md](./02-tools.md) §4.3）。
- `get_user_watch_history` 替换 V1 的「点赞+收藏近似看过」，同步删除相关 disclaimer。
- CTR/完播率的口径（分母取曝光还是播放）必须在 stats 服务文档中唯一定义，Agent Prompt 引用同一口径描述。

## 5. 批次三+：推荐位模型（暂缓，仅记录）

「调整推荐位」需先有推荐位实体（slot 定义、坑位与 feed 的绑定、生效时间）。当前 `GetRecommendTimeline` 是算法/规则流，无人工坑位。建议 V2 稳定后单独立项，暂不出草案。

## 6. 缓存与一致性

批次一/二遵循仓库「先写 DB 再删缓存」约定；批次三写路径采用「Redis 先行 + MQ 异步落库」削峰（同 Interaction 例外条款，见 `../data-model.md` 第 5 节）。

## 7. 测试策略

- 批次一：加列后回归 Feed 服务全部现有测试（`go test ./app/feed/rpc/...`），确认 model 重新生成无破坏。
- 批次二：`UpdateFeed` 权限/越权用例、field_mask 部分更新用例、缓存剔除一致性用例。
- 批次三：事件上报幂等、聚合任务重跑幂等、指标口径单测。

## 8. 演进与 TODO

- [ ] 批次一二实施时，同步更新 `../data-model.md` 与 `../feed/` 相关设计文档。
- [ ] stats 服务立项后另建 `docs/design/stats/` 目录，本文档 §4 迁移为其 overview 的输入。

## 关联文档

- [Agent 服务总览](./00-overview.md)
- [工具契约](./02-tools.md)
- [场景分版设计](./05-scenarios.md)
- [产品需求与建设路线](./09-product-requirements.md)
- [数据模型总览](../data-model.md)
- [服务拆分方案](../service-design.md)
