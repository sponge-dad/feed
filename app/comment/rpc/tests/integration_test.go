// Package tests 是 Comment RPC 服务的集成测试。
//
// 依赖真实 MySQL（feed_comment_test 库，执行 deploy/sql/comment.sql 同构建表），
// 通过真实 gRPC server/client 端到端验证「发表 -> 列表 -> 计数 -> 删除」闭环，
// 覆盖事务联动（reply_count）、窗口函数预览 SQL、游标翻页等只有真库才能验证的路径。
// Redis 使用内嵌 miniredis（本地无常驻 Redis 时保持自包含）；User/Feed RPC 用 stub；
// MQ 不发送（Producer 为空，发送逻辑 nil 安全）。
// MySQL 不可达时整包跳过，保证无基础设施环境下 go test ./... 仍可通过。
package tests

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"time"

	"testing"

	"github.com/alicebob/miniredis/v2"
	_ "github.com/go-sql-driver/mysql"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/sponge-dad/feed/app/comment/model"
	"github.com/sponge-dad/feed/app/comment/rpc/comment"
	"github.com/sponge-dad/feed/app/comment/rpc/internal/config"
	"github.com/sponge-dad/feed/app/comment/rpc/internal/server"
	"github.com/sponge-dad/feed/app/comment/rpc/internal/serverinterceptors"
	"github.com/sponge-dad/feed/app/comment/rpc/internal/svc"
	feedpb "github.com/sponge-dad/feed/app/feed/rpc/feed"
	"github.com/sponge-dad/feed/app/feed/rpc/feedclient"
	userpb "github.com/sponge-dad/feed/app/user/rpc/user"
	"github.com/sponge-dad/feed/app/user/rpc/userClient"
	"github.com/sponge-dad/feed/common/idgen"
	"github.com/sponge-dad/feed/common/testutil"

	"github.com/zeromicro/go-zero/core/conf"
	"github.com/zeromicro/go-zero/core/stores/redis"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
	"github.com/zeromicro/go-zero/zrpc"
)

var (
	// 由 TestMain 初始化，所有测试共享同一服务实例与 gRPC 客户端。
	testClient comment.CommentClient
	testCtx    *svc.ServiceContext
	testDB     *sql.DB
	// skipReason 非空表示外部基础设施不可用，所有集成测试统一 Skip。
	skipReason string
	// idSeq 运行期唯一 ID 序列（时间戳打底），保证多次运行数据互不污染。
	idSeq atomic.Int64
)

// requireEnv 集成测试统一环境探活入口：仅当基础设施缺失时 Skip。
func requireEnv(t *testing.T) {
	t.Helper()
	if skipReason != "" {
		t.Skip("integration dependency unavailable: " + skipReason)
	}
}

// nextID 生成本次运行内唯一的业务 ID（feed/user 通用），
// 替代固定 ID + TRUNCATE，测试之间及多次运行之间互不污染。
func nextID() int64 {
	return idSeq.Add(1)
}

// stubUserRpc 返回固定昵称，避免依赖真实 User 服务。
type stubUserRpc struct {
	userClient.User
}

func (s *stubUserRpc) BatchGetUsers(_ context.Context, in *userpb.BatchGetUsersReq, _ ...grpc.CallOption) (*userpb.BatchGetUsersResp, error) {
	resp := &userpb.BatchGetUsersResp{}
	for _, id := range in.UserIds {
		resp.Users = append(resp.Users, &userpb.UserBrief{
			Id: id, Nickname: fmt.Sprintf("nick-%d", id), Avatar: fmt.Sprintf("avatar-%d", id),
		})
	}
	return resp, nil
}

// stubFeedRpc 认为所有帖子均存在，避免依赖真实 Feed 服务。
type stubFeedRpc struct {
	feedclient.Feed
}

func (s *stubFeedRpc) GetFeed(_ context.Context, in *feedpb.GetFeedReq, _ ...grpc.CallOption) (*feedpb.GetFeedResp, error) {
	return &feedpb.GetFeedResp{}, nil
}

func TestMain(m *testing.M) {
	code, err := run(m)
	if err != nil {
		// 只有环境探活通过后仍出错才算 setup 失败；探活失败走 skipReason 路径。
		fmt.Fprintf(os.Stderr, "integration test setup failed: %v\n", err)
		os.Exit(1)
	}
	os.Exit(code)
}

func run(m *testing.M) (int, error) {
	if err := idgen.Init(1); err != nil {
		return 0, err
	}
	idSeq.Store(time.Now().UnixNano() / 1000) // 微秒级基准，每次运行唯一

	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		return 0, fmt.Errorf("failed to get current file path")
	}
	configPath := filepath.Join(filepath.Dir(currentFile), "..", "etc", "comment-test.yaml")

	var c config.Config
	conf.MustLoad(configPath, &c)

	// 统一环境探活（短超时）：仅 MySQL 为硬依赖（Redis 用 miniredis、RPC 用 stub）。
	// 不可用时置 skipReason，用例通过 requireEnv 统一 Skip，而不是掩盖 setup 错误。
	if addr := testutil.MySQLAddrFromDSN(c.Mysql.DataSource); !testutil.DialOK(addr) {
		skipReason = "mysql(" + addr + ")"
		return m.Run(), nil
	}

	db, err := sql.Open("mysql", c.Mysql.DataSource)
	if err != nil {
		return 0, err
	}
	if err := db.Ping(); err != nil {
		return 0, fmt.Errorf("mysql ping failed: %w", err)
	}
	testDB = db

	// Redis 用内嵌 miniredis，保证测试自包含
	mr, err := miniredis.Run()
	if err != nil {
		return 0, err
	}
	defer mr.Close()

	// 手工组装 ServiceContext：真实 MySQL + miniredis + RPC stub + 不发 MQ
	testCtx = &svc.ServiceContext{
		Config:       c,
		CommentModel: model.NewCommentsModel(sqlx.NewMysql(c.Mysql.DataSource)),
		Redis:        redis.MustNewRedis(redis.RedisConf{Type: "node", Host: mr.Addr()}),
		IdGen:        idgen.Next,
		UserRpc:      &stubUserRpc{},
		FeedRpc:      &stubFeedRpc{},
	}

	// 动态申请空闲端口，避免与业务服务或其他测试包冲突；客户端直连实际地址。
	listenOn, err := testutil.FreePort()
	if err != nil {
		return 0, err
	}
	c.ListenOn = listenOn

	srv := zrpc.MustNewServer(c.RpcServerConf, func(grpcServer *grpc.Server) {
		comment.RegisterCommentServer(grpcServer, server.NewCommentServer(testCtx))
	})
	srv.AddUnaryInterceptors(serverinterceptors.ErrorInterceptor)
	defer srv.Stop()

	go func() {
		fmt.Printf("Starting integration test rpc server at %s...\n", c.ListenOn)
		srv.Start()
	}()
	// 轮询等待服务就绪，替代固定 Sleep。
	if err := testutil.WaitReady(listenOn, 5*time.Second); err != nil {
		return 0, err
	}

	conn, err := grpc.NewClient(listenOn, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return 0, err
	}
	defer conn.Close()
	testClient = comment.NewCommentClient(conn)

	return m.Run(), nil
}

// dbCommentCount 直接查 MySQL 统计帖子可见评论数，用于与缓存计数对账。
// 口径与服务端一致（方案 A）：子回复排除「根评论已删」的折叠楼。
func dbCommentCount(t *testing.T, feedID int64) int64 {
	t.Helper()
	var count int64
	err := testDB.QueryRow(
		`SELECT COUNT(*) FROM comments c LEFT JOIN comments r ON c.root_id = r.id
		 WHERE c.feed_id = ? AND c.status = 1 AND (c.root_id = 0 OR r.status = 1)`, feedID).Scan(&count)
	if err != nil {
		t.Fatalf("query count failed: %v", err)
	}
	return count
}
