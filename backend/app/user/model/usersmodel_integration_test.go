// usersmodel_integration_test.go
//
// 职责：users 表唯一索引 uk_username 的 Model 集成测试（真实 MySQL）。
// Baseline: U-REG-04（P0 第 6 项：唯一索引 Model 集成）
//
// 运行要求：本地 MySQL 8.0 可用（docker-compose）。连接信息优先取环境变量：
//
//	FEED_TEST_MYSQL_ADDR / FEED_TEST_MYSQL_USER / FEED_TEST_MYSQL_PASS
//
// 未设置时回退到仓库测试配置（app/relation/rpc/etc/relation-test.yaml）中的本地测试凭证。
// 环境不可用时 t.Skip；测试自动建库建表（feed_user_test）并清理自身数据。
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

// usersTestDDL 与 deploy/sql/user.sql 保持一致（仅保留结构定义）。
const usersTestDDL = "CREATE TABLE IF NOT EXISTS `users` (" +
	"`id` BIGINT UNSIGNED NOT NULL," +
	"`username` VARCHAR(64) NOT NULL DEFAULT ''," +
	"`password` VARCHAR(256) NOT NULL DEFAULT ''," +
	"`nickname` VARCHAR(64) NOT NULL DEFAULT ''," +
	"`avatar` VARCHAR(512) NOT NULL DEFAULT ''," +
	"`bio` VARCHAR(512) NOT NULL DEFAULT ''," +
	"`email` VARCHAR(128) NULL DEFAULT NULL," +
	"`phone` VARCHAR(20) NULL DEFAULT NULL," +
	"`city_code` VARCHAR(16) NOT NULL DEFAULT ''," +
	"`city_name` VARCHAR(64) NOT NULL DEFAULT ''," +
	"`status` TINYINT NOT NULL DEFAULT 1," +
	"`created_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP," +
	"`updated_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP," +
	"PRIMARY KEY (`id`)," +
	"UNIQUE KEY `uk_username` (`username`)," +
	"UNIQUE KEY `uk_phone` (`phone`)," +
	"UNIQUE KEY `uk_email` (`email`)" +
	") ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci"

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// openUsersTestConn 探活 MySQL 并准备 feed_user_test 库与 users 表。
// MySQL 不可用时直接 t.Skip，保证单元测试链路不因环境缺失而失败。
func openUsersTestConn(t *testing.T) sqlx.SqlConn {
	t.Helper()

	addr := envOrDefault("FEED_TEST_MYSQL_ADDR", "127.0.0.1:3306")
	user := envOrDefault("FEED_TEST_MYSQL_USER", "root")
	pass := envOrDefault("FEED_TEST_MYSQL_PASS", "F3d!MysQL#r00T_2024xV")

	serverDSN := fmt.Sprintf("%s:%s@tcp(%s)/?charset=utf8mb4&parseTime=true&loc=Local", user, pass, addr)
	db, err := sql.Open("mysql", serverDSN)
	require.NoError(t, err)
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		t.Skipf("MySQL 不可用（%s），跳过 Model 集成测试: %v", addr, err)
	}

	_, err = db.ExecContext(ctx, "CREATE DATABASE IF NOT EXISTS `feed_user_test` DEFAULT CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci")
	require.NoError(t, err)

	dbDSN := fmt.Sprintf("%s:%s@tcp(%s)/feed_user_test?charset=utf8mb4&parseTime=true&loc=Local", user, pass, addr)
	conn := sqlx.NewMysql(dbDSN)
	_, err = conn.ExecCtx(context.Background(), usersTestDDL)
	require.NoError(t, err)
	return conn
}

// usersTestCacheConf 用 miniredis 提供 goctl 缓存节点，避免依赖真实 Redis。
func usersTestCacheConf(t *testing.T) cache.CacheConf {
	t.Helper()
	mr := miniredis.RunT(t)
	return cache.CacheConf{{RedisConf: redis.RedisConf{Host: mr.Addr(), Type: "node"}, Weight: 100}}
}

// TestUsersModelInsert_DuplicateUsername_ReturnsMySQLError1062
// Baseline: U-REG-04
// 同名二次 Insert 应命中 uk_username 返回 MySQL 1062，且表中不新增行。
func TestUsersModelInsert_DuplicateUsername_ReturnsMySQLError1062(t *testing.T) {
	conn := openUsersTestConn(t)
	m := NewUsersModel(conn, usersTestCacheConf(t))
	ctx := context.Background()

	username := fmt.Sprintf("uk_it_%d", time.Now().UnixNano())
	t.Cleanup(func() {
		_, _ = conn.ExecCtx(context.Background(), "DELETE FROM `users` WHERE `username` = ?", username)
	})

	first := &Users{Id: time.Now().UnixNano(), Username: username, Password: "hash-a", Nickname: "n1", Status: 1}
	_, err := m.Insert(ctx, first)
	require.NoError(t, err, "首次插入应成功")

	second := &Users{Id: first.Id + 1, Username: username, Password: "hash-b", Nickname: "n2", Status: 1}
	_, err = m.Insert(ctx, second)
	require.Error(t, err, "同名二次插入应失败")

	var mysqlErr *mysql.MySQLError
	require.True(t, errors.As(err, &mysqlErr), "错误应为 *mysql.MySQLError，实际: %T %v", err, err)
	assert.Equal(t, uint16(1062), mysqlErr.Number, "应为唯一索引冲突 1062")
	assert.Contains(t, mysqlErr.Message, "uk_username", "冲突应发生在 uk_username 索引")

	// 唯一索引拦截后表中不新增行。
	var count int64
	require.NoError(t, conn.QueryRowCtx(ctx, &count, "SELECT COUNT(*) FROM `users` WHERE `username` = ?", username))
	assert.Equal(t, int64(1), count, "同名用户在表中只应存在 1 行")

	// 保留行应是第一次插入的数据（未被覆盖）。
	got, err := m.FindOne(ctx, first.Id)
	require.NoError(t, err)
	assert.Equal(t, "hash-a", got.Password)
}
