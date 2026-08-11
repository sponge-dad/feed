-- interaction.sql
--
-- 职责：Interaction 服务（点赞/收藏）的建表脚本。
-- 首次启动 docker compose 里的 mysql 容器时会被自动执行。

CREATE DATABASE IF NOT EXISTS `feed_interaction` DEFAULT CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;
USE `feed_interaction`;

-- likes 表：用户点赞记录
-- 使用 status 软删除保留取消记录，用于审计和恢复。
CREATE TABLE IF NOT EXISTS `likes` (
    `id`          BIGINT UNSIGNED NOT NULL COMMENT 'Snowflake ID，应用层生成',
    `user_id`     BIGINT UNSIGNED NOT NULL COMMENT '点赞用户 ID',
    `feed_id`     BIGINT UNSIGNED NOT NULL COMMENT '被点赞帖子 ID',
    `status`      TINYINT         NOT NULL DEFAULT 1 COMMENT '1:已点赞 2:已取消',
    `created_at`  DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '点赞时间',
    `updated_at`  DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP COMMENT '状态更新时间',
    PRIMARY KEY (`id`),
    -- 同一用户对同一帖子只能有一条记录，保证幂等
    UNIQUE KEY `uk_user_feed` (`user_id`, `feed_id`),
    -- 按帖子聚合统计/重建缓存
    KEY `idx_feed` (`feed_id`),
    -- 用户点赞列表按时间倒序分页
    KEY `idx_user_created` (`user_id`, `created_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='用户点赞记录表';

-- collections 表：用户收藏记录
-- 结构与 likes 同构，但独立存储，便于后续按业务维度独立扩容/归档。
CREATE TABLE IF NOT EXISTS `collections` (
    `id`          BIGINT UNSIGNED NOT NULL COMMENT 'Snowflake ID，应用层生成',
    `user_id`     BIGINT UNSIGNED NOT NULL COMMENT '收藏用户 ID',
    `feed_id`     BIGINT UNSIGNED NOT NULL COMMENT '被收藏帖子 ID',
    `status`      TINYINT         NOT NULL DEFAULT 1 COMMENT '1:已收藏 2:已取消',
    `created_at`  DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '收藏时间',
    `updated_at`  DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP COMMENT '状态更新时间',
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_user_feed` (`user_id`, `feed_id`),
    KEY `idx_feed` (`feed_id`),
    KEY `idx_user_created` (`user_id`, `created_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='用户收藏记录表';

-- user_interest_profiles：用户兴趣画像快照（US5，见 docs/design/agent/10-data-model.md §5）
-- 双写模型：Redis ZSet user:interest:{user_id}（实时）+ 本表快照（兜底/离线）
-- version 单调递增：并发写入时识别新旧快照，防旧快照回退覆盖新数据
CREATE TABLE IF NOT EXISTS `user_interest_profiles` (
    `id`            BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    `user_id`       BIGINT   NOT NULL COMMENT '用户 ID（uk）',
    `interest_json` JSON     NOT NULL COMMENT '{categories:[],topics:[],total_actions,window_days}',
    `version`       BIGINT   NOT NULL DEFAULT 1 COMMENT '单调递增，识别新旧快照',
    `calculated_at` DATETIME(3) NOT NULL COMMENT '计算时间',
    `created_at`    DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    `updated_at`    DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_user_id` (`user_id`),
    KEY `idx_calculated_at` (`calculated_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='用户兴趣画像快照';
