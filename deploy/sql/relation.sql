-- relation.sql
--
-- 职责：Relation 服务（关注/取关/粉丝列表）的建表脚本。
-- 首次启动 docker compose 里的 mysql 容器时会被自动执行。

CREATE DATABASE IF NOT EXISTS `feed_relation` DEFAULT CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;
USE `feed_relation`;

-- relations 表：记录谁关注了谁。
-- 设计上关注关系本身没有"更新"操作，只有"新增"和"删除"。
CREATE TABLE IF NOT EXISTS `relations` (
    `id`           BIGINT UNSIGNED NOT NULL COMMENT 'Snowflake ID，应用层生成',
    `follower_id`  BIGINT UNSIGNED NOT NULL COMMENT '发起关注的用户ID',
    `followee_id`  BIGINT UNSIGNED NOT NULL COMMENT '被关注的用户ID',
    `created_at`   BIGINT          NOT NULL COMMENT '关注时间（Unix时间戳秒）',
    PRIMARY KEY (`id`),
    -- 唯一索引：同一个用户不能重复关注同一个人
    UNIQUE KEY `uk_follow` (`follower_id`, `followee_id`),
    -- 关注者索引：用于查"我关注了谁"
    KEY `idx_follower_id` (`follower_id`, `created_at`),
    -- 被关注者索引：用于查"谁关注了我"（粉丝列表）
    KEY `idx_followee_id` (`followee_id`, `created_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='关注关系表';
