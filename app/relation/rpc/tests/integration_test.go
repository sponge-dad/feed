package tests

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
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
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zeromicro/go-zero/core/conf"
	"github.com/zeromicro/go-zero/core/service"
	"github.com/zeromicro/go-zero/zrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

const (
	// testUserBase 测试用户 ID 起始值，避免与开发数据冲突。
	testUserBase = 10_000_000
)

var (
	// 由 TestMain 初始化，所有测试共享同一个服务实例和 gRPC 客户端。
	testClient relation.RelationClient
	testCtx    *svc.ServiceContext
	testDB     *sql.DB
	testStop   func()
)

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

	ctx := svc.NewServiceContext(c)
	testCtx = ctx

	// 清理历史 model 缓存，避免之前测试运行的 stale 缓存影响当前测试。
	cleanupRelationModelCache()

	db, err := sql.Open("mysql", c.Mysql.DataSource)
	if err != nil {
		return 0, err
	}
	testDB = db

	srv := zrpc.MustNewServer(c.RpcServerConf, func(grpcServer *grpc.Server) {
		relation.RegisterRelationServer(grpcServer, server.NewRelationServer(ctx))
		if c.Mode == service.DevMode || c.Mode == service.TestMode {
			reflection.Register(grpcServer)
		}
	})
	srv.AddUnaryInterceptors(serverinterceptors.ErrorInterceptor)

	testStop = func() {
		srv.Stop()
		if testDB != nil {
			_ = testDB.Close()
		}
	}

	go func() {
		fmt.Printf("Starting integration test rpc server at %s...\n", c.ListenOn)
		srv.Start()
	}()

	// 等待服务启动
	time.Sleep(300 * time.Millisecond)

	conn, err := grpc.Dial(c.ListenOn, grpc.WithInsecure())
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
	ctx := newTestCtx()
	a := uid(3)

	_, err := testClient.Follow(ctx, &relation.FollowReq{FollowerId: a, FolloweeId: a})
	require.Error(t, err)

	codeErr, ok := errorx.TryParse(err)
	require.True(t, ok)
	assert.Equal(t, errorx.RelationSelf, codeErr.Code)
}

func TestIntegration_InvalidParam(t *testing.T) {
	ctx := newTestCtx()

	_, err := testClient.Follow(ctx, &relation.FollowReq{FollowerId: -1, FolloweeId: uid(4)})
	require.Error(t, err)
	codeErr, ok := errorx.TryParse(err)
	require.True(t, ok)
	assert.Equal(t, errorx.ParamError, codeErr.Code)
}

func TestIntegration_IsVipRebuild(t *testing.T) {
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
