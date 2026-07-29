// relationsmodel_integration_test.go
//
// 职责：relations 表唯一索引 uk_follow(follower_id, followee_id) 的 Model 集成测试（真实 MySQL）。
// Baseline: R-FL-04（P0 第 6 项：唯一索引 Model 集成）
//
// 连接信息优先取环境变量 FEED_TEST_MYSQL_ADDR / FEED_TEST_MYSQL_USER / FEED_TEST_MYSQL_PASS，
// 未设置时回退到仓库测试配置中的本地测试凭证；MySQL 不可用时 t.Skip；
// 测试自动建库建表（feed_relation_test）并清理自身数据。
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

// relationsTestDDL 与 deploy/sql/relation.sql 保持一致（仅保留结构定义）。
const relationsTestDDL = "CREATE TABLE IF NOT EXISTS `relations` (" +
	"`id` BIGINT UNSIGNED NOT NULL," +
	"`follower_id` BIGINT UNSIGNED NOT NULL," +
	"`followee_id` BIGINT UNSIGNED NOT NULL," +
	"`created_at` BIGINT NOT NULL," +
	"PRIMARY KEY (`id`)," +
	"UNIQUE KEY `uk_follow` (`follower_id`, `followee_id`)," +
	"KEY `idx_follower_id` (`follower_id`, `created_at`)," +
	"KEY `idx_followee_id` (`followee_id`, `created_at`)" +
	") ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci"

func relEnvOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// openRelationsTestConn 探活 MySQL 并准备 feed_relation_test 库与 relations 表。
func openRelationsTestConn(t *testing.T) sqlx.SqlConn {
	t.Helper()

	addr := relEnvOrDefault("FEED_TEST_MYSQL_ADDR", "127.0.0.1:3306")
	user := relEnvOrDefault("FEED_TEST_MYSQL_USER", "root")
	pass := relEnvOrDefault("FEED_TEST_MYSQL_PASS", "F3d!MysQL#r00T_2024xV")

	serverDSN := fmt.Sprintf("%s:%s@tcp(%s)/?charset=utf8mb4&parseTime=true&loc=Local", user, pass, addr)
	db, err := sql.Open("mysql", serverDSN)
	require.NoError(t, err)
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		t.Skipf("MySQL 不可用（%s），跳过 Model 集成测试: %v", addr, err)
	}

	_, err = db.ExecContext(ctx, "CREATE DATABASE IF NOT EXISTS `feed_relation_test` DEFAULT CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci")
	require.NoError(t, err)

	dbDSN := fmt.Sprintf("%s:%s@tcp(%s)/feed_relation_test?charset=utf8mb4&parseTime=true&loc=Local", user, pass, addr)
	conn := sqlx.NewMysql(dbDSN)
	_, err = conn.ExecCtx(context.Background(), relationsTestDDL)
	require.NoError(t, err)
	return conn
}

func relationsTestCacheConf(t *testing.T) cache.CacheConf {
	t.Helper()
	mr := miniredis.RunT(t)
	return cache.CacheConf{{RedisConf: redis.RedisConf{Host: mr.Addr(), Type: "node"}, Weight: 100}}
}

// TestRelationsModelInsert_DuplicateFollowPair_ReturnsMySQLError1062
// Baseline: R-FL-04
// 同一 (follower_id, followee_id) 双写：第二次 Insert 命中 uk_follow 返回 1062，表中仅 1 行。
func TestRelationsModelInsert_DuplicateFollowPair_ReturnsMySQLError1062(t *testing.T) {
	conn := openRelationsTestConn(t)
	m := NewRelationsModel(conn, relationsTestCacheConf(t))
	ctx := context.Background()

	base := uint64(time.Now().UnixNano())
	followerID, followeeID := base, base+1
	t.Cleanup(func() {
		_, _ = conn.ExecCtx(context.Background(),
			"DELETE FROM `relations` WHERE `follower_id` = ? AND `followee_id` = ?", followerID, followeeID)
	})

	first := &Relations{Id: base + 10, FollowerId: followerID, FolloweeId: followeeID, CreatedAt: time.Now().Unix()}
	_, err := m.Insert(ctx, first)
	require.NoError(t, err, "首次插入关注关系应成功")

	second := &Relations{Id: base + 11, FollowerId: followerID, FolloweeId: followeeID, CreatedAt: time.Now().Unix()}
	_, err = m.Insert(ctx, second)
	require.Error(t, err, "重复关注关系插入应失败")

	var mysqlErr *mysql.MySQLError
	require.True(t, errors.As(err, &mysqlErr), "错误应为 *mysql.MySQLError，实际: %T %v", err, err)
	assert.Equal(t, uint16(1062), mysqlErr.Number, "应为唯一索引冲突 1062")
	assert.Contains(t, mysqlErr.Message, "uk_follow", "冲突应发生在 uk_follow 索引")

	var count int64
	require.NoError(t, conn.QueryRowCtx(ctx, &count,
		"SELECT COUNT(*) FROM `relations` WHERE `follower_id` = ? AND `followee_id` = ?", followerID, followeeID))
	assert.Equal(t, int64(1), count, "同一关注关系在表中只应存在 1 行")

	// 保留行应为第一次插入的记录。
	got, err := m.FindOneByFollowerIdFolloweeId(ctx, followerID, followeeID)
	require.NoError(t, err)
	assert.Equal(t, first.Id, got.Id)
}
