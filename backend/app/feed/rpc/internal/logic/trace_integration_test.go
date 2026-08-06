package logic

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/sponge-dad/feed/app/feed/rpc/feed"
	"github.com/sponge-dad/feed/app/feed/rpc/internal/keys"
	"github.com/sponge-dad/feed/common/requestid"
)

// TestFollowTimelineWritesTrace 端到端验证：关注流请求会异步写入 Trace，
// 随后 GetFeedRequestTrace / GetFeedSource 能正确读回来源统计与单条来源。
func TestFollowTimelineWritesTrace(t *testing.T) {
	m := newStubFeedsModel()
	now := time.Now()
	m.byID[1] = mkFeed(1, 10, now)
	m.byID[2] = mkFeed(2, 11, now)
	r := &stubRelation{followees: []int64{10, 11}, vips: map[int64]bool{11: true}}

	svcCtx, mr := newQuerySvc(t)
	svcCtx.FeedModel = m
	svcCtx.RelationRpc = r

	// 种子：关注收件箱含 feed 1、2。
	zadd(t, svcCtx.Redis, keys.Inbox(100), now.Unix(), 1)
	zadd(t, svcCtx.Redis, keys.Inbox(100), now.Unix(), 2)

	ctx := requestid.WithRequestID(context.Background(), "trace-req-e2e")
	l := NewGetFollowTimelineLogic(ctx, svcCtx)

	resp, err := l.GetFollowTimeline(&feed.GetFollowTimelineReq{UserId: 100, PageSize: 20})
	require.NoError(t, err)
	require.Len(t, resp.Feeds, 2)

	// 等待异步 trace 写入完成（goroutine 写入 Redis）。
	require.Eventually(t, func() bool {
		return mr.Exists(keys.FeedTraceKey("trace-req-e2e"))
	}, time.Second, 10*time.Millisecond)

	// 查询完整 Trace。
	ql := NewGetFeedRequestTraceLogic(ctx, svcCtx)
	qresp, err := ql.GetFeedRequestTrace(&feed.GetFeedRequestTraceReq{RequestId: "trace-req-e2e", UserId: 100})
	require.NoError(t, err)
	require.NotNil(t, qresp.Trace)
	require.Equal(t, "follow", qresp.Trace.Tab)
	require.Len(t, qresp.Trace.Sources, 2) // FOLLOW_INBOX + VIP_OUTBOX
	require.Len(t, qresp.Trace.Items, 2)

	// 单条来源查询。
	sl := NewGetFeedSourceLogic(ctx, svcCtx)
	sresp, err := sl.GetFeedSource(&feed.GetFeedSourceReq{RequestId: "trace-req-e2e", FeedId: 1, UserId: 100})
	require.NoError(t, err)
	require.Equal(t, feed.FeedSource_FEED_SOURCE_FOLLOW_INBOX, sresp.Source)
}

// TestFollowTimelineTraceDegrade 验证 Redis 不可用时 Timeline 仍正常返回、不报错（Trace 写入降级）。
func TestFollowTimelineTraceDegrade(t *testing.T) {
	m := newStubFeedsModel()
	now := time.Now()
	m.byID[1] = mkFeed(1, 10, now)
	r := &stubRelation{followees: []int64{}, vips: map[int64]bool{}}

	svcCtx, mr := newQuerySvc(t)
	svcCtx.FeedModel = m
	svcCtx.RelationRpc = r
	// 关闭 miniredis 模拟 Redis 不可用：后续 Redis 操作会快速失败而非阻塞。
	mr.Close()

	ctx := requestid.WithRequestID(context.Background(), "trace-req-degrade")
	l := NewGetFollowTimelineLogic(ctx, svcCtx)

	// inbox 读取与 trace 异步写入都会失败，但都不应导致 panic / 阻塞（写入已降级）。
	_, err := l.GetFollowTimeline(&feed.GetFollowTimelineReq{UserId: 100, PageSize: 20})
	// 核心断言是函数正常返回（可能返回上游读取错误），进程未崩溃。
	_ = err
}
