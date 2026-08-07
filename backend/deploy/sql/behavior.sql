USE `feed_interaction`;

-- 行为明细（抽样落库，用于审计 / 反作弊复盘）。
-- 全量行为不进 MySQL，仅 EXPOSE 抽样 + 主动行为全量写入。
CREATE TABLE IF NOT EXISTS `feed_behavior_events` (
  `id`          BIGINT UNSIGNED NOT NULL COMMENT 'Snowflake ID(由应用生成)',
  `event_id`    VARCHAR(64)     NOT NULL COMMENT '客户端幂等键(uuid)',
  `user_id`     BIGINT UNSIGNED NOT NULL COMMENT '行为用户',
  `feed_id`     BIGINT UNSIGNED NOT NULL COMMENT '帖子 ID',
  `action`      VARCHAR(32)     NOT NULL COMMENT 'expose/like/collect/comment/share',
  `target_id`   BIGINT UNSIGNED NOT NULL DEFAULT 0 COMMENT 'comment/share 所指目标（评论 ID / 被分享源 feed_id)',
  `client_ip`   VARCHAR(64)     NOT NULL DEFAULT '' COMMENT '客户端 IP',
  `user_agent`  VARCHAR(512)    NOT NULL DEFAULT '' COMMENT 'User-Agent',
  `req_id`      VARCHAR(64)     NOT NULL DEFAULT '' COMMENT '请求链路 ID',
  `created_at`  DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '行为入库时间',
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_event` (`event_id`),
  KEY `idx_feed_action` (`feed_id`, `action`),
  KEY `idx_user_created` (`user_id`, `created_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='行为明细抽样表';

-- 帖子维度小时级聚合指标。
-- 数据来源于 Redis HINCRBY 累加，定时任务按小时桶（UTC 整点）刷入本表。
CREATE TABLE IF NOT EXISTS `feed_metrics_hourly` (
  `id`          BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  `feed_id`     BIGINT UNSIGNED NOT NULL COMMENT '帖子 ID',
  `hour_bucket` DATETIME        NOT NULL COMMENT '小时桶（UTC 对齐到整点）',
  `pv`          BIGINT          NOT NULL DEFAULT 0 COMMENT '详情页 PV（含曝光）',
  `expose`      BIGINT          NOT NULL DEFAULT 0 COMMENT '曝光次数',
  `like`        BIGINT          NOT NULL DEFAULT 0 COMMENT '点赞次数',
  `collect`     BIGINT          NOT NULL DEFAULT 0 COMMENT '收藏次数',
  `comment`     BIGINT          NOT NULL DEFAULT 0 COMMENT '评论次数',
  `share`       BIGINT          NOT NULL DEFAULT 0 COMMENT '分享次数',
  `duration_ms` BIGINT          NOT NULL DEFAULT 0 COMMENT '曝光停留总时长（ms，采样累加）',
  `updated_at`  DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_feed_hour` (`feed_id`, `hour_bucket`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='帖子小时级指标表';
