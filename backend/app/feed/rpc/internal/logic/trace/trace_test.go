package trace

import (
	"context"
	"encoding/json"
	"strconv"
	"sync"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/stretchr/testify/require"
	"github.com/zeromicro/go-zero/core/stores/redis"

	"github.com/sponge-dad/feed/app/feed/rpc/internal/keys"
)

func TestBuilderAccumulates(t *testing.T) {
	tb := NewBuilder("req-1", 42, "follow", "cursor", 10)
	tb.RecordSource("FEED_SOURCE_FOLLOW_INBOX", 5, 2)
	tb.RecordSource("FEED_SOURCE_VIP_OUTBOX", 3, 1)
	tb.AddItem(101, "FEED_SOURCE_FOLLOW_INBOX", 0, 100)
	tb.AddItem(102, "FEED_SOURCE_VIP_OUTBOX", 1, 99)
	tb.SetMergedCount(8)
	tb.SetReturnedCount(2)
	tb.SetFilteredCount(0)
	tr := tb.Build()

	require.Equal(t, "req-1", tr.RequestId)
	require.Equal(t, int64(42), tr.UserId)
	require.Equal(t, "follow", tr.Tab)
	require.Len(t, tr.Sources, 2)
	require.Equal(t, int32(5), tr.Sources[0].Count)
	require.Len(t, tr.Items, 2)
	require.Equal(t, int64(101), tr.Items[0].FeedId)
	require.Equal(t, int32(8), tr.MergedCount)
	require.Equal(t, int32(2), tr.ReturnedCount)
	require.GreaterOrEqual(t, tr.CostMs, int64(0))
}

func TestBuilderJSONRoundTrip(t *testing.T) {
	tb := NewBuilder("req-2", 7, "recommend", "", 5)
	tb.RecordSource("FEED_SOURCE_RECOMMEND_POOL", 3, 4)
	tb.AddItem(201, "FEED_SOURCE_RECOMMEND_POOL", 0, 200)
	tr := tb.Build()

	data, err := json.Marshal(tr)
	require.NoError(t, err)

	var got FeedRequestTrace
	require.NoError(t, json.Unmarshal(data, &got))
	require.Equal(t, tr.RequestId, got.RequestId)
	require.Len(t, got.Items, 1)
	require.Equal(t, int64(201), got.Items[0].FeedId)
	require.Equal(t, "FEED_SOURCE_RECOMMEND_POOL", got.Items[0].Source)
}

// TestBuilderConcurrent 验证 Builder 在并发调用 RecordSource/AddItem 时线程安全（需配合 -race 运行）。
func TestBuilderConcurrent(t *testing.T) {
	tb := NewBuilder("req-c", 1, "follow", "", 100)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int64) {
			defer wg.Done()
			tb.RecordSource("FEED_SOURCE_FOLLOW_INBOX", 1, 1)
			tb.AddItem(i, "FEED_SOURCE_FOLLOW_INBOX", int32(i), i)
		}(int64(i))
	}
	wg.Wait()
	tr := tb.Build()
	require.Len(t, tr.Sources, 20)
	require.Len(t, tr.Items, 20)
}

// TestWriteSampleSkip 验证采样率<=0 时直接返回、不触碰 rdb（传入 nil 也不应 panic）。
func TestWriteSampleSkip(t *testing.T) {
	Write(context.Background(), nil, &FeedRequestTrace{RequestId: "r"}, 0, 0)
	Write(context.Background(), nil, &FeedRequestTrace{RequestId: "r"}, 0, -1)
}

func TestWriteToRedis(t *testing.T) {
	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()
	rdb := redis.MustNewRedis(redis.RedisConf{Type: "node", Host: mr.Addr()})

	tb := NewBuilder("req-3", 99, "city", "", 5)
	tb.AddItem(301, "FEED_SOURCE_CITY_POOL", 0, 300)
	tr := tb.Build()

	Write(context.Background(), rdb, tr, 100, 1)

	key := keys.FeedTraceKey("req-3")
	meta := mr.HGet(key, "meta")
	require.NotEmpty(t, meta)

	src := mr.HGet(key, "f:"+strconv.FormatInt(301, 10))
	require.Equal(t, "FEED_SOURCE_CITY_POOL", src)

	require.True(t, mr.Exists(key))
	ttl := mr.TTL(key)
	require.Greater(t, int64(ttl), int64(0))
}
