// interactionmodel_integration_test.go
//
// 职责：likes / collections 表唯一索引 uk_user_feed(user_id, feed_id) 的 Model 集成测试（真实 MySQL）。
// Baseline: P0 第 6 项「唯一索引 Model 集成」（对应 I-LK / I-CO 系列的 DB 幂等基石）。
//
// 连接信息优先取环境变量 FEED_TEST_MYSQL_ADDR / FEED_TEST_MYSQL_USER / FEED_TEST_MYSQL_PASS，
// 未设置时回退到仓库测试配置中的本地测试凭证；MySQL 不可用时 t.Skip；
// 测试自动建库建表（feed_interaction_test）并清理自身数据。
package model

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/go-sql-driver/mysql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zeromicro/go-zero/core/stores/cache"
	"github.com/zeromicro/go-zero/core/stores/redis"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

// likesTestDDL / collectionsTestDDL 与 deploy/sql/interaction.sql 保持一致（仅保留结构定义）。
const likesTestDDL = "CREATE TABLE IF NOT EXISTS `likes` (" +
	"`id` BIGINT UNSIGNED NOT NULL," +
	"`user_id` BIGINT UNSIGNED NOT NULL," +
	"`feed_id` BIGINT UNSIGNED NOT NULL," +
	"`status` TINYINT NOT NULL DEFAULT 1," +
	"`created_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP," +
	"`updated_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP," +
	"PRIMARY KEY (`id`)," +
	"UNIQUE KEY `uk_user_feed` (`user_id`, `feed_id`)," +
	"KEY `idx_feed` (`feed_id`)," +
	"KEY `idx_user_created` (`user_id`, `created_at`)" +
	") ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci"

const collectionsTestDDL = "CREATE TABLE IF NOT EXISTS `collections` (" +
	"`id` BIGINT UNSIGNED NOT NULL," +
	"`user_id` BIGINT UNSIGNED NOT NULL," +
	"`feed_id` BIGINT UNSIGNED NOT NULL," +
	"`status` TINYINT NOT NULL DEFAULT 1," +
	"`created_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP," +
	"`updated_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP," +
	"PRIMARY KEY (`id`)," +
	"UNIQUE KEY `uk_user_feed` (`user_id`, `feed_id`)," +
	"KEY `idx_feed` (`feed_id`)," +
	"KEY `idx_user_created` (`user_id`, `created_at`)" +
	") ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci"

func iEnvOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// openInteractionTestConn 探活 MySQL 并准备 feed_interaction_test 库与 likes/collections 表。
func openInteractionTestConn(t *testing.T) sqlx.SqlConn {
	t.Helper()

	addr := iEnvOrDefault("FEED_TEST_MYSQL_ADDR", "127.0.0.1:3306")
	user := iEnvOrDefault("FEED_TEST_MYSQL_USER", "root")
	pass := iEnvOrDefault("FEED_TEST_MYSQL_PASS", "F3d!MysQL#r00T_2024xV")

	serverDSN := fmt.Sprintf("%s:%s@tcp(%s)/?charset=utf8mb4&parseTime=true&loc=Local", user, pass, addr)
	db, err := sql.Open("mysql", serverDSN)
	require.NoError(t, err)
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		t.Skipf("MySQL 不可用（%s），跳过 Model 集成测试: %v", addr, err)
	}

	_, err = db.ExecContext(ctx, "CREATE DATABASE IF NOT EXISTS `feed_interaction_test` DEFAULT CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci")
	require.NoError(t, err)

	dbDSN := fmt.Sprintf("%s:%s@tcp(%s)/feed_interaction_test?charset=utf8mb4&parseTime=true&loc=Local", user, pass, addr)
	conn := sqlx.NewMysql(dbDSN)
	_, err = conn.ExecCtx(context.Background(), likesTestDDL)
	require.NoError(t, err)
	_, err = conn.ExecCtx(context.Background(), collectionsTestDDL)
	require.NoError(t, err)
	return conn
}

func interactionTestCacheConf(t *testing.T) cache.CacheConf {
	t.Helper()
	mr := miniredis.RunT(t)
	return cache.CacheConf{{RedisConf: redis.RedisConf{Host: mr.Addr(), Type: "node"}, Weight: 100}}
}

// assertMySQL1062 断言错误为 MySQL 唯一索引冲突 1062，且命中 uk_user_feed。
func assertMySQL1062(t *testing.T, err error) {
	t.Helper()
	var mysqlErr *mysql.MySQLError
	require.True(t, errors.As(err, &mysqlErr), "错误应为 *mysql.MySQLError，实际: %T %v", err, err)
	assert.Equal(t, uint16(1062), mysqlErr.Number, "应为唯一索引冲突 1062")
	assert.Contains(t, mysqlErr.Message, "uk_user_feed", "冲突应发生在 uk_user_feed 索引")
}

// TestLikesModelInsert_DuplicateUserFeed_ReturnsMySQLError1062
// Risk baseline: P0-6（likes uk_user_feed，Worker 落库幂等的 DB 基石）
func TestLikesModelInsert_DuplicateUserFeed_ReturnsMySQLError1062(t *testing.T) {
	conn := openInteractionTestConn(t)
	m := NewLikesModel(conn, interactionTestCacheConf(t))
	ctx := context.Background()

	base := uint64(time.Now().UnixNano())
	userID, feedID := base, base+1
	t.Cleanup(func() {
		_, _ = conn.ExecCtx(context.Background(),
			"DELETE FROM `likes` WHERE `user_id` = ? AND `feed_id` = ?", userID, feedID)
	})

	_, err := m.Insert(ctx, &Likes{Id: base + 10, UserId: userID, FeedId: feedID, Status: 1})
	require.NoError(t, err, "首次插入点赞记录应成功")

	_, err = m.Insert(ctx, &Likes{Id: base + 11, UserId: userID, FeedId: feedID, Status: 1})
	require.Error(t, err, "同一 (user_id, feed_id) 二次插入应失败")
	assertMySQL1062(t, err)

	var count int64
	require.NoError(t, conn.QueryRowCtx(ctx, &count,
		"SELECT COUNT(*) FROM `likes` WHERE `user_id` = ? AND `feed_id` = ?", userID, feedID))
	assert.Equal(t, int64(1), count, "同一用户对同一帖子只应有 1 条点赞记录")
}

// TestCollectionsModelInsert_DuplicateUserFeed_ReturnsMySQLError1062
// Risk baseline: P0-6（collections uk_user_feed）
func TestCollectionsModelInsert_DuplicateUserFeed_ReturnsMySQLError1062(t *testing.T) {
	conn := openInteractionTestConn(t)
	m := NewCollectionsModel(conn, interactionTestCacheConf(t))
	ctx := context.Background()

	base := uint64(time.Now().UnixNano())
	userID, feedID := base+2, base+3
	t.Cleanup(func() {
		_, _ = conn.ExecCtx(context.Background(),
			"DELETE FROM `collections` WHERE `user_id` = ? AND `feed_id` = ?", userID, feedID)
	})

	_, err := m.Insert(ctx, &Collections{Id: base + 20, UserId: userID, FeedId: feedID, Status: 1})
	require.NoError(t, err, "首次插入收藏记录应成功")

	_, err = m.Insert(ctx, &Collections{Id: base + 21, UserId: userID, FeedId: feedID, Status: 1})
	require.Error(t, err, "同一 (user_id, feed_id) 二次插入应失败")
	assertMySQL1062(t, err)

	var count int64
	require.NoError(t, conn.QueryRowCtx(ctx, &count,
		"SELECT COUNT(*) FROM `collections` WHERE `user_id` = ? AND `feed_id` = ?", userID, feedID))
	assert.Equal(t, int64(1), count, "同一用户对同一帖子只应有 1 条收藏记录")
}
