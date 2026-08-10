USE `feed_interaction`;

-- 行为明细表。见 docs/design/agent/03-behavior-event.md §5。
--
-- 采样策略：PLAY / EFFECTIVE_PLAY / FINISH / SKIP / SHARE 全量落库；
-- EXPOSE 按 ExposeSampleRate（默认 10%）采样——曝光量级最大，且指标聚合已在
-- Redis 累加，明细仅用于抽样核对。
--
-- 隐私约束：本表不存任何内容原文、IP、UA 等隐私字段。
-- 保留策略：仅保留 30 天，按 event_time 定时清理；聚合结果长期保存。
DROP TABLE IF EXISTS `feed_behavior_events`;
CREATE TABLE `feed_behavior_events` (
  `id`                BIGINT UNSIGNED NOT NULL COMMENT 'Snowflake ID(由应用生成)',
  `event_id`          VARCHAR(64)     NOT NULL COMMENT '服务端生成的事件唯一 ID(uuid v4)，落库幂等兜底',
  `request_id`        VARCHAR(64)     NOT NULL DEFAULT '' COMMENT '来源 Timeline 请求 ID',
  `user_id`           BIGINT UNSIGNED NOT NULL COMMENT '行为用户(取自 JWT)',
  `feed_id`           BIGINT UNSIGNED NOT NULL COMMENT '目标帖子 ID',
  `author_id`         BIGINT UNSIGNED NOT NULL DEFAULT 0 COMMENT '帖子作者 ID(服务端校正)',
  `action_type`       VARCHAR(32)     NOT NULL COMMENT 'EXPOSE/PLAY/EFFECTIVE_PLAY/FINISH/SKIP/SHARE',
  `position`          INT             NOT NULL DEFAULT 0 COMMENT '在本次 Feed 结果中的位置，从 0 开始',
  `watch_duration_ms` BIGINT          NOT NULL DEFAULT 0 COMMENT '观看时长(ms)',
  `media_duration_ms` BIGINT          NOT NULL DEFAULT 0 COMMENT '媒体总时长(ms，服务端校正)',
  `abnormal`          TINYINT         NOT NULL DEFAULT 0 COMMENT '数据质量标记：1=异常序列(如无 EXPOSE 直接 PLAY)',
  `event_time`        DATETIME        NOT NULL COMMENT '行为发生时间(服务端纠偏后)',
  `created_at`        DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '入库时间',
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_event_id` (`event_id`),
  KEY `idx_feed_action_time` (`feed_id`, `action_type`, `event_time`),
  KEY `idx_author_time` (`author_id`, `event_time`),
  KEY `idx_user_time` (`user_id`, `event_time`),
  KEY `idx_event_time` (`event_time`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='Feed 行为明细表(EXPOSE 采样，其余全量，保留 30 天)';

-- 帖子维度小时级聚合指标。见 docs/design/agent/03-behavior-event.md §6。
--
-- 数据来源：Redis Hash feed:metrics:h:{feed_id}:{yyyyMMddHH} 累加，
-- 由定时 flush 任务（默认 60s）写入本表。
--
-- 关键设计：flush 写入的是 Redis 中的【绝对值】而非增量
-- （ON DUPLICATE KEY UPDATE xxx = VALUES(xxx)），
-- 因此 flush 重复执行不会造成指标重复累加。
DROP TABLE IF EXISTS `feed_metrics_hourly`;
CREATE TABLE `feed_metrics_hourly` (
  `id`                   BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  `feed_id`              BIGINT UNSIGNED NOT NULL COMMENT '帖子 ID',
  `author_id`            BIGINT UNSIGNED NOT NULL DEFAULT 0 COMMENT '帖子作者 ID',
  `stat_hour`            DATETIME        NOT NULL COMMENT '小时桶(对齐到整点)',
  `expose_count`         BIGINT          NOT NULL DEFAULT 0 COMMENT '曝光次数',
  `play_count`           BIGINT          NOT NULL DEFAULT 0 COMMENT '起播次数',
  `effective_play_count` BIGINT          NOT NULL DEFAULT 0 COMMENT '有效播放次数',
  `finish_count`         BIGINT          NOT NULL DEFAULT 0 COMMENT '完播次数',
  `skip_count`           BIGINT          NOT NULL DEFAULT 0 COMMENT '快划次数',
  `share_count`          BIGINT          NOT NULL DEFAULT 0 COMMENT '分享次数',
  `like_count`           BIGINT          NOT NULL DEFAULT 0 COMMENT '点赞次数(来自 interaction-event)',
  `collect_count`        BIGINT          NOT NULL DEFAULT 0 COMMENT '收藏次数(来自 interaction-event)',
  `comment_count`        BIGINT          NOT NULL DEFAULT 0 COMMENT '评论次数(来自 comment-event)',
  `watch_duration_ms`    BIGINT          NOT NULL DEFAULT 0 COMMENT '观看总时长(ms)',
  `updated_at`           DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_feed_hour` (`feed_id`, `stat_hour`),
  KEY `idx_author_hour` (`author_id`, `stat_hour`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='帖子小时级指标表(flush 写绝对值，幂等)';
