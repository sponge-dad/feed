// integration_test.go
//
// User RPC 集成测试：启动真实 User RPC Server（依赖真实 MySQL + Redis），
// 通过真实 gRPC Client 调用 Register / Login 等接口，验证端到端行为。
// 基础设施（MySQL / Redis）不可用时统一 Skip，不掩盖业务缺陷。
//
// 对应基线：U-REG-05（并发同名注册唯一索引冲突）。
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
	"github.com/sponge-dad/feed/app/user/rpc/internal/config"
	"github.com/sponge-dad/feed/app/user/rpc/internal/server"
	"github.com/sponge-dad/feed/app/user/rpc/internal/serverinterceptors"
	"github.com/sponge-dad/feed/app/user/rpc/internal/svc"
	"github.com/sponge-dad/feed/app/user/rpc/user"
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
	// 由 TestMain 初始化，所有测试共享同一个服务实例和 gRPC 客户端。
	testClient user.UserClient
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
	configPath := filepath.Join(filepath.Dir(currentFile), "..", "etc", "user-test.yaml")

	var c config.Config
	conf.MustLoad(configPath, &c)

	// 统一环境探活（短超时）：MySQL 与 Redis 缺一不可。
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

	// 动态申请空闲端口，避免与业务服务（User RPC 9001）或其他测试包端口冲突；
	// 同时清空 etcd 注册配置：测试客户端直连实际监听地址，
	// 绝不依赖 etcd 中开发环境的 User 服务注册信息。
	listenOn, err := testutil.FreePort()
	if err != nil {
		return 0, err
	}
	c.ListenOn = listenOn
	c.Etcd = discov.EtcdConf{}

	ctx := svc.NewServiceContext(c)
	testCtx = ctx

	// 清理历史 model 缓存与业务缓存，避免之前测试运行的 stale 缓存影响当前测试。
	cleanupUserModelCache()

	db, err := sql.Open("mysql", c.Mysql.DataSource)
	if err != nil {
		return 0, err
	}
	testDB = db

	srv := zrpc.MustNewServer(c.RpcServerConf, func(grpcServer *grpc.Server) {
		user.RegisterUserServer(grpcServer, server.NewUserServer(ctx))
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
		fmt.Printf("Starting user integration test rpc server at %s...\n", c.ListenOn)
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
	testClient = user.NewUserClient(conn)

	code := m.Run()

	testStop()
	return code, nil
}

// cleanupUserModelCache 删除匹配 cache:users:* 与 user:brief:* 的所有 key，
// 避免历史 stale 缓存影响测试。测试环境数据量小，使用 KEYS 命令可接受。
func cleanupUserModelCache() {
	script := `local n=0; for _,p in ipairs(ARGV) do local ks=redis.call('keys', p); for i=1,#ks do redis.call('del', ks[i]); n=n+1; end; end; return n`
	_, _ = testCtx.Redis.Eval(script, []string{}, "cache:users:*", "user:brief:*")
}

func newTestCtx() context.Context {
	return context.Background()
}

func randUsername(prefix string) string {
	return fmt.Sprintf("%s_%d_%d", prefix, time.Now().UnixNano(), rand.Int63())
}

// Baseline: U-REG-05
// 真实 User RPC + 真实 MySQL + 真实 bcrypt/JWT 流程下的并发同名注册。
// 至少 20 个 goroutine 同时使用完全相同的用户名注册，验证：
//   - users 表最终只有一条该用户名记录；
//   - 只有一个请求注册成功；
//   - 其他请求返回业务码 UsernameExists(10001)，不得透传原始 1062；
//   - 所有 goroutine 在超时时间内结束；
//   - go test -race 无数据竞争。
func TestIntegration_Register_ConcurrentSameUsername_UREG05(t *testing.T) {
	requireEnv(t)
	ctx := newTestCtx()

	username := randUsername("ureg05")
	const workers = 20

	var wg sync.WaitGroup
	errs := make([]error, workers)
	var start sync.WaitGroup
	start.Add(1)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			start.Wait()
			_, err := testClient.Register(ctx, &user.RegisterReq{
				Username: username,
				Password: "s3cret-pass",
				Nickname: "并发用户",
				CityCode: "440300",
				CityName: "深圳",
			})
			errs[idx] = err
		}(i)
	}
	start.Done()

	// 所有 goroutine 必须在超时内结束，避免永久阻塞。
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("并发注册未在 30s 内全部结束，可能存在死锁或长期阻塞")
	}

	// 清理：删除本次注册产生的用户名记录，保证数据隔离。
	defer func() {
		_, _ = testDB.Exec("DELETE FROM users WHERE username = ?", username)
	}()

	var successCount, existsCount int
	for _, err := range errs {
		if err == nil {
			successCount++
			continue
		}
		codeErr, ok := errorx.TryParse(err)
		require.True(t, ok, "并发撞唯一键应返回可解析的业务错误码，实际: %v", err)
		assert.Equal(t, errorx.UserExists, codeErr.Code,
			"1062 应转为 UsernameExists，不应透传原始 *mysql.MySQLError(退化为 ServerError)")
		existsCount++
	}

	assert.Equal(t, 1, successCount, "同一用户名最终只能注册成功一次")
	assert.Equal(t, workers, successCount+existsCount, "所有请求要么成功要么返回 UserExists")

	// 数据库最终仅一条记录（并发唯一索引保证最终一致）。
	var cnt int
	require.NoError(t, testDB.QueryRow("SELECT COUNT(*) FROM users WHERE username = ?", username).Scan(&cnt))
	assert.Equal(t, 1, cnt, "users 表该用户名应仅一条记录")
}
