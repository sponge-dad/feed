-- 内容画像库。见 docs/design/agent/10-data-model.md §2。
--
-- 注意：MySQL 容器只在首次启动执行 deploy/sql 下的脚本（AGENTS.md §9 已知陷阱 #2）。
-- 已有环境必须手动执行本脚本：
--   mysql -uroot -p < deploy/sql/content.sql
--
-- 幂等说明：本脚本用 CREATE DATABASE IF NOT EXISTS + DROP/CREATE 保证可重复执行，
-- 但 DROP TABLE 会清空数据，仅用于开发环境初始化，生产请使用增量迁移。
CREATE DATABASE IF NOT EXISTS `feed_content` DEFAULT CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;
USE `feed_content`;

-- 视频内容画像表。一 feed 一画像（uk_feed_id 幂等主保障）。
--
-- 设计要点：
--   * author_id 冗余存储，避免每次权限校验回查 Feed RPC
--   * transcript_segments 格式 [{start_ms,end_ms,text}]，支撑「开头 3 秒讲了什么」
--   * media_duration_ms 由 ffprobe 探测的真实时长（替代客户端不可信上报）
--   * analysis_status × degraded 正交建模：部分阶段失败但结果可用 = COMPLETED + degraded=1
--   * media_hash + model_version 用于判断是否需要重跑，不建唯一键（换模型不产生多行）
--   * error_message 必须脱敏，禁止含 COS 签名地址（含临时凭证）
DROP TABLE IF EXISTS `feed_content_profiles`;
CREATE TABLE `feed_content_profiles` (
  `id`                   BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  `feed_id`              BIGINT          NOT NULL COMMENT '关联 feeds.id（Snowflake）',
  `author_id`            BIGINT          NOT NULL DEFAULT 0 COMMENT '作者ID（冗余，权限校验用）',
  `media_hash`           VARCHAR(128)    NOT NULL DEFAULT '' COMMENT '媒体指纹（ETag/CRC64/SHA-256），幂等依据',
  `category`             VARCHAR(64)     NOT NULL DEFAULT '' COMMENT '内容类别（白名单类目）',
  `summary`              TEXT            NULL COMMENT '内容摘要',
  `topics`               JSON            NULL COMMENT '主题标签 ["露营"]',
  `objects`              JSON            NULL COMMENT '画面实体',
  `scenes`               JSON            NULL COMMENT '场景标签',
  `styles`               JSON            NULL COMMENT '内容形式标签',
  `transcript`           MEDIUMTEXT      NULL COMMENT '语音字幕全文',
  `transcript_segments`  JSON            NULL COMMENT '分段字幕 [{start_ms,end_ms,text}]',
  `ocr_text`             TEXT            NULL COMMENT '关键帧文字（JSON 数组）',
  `language`             VARCHAR(32)     NOT NULL DEFAULT '' COMMENT '语言（如 zh-CN）',
  `media_duration_ms`    BIGINT          NOT NULL DEFAULT 0 COMMENT 'ffprobe 探测的真实时长(ms)',
  `key_frame_count`      INT             NOT NULL DEFAULT 0 COMMENT '关键帧数量',
  `analysis_status`      VARCHAR(32)     NOT NULL COMMENT 'PENDING/DOWNLOADING/EXTRACTING/ASR_RUNNING/OCR_RUNNING/VISION_RUNNING/INDEXING/COMPLETED/FAILED/DISABLED',
  `degraded`             TINYINT         NOT NULL DEFAULT 0 COMMENT '1=部分阶段失败但结果可用',
  `retry_count`          INT             NOT NULL DEFAULT 0 COMMENT '任务级重试次数',
  `model_version`        VARCHAR(64)     NOT NULL DEFAULT '' COMMENT '生成该画像的模型版本',
  `error_message`        VARCHAR(1024)   NOT NULL DEFAULT '' COMMENT '已脱敏，禁止含签名地址',
  `analyzed_at`          DATETIME(3)     NULL COMMENT '分析完成时间',
  `created_at`           DATETIME(3)     NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  `updated_at`           DATETIME(3)     NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_feed_id` (`feed_id`),
  KEY `idx_category_status` (`category`, `analysis_status`),
  KEY `idx_status_updated` (`analysis_status`, `updated_at`),
  KEY `idx_author` (`author_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='视频内容画像';
