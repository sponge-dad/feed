package logic

import (
	"context"
	"encoding/json"
	"strconv"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/stretchr/testify/require"
	"github.com/zeromicro/go-zero/core/stores/redis"

	"github.com/sponge-dad/feed/app/feed/rpc/feed"
	"github.com/sponge-dad/feed/app/feed/rpc/internal/config"
	"github.com/sponge-dad/feed/app/feed/rpc/internal/keys"
	"github.com/sponge-dad/feed/app/feed/rpc/internal/svc"
)

// newQuerySvc 构造仅含 Redis 与 Config 的 ServiceContext（查询 RPC 不需要 FeedModel/RelationRpc）。
func newQuerySvc(t *testing.T) (*svc.ServiceContext, *miniredis.Miniredis) {
	t.Helper()
	mr, err := miniredis.Run()
	require.NoError(t, err)
	t.Cleanup(mr.Close)
	rdb := redis.MustNewRedis(redis.RedisConf{Type: "node", Host: mr.Addr()})
	cfg := config.Config{}
	cfg.Trace.SampleRate = 1
	cfg.Trace.TTL = 100
	return &svc.ServiceContext{Redis: rdb, Config: cfg}, mr
}

// seedTrace 直接向 miniredis 写入一次 Trace（meta + 各 f:{feed_id}），模拟 timeline 逻辑异步写入的结果。
func seedTrace(t *testing.T, mr *miniredis.Miniredis, tr *feed.FeedRequestTrace) {
	t.Helper()
	data, err := json.Marshal(tr)
	require.NoError(t, err)
	mr.HSet(keys.FeedTraceKey(tr.RequestId), "meta", string(data))
	for _, it := range tr.Items {
		mr.HSet(keys.FeedTraceKey(tr.RequestId), "f:"+strconv.FormatInt(it.FeedId, 10), it.Source)
	}
}

func TestGetFeedRequestTrace(t *testing.T) {
	svcCtx, mr := newQuerySvc(t)
	tr := &feed.FeedRequestTrace{
		RequestId: "r1",
		UserId:    100,
		Tab:       "follow",
		Sources:   []*feed.SourceStat{{Source: "FEED_SOURCE_FOLLOW_INBOX", Count: 3}},
		Items:     []*feed.TraceItem{{FeedId: 1, Source: "FEED_SOURCE_FOLLOW_INBOX"}},
	}
	seedTrace(t, mr, tr)

	l := NewGetFeedRequestTraceLogic(context.Background(), svcCtx)

	// 本人查询：返回完整 Trace。
	resp, err := l.GetFeedRequestTrace(&feed.GetFeedRequestTraceReq{RequestId: "r1", UserId: 100})
	require.NoError(t, err)
	require.NotNil(t, resp.Trace)
	require.Equal(t, int64(100), resp.Trace.UserId)
	require.Len(t, resp.Trace.Sources, 1)

	// 他人查询：无权限。
	_, err = l.GetFeedRequestTrace(&feed.GetFeedRequestTraceReq{RequestId: "r1", UserId: 200})
	require.Error(t, err)

	// 内部用户查询他人：放行。
	svcCtx.Config.Trace.InternalUserIDs = []int64{200}
	l2 := NewGetFeedRequestTraceLogic(context.Background(), svcCtx)
	resp2, err := l2.GetFeedRequestTrace(&feed.GetFeedRequestTraceReq{RequestId: "r1", UserId: 200})
	require.NoError(t, err)
	require.NotNil(t, resp2.Trace)

	// 不存在的 request_id：返回空 Trace，不报错。
	resp3, err := l.GetFeedRequestTrace(&feed.GetFeedRequestTraceReq{RequestId: "nope", UserId: 100})
	require.NoError(t, err)
	require.Nil(t, resp3.Trace)
}

func TestGetFeedSource(t *testing.T) {
	svcCtx, mr := newQuerySvc(t)
	tr := &feed.FeedRequestTrace{
		RequestId: "r2",
		UserId:    100,
		Items:     []*feed.TraceItem{{FeedId: 1, Source: "FEED_SOURCE_FOLLOW_INBOX"}},
	}
	seedTrace(t, mr, tr)

	l := NewGetFeedSourceLogic(context.Background(), svcCtx)

	// 本人查询命中。
	resp, err := l.GetFeedSource(&feed.GetFeedSourceReq{RequestId: "r2", FeedId: 1, UserId: 100})
	require.NoError(t, err)
	require.Equal(t, feed.FeedSource_FEED_SOURCE_FOLLOW_INBOX, resp.Source)

	// 不存在的 feed：UNKNOWN。
	resp2, err := l.GetFeedSource(&feed.GetFeedSourceReq{RequestId: "r2", FeedId: 999, UserId: 100})
	require.NoError(t, err)
	require.Equal(t, feed.FeedSource_FEED_SOURCE_UNKNOWN, resp2.Source)

	// 不存在的 request_id：UNKNOWN。
	resp3, err := l.GetFeedSource(&feed.GetFeedSourceReq{RequestId: "nope", FeedId: 1, UserId: 100})
	require.NoError(t, err)
	require.Equal(t, feed.FeedSource_FEED_SOURCE_UNKNOWN, resp3.Source)

	// 他人查询：无权限。
	_, err = l.GetFeedSource(&feed.GetFeedSourceReq{RequestId: "r2", FeedId: 1, UserId: 200})
	require.Error(t, err)
}
