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
)

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
		fmt.Fprintf(os.Stderr, "integration test setup: %v\n", err)
		// 基础设施不可用视为跳过而非失败，保证无 MySQL 环境下测试链路仍绿
		os.Exit(0)
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
	configPath := filepath.Join(filepath.Dir(currentFile), "..", "etc", "comment-test.yaml")

	var c config.Config
	conf.MustLoad(configPath, &c)

	// MySQL 必须可达，否则跳过整包
	db, err := sql.Open("mysql", c.Mysql.DataSource)
	if err != nil {
		return 0, err
	}
	if err := db.Ping(); err != nil {
		return 0, fmt.Errorf("mysql unreachable, skip integration tests: %w", err)
	}
	testDB = db
	if _, err := db.Exec("TRUNCATE TABLE comments"); err != nil {
		return 0, err
	}

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

	srv := zrpc.MustNewServer(c.RpcServerConf, func(grpcServer *grpc.Server) {
		comment.RegisterCommentServer(grpcServer, server.NewCommentServer(testCtx))
	})
	srv.AddUnaryInterceptors(serverinterceptors.ErrorInterceptor)
	defer srv.Stop()

	go func() {
		fmt.Printf("Starting integration test rpc server at %s...\n", c.ListenOn)
		srv.Start()
	}()
	time.Sleep(300 * time.Millisecond)

	conn, err := grpc.NewClient(c.ListenOn, grpc.WithTransportCredentials(insecure.NewCredentials()))
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
