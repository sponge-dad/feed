// integration_test.go
//
// 职责：Interaction RPC 服务集成测试（docs/design/interaction/08-test-strategy §4）。
// 启动真实 zrpc 服务 + MySQL/Redis，通过 gRPC 客户端验证端到端行为：
// 点赞/收藏主链路、幂等、批量查询、列表分页、冷 key 回源重建、参数校验。
//
// 环境约定：
//   - 配置文件 etc/interaction-test.yaml：独立测试库 feed_interaction_test、独立端口 9105；
//   - TestMain 自动探测依赖：MySQL 与 Redis 任一不可达时全部用例自动 Skip（CI 无存储环境不失败）；
//   - MySQL 测试库与表由 TestMain 自动创建（DDL 与 deploy/sql/interaction.sql 一致）；
//   - RocketMQ 可达时走真实「生产者 → Broker → 持久化消费者」链路；
//     不可达时降级为进程内桥接（bridgePublisher 直接调用 worker.HandleEvent），
//     仍完整覆盖异步落库逻辑，仅少了 MQ 传输一环（对 08-test-strategy 的声明性偏差）；
//   - 每次运行使用时间戳派生的唯一 user/feed ID，避免与历史数据互相污染。
package tests

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/sponge-dad/feed/app/interaction/model"
	"github.com/sponge-dad/feed/app/interaction/rpc/interaction"
	"github.com/sponge-dad/feed/app/interaction/rpc/internal/config"
	"github.com/sponge-dad/feed/app/interaction/rpc/internal/keys"
	"github.com/sponge-dad/feed/app/interaction/rpc/internal/server"
	"github.com/sponge-dad/feed/app/interaction/rpc/internal/serverinterceptors"
	"github.com/sponge-dad/feed/app/interaction/rpc/internal/svc"
	"github.com/sponge-dad/feed/app/interaction/rpc/internal/worker"
	"github.com/sponge-dad/feed/common/errorx"
	event "github.com/sponge-dad/feed/common/event/interaction"
	"github.com/sponge-dad/feed/common/idgen"
	"github.com/sponge-dad/feed/common/mq"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zeromicro/go-zero/core/conf"
	"github.com/zeromicro/go-zero/core/stores/redis"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
	"github.com/zeromicro/go-zero/zrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

var (
	// skipReason 非空表示依赖缺失，所有用例 Skip。
	skipReason string
	// testClient 所有测试共享的 gRPC 客户端。
	testClient interaction.InteractionClient
	// testCtx 服务上下文，用于直接操作 Redis 制造冷 key 场景。
	testCtx *svc.ServiceContext
	// testDB 直连测试库，用于校验异步落库结果。
	testDB *sql.DB
	// testStop 资源清理回调。
	testStop func()
	// idSeq 运行期唯一 ID 序列（时间戳打底），保证多次运行互不污染。
	idSeq atomic.Int64
)

// bridgePublisher MQ 不可达时的进程内桥接：把事件直接交给持久化 worker，
// 覆盖「Redis 先行 + 异步落库」链路中除 MQ 传输外的全部逻辑。
type bridgePublisher struct {
	wk *worker.Worker
}

// SendSync 实现 svc.Publisher：解析事件并同步落库。
func (b bridgePublisher) SendSync(_ string, body []byte) error {
	var ev event.Event
	if err := json.Unmarshal(body, &ev); err != nil {
		return err
	}
	return b.wk.HandleEvent(context.Background(), &ev)
}

func TestMain(m *testing.M) {
	code, err := run(m)
	if err != nil {
		fmt.Fprintf(os.Stderr, "integration test setup failed: %v\n", err)
		os.Exit(1)
	}
	os.Exit(code)
}

// run 装配集成测试环境；依赖缺失时置 skipReason 后照常执行（用例内部 Skip）。
func run(m *testing.M) (int, error) {
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		return 0, fmt.Errorf("failed to get current file path")
	}
	configPath := filepath.Join(filepath.Dir(currentFile), "..", "etc", "interaction-test.yaml")

	var c config.Config
	conf.MustLoad(configPath, &c)

	mysqlAddr := mysqlAddrFromDSN(c.Mysql.DataSource)
	switch {
	case !dialOK(mysqlAddr):
		skipReason = fmt.Sprintf("integration: mysql %s unreachable", mysqlAddr)
	case !dialOK(c.CacheRedis[0].Host):
		skipReason = fmt.Sprintf("integration: redis %s unreachable", c.CacheRedis[0].Host)
	}
	if skipReason != "" {
		fmt.Fprintln(os.Stderr, skipReason+", skipping all integration tests")
		return m.Run(), nil
	}

	if err := idgen.Init(1); err != nil {
		return 0, err
	}
	idSeq.Store(time.Now().UnixNano() / 1000) // 微秒级基准，每次运行唯一

	if err := bootstrapSchema(c.Mysql.DataSource); err != nil {
		return 0, err
	}

	db, err := sql.Open("mysql", c.Mysql.DataSource)
	if err != nil {
		return 0, err
	}
	testDB = db

	conn := sqlx.NewMysql(c.Mysql.DataSource)
	rds := redis.MustNewRedis(c.CacheRedis[0].RedisConf)
	ctx := &svc.ServiceContext{
		Config:           c,
		Redis:            rds,
		LikesModel:       model.NewLikesModel(conn, c.CacheRedis),
		CollectionsModel: model.NewCollectionsModel(conn, c.CacheRedis),
		IdGen:            idgen.Next,
	}
	testCtx = ctx

	wk := worker.NewWorker(ctx)
	mqUp := len(c.RocketMQ.NameServer) > 0 && dialOK(c.RocketMQ.NameServer[0])
	if mqUp {
		producer, perr := mq.NewProducer(c.RocketMQ.NameServer, c.RocketMQ.GroupName)
		if perr != nil {
			return 0, perr
		}
		consumer, cerr := mq.NewConsumer(c.RocketMQ.NameServer, c.RocketMQ.ConsumeGroup)
		if cerr != nil {
			return 0, cerr
		}
		ctx.Producer = producer
		ctx.Consumer = consumer
		if werr := wk.Start(); werr != nil {
			return 0, werr
		}
	} else {
		fmt.Fprintln(os.Stderr, "integration: rocketmq unreachable, using in-process bridge publisher")
		ctx.Producer = bridgePublisher{wk: wk}
	}

	srv := zrpc.MustNewServer(c.RpcServerConf, func(grpcServer *grpc.Server) {
		interaction.RegisterInteractionServer(grpcServer, server.NewInteractionServer(ctx))
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
	time.Sleep(300 * time.Millisecond)

	grpcConn, err := grpc.Dial(c.ListenOn, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		testStop()
		return 0, err
	}
	testClient = interaction.NewInteractionClient(grpcConn)

	code := m.Run()
	testStop()
	return code, nil
}

// ---------- 环境辅助 ----------

// dialOK TCP 探测目标地址是否可达。
func dialOK(addr string) bool {
	conn, err := net.DialTimeout("tcp", addr, 500*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// mysqlAddrFromDSN 从 go-sql-driver DSN 中提取 tcp 地址。
func mysqlAddrFromDSN(dsn string) string {
	start := strings.Index(dsn, "tcp(")
	if start < 0 {
		return "127.0.0.1:3306"
	}
	rest := dsn[start+len("tcp("):]
	end := strings.Index(rest, ")")
	if end < 0 {
		return "127.0.0.1:3306"
	}
	return rest[:end]
}

// bootstrapSchema 创建测试库与表（幂等），DDL 与 deploy/sql/interaction.sql 一致。
func bootstrapSchema(dsn string) error {
	// 去掉库名连接到 server 级别：root:root@tcp(host)/dbname?params -> root:root@tcp(host)/?params
	slash := strings.Index(dsn, ")/")
	if slash < 0 {
		return fmt.Errorf("unexpected mysql dsn: %s", dsn)
	}
	dbAndParams := dsn[slash+2:]
	dbName := dbAndParams
	if q := strings.Index(dbAndParams, "?"); q >= 0 {
		dbName = dbAndParams[:q]
	}
	serverDSN := dsn[:slash+2] + dsn[slash+1+len(dbName)+1:]

	db, err := sql.Open("mysql", serverDSN)
	if err != nil {
		return err
	}
	defer db.Close()

	ddls := []string{
		"CREATE DATABASE IF NOT EXISTS `" + dbName + "` DEFAULT CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci",
		"CREATE TABLE IF NOT EXISTS `" + dbName + "`.`likes` (" +
			"`id` BIGINT UNSIGNED NOT NULL, `user_id` BIGINT UNSIGNED NOT NULL, `feed_id` BIGINT UNSIGNED NOT NULL," +
			"`status` TINYINT NOT NULL DEFAULT 1," +
			"`created_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP," +
			"`updated_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP," +
			"PRIMARY KEY (`id`), UNIQUE KEY `uk_user_feed` (`user_id`,`feed_id`)," +
			"KEY `idx_feed` (`feed_id`), KEY `idx_user_created` (`user_id`,`created_at`)" +
			") ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci",
		"CREATE TABLE IF NOT EXISTS `" + dbName + "`.`collections` (" +
			"`id` BIGINT UNSIGNED NOT NULL, `user_id` BIGINT UNSIGNED NOT NULL, `feed_id` BIGINT UNSIGNED NOT NULL," +
			"`status` TINYINT NOT NULL DEFAULT 1," +
			"`created_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP," +
			"`updated_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP," +
			"PRIMARY KEY (`id`), UNIQUE KEY `uk_user_feed` (`user_id`,`feed_id`)," +
			"KEY `idx_feed` (`feed_id`), KEY `idx_user_created` (`user_id`,`created_at`)" +
			") ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci",
	}
	for _, ddl := range ddls {
		if _, err := db.Exec(ddl); err != nil {
			return fmt.Errorf("exec ddl failed: %w", err)
		}
	}
	return nil
}

// requireEnv 依赖缺失时跳过当前用例。
func requireEnv(t *testing.T) {
	t.Helper()
	if skipReason != "" {
		t.Skip(skipReason)
	}
}

// nextID 生成本次运行内唯一的业务 ID（user/feed 通用）。
func nextID() int64 {
	return idSeq.Add(1)
}

// requireBizCode 断言 gRPC 错误可还原为指定业务码。
func requireBizCode(t *testing.T, err error, code int) {
	t.Helper()
	require.Error(t, err)
	codeErr, ok := errorx.TryParse(err)
	require.True(t, ok, "expect biz error, got: %v", err)
	assert.Equal(t, code, codeErr.Code)
}

// waitRowStatus 轮询等待异步落库达到期望状态（table ∈ likes/collections）。
func waitRowStatus(t *testing.T, table string, userID, feedID, want int64) {
	t.Helper()
	query := fmt.Sprintf("SELECT status FROM %s WHERE user_id = ? AND feed_id = ?", table)
	deadline := time.Now().Add(5 * time.Second)
	var last string
	for time.Now().Before(deadline) {
		var status int64
		err := testDB.QueryRow(query, userID, feedID).Scan(&status)
		switch {
		case err == nil && status == want:
			return
		case err == nil:
			last = fmt.Sprintf("status=%d", status)
		default:
			last = err.Error()
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("wait %s(user=%d feed=%d) status=%d timeout, last=%s", table, userID, feedID, want, last)
}

// waitRowCount 轮询等待表内有效记录数达到期望值。
func waitRowCount(t *testing.T, table string, feedID, want int64) {
	t.Helper()
	query := fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE feed_id = ? AND status = 1", table)
	deadline := time.Now().Add(10 * time.Second)
	var last int64 = -1
	for time.Now().Before(deadline) {
		var cnt int64
		if err := testDB.QueryRow(query, feedID).Scan(&cnt); err == nil {
			if cnt == want {
				return
			}
			last = cnt
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("wait %s(feed=%d) count=%d timeout, last=%d", table, feedID, want, last)
}

// getStats 查询单帖计数。
func getStats(t *testing.T, feedID int64) *interaction.FeedStats {
	t.Helper()
	resp, err := testClient.GetFeedStats(context.Background(), &interaction.GetFeedStatsReq{FeedId: feedID})
	require.NoError(t, err)
	require.NotNil(t, resp.Stats)
	return resp.Stats
}

// getStatus 查询用户互动状态。
func getStatus(t *testing.T, userID, feedID int64) *interaction.UserInteractionStatus {
	t.Helper()
	resp, err := testClient.GetUserInteractionStatus(context.Background(),
		&interaction.GetUserInteractionStatusReq{UserId: userID, FeedId: feedID})
	require.NoError(t, err)
	require.NotNil(t, resp.Status)
	return resp.Status
}

// ---------- 端到端功能用例（08-test-strategy §4.1） ----------

// TestIntegration_LikeMainFlow 点赞主链路：点赞 → 状态/计数 → 幂等 → 落库 → 取消 → 落库。
func TestIntegration_LikeMainFlow(t *testing.T) {
	requireEnv(t)
	ctx := context.Background()
	user, feed := nextID(), nextID()

	_, err := testClient.LikeFeed(ctx, &interaction.LikeFeedReq{UserId: user, FeedId: feed})
	require.NoError(t, err)

	assert.True(t, getStatus(t, user, feed).IsLiked)
	assert.Equal(t, int64(1), getStats(t, feed).LikeCount)

	// 重复点赞幂等：计数不变
	_, err = testClient.LikeFeed(ctx, &interaction.LikeFeedReq{UserId: user, FeedId: feed})
	require.NoError(t, err)
	assert.Equal(t, int64(1), getStats(t, feed).LikeCount)

	// 异步落库 status=1
	waitRowStatus(t, "likes", user, feed, 1)

	// 取消点赞
	_, err = testClient.UnlikeFeed(ctx, &interaction.UnlikeFeedReq{UserId: user, FeedId: feed})
	require.NoError(t, err)
	assert.False(t, getStatus(t, user, feed).IsLiked)
	assert.Equal(t, int64(0), getStats(t, feed).LikeCount)

	// 重复取消幂等：计数不为负
	_, err = testClient.UnlikeFeed(ctx, &interaction.UnlikeFeedReq{UserId: user, FeedId: feed})
	require.NoError(t, err)
	assert.Equal(t, int64(0), getStats(t, feed).LikeCount)

	// 异步落库 status=2
	waitRowStatus(t, "likes", user, feed, 2)
}

// TestIntegration_CollectMainFlow 收藏主链路，与点赞同构。
func TestIntegration_CollectMainFlow(t *testing.T) {
	requireEnv(t)
	ctx := context.Background()
	user, feed := nextID(), nextID()

	_, err := testClient.CollectFeed(ctx, &interaction.CollectFeedReq{UserId: user, FeedId: feed})
	require.NoError(t, err)
	assert.True(t, getStatus(t, user, feed).IsCollected)
	assert.Equal(t, int64(1), getStats(t, feed).CollectCount)
	waitRowStatus(t, "collections", user, feed, 1)

	_, err = testClient.UncollectFeed(ctx, &interaction.UncollectFeedReq{UserId: user, FeedId: feed})
	require.NoError(t, err)
	assert.False(t, getStatus(t, user, feed).IsCollected)
	assert.Equal(t, int64(0), getStats(t, feed).CollectCount)
	waitRowStatus(t, "collections", user, feed, 2)
}

// TestIntegration_BatchQueries 批量计数/状态查询：结果与请求顺序一致，未互动帖子为零值。
func TestIntegration_BatchQueries(t *testing.T) {
	requireEnv(t)
	ctx := context.Background()
	userA, userB := nextID(), nextID()
	feedX, feedY := nextID(), nextID() // feedY 无任何互动

	for _, u := range []int64{userA, userB} {
		_, err := testClient.LikeFeed(ctx, &interaction.LikeFeedReq{UserId: u, FeedId: feedX})
		require.NoError(t, err)
	}
	_, err := testClient.CollectFeed(ctx, &interaction.CollectFeedReq{UserId: userA, FeedId: feedX})
	require.NoError(t, err)

	stats, err := testClient.BatchGetFeedStats(ctx, &interaction.BatchGetFeedStatsReq{FeedIds: []int64{feedX, feedY}})
	require.NoError(t, err)
	require.Len(t, stats.StatsList, 2)
	assert.Equal(t, feedX, stats.StatsList[0].FeedId)
	assert.Equal(t, int64(2), stats.StatsList[0].LikeCount)
	assert.Equal(t, int64(1), stats.StatsList[0].CollectCount)
	assert.Equal(t, feedY, stats.StatsList[1].FeedId)
	assert.Equal(t, int64(0), stats.StatsList[1].LikeCount)

	status, err := testClient.BatchGetUserInteractionStatus(ctx,
		&interaction.BatchGetUserInteractionStatusReq{UserId: userA, FeedIds: []int64{feedX, feedY}})
	require.NoError(t, err)
	require.Len(t, status.StatusList, 2)
	assert.True(t, status.StatusList[0].IsLiked)
	assert.True(t, status.StatusList[0].IsCollected)
	assert.False(t, status.StatusList[1].IsLiked)
	assert.False(t, status.StatusList[1].IsCollected)
}

// TestIntegration_LikedListPagination 点赞列表游标分页（08-test-strategy §4.2）：
// 翻页无重复无遗漏，取消点赞后从列表移除。
func TestIntegration_LikedListPagination(t *testing.T) {
	requireEnv(t)
	ctx := context.Background()
	user := nextID()
	feeds := make([]int64, 5)
	for i := range feeds {
		feeds[i] = nextID()
		_, err := testClient.LikeFeed(ctx, &interaction.LikeFeedReq{UserId: user, FeedId: feeds[i]})
		require.NoError(t, err)
	}

	var got []int64
	cursor := ""
	for page := 0; page < 10; page++ {
		resp, err := testClient.GetUserLikedFeeds(ctx, &interaction.GetUserLikedFeedsReq{
			UserId: user, PageSize: 2, Cursor: cursor,
		})
		require.NoError(t, err)
		assert.Equal(t, int64(5), resp.Total)
		got = append(got, resp.FeedIds...)
		if resp.NextCursor == "" {
			break
		}
		cursor = resp.NextCursor
	}
	// 无重复无遗漏
	require.Len(t, got, 5)
	assert.ElementsMatch(t, feeds, got)

	// 取消一个后列表移除
	_, err := testClient.UnlikeFeed(ctx, &interaction.UnlikeFeedReq{UserId: user, FeedId: feeds[2]})
	require.NoError(t, err)
	resp, err := testClient.GetUserLikedFeeds(ctx, &interaction.GetUserLikedFeedsReq{UserId: user, PageSize: 10})
	require.NoError(t, err)
	assert.Equal(t, int64(4), resp.Total)
	assert.NotContains(t, resp.FeedIds, feeds[2])
}

// TestIntegration_ColdKeyRebuild 冷 key 回源：清空 Redis 后计数/状态/列表均可从 MySQL 重建，
// 且重建后取消点赞不丢失（回归「冷 key 丢失取消」缺陷）。
func TestIntegration_ColdKeyRebuild(t *testing.T) {
	requireEnv(t)
	ctx := context.Background()
	user, feed := nextID(), nextID()

	_, err := testClient.LikeFeed(ctx, &interaction.LikeFeedReq{UserId: user, FeedId: feed})
	require.NoError(t, err)
	waitRowStatus(t, "likes", user, feed, 1) // 确保 MySQL 已有数据可供回源

	// 清空业务 Redis key，模拟缓存过期
	_, err = testCtx.Redis.DelCtx(ctx, keys.LikeFeed(feed), keys.UserLikes(user), keys.FeedStats(feed))
	require.NoError(t, err)

	assert.Equal(t, int64(1), getStats(t, feed).LikeCount, "计数应从 MySQL 重建")
	assert.True(t, getStatus(t, user, feed).IsLiked, "状态应从 MySQL 重建")

	list, err := testClient.GetUserLikedFeeds(ctx, &interaction.GetUserLikedFeedsReq{UserId: user, PageSize: 10})
	require.NoError(t, err)
	assert.Contains(t, list.FeedIds, feed, "列表应从 MySQL 重建")

	// 再次清空后直接取消点赞：先回源再扣减，不应丢失
	_, err = testCtx.Redis.DelCtx(ctx, keys.LikeFeed(feed), keys.UserLikes(user), keys.FeedStats(feed))
	require.NoError(t, err)
	_, err = testClient.UnlikeFeed(ctx, &interaction.UnlikeFeedReq{UserId: user, FeedId: feed})
	require.NoError(t, err)
	assert.Equal(t, int64(0), getStats(t, feed).LikeCount)
	assert.False(t, getStatus(t, user, feed).IsLiked)
	waitRowStatus(t, "likes", user, feed, 2)
}

// TestIntegration_InvalidParam 非法参数返回 errorx.ParamError 业务码。
func TestIntegration_InvalidParam(t *testing.T) {
	requireEnv(t)
	ctx := context.Background()

	_, err := testClient.LikeFeed(ctx, &interaction.LikeFeedReq{UserId: -1, FeedId: nextID()})
	requireBizCode(t, err, errorx.ParamError)

	// 非法游标：列表非空时才会走到游标解析（空列表短路返回，与单测行为一致）
	user := nextID()
	_, err = testClient.LikeFeed(ctx, &interaction.LikeFeedReq{UserId: user, FeedId: nextID()})
	require.NoError(t, err)
	_, err = testClient.GetUserLikedFeeds(ctx, &interaction.GetUserLikedFeedsReq{UserId: user, PageSize: 10, Cursor: "!!!bad-cursor!!!"})
	requireBizCode(t, err, errorx.ParamError)
}
