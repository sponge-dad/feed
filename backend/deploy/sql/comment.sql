-- comment.sql
--
-- 职责：Comment 服务（评论/楼中楼）的建表脚本。
-- 首次启动 docker compose 里的 mysql 容器时会被自动执行；
-- 已初始化过的环境需手动执行本脚本（见 AGENTS.md「已知陷阱」2）。
--
-- 注意：id 由应用层 Snowflake（common/idgen）生成写入，禁止 AUTO_INCREMENT
-- （见 docs/design/comment/01-data-model.md 1.2）。

CREATE DATABASE IF NOT EXISTS `feed_comment` DEFAULT CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;
USE `feed_comment`;

CREATE TABLE IF NOT EXISTS `comments` (
    `id`            BIGINT UNSIGNED NOT NULL COMMENT 'Snowflake ID，应用层生成',
    `feed_id`       BIGINT UNSIGNED NOT NULL COMMENT '所属帖子ID',
    `user_id`       BIGINT UNSIGNED NOT NULL COMMENT '评论者ID',
    `content`       VARCHAR(1000)   NOT NULL DEFAULT '' COMMENT '评论内容，超长由 logic 层拦截',
    `root_id`       BIGINT UNSIGNED NOT NULL DEFAULT 0 COMMENT '根评论ID；一级评论=0',
    `parent_id`     BIGINT UNSIGNED NOT NULL DEFAULT 0 COMMENT '直接回复的评论ID；一级评论=0',
    `reply_user_id` BIGINT UNSIGNED NOT NULL DEFAULT 0 COMMENT '被回复者ID（@谁）；一级评论=0',
    `like_count`    INT UNSIGNED    NOT NULL DEFAULT 0 COMMENT '点赞数，由 Interaction 服务异步同步',
    `reply_count`   INT UNSIGNED    NOT NULL DEFAULT 0 COMMENT '子回复数，仅根评论维护',
    `status`        TINYINT         NOT NULL DEFAULT 1 COMMENT '1:正常 2:已删除（软删除）',
    `created_at`    DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间',
    PRIMARY KEY (`id`),
    KEY `idx_feed_root` (`feed_id`, `root_id`, `created_at`),
    KEY `idx_root` (`root_id`, `created_at`),
    KEY `idx_user` (`user_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='评论表（楼中楼两层平铺存储）';
