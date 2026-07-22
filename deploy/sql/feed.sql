-- feed.sql
--
-- 职责：Feed 服务（动态列表）的建表脚本。
-- 首次启动 docker compose 里的 mysql 容器时会被自动执行。

CREATE DATABASE IF NOT EXISTS `feed_feed` DEFAULT CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;
USE `feed_feed`;

CREATE TABLE IF NOT EXISTS `feeds` (
    `id`            BIGINT UNSIGNED NOT NULL COMMENT 'Snowflake ID，应用层生成',
    `user_id`       BIGINT UNSIGNED NOT NULL COMMENT '作者ID',
    `feed_type`     TINYINT         NOT NULL DEFAULT 1 COMMENT '动态类型，1：图文，2：视频',
    `title`         VARCHAR(256)    NOT NULL DEFAULT '' COMMENT '标题/主题',
    `description`   TEXT            NOT NULL COMMENT '正文/描述',
    `media_urls`    JSON            DEFAULT NULL COMMENT '媒体资源 ["url1","url2"]',
    `cover_url`     VARCHAR(512)    NOT NULL DEFAULT '' COMMENT '视频封面图',
    `city_code`     VARCHAR(16)     NOT NULL DEFAULT '' COMMENT '发布时IP城市编码',
    `city_name`     VARCHAR(64)     NOT NULL DEFAULT '' COMMENT '发布时IP城市名',
    `ip_location`   VARCHAR(64)     NOT NULL DEFAULT '' COMMENT 'IP属地，如"广东"',
    `status`        TINYINT         NOT NULL DEFAULT 1 COMMENT '1:正常 2:已删除 3:审核中',
    `is_vip_feed`   TINYINT         NOT NULL DEFAULT 0 COMMENT '0:普通 1:大V发帖',
    `like_count`    INT UNSIGNED    NOT NULL DEFAULT 0 COMMENT '点赞数',
    `comment_count` INT UNSIGNED    NOT NULL DEFAULT 0 COMMENT '评论数',
    `collect_count` INT UNSIGNED    NOT NULL DEFAULT 0 COMMENT '收藏数',
    `created_at`    DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '发布时间',
    `updated_at`    DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP COMMENT '最近更新时间',
    PRIMARY KEY (`id`),
    KEY `idx_user_created` (`user_id`, `created_at`),
    KEY `idx_city_created` (`city_code`, `created_at`),
    KEY `idx_status_created` (`status`, `created_at`),
    KEY `idx_created_at` (`created_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='动态表';
