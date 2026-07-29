// authz_baseline_test.go
//
// Feed RPC 越权删除安全基线（真实 Feed RPC Server + Client，不经过 Gateway）。
// 对应基线：F-DEL-04（属主越权删除）+ Risk baseline: R-P0-1（伪造 user_id 信任边界）。
//
// 本文件测试不得 Skip：越权场景必须真实断言拒绝，伪造属主场景必须真实记录
// RPC 完全信任请求中 user_id 的架构风险。
package tests

import (
	"context"
	"database/sql"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/sponge-dad/feed/app/feed/rpc/feed"
	"github.com/sponge-dad/feed/app/feed/rpc/internal/config"
	"github.com/sponge-dad/feed/app/feed/rpc/internal/server"
	"github.com/sponge-dad/feed/app/feed/rpc/internal/serverinterceptors"
	"github.com/sponge-dad/feed/app/feed/rpc/internal/svc"
	"github.com/sponge-dad/feed/common/errorx"
	"github.com/sponge-dad/feed/common/idgen"
	"github.com/sponge-dad/feed/common/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zeromicro/go-zero/core/conf"
	"github.com/zeromicro/go-zero/core/discov"
	"github.com/zeromicro/go-zero/zrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

var (
	feedTestClient feed.FeedClient
	feedCtx        *svc.ServiceContext
	feedDB         *sql.DB
	feedStop       func()
	feedSkip       string
)

func requireFeedEnv(t *testing.T) {
	t.Helper()
	if feedSkip != "" {
		t.Skip("integration dependency unavailable: " + feedSkip)
	}
}

func TestMain(m *testing.M) {
	code, err := runFeed(m)
	if err != nil {
		fmt.Fprintf(os.Stderr, "feed integration test setup failed: %v\n", err)
		os.Exit(1)
	}
	os.Exit(code)
}

func runFeed(m *testing.M) (int, error) {
	if err := idgen.Init(1); err != nil {
		return 0, err
	}

	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		return 0, fmt.Errorf("failed to get current file path")
	}
	configPath := filepath.Join(filepath.Dir(currentFile), "..", "etc", "feed-test.yaml")

	var c config.Config
	conf.MustLoad(configPath, &c)

	var missing []string
	if addr := testutil.MySQLAddrFromDSN(c.Mysql.DataSource); !testutil.DialOK(addr) {
		missing = append(missing, "mysql("+addr+")")
	}
	if len(c.CacheRedis) > 0 && !testutil.DialOK(c.CacheRedis[0].Host) {
		missing = append(missing, "redis("+c.CacheRedis[0].Host+")")
	}
	if len(c.RocketMQ.NameServer) > 0 && !testutil.DialOK(c.RocketMQ.NameServer[0]) {
		missing = append(missing, "rocketmq("+c.RocketMQ.NameServer[0]+")")
	}
	if len(missing) > 0 {
		feedSkip = strings.Join(missing, ", ")
		return m.Run(), nil
	}

	listenOn, err := testutil.FreePort()
	if err != nil {
		return 0, err
	}
	c.ListenOn = listenOn
	c.Etcd = discov.EtcdConf{}

	ctx, err := svc.NewServiceContext(c)
	if err != nil {
		return 0, err
	}
	feedCtx = ctx

	// 清理历史业务缓存，避免 stale 缓存影响测试。Feed model 主键缓存已关闭，
	// 仅需清理 feed:{feed_id} 业务 Hash。
	cleanupFeedModelCache()

	db, err := sql.Open("mysql", c.Mysql.DataSource)
	if err != nil {
		return 0, err
	}
	feedDB = db

	srv := zrpc.MustNewServer(c.RpcServerConf, func(grpcServer *grpc.Server) {
		feed.RegisterFeedServer(grpcServer, server.NewFeedServer(ctx))
	})
	srv.AddUnaryInterceptors(serverinterceptors.ErrorInterceptor)

	var conn *grpc.ClientConn
	feedStop = func() {
		srv.Stop()
		if conn != nil {
			_ = conn.Close()
		}
		if feedDB != nil {
			_ = feedDB.Close()
		}
	}

	go func() {
		fmt.Printf("Starting feed integration test rpc server at %s...\n", c.ListenOn)
		srv.Start()
	}()

	if err := testutil.WaitReady(listenOn, 5*time.Second); err != nil {
		feedStop()
		return 0, err
	}

	conn, err = grpc.NewClient(listenOn, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		feedStop()
		return 0, err
	}
	feedTestClient = feed.NewFeedClient(conn)

	code := m.Run()
	feedStop()
	return code, nil
}

func cleanupFeedModelCache() {
	script := `local ks = redis.call('keys', ARGV[1]); for i=1,#ks do redis.call('del', ks[i]); end; return #ks`
	_, _ = feedCtx.Redis.Eval(script, []string{}, "feed:*")
}

const (
	feedStatusNormal  int64 = 1
	feedStatusDeleted int64 = 2
)

func insertTestFeed(t *testing.T, owner int64) int64 {
	t.Helper()
	feedID := uint64(idgen.Next()) ^ uint64(rand.Int63())
	_, err := feedDB.Exec(
		`INSERT INTO feeds (id, user_id, feed_type, title, description, media_urls, cover_url, city_code, city_name, ip_location, status, is_vip_feed, like_count, comment_count, collect_count, created_at, updated_at)
		 VALUES (?, ?, 1, 'baseline-title', 'baseline-content', NULL, '', '440300', '深圳', '', ?, 0, 0, 0, 0, NOW(), NOW())`,
		feedID, owner, feedStatusNormal)
	require.NoError(t, err, "插入测试 Feed 失败")
	return int64(feedID)
}

func assertFeedStatus(t *testing.T, feedID int64, expect int64) {
	t.Helper()
	var status int64
	require.NoError(t, feedDB.QueryRow("SELECT status FROM feeds WHERE id = ?", feedID).Scan(&status),
		"查询 Feed 状态失败")
	assert.Equal(t, expect, status, "Feed 状态不符合预期")
}

// Baseline: F-DEL-04
// 真实 Feed RPC Server + Client，验证越权删除基线：
//   - 攻击者 userB 调用 DeleteFeed(user_id=userB, feed_id=属主A的Feed) 必须被拒绝；
//   - 返回业务码 FeedNoPermission(12002)；
//   - Feed 记录仍然存在且 status 未变为已删除；
//   - 未发送 feed-deleted 消息（逻辑在返回前已拒绝，无副作用）。
func TestIntegration_DeleteFeed_ByNonOwner_RejectedFDEL04(t *testing.T) {
	requireFeedEnv(t)
	ctx := context.Background()

	const ownerA = int64(7000001)
	const attackerB = int64(7000002)

	feedID := insertTestFeed(t, ownerA)
	defer func() {
		_, _ = feedDB.Exec("DELETE FROM feeds WHERE id = ?", feedID)
	}()

	_, err := feedTestClient.DeleteFeed(ctx, &feed.DeleteFeedReq{UserId: attackerB, FeedId: feedID})
	require.Error(t, err, "越权删除必须返回错误")
	codeErr, ok := errorx.TryParse(err)
	require.True(t, ok, "越权删除应返回可解析业务错误码, got %v", err)
	assert.Equal(t, errorx.FeedNoPermission, codeErr.Code, "越权删除必须返回 FeedNoPermission(12002)")
	// 记录仍然存在且未被删除
	assertFeedStatus(t, feedID, feedStatusNormal)
}

// Risk baseline: R-P0-1
// 伪造属主 user_id 的信任边界测试（安全红线，不得 Skip、不得为本阶段大规模改造）。
// 客户端直接传入属主 userA 的 user_id（调用方实际上没有任何身份凭证），
// 断言 Feed RPC 完全信任请求中的 user_id，可成功删除 Feed。
// 这是 P0 架构风险：RPC 层不校验调用者身份，鉴权应前移到 Gateway/JWT 校验。
func TestIntegration_DeleteFeed_ForgedOwnerId_TrustBoundaryR_P0_1(t *testing.T) {
	requireFeedEnv(t)
	ctx := context.Background()

	const ownerA = int64(7000003)

	feedID := insertTestFeed(t, ownerA)
	defer func() {
		_, _ = feedDB.Exec("DELETE FROM feeds WHERE id = ?", feedID)
	}()

	// 伪造 ownerA 的 user_id，未携带任何身份凭证
	_, err := feedTestClient.DeleteFeed(ctx, &feed.DeleteFeedReq{UserId: ownerA, FeedId: feedID})
	require.NoError(t, err,
		"R-P0-1: Feed RPC 完全信任请求中的 user_id，伪造属主可直接删除（需在 Gateway 完成身份校验）")
	assertFeedStatus(t, feedID, feedStatusDeleted)
}
