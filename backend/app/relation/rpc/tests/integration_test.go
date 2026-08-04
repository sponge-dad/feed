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
	"sync"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/sponge-dad/feed/app/relation/rpc/internal/config"
	"github.com/sponge-dad/feed/app/relation/rpc/internal/server"
	"github.com/sponge-dad/feed/app/relation/rpc/internal/serverinterceptors"
	"github.com/sponge-dad/feed/app/relation/rpc/internal/svc"
	"github.com/sponge-dad/feed/app/relation/rpc/relation"
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

// testUserBase 测试用户 ID 起始值。每次运行随机化，保证：
// 1. 不与开发数据冲突；2. 多次运行/并行运行之间数据互相隔离。
// uid 偏移量 < 10_000，因此不同 run 之间的 base 间隔取 10_000 的整数倍。
var testUserBase = 10_000_000_000 + rand.Int63n(1_000_000_000)*10_000

var (
	// 由 TestMain 初始化，所有测试共享同一个服务实例和 gRPC 客户端。
	testClient relation.RelationClient
	testCtx    *svc.ServiceContext
	testDB     *sql.DB
	testStop   func()
	// skipReason 非空表示外部基础设施不可用，所有集成测试统一 Skip。
	skipReason string
)

// requireEnv 集成测试统一环境探活入口：基础设施缺失时 Skip，
// 只允许因外部依赖不可用而跳过，探活在 TestMain 中一次性完成。
func requireEnv(t *testing.T) {
	t.Helper()
	if skipReason != "" {
		t.Skip("integration dependency unavailable: " + skipReason)
	}
}

func TestMain(m *testing.M) {
	code, err := run(m)
	if err != nil {
		fmt.Fprintf(os.Stderr, "integration test setup failed: %v\n", err)
		os.Exit(1)
	}
	os.Exit(code)
}

func run(m *testing.M) (int, error) {
	if err := idgen.Init(1); err != nil {
		return 0, err
	}

	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		return 0, fmt.Errorf("failed to get current file path")
	}
	configPath := filepath.Join(filepath.Dir(currentFile), "..", "etc", "relation-test.yaml")

	var c config.Config
	conf.MustLoad(configPath, &c)

	// 统一环境探活（短超时）：MySQL 与 Redis 缺一不可。
	// 不可用时不启动服务，所有测试通过 requireEnv 统一 Skip。
	var missing []string
	if addr := testutil.MySQLAddrFromDSN(c.Mysql.DataSource); !testutil.DialOK(addr) {
		missing = append(missing, "mysql("+addr+")")
	}
	if len(c.CacheRedis) > 0 && !testutil.DialOK(c.CacheRedis[0].Host) {
		missing = append(missing, "redis("+c.CacheRedis[0].Host+")")
	}
	if len(missing) > 0 {
		skipReason = strings.Join(missing, ", ")
		return m.Run(), nil
	}

	// 动态申请空闲端口，避免与业务服务（如 Feed RPC 9003）或其他测试包端口冲突；
	// 同时清空 etcd 注册配置：测试客户端直连实际监听地址，
	// 绝不依赖 etcd 中开发环境的 Relation 服务注册信息。
	listenOn, err := testutil.FreePort()
	if err != nil {
		return 0, err
	}
	c.ListenOn = listenOn
	c.Etcd = discov.EtcdConf{}

	ctx := svc.NewServiceContext(c)
	testCtx = ctx

	// 清理历史 model 缓存，避免之前测试运行的 stale 缓存影响当前测试。
	cleanupRelationModelCache()
	// 清理跨用户共享 key（如全局大V集合），防止跨运行串扰。
	cleanupSharedKeys()

	db, err := sql.Open("mysql", c.Mysql.DataSource)
	if err != nil {
		return 0, err
	}
	testDB = db

	srv := zrpc.MustNewServer(c.RpcServerConf, func(grpcServer *grpc.Server) {
		relation.RegisterRelationServer(grpcServer, server.NewRelationServer(ctx))
	})
	srv.AddUnaryInterceptors(serverinterceptors.ErrorInterceptor)

	var conn *grpc.ClientConn
	testStop = func() {
		srv.Stop()
		if conn != nil {
			_ = conn.Close()
		}
		if testDB != nil {
			_ = testDB.Close()
		}
	}

	go func() {
		fmt.Printf("Starting integration test rpc server at %s...\n", c.ListenOn)
		srv.Start()
	}()

	// 轮询等待服务就绪，替代固定 Sleep。
	if err := testutil.WaitReady(listenOn, 5*time.Second); err != nil {
		testStop()
		return 0, err
	}

	conn, err = grpc.NewClient(listenOn, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		testStop()
		return 0, err
	}
	testClient = relation.NewRelationClient(conn)

	code := m.Run()

	testStop()
	return code, nil
}

func newTestCtx() context.Context {
	return context.Background()
}

func uid(n int64) int64 {
	return testUserBase + n
}

func TestIntegration_FollowAndUnfollow(t *testing.T) {
	requireEnv(t)
	ctx := newTestCtx()
	a, b := uid(1), uid(2)

	// 关注
	resp, err := testClient.Follow(ctx, &relation.FollowReq{FollowerId: a, FolloweeId: b})
	require.NoError(t, err)
	assert.True(t, resp.Success)

	// 重复关注仍成功
	resp, err = testClient.Follow(ctx, &relation.FollowReq{FollowerId: a, FolloweeId: b})
	require.NoError(t, err)
	assert.True(t, resp.Success)

	// 查询关注列表
	follows, err := testClient.GetFollows(ctx, &relation.GetFollowsReq{UserId: a, Page: 1, PageSize: 10})
	require.NoError(t, err)
	assert.Contains(t, follows.FolloweeIds, b)

	// 查询粉丝列表
	fans, err := testClient.GetFans(ctx, &relation.GetFansReq{UserId: b, Page: 1, PageSize: 10})
	require.NoError(t, err)
	assert.Contains(t, fans.FollowerIds, a)

	// IsFollow
	isFollow, err := testClient.IsFollow(ctx, &relation.IsFollowReq{FollowerId: a, FolloweeIds: []int64{b}})
	require.NoError(t, err)
	assert.True(t, isFollow.Results[b])

	// 取关
	unfollow, err := testClient.Unfollow(ctx, &relation.UnfollowReq{FollowerId: a, FolloweeId: b})
	require.NoError(t, err)
	assert.True(t, unfollow.Success)

	// 重复取关仍成功
	unfollow, err = testClient.Unfollow(ctx, &relation.UnfollowReq{FollowerId: a, FolloweeId: b})
	require.NoError(t, err)
	assert.True(t, unfollow.Success)

	// 取关后查询
	follows, err = testClient.GetFollows(ctx, &relation.GetFollowsReq{UserId: a, Page: 1, PageSize: 10})
	require.NoError(t, err)
	assert.NotContains(t, follows.FolloweeIds, b)
}

func TestIntegration_FollowSelf(t *testing.T) {
	requireEnv(t)
	ctx := newTestCtx()
	a := uid(3)

	_, err := testClient.Follow(ctx, &relation.FollowReq{FollowerId: a, FolloweeId: a})
	require.Error(t, err)

	codeErr, ok := errorx.TryParse(err)
	require.True(t, ok)
	assert.Equal(t, errorx.RelationSelf, codeErr.Code)
}

func TestIntegration_InvalidParam(t *testing.T) {
	requireEnv(t)
	ctx := newTestCtx()

	_, err := testClient.Follow(ctx, &relation.FollowReq{FollowerId: -1, FolloweeId: uid(4)})
	require.Error(t, err)
	codeErr, ok := errorx.TryParse(err)
	require.True(t, ok)
	assert.Equal(t, errorx.ParamError, codeErr.Code)
}

func TestIntegration_IsVipRebuild(t *testing.T) {
	requireEnv(t)
	ctx := newTestCtx()
	vipUser := uid(100)
	threshold := int64(5)

	// 制造 5 个粉丝
	for i := int64(1); i <= threshold; i++ {
		_, err := testClient.Follow(ctx, &relation.FollowReq{FollowerId: uid(1000 + i), FolloweeId: vipUser})
		require.NoError(t, err)
	}

	// 当前配置阈值是 10000，所以不是大 V；这里只验证粉丝数回源逻辑可用。
	isVip, err := testClient.IsVip(ctx, &relation.IsVipReq{UserId: vipUser})
	require.NoError(t, err)
	assert.False(t, isVip.IsVip)
}

func TestIntegration_ConcurrentFollow(t *testing.T) {
	requireEnv(t)
	ctx := newTestCtx()
	a, b := uid(200), uid(201)

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := testClient.Follow(ctx, &relation.FollowReq{FollowerId: a, FolloweeId: b})
			assert.NoError(t, err)
		}()
	}
	wg.Wait()

	// 最终只能有 1 条记录
	fans, err := testClient.GetFans(ctx, &relation.GetFansReq{UserId: b, Page: 1, PageSize: 10})
	require.NoError(t, err)
	assert.Len(t, fans.FollowerIds, 1)
	assert.Contains(t, fans.FollowerIds, a)
}
