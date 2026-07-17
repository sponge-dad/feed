-- user.sql
--
-- 职责：User 服务的 MySQL 建表脚本，对应 docs/data-model.md 第1节的设计。
-- 使用方式：在目标数据库中手动执行一次即可（无需 ORM 迁移工具，团队约定手动建表）。
--
-- 重要约定（详见 docs/dev-guidelines.md）：
--   1. id 不使用 MySQL AUTO_INCREMENT，而是由应用层 common/idgen（Snowflake）生成后插入。
--      原因：未来 users 表可能需要分库分表，提前统一用 Snowflake 全局ID，
--      避免到时候还要单独迁移 ID 生成策略。
--   2. phone / email 允许为 NULL（而不是默认空字符串 ''），因为它们上有唯一索引，
--      MySQL 唯一索引允许多个 NULL 同时存在，但不允许多个空字符串 '' 同时存在。
--      如果给它们默认空字符串，第二个不填手机号的用户注册就会因为唯一键冲突报错。
--   3. 当前 Register 接口（见 api/proto/user/user.proto）只需要 username + password，
--      phone / email 是预留字段，暂不参与业务逻辑，未来做手机号登录/绑定邮箱时再启用。

CREATE TABLE IF NOT EXISTS `users` (
    -- 主键：Snowflake 生成的分布式 ID，由应用层写入，不使用数据库自增
    `id`           BIGINT UNSIGNED NOT NULL COMMENT 'Snowflake ID，应用层生成',

    -- 登录凭证
    `username`     VARCHAR(64)     NOT NULL DEFAULT ''  COMMENT '登录用户名，唯一',
    `password`     VARCHAR(256)    NOT NULL DEFAULT ''  COMMENT 'bcrypt 哈希后的密码，绝不存明文',

    -- 展示信息
    `nickname`     VARCHAR(64)     NOT NULL DEFAULT ''  COMMENT '昵称，展示用，可重复',
    `avatar`       VARCHAR(512)    NOT NULL DEFAULT ''  COMMENT '头像 URL（腾讯云 COS）',
    `bio`          VARCHAR(512)    NOT NULL DEFAULT ''  COMMENT '个人简介',

    -- 预留登录方式（当前未启用，见上方说明3）
    `email`        VARCHAR(128)    NULL     DEFAULT NULL COMMENT '邮箱，预留字段，允许NULL避免唯一键冲突',
    `phone`        VARCHAR(20)     NULL     DEFAULT NULL COMMENT '手机号，预留字段，允许NULL避免唯一键冲突',

    -- 地理位置（配合"同城流"使用）
    `city_code`    VARCHAR(16)     NOT NULL DEFAULT ''  COMMENT '城市编码，如440300=深圳，注册时由IP定位写入',
    `city_name`    VARCHAR(64)     NOT NULL DEFAULT ''  COMMENT '城市名，如"深圳"，用户可手动修改',

    -- 状态与时间
    `status`       TINYINT         NOT NULL DEFAULT 1   COMMENT '1:正常 2:禁用',
    `created_at`   DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '注册时间',
    `updated_at`   DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP COMMENT '最近更新时间',

    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_username` (`username`),
    UNIQUE KEY `uk_phone` (`phone`),
    UNIQUE KEY `uk_email` (`email`),
    KEY `idx_city` (`city_code`),
    KEY `idx_created_at` (`created_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='用户表';
